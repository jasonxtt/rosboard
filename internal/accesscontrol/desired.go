package accesscontrol

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/netip"
	"sort"
	"strings"

	"rosboard/internal/routeros"
)

type DesiredObject struct {
	LogicalID string
	Menu      routeros.MutationMenu
	Fields    map[string]string
	Phase     string
}

// Issue is a plan-level finding. Blockers stop the apply; Issues (degradations)
// are member-local problems that must not block the rest of the device.
type Issue struct {
	Code   string
	RuleID string
	Family string
	Reason string
}

type DesiredResult struct {
	Objects     []DesiredObject
	Blockers    []Issue
	Issues      []Issue
	Resolutions []MemberResolution
}

type DesiredInput struct {
	ManagerID string
	DeviceID  string
	Rules     []AccessRule
	Members   []RuleMember
	Terminals []Terminal
	// SourceList maps sourceID to its materialized RouterOS address-list name.
	SourceList map[string]string
	// SourceDisabled marks source lists that must not be matched by access
	// control while their policy source is disabled. The list may still contain
	// stale RouterOS entries until DNS TTL/cleanup catches up, so disabling the
	// access jumps is part of the safety boundary.
	SourceDisabled map[string]bool
	// Scope carries local-network evidence used to reject a local interface as
	// an internet egress. Internet rules themselves use the route-derived
	// interfaces below.
	Scope Scope
	// InternetEgresses maps an address family to the RouterOS interfaces that
	// can carry its configured default routes. Each interface gets direct
	// forward rules; no synthetic chain or 0.0.0.0/0 address list is used.
	InternetEgresses map[string][]string
	// InternetEgressIssues carries a family-specific discovery reason. It is
	// consumed when the corresponding family has no usable egress interface.
	InternetEgressIssues map[string]string
}

func BuildDesired(input DesiredInput) DesiredResult {
	result := DesiredResult{Objects: []DesiredObject{}, Blockers: []Issue{}, Issues: []Issue{}, Resolutions: []MemberResolution{}}
	rules := append([]AccessRule(nil), input.Rules...)
	sort.Slice(rules, func(i, j int) bool { return rules[i].ID < rules[j].ID })
	membersByRule := make(map[string][]RuleMember)
	for _, member := range input.Members {
		membersByRule[member.RuleID] = append(membersByRule[member.RuleID], member)
	}
	for ruleID := range membersByRule {
		members := membersByRule[ruleID]
		sort.Slice(members, func(i, j int) bool { return members[i].TerminalID < members[j].TerminalID })
		membersByRule[ruleID] = members
	}
	addedAddresses := make(map[string]bool)
	for _, rule := range rules {
		if err := ValidateRule(rule); err != nil {
			result.Blockers = append(result.Blockers, Issue{Code: "invalid_access_rule", RuleID: rule.ID, Reason: err.Error()})
			continue
		}
		members := membersByRule[rule.ID]
		if len(members) == 0 {
			result.Blockers = append(result.Blockers, Issue{Code: "access_rule_without_members", RuleID: rule.ID, Reason: "规则没有任何受控设备"})
			continue
		}
		for _, member := range members {
			if err := ValidateMember(member); err != nil {
				result.Blockers = append(result.Blockers, Issue{Code: "invalid_access_member", RuleID: rule.ID, Reason: err.Error()})
			}
		}
		if rule.TargetScope == TargetScopeSources {
			available := true
			for _, sourceID := range rule.SourceIDs {
				if strings.TrimSpace(input.SourceList[sourceID]) == "" {
					result.Blockers = append(result.Blockers, Issue{Code: "access_source_unavailable", RuleID: rule.ID, Reason: "来源 " + sourceID + " 的地址列表不可用"})
					available = false
				}
			}
			if !available {
				continue
			}
		}
		evaluations := EvaluateMembers(members, input.Terminals)
		for _, evaluation := range evaluations {
			if evaluation.State != MemberResolved && evaluation.Member.Binding == BindingAuto {
				result.Issues = append(result.Issues, Issue{Code: "access_member_" + evaluation.State, RuleID: rule.ID, Reason: evaluation.Reason})
			}
			projectionChanged := !sameAddressSet(evaluation.IPv4, evaluation.Member.LastIPv4) || !sameAddressSet(evaluation.IPv6, evaluation.Member.LastIPv6)
			if evaluation.Member.Binding == BindingAuto && IsReliableMAC(evaluation.Member.AnchorMAC) && projectionChanged &&
				(evaluation.State == MemberResolved || evaluation.IdentityChanged || len(evaluation.RemovedIPv4)+len(evaluation.RemovedIPv6) > 0) {
				result.Resolutions = append(result.Resolutions, MemberResolution{
					RuleID: rule.ID, TerminalID: evaluation.Member.TerminalID, AnchorMAC: evaluation.Member.AnchorMAC,
					IPv4: append([]string{}, evaluation.IPv4...), IPv6: append([]string{}, evaluation.IPv6...),
				})
			}
		}
		ipv4, ipv6 := ruleAddresses(evaluations)
		if len(ipv4)+len(ipv6) == 0 {
			// 无任何可投影地址：成员级 degraded 已上报，但绝不因此阻断
			// 本设备其他规则的同步。
			continue
		}
		ruleList := RuleMemberListName(input.ManagerID, input.DeviceID, rule.ID)
		for _, address := range append(append([]string{}, ipv4...), ipv6...) {
			parsed, _ := netip.ParseAddr(address)
			menu := routeros.MenuIPFirewallAddressList
			family := FamilyIPv4
			if parsed.Is6() {
				menu = routeros.MenuIPv6FirewallAddressList
				family = FamilyIPv6
			}
			logicalID := "access-member:" + rule.ID + ":" + family + ":" + address
			key := string(menu) + "\x00" + logicalID
			if addedAddresses[key] {
				continue
			}
			addedAddresses[key] = true
			result.Objects = append(result.Objects, desiredObject(input, logicalID, menu, "foundation", "访问控制成员地址", map[string]string{
				"list": ruleList, "address": address, "disabled": "no",
			}))
		}
		for _, family := range []struct {
			name      string
			addresses []string
			menu      routeros.MutationMenu
		}{{FamilyIPv4, ipv4, routeros.MenuIPFirewallFilter}, {FamilyIPv6, ipv6, routeros.MenuIPv6FirewallFilter}} {
			if len(family.addresses) == 0 {
				continue
			}
			if rule.TargetScope == TargetScopeInternet {
				egresses := sortedUniqueStrings(input.InternetEgresses[family.name])
				if len(egresses) == 0 {
					reason := input.InternetEgressIssues[family.name]
					if strings.TrimSpace(reason) == "" {
						reason = "无法确认 " + family.name + " 的实际互联网出口接口，暂未应用整个互联网规则。"
					}
					result.Blockers = append(result.Blockers, Issue{
						Code:   "access_internet_egress_unavailable",
						RuleID: rule.ID,
						Family: family.name,
						Reason: reason,
					})
					continue
				}
				if len(egresses) > 1 {
					ensureInternetEgressList(input, family.name, egresses, &result)
				}
				result.Objects = append(result.Objects, internetFilterObjects(input, rule, family.menu, ruleList, egresses, family.name)...)
			} else {
				result.Objects = append(result.Objects, sourcesFilterObjects(input, rule, family.menu, ruleList, input.SourceList, input.SourceDisabled, addedAddresses, family.name)...)
			}
		}
	}
	return result
}

