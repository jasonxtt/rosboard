package policyv2

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"rosboard/internal/subject"
)

// RoutingSourceKind is the routing-only source discriminator. It deliberately
// does not live on the shared Subject type because AccessRule has different
// source semantics.
type RoutingSourceKind string

const (
	RoutingSourceDevice        RoutingSourceKind = "device"
	RoutingSourceIP            RoutingSourceKind = "ip"
	RoutingSourceInterface     RoutingSourceKind = "interface"
	RoutingSourceInterfaceList RoutingSourceKind = "interface-list"
	RoutingSourceAll           RoutingSourceKind = "all"

	// RoutingSourceInterfaceListAllDeferredCode is the stable plan blocker for
	// the built-in `all` interface-list. The typed model may persist it so the
	// fact remains visible, but the current aggregate-list materializer must
	// not turn it into a broad executable ingress matcher.
	RoutingSourceInterfaceListAllDeferredCode = "routing_source_interface_list_all_deferred"
	// RoutingSourceAllDeferredCode keeps the unconstrained first-class source
	// visible in the model while its RouterOS chain-safety proof is deferred.
	RoutingSourceAllDeferredCode = "routing_source_all_deferred"
)

// RoutingSourceScope stores only the typed discriminator and the selector
// names needed by interface/interface-list sources. Device and IP payloads
// continue to use RoutingRule.Subject and its existing child-table
// persistence. An interface source may select several RouterOS interfaces and
// interface lists at once and may exclude individual source addresses; the
// exclusion is only meaningful while the ingress boundary is interface-based.
type RoutingSourceScope struct {
	Kind RoutingSourceKind `json:"kind"`
	// Name is the legacy single-selector field written by the first typed
	// source revision. NormalizeRoutingSourceScope folds it into Interfaces
	// (interface) or keeps it (interface-list) so old rows stay valid.
	Name            string   `json:"name,omitempty"`
	Interfaces      []string `json:"interfaces,omitempty"`
	InterfaceLists  []string `json:"interfaceLists,omitempty"`
	ExcludePrefixes []string `json:"excludePrefixes,omitempty"`
}

var (
	ErrRoutingSourceScopeInvalid  = errors.New("无效的路由来源范围")
	ErrRoutingSourceScopeConflict = errors.New("路由来源范围与旧版来源投影不一致")
)

func IsDeferredRoutingSourceInterfaceListName(name string) bool {
	return strings.EqualFold(strings.TrimSpace(name), "all")
}

func IsDeferredRoutingSource(scope *RoutingSourceScope) bool {
	if scope == nil {
		return false
	}
	if scope.Kind == RoutingSourceInterfaceList && IsDeferredRoutingSourceInterfaceListName(scope.Name) {
		return true
	}
	if scope.Kind == RoutingSourceInterface {
		for _, name := range scope.InterfaceLists {
			if IsDeferredRoutingSourceInterfaceListName(name) {
				return true
			}
		}
	}
	return false
}

func routingSourceInterfaceListAllDeferredIssue(logicalID string) PlanIssue {
	return PlanIssue{
		Code:      RoutingSourceInterfaceListAllDeferredCode,
		Status:    "blocker",
		LogicalID: logicalID,
		Reason:    `暂不支持以 RouterOS 内置「all」接口列表作为路由来源，待安全的匹配实现就绪后开放`,
	}
}

func routingSourceAllDeferredIssue(logicalID string) PlanIssue {
	return PlanIssue{
		Code:      RoutingSourceAllDeferredCode,
		Status:    "blocker",
		LogicalID: logicalID,
		Reason:    `「全部来源」暂不可用：RouterOS prerouting/output 与环路安全语义尚未验证完成`,
	}
}

// NormalizeRoutingSourceScope validates the small canonical selector payload.
// A nil value means that the row is still using the legacy Subject+Ingress
// representation.
func NormalizeRoutingSourceScope(value *RoutingSourceScope) (*RoutingSourceScope, error) {
	if value == nil {
		return nil, nil
	}
	normalized := &RoutingSourceScope{Kind: RoutingSourceKind(strings.ToLower(strings.TrimSpace(string(value.Kind)))), Name: strings.TrimSpace(value.Name)}
	switch normalized.Kind {
	case RoutingSourceDevice, RoutingSourceIP, RoutingSourceAll:
		if normalized.Name != "" || len(value.Interfaces) != 0 || len(value.InterfaceLists) != 0 || len(value.ExcludePrefixes) != 0 {
			return nil, fmt.Errorf("%w：%s 来源不能包含接口选择器或排除地址", ErrRoutingSourceScopeInvalid, normalized.Kind)
		}
	case RoutingSourceInterfaceList:
		if normalized.Name == "" {
			return nil, fmt.Errorf("%w：%s 来源需要一个名称", ErrRoutingSourceScopeInvalid, normalized.Kind)
		}
		if len(value.Interfaces) != 0 || len(value.InterfaceLists) != 0 || len(value.ExcludePrefixes) != 0 {
			return nil, fmt.Errorf("%w：%s 来源仅保留一个旧版列表名称", ErrRoutingSourceScopeInvalid, normalized.Kind)
		}
	case RoutingSourceInterface:
		interfaces := append([]string{}, value.Interfaces...)
		if normalized.Name != "" {
			interfaces = append(interfaces, normalized.Name)
		}
		normalized.Name = ""
		normalized.Interfaces = normalizedNames(interfaces)
		normalized.InterfaceLists = normalizedNames(value.InterfaceLists)
		if len(normalized.Interfaces) == 0 && len(normalized.InterfaceLists) == 0 {
			return nil, fmt.Errorf("%w：接口来源至少需要一个接口或接口列表", ErrRoutingSourceScopeInvalid)
		}
		exclusions, err := normalizeRoutingSourceExclusions(value.ExcludePrefixes)
		if err != nil {
			return nil, err
		}
		normalized.ExcludePrefixes = exclusions
	default:
		return nil, fmt.Errorf("%w：不支持的来源类型 %q", ErrRoutingSourceScopeInvalid, value.Kind)
	}
	return normalized, nil
}

// normalizeRoutingSourceExclusions canonicalizes the optional per-source
// address exclusions. Every entry accepts a plain IP or a CIDR, exactly like
// subject prefixes.
func normalizeRoutingSourceExclusions(values []string) ([]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		canonical, err := subject.NormalizePrefix(value)
		if err != nil {
			return nil, fmt.Errorf("%w：无效的排除来源地址 %q", ErrRoutingSourceScopeInvalid, strings.TrimSpace(value))
		}
		result = append(result, canonical)
	}
	sort.Strings(result)
	unique := result[:0]
	for _, value := range result {
		if len(unique) == 0 || unique[len(unique)-1] != value {
			unique = append(unique, value)
		}
	}
	return unique, nil
}

// RoutingSourceDirectSelector reports the single-name direct matcher for an
// interface source: exactly one interface or exactly one list and no
// exclusions compile to a plain in-interface / in-interface-list matcher
// without the managed aggregate list.
func RoutingSourceDirectSelector(scope *RoutingSourceScope) (kind RoutingSourceKind, name string, ok bool) {
	if scope == nil || scope.Kind != RoutingSourceInterface || len(scope.ExcludePrefixes) != 0 {
		return "", "", false
	}
	if len(scope.InterfaceLists) == 0 && len(scope.Interfaces) == 1 {
		return RoutingSourceInterface, scope.Interfaces[0], true
	}
	if len(scope.Interfaces) == 0 && len(scope.InterfaceLists) == 1 {
		return RoutingSourceInterfaceList, scope.InterfaceLists[0], true
	}
	return "", "", false
}