func ensureInternetEgressList(input DesiredInput, family string, egresses []string, result *DesiredResult) {
	listName := InternetEgressListName(input.ManagerID, input.DeviceID, family)
	for _, object := range result.Objects {
		if object.LogicalID == "access-internet-egress:list:"+family {
			return
		}
	}
	result.Objects = append(result.Objects, internetEgressInterfaceListObjects(input, family, listName, egresses)...)
}

func sameAddressSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

// ruleAddresses unions the evaluated member addresses per family. Fixed
// members always contribute; auto members contribute whatever their state
// still proves (current or last trusted).
func ruleAddresses(evaluations []MemberEvaluation) (ipv4, ipv6 []string) {
	seenIPv4 := make(map[string]bool)
	seenIPv6 := make(map[string]bool)
	ipv4 = []string{}
	ipv6 = []string{}
	for _, evaluation := range evaluations {
		for _, address := range evaluation.IPv4 {
			if !seenIPv4[address] {
				seenIPv4[address] = true
				ipv4 = append(ipv4, address)
			}
		}
		for _, address := range evaluation.IPv6 {
			if !seenIPv6[address] {
				seenIPv6[address] = true
				ipv6 = append(ipv6, address)
			}
		}
	}
	sort.Strings(ipv4)
	sort.Strings(ipv6)
	return ipv4, ipv6
}