// RoutingSourceIngressScope derives the interface boundary of an interface
// source as a TrafficIngressScope so the planner can reuse the managed
// aggregate-list projection.
func RoutingSourceIngressScope(scope *RoutingSourceScope) TrafficIngressScope {
	if scope == nil || scope.Kind != RoutingSourceInterface {
		return TrafficIngressScope{}
	}
	return NormalizeTrafficIngressScopeUnvalidated(TrafficIngressScope{Interfaces: scope.Interfaces, InterfaceLists: scope.InterfaceLists})
}

// RoutingSourceUsesSubjectPayload tells API boundaries whether the shared
// Subject needs terminal/IP canonicalization. Interface, interface-list and
// all sources derive their compatibility projection instead.
func RoutingSourceUsesSubjectPayload(value *RoutingSourceScope) bool {
	if value == nil {
		return true
	}
	return value.Kind == RoutingSourceDevice || value.Kind == RoutingSourceIP
}

// RoutingSourceLegacyProjection returns the deterministic legacy fields that
// the current planner can still consume. The projection is never a second
// source authority.
func RoutingSourceLegacyProjection(scope RoutingSourceScope, source Subject) (Subject, TrafficIngressScope, error) {
	normalizedScope, err := NormalizeRoutingSourceScope(&scope)
	if err != nil {
		return Subject{}, TrafficIngressScope{}, err
	}
	normalizedSubject, err := normalizeRoutingSubjectForSource(*normalizedScope, source)
	if err != nil {
		return Subject{}, TrafficIngressScope{}, err
	}
	switch normalizedScope.Kind {
	case RoutingSourceDevice, RoutingSourceIP:
		return normalizedSubject, NormalizeTrafficIngressScopeUnvalidated(TrafficIngressScope{}), nil
	case RoutingSourceInterface:
		ingress := RoutingSourceIngressScope(normalizedScope)
		if len(normalizedScope.ExcludePrefixes) > 0 {
			return Subject{Mode: SubjectModeExcluded, Prefixes: append([]string{}, normalizedScope.ExcludePrefixes...)}, ingress, nil
		}
		return Subject{Mode: SubjectModeAll}, ingress, nil
	case RoutingSourceInterfaceList:
		return Subject{Mode: SubjectModeAll}, NormalizeTrafficIngressScopeUnvalidated(TrafficIngressScope{InterfaceLists: []string{normalizedScope.Name}}), nil
	case RoutingSourceAll:
		return Subject{Mode: SubjectModeAll}, NormalizeTrafficIngressScopeUnvalidated(TrafficIngressScope{}), nil
	default:
		return Subject{}, TrafficIngressScope{}, fmt.Errorf("%w：不支持的来源类型 %q", ErrRoutingSourceScopeInvalid, normalizedScope.Kind)
	}
}

func normalizeRoutingSubjectForSource(scope RoutingSourceScope, source Subject) (Subject, error) {
	normalized, err := subject.Normalize(source)
	if err != nil {
		return Subject{}, fmt.Errorf("%w: %v", ErrRoutingSourceScopeInvalid, err)
	}
	switch scope.Kind {
	case RoutingSourceDevice:
		if normalized.Mode != SubjectModeSelected || len(normalized.Members) == 0 || len(normalized.Prefixes) != 0 {
			return Subject{}, fmt.Errorf("%w：终端来源只能包含选中的终端成员", ErrRoutingSourceScopeInvalid)
		}
	case RoutingSourceIP:
		if normalized.Mode != SubjectModeSelected || len(normalized.Members) != 0 || len(normalized.Prefixes) == 0 {
			return Subject{}, fmt.Errorf("%w：IP 来源只能包含手动地址或地址段", ErrRoutingSourceScopeInvalid)
		}
	case RoutingSourceInterface:
		if len(scope.ExcludePrefixes) == 0 {
			if normalized.Mode != SubjectModeAll || len(normalized.Members) != 0 || len(normalized.Prefixes) != 0 {
				return Subject{}, fmt.Errorf("%w：%s 来源使用「全部终端」兼容投影", ErrRoutingSourceScopeInvalid, scope.Kind)
			}
			return normalized, nil
		}
		// An interface source with exclusions projects to an excluded subject
		// over the exclusion prefixes. Clients may still submit the empty/all
		// placeholder; the projection fills the exclusion in.
		if normalized.Mode == SubjectModeAll && len(normalized.Members) == 0 && len(normalized.Prefixes) == 0 {
			return normalized, nil
		}
		if normalized.Mode != SubjectModeExcluded || len(normalized.Members) != 0 || !reflect.DeepEqual(normalized.Prefixes, scope.ExcludePrefixes) {
			return Subject{}, fmt.Errorf("%w：带排除地址的接口来源需要匹配的「排除」来源投影", ErrRoutingSourceScopeInvalid)
		}
		return normalized, nil
	case RoutingSourceInterfaceList, RoutingSourceAll:
		if normalized.Mode != SubjectModeAll || len(normalized.Members) != 0 || len(normalized.Prefixes) != 0 {
			return Subject{}, fmt.Errorf("%w：%s 来源使用「全部终端」兼容投影", ErrRoutingSourceScopeInvalid, scope.Kind)
		}
	}
	return normalized, nil
}

// IsLegacyRoutingSourceProjectionOmitted recognizes an old-client payload that
// carries no source fields at all. It intentionally does not treat a partial
// Subject/Ingress payload as omitted, so a source edit cannot be silently
// ignored.
func IsLegacyRoutingSourceProjectionOmitted(rule RoutingRule) bool {
	return strings.TrimSpace(rule.Subject.Mode) == "" && len(rule.Subject.Members) == 0 && len(rule.Subject.Prefixes) == 0 && !HasTrafficIngress(rule.Ingress)
}

// routingSourceLegacySubject is the portion of Subject that an old client can
// express. AnchorMAC and last trusted addresses are deliberately excluded:
// they are server-managed identity state and are hidden from the JSON API.
type routingSourceLegacySubject struct {
	Mode     string                             `json:"mode"`
	Members  []routingSourceLegacySubjectMember `json:"members,omitempty"`
	Prefixes []string                           `json:"prefixes,omitempty"`
}

type routingSourceLegacySubjectMember struct {
	TerminalID string   `json:"terminalId"`
	Binding    string   `json:"binding"`
	PinnedIPv4 []string `json:"pinnedIpv4,omitempty"`
	PinnedIPv6 []string `json:"pinnedIpv6,omitempty"`
}

func routingSourceLegacySubjectValue(value Subject) (routingSourceLegacySubject, error) {
	normalized, err := subject.Normalize(value)
	if err != nil {
		return routingSourceLegacySubject{}, err
	}
	result := routingSourceLegacySubject{
		Mode:     normalized.Mode,
		Members:  make([]routingSourceLegacySubjectMember, 0, len(normalized.Members)),
		Prefixes: append([]string(nil), normalized.Prefixes...),
	}
	for _, member := range normalized.Members {
		result.Members = append(result.Members, routingSourceLegacySubjectMember{
			TerminalID: member.TerminalID,
			Binding:    member.Binding,
			PinnedIPv4: append([]string(nil), member.PinnedIPv4...),
			PinnedIPv6: append([]string(nil), member.PinnedIPv6...),
		})
	}
	return result, nil
}