func sourcesFilterObjects(input DesiredInput, rule AccessRule, menu routeros.MutationMenu, ruleList string, sourceList map[string]string, sourceDisabled map[string]bool, added map[string]bool, family string) []DesiredObject {
	disabled := "no"
	if !rule.Enabled {
		disabled = "yes"
	}
	chain := RuleChainName(input.ManagerID, input.DeviceID, rule.ID)
	prefix := "access:" + rule.ID + ":" + family + ":"
	objects := make([]DesiredObject, 0, 2*len(rule.SourceIDs)+3)
	for _, sourceID := range rule.SourceIDs {
		list := strings.TrimSpace(sourceList[sourceID])
		if list == "" {
			// 由调用方统一上报 access_source_unavailable，这里跳过避免半套规则。
			continue
		}
		sourceJumpDisabled := disabled
		if sourceDisabled[sourceID] {
			sourceJumpDisabled = "yes"
		}
		objects = append(objects,
			desiredObject(input, prefix+"jump-out:"+sourceID, menu, "activation", "访问控制出站入口", map[string]string{
				"chain": "forward", "src-address-list": ruleList, "dst-address-list": list, "action": "jump", "jump-target": chain, "disabled": sourceJumpDisabled,
			}),
			desiredObject(input, prefix+"jump-in:"+sourceID, menu, "activation", "访问控制回程入口", map[string]string{
				"chain": "forward", "src-address-list": list, "dst-address-list": ruleList, "action": "jump", "jump-target": chain, "disabled": sourceJumpDisabled,
			}),
		)
	}
	return append(objects, chainDenyObjects(input, menu, chain, prefix, disabled)...)
}

func internetFilterObjects(input DesiredInput, rule AccessRule, menu routeros.MutationMenu, ruleList string, egresses []string, family string) []DesiredObject {
	disabled := "no"
	if !rule.Enabled {
		disabled = "yes"
	}
	prefix := "access:" + rule.ID + ":" + family + ":"
	targets := egresses
	if len(egresses) > 1 {
		targets = []string{"interface-list"}
	}
	objects := make([]DesiredObject, 0, len(targets)*6)
	interfaceLabel := "接口"
	if len(egresses) > 1 {
		interfaceLabel = "接口列表"
	}
	for _, egress := range targets {
		for _, direction := range []struct {
			name           string
			interfaceField string
			addressField   string
			label          string
		}{
			{name: "out", interfaceField: "out-interface", addressField: "src-address-list", label: "访问控制出站"},
			{name: "in", interfaceField: "in-interface", addressField: "dst-address-list", label: "访问控制回程"},
		} {
			base := map[string]string{
				"chain": "forward", direction.addressField: ruleList,
				"disabled": disabled,
			}
			if len(egresses) > 1 {
				base[direction.interfaceField+"-list"] = InternetEgressListName(input.ManagerID, input.DeviceID, family)
			} else {
				base[direction.interfaceField] = egress
			}
			logicalTarget := egress
			if len(egresses) > 1 {
				logicalTarget = "interface-list"
			}
			objects = append(objects,
				desiredObject(input, prefix+direction.name+":"+logicalTarget+":tcp", menu, "activation", direction.label+interfaceLabel+" TCP 重置", mergeFields(base, map[string]string{
					"protocol": "tcp", "action": "reject", "reject-with": "tcp-reset",
				})),
				desiredObject(input, prefix+direction.name+":"+logicalTarget+":udp", menu, "activation", direction.label+interfaceLabel+" UDP 丢弃", mergeFields(base, map[string]string{
					"protocol": "udp", "action": "drop",
				})),
				desiredObject(input, prefix+direction.name+":"+logicalTarget+":other", menu, "activation", direction.label+interfaceLabel+"其他协议丢弃", mergeFields(base, map[string]string{
					"action": "drop",
				})),
			)
		}
	}
	return objects
}

func internetEgressInterfaceListObjects(input DesiredInput, family, listName string, egresses []string) []DesiredObject {
	objects := []DesiredObject{
		desiredObject(input, "access-internet-egress:list:"+family, routeros.MenuInterfaceList, "foundation", "访问控制互联网出口接口列表", map[string]string{"name": listName}),
	}
	for _, egress := range egresses {
		objects = append(objects, desiredObject(input, "access-internet-egress:member:"+family+":"+egress, routeros.MenuInterfaceListMember, "foundation", "访问控制互联网出口接口 "+egress, map[string]string{"list": listName, "interface": egress}))
	}
	return objects
}

func mergeFields(base, additions map[string]string) map[string]string {
	result := make(map[string]string, len(base)+len(additions))
	for key, value := range base {
		result[key] = value
	}
	for key, value := range additions {
		result[key] = value
	}
	return result
}

func sortedUniqueStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