// LegacyRoutingSourceProjectionMatches compares the effective legacy source
// semantics of an old-client payload with a canonical typed rule.
func LegacyRoutingSourceProjectionMatches(canonical, incoming RoutingRule) (bool, error) {
	if canonical.SourceScope == nil {
		return false, fmt.Errorf("%w：缺少规范的来源范围", ErrRoutingSourceScopeConflict)
	}
	expectedSubject, expectedIngress, err := RoutingSourceLegacyProjection(*canonical.SourceScope, canonical.Subject)
	if err != nil {
		return false, err
	}
	if IsLegacyRoutingSourceProjectionOmitted(incoming) {
		return true, nil
	}
	actualSubject := incoming.Subject
	if strings.TrimSpace(actualSubject.Mode) == "" && len(actualSubject.Members) == 0 && len(actualSubject.Prefixes) == 0 {
		actualSubject = expectedSubject
	} else {
		actualSubject, err = subject.Normalize(actualSubject)
		if err != nil {
			return false, fmt.Errorf("%w: %v", ErrRoutingSourceScopeConflict, err)
		}
	}
	actualIngress := NormalizeTrafficIngressScopeUnvalidated(incoming.Ingress)
	if actualSubject.Mode == SubjectModeSelected {
		// This is the existing legacy rule semantic: selected subjects are
		// source-only and ignore an attached ingress compatibility field.
		actualIngress = TrafficIngressScope{}
	}
	expectedLegacySubject, err := routingSourceLegacySubjectValue(expectedSubject)
	if err != nil {
		return false, fmt.Errorf("%w: %v", ErrRoutingSourceScopeConflict, err)
	}
	actualLegacySubject, err := routingSourceLegacySubjectValue(actualSubject)
	if err != nil {
		return false, fmt.Errorf("%w: %v", ErrRoutingSourceScopeConflict, err)
	}
	expectedIngress = NormalizeTrafficIngressScopeUnvalidated(expectedIngress)
	actualIngress = NormalizeTrafficIngressScopeUnvalidated(actualIngress)
	return reflect.DeepEqual(actualLegacySubject, expectedLegacySubject) && reflect.DeepEqual(actualIngress, expectedIngress), nil
}

// UpgradeLegacyRoutingSource converts a legacy Subject+Ingress-only rule to
// its typed source scope when the shape maps one-to-one:
//
//	all + ingress                -> interface source over the same selectors
//	excluded with prefixes only  -> interface source with excluded addresses
//	selected with terminals only -> device source
//	selected with prefixes only  -> ip source
//
// Mixed selected rows, member-based exclusions and ingress-less all rows have
// no typed equivalent and keep the legacy representation. The mapping is
// semantics-preserving: RoutingSourceLegacyProjection of the result yields
// exactly the original Subject and Ingress.
func UpgradeLegacyRoutingSource(rule RoutingRule) RoutingRule {
	if rule.SourceScope != nil {
		return rule
	}
	ingress := NormalizeTrafficIngressScopeUnvalidated(rule.Ingress)
	switch strings.TrimSpace(rule.Subject.Mode) {
	case SubjectModeAll:
		if !HasTrafficIngress(ingress) {
			return rule
		}
		rule.SourceScope = &RoutingSourceScope{Kind: RoutingSourceInterface, Interfaces: ingress.Interfaces, InterfaceLists: ingress.InterfaceLists}
	case SubjectModeExcluded:
		if len(rule.Subject.Members) != 0 || len(rule.Subject.Prefixes) == 0 || !HasTrafficIngress(ingress) {
			return rule
		}
		rule.SourceScope = &RoutingSourceScope{Kind: RoutingSourceInterface, Interfaces: ingress.Interfaces, InterfaceLists: ingress.InterfaceLists, ExcludePrefixes: append([]string{}, rule.Subject.Prefixes...)}
	case SubjectModeSelected:
		switch {
		case len(rule.Subject.Members) > 0 && len(rule.Subject.Prefixes) == 0:
			rule.SourceScope = &RoutingSourceScope{Kind: RoutingSourceDevice}
		case len(rule.Subject.Members) == 0 && len(rule.Subject.Prefixes) > 0:
			rule.SourceScope = &RoutingSourceScope{Kind: RoutingSourceIP}
		}
	}
	return rule
}

// PrepareRoutingRuleWrite applies the canonical-write compatibility rule and
// then performs the normal routing-rule normalization. Store callers pass the
// current row when one exists so this decision is made before any mutation.
func PrepareRoutingRuleWrite(value RoutingRule, current *RoutingRule) (RoutingRule, error) {
	if current != nil && current.SourceScope != nil && value.SourceScope == nil {
		matches, err := LegacyRoutingSourceProjectionMatches(*current, value)
		if err != nil {
			return RoutingRule{}, err
		}
		if !matches {
			return RoutingRule{}, fmt.Errorf("%w：旧版来源/入口字段与规范来源不一致", ErrRoutingSourceScopeConflict)
		}
		// Preserve the canonical source payload wholesale for old-client
		// non-source edits. This also preserves hidden identity state such as
		// AnchorMAC and last trusted addresses when those fields were omitted.
		scopeCopy := *current.SourceScope
		scopeCopy.Interfaces = append([]string(nil), current.SourceScope.Interfaces...)
		scopeCopy.InterfaceLists = append([]string(nil), current.SourceScope.InterfaceLists...)
		scopeCopy.ExcludePrefixes = append([]string(nil), current.SourceScope.ExcludePrefixes...)
		value.SourceScope = &scopeCopy
		value.Subject = current.Subject
		value.Ingress = current.Ingress
	}
	if value.SourceScope == nil && (current == nil || current.SourceScope == nil) {
		// Rules written through a legacy payload adopt the typed source
		// immediately, so the stored shape and any plan overlay stay identical
		// before and after commit. Unconvertible legacy shapes are untouched.
		value = UpgradeLegacyRoutingSource(value)
	}
	return NormalizeRoutingRule(value)
}

type routingSubjectHash struct {
	Mode     string                     `json:"mode"`
	Members  []routingSubjectMemberHash `json:"members,omitempty"`
	Prefixes []string                   `json:"prefixes,omitempty"`
}

type routingSubjectMemberHash struct {
	TerminalID string   `json:"terminalId"`
	Binding    string   `json:"binding"`
	AnchorMAC  string   `json:"anchorMac,omitempty"`
	PinnedIPv4 []string `json:"pinnedIpv4,omitempty"`
	PinnedIPv6 []string `json:"pinnedIpv6,omitempty"`
	LastIPv4   []string `json:"lastIpv4,omitempty"`
	LastIPv6   []string `json:"lastIpv6,omitempty"`
}

type routingRuleSourceHash struct {
	ID                    string              `json:"id"`
	IncludeKeywordDomains bool                `json:"includeKeywordDomains"`
	SourceScope           *RoutingSourceScope `json:"sourceScope,omitempty"`
	Subject               routingSubjectHash  `json:"subject"`
	Ingress               TrafficIngressScope `json:"ingress"`
}

func newRoutingRuleSourceHash(rule RoutingRule) routingRuleSourceHash {
	members := make([]routingSubjectMemberHash, 0, len(rule.Subject.Members))
	for _, member := range rule.Subject.Members {
		members = append(members, routingSubjectMemberHash{
			TerminalID: member.TerminalID, Binding: member.Binding, AnchorMAC: member.AnchorMAC,
			PinnedIPv4: append([]string(nil), member.PinnedIPv4...), PinnedIPv6: append([]string(nil), member.PinnedIPv6...),
			LastIPv4: append([]string(nil), member.LastIPv4...), LastIPv6: append([]string(nil), member.LastIPv6...),
		})
	}
	sort.Slice(members, func(i, j int) bool { return members[i].TerminalID < members[j].TerminalID })
	return routingRuleSourceHash{
		ID: rule.ID, IncludeKeywordDomains: rule.IncludeKeywordDomains, SourceScope: rule.SourceScope,
		Subject: routingSubjectHash{Mode: rule.Subject.Mode, Members: members, Prefixes: append([]string(nil), rule.Subject.Prefixes...)},
		Ingress: NormalizeTrafficIngressScopeUnvalidated(rule.Ingress),
	}
}

func routingRuleSourceHashes(rules []RoutingRule) []routingRuleSourceHash {
	result := make([]routingRuleSourceHash, 0)
	for _, rule := range rules {
		if rule.SourceScope == nil {
			continue
		}
		result = append(result, newRoutingRuleSourceHash(rule))
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}