// chainDenyObjects builds the dedicated sub-chain body for source-list rules.
// TCP is reset with the official reject-with=tcp-reset, UDP and every other
// protocol are dropped.
func chainDenyObjects(input DesiredInput, menu routeros.MutationMenu, chain, prefix, disabled string) []DesiredObject {
	return []DesiredObject{
		desiredObject(input, prefix+"tcp", menu, "activation", "访问控制 TCP 重置", map[string]string{
			"chain": chain, "protocol": "tcp", "action": "reject", "reject-with": "tcp-reset", "disabled": disabled,
		}),
		desiredObject(input, prefix+"udp", menu, "activation", "访问控制 UDP 丢弃", map[string]string{
			"chain": chain, "protocol": "udp", "action": "drop", "disabled": disabled,
		}),
		desiredObject(input, prefix+"other", menu, "activation", "访问控制其他协议丢弃", map[string]string{
			"chain": chain, "action": "drop", "disabled": disabled,
		}),
	}
}

func desiredObject(input DesiredInput, logicalID string, menu routeros.MutationMenu, phase, label string, fields map[string]string) DesiredObject {
	fields["comment"] = ManagedComment(input.ManagerID, input.DeviceID, logicalID, label)
	return DesiredObject{LogicalID: logicalID, Menu: menu, Fields: fields, Phase: phase}
}

const (
	FamilyIPv4 = "ipv4"
	FamilyIPv6 = "ipv6"
)

func RuleMemberListName(managerID, deviceID, ruleID string) string {
	return "rbac_rule_" + shortHash("rule:"+managerID+":"+deviceID+":"+ruleID, 10)
}

func LocalPrefixListName(managerID, deviceID string) string {
	return "rbac_local_" + shortHash("local:"+managerID+":"+deviceID, 10)
}

func RuleChainName(managerID, deviceID, ruleID string) string {
	return "rbac_" + shortHash("policy:"+managerID+":"+deviceID+":"+ruleID, 10)
}

func InternetEgressListName(managerID, deviceID, family string) string {
	return "rbac_internet_" + shortHash("internet-egress:"+managerID+":"+deviceID+":"+family, 10)
}

func ManagedComment(managerID, deviceID, logicalID, label string) string {
	identity := "rb_" + shortHash(logicalID, 8)
	label = strings.Join(strings.Fields(strings.ReplaceAll(label, "|", "/")), " ")
	if label == "" {
		return identity
	}
	return identity + " | " + label
}

// LegacyV1ManagedComment returns the identity used by the immediately
// preceding access-control implementation. It is only used to migrate an
// exact known logical object in place.
func LegacyV1ManagedComment(managerID, deviceID, logicalID string) string {
	return legacyV1ManagedCommentPrefix(managerID, deviceID) + shortHash("logical:"+managerID+":"+deviceID+":"+logicalID, 16)
}

// LegacyManagedComment returns the original short access identity. It is only
// used to migrate an exact known logical object in place.
func LegacyManagedComment(managerID, deviceID, logicalID string) string {
	return "ra_" + shortHash("access:"+managerID+":"+deviceID+":"+logicalID, 8)
}

func IsManagedComment(comment string) bool {
	identity := commentIdentity(comment)
	if strings.HasPrefix(identity, "rb_") {
		return hasHex(strings.TrimPrefix(identity, "rb_"), 8)
	}
	return IsLegacyManagedComment(identity)
}

func IsLegacyManagedComment(comment string) bool {
	identity := commentIdentity(comment)
	if strings.HasPrefix(identity, "ra_v1_") {
		parts := strings.Split(strings.TrimPrefix(identity, "ra_v1_"), "_")
		return len(parts) == 3 && hasHex(parts[0], 12) && hasHex(parts[1], 12) && hasHex(parts[2], 16)
	}
	return strings.HasPrefix(identity, "ra_") && hasHex(strings.TrimPrefix(identity, "ra_"), 8)
}

func IsManagedCommentFor(managerID, deviceID, comment string) bool {
	identity := commentIdentity(comment)
	if !IsLegacyManagedComment(identity) || !strings.HasPrefix(identity, "ra_v1_") {
		return false
	}
	return strings.HasPrefix(identity, legacyV1ManagedCommentPrefix(managerID, deviceID))
}

func legacyV1ManagedCommentPrefix(managerID, deviceID string) string {
	return "ra_v1_" + shortHash("manager:"+managerID, 12) + "_" + shortHash("device:"+deviceID, 12) + "_"
}

func commentIdentity(comment string) string {
	identity := strings.TrimSpace(comment)
	if index := strings.Index(identity, " | "); index >= 0 {
		identity = strings.TrimSpace(identity[:index])
	}
	return identity
}

func hasHex(value string, length int) bool {
	if len(value) != length {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func shortHash(value string, length int) string {
	digest := sha256.Sum256([]byte(value))
	encoded := hex.EncodeToString(digest[:])
	if length > len(encoded) {
		return encoded
	}
	return encoded[:length]
}

func (issue Issue) Error() string {
	return fmt.Sprintf("%s: %s", issue.Code, issue.Reason)
}
