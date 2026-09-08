package accesscontrol

import (
	"errors"
	"net/netip"
	"sort"
	"strings"
)

// MemberEvaluation is the per-member projection state computed from the
// monitor snapshot. It distinguishes "temporarily unknown" from "known to be
// wrong": a temporarily unseen terminal may keep its last trusted projection,
// while an address proven to belong to another identity must be dropped.
type MemberEvaluation struct {
	Member RuleMember
	State  string
	// IPv4/IPv6 are the addresses that should be projected for this member
	// right now (possibly the last trusted ones).
	IPv4 []string
	IPv6 []string
	// RemovedIPv4/RemovedIPv6 are addresses withheld because they are now
	// observed on another terminal identity.
	RemovedIPv4 []string
	RemovedIPv6 []string
	// IdentityChanged is true when the current terminal ID is now backed by a
	// different MAC than the member's anchor. It lets the desired builder clear
	// the old trusted projection only after the empty projection is applied.
	IdentityChanged bool
	Reason          string
}

// EvaluateMembers resolves every member of a rule against the current monitor
// snapshot. A fixed member always resolves to its pinned addresses; an
// auto-follow member resolves from the snapshot, falls back to its last
// trusted resolution, and drops addresses that demonstrably moved to another
// device. States are member-local: one broken member must never block the
// other members or rules.
func EvaluateMembers(members []RuleMember, terminals []Terminal) []MemberEvaluation {
	byID := make(map[string]Terminal, len(terminals))
	// addressHolders maps an observed address to the distinct MAC identities
	// currently holding it; more than one distinct MAC is reassignment
	// evidence.
	addressHolders := make(map[string]map[string]bool)
	for _, terminal := range terminals {
		byID[terminal.ID] = terminal
		mac, err := NormalizeMAC(terminal.MACAddress)
		if err != nil {
			continue
		}
		for _, familyAddresses := range [][]string{normalizeOrEmpty(terminal.IPv4, true), normalizeOrEmpty(terminal.IPv6, false)} {
			for _, address := range familyAddresses {
				if addressHolders[address] == nil {
					addressHolders[address] = make(map[string]bool)
				}
				addressHolders[address][mac] = true
			}
		}
	}

	result := make([]MemberEvaluation, 0, len(members))
	for _, member := range members {
		member.LastIPv4 = normalizeOrEmpty(member.LastIPv4, true)
		member.LastIPv6 = normalizeOrEmpty(member.LastIPv6, false)
		evaluation := MemberEvaluation{Member: member, State: MemberResolved, IPv4: []string{}, IPv6: []string{}}
		if member.Binding == BindingFixed {
			evaluation.IPv4 = normalizeOrEmpty(member.PinnedIPv4, true)
			evaluation.IPv6 = normalizeOrEmpty(member.PinnedIPv6, false)
			result = append(result, evaluation)
			continue
		}

		terminal, found := byID[member.TerminalID]
		currentIPv4, currentIPv6 := []string{}, []string{}
		if found {
			currentIPv4 = normalizeOrEmpty(terminal.IPv4, true)
			currentIPv6 = normalizeOrEmpty(terminal.IPv6, false)
		}
		anchorMAC, anchorErr := NormalizeMAC(member.AnchorMAC)
		if anchorErr != nil {
			anchorMAC = ""
		} else {
			evaluation.Member.AnchorMAC = anchorMAC
		}
		currentMAC := ""
		if found {
			currentMAC, _ = NormalizeMAC(terminal.MACAddress)
		}
		if anchorMAC == "" {
			evaluation.State = MemberUnresolved
			evaluation.Reason = "设备缺少可信的 MAC 身份锚点，暂未投影地址；请重新保存该规则。"
			result = append(result, evaluation)
			continue
		}
		if found && currentMAC == "" {
			evaluation.State = MemberUnresolved
			evaluation.Reason = "设备当前 MAC 身份不可验证，暂未更新地址投影。"
			trustedIPv4 := normalizeOrEmpty(member.LastIPv4, true)
			trustedIPv6 := normalizeOrEmpty(member.LastIPv6, false)
			keptIPv4, removedIPv4 := splitConflicted(append([]string{}, trustedIPv4...), anchorMAC, addressHolders)
			keptIPv6, removedIPv6 := splitConflicted(append([]string{}, trustedIPv6...), anchorMAC, addressHolders)
			evaluation.IPv4, evaluation.IPv6 = keptIPv4, keptIPv6
			evaluation.RemovedIPv4, evaluation.RemovedIPv6 = removedIPv4, removedIPv6
			result = append(result, evaluation)
			continue
		}
		if found && currentMAC != anchorMAC {
			evaluation.State = MemberConflicted
			evaluation.IdentityChanged = true
			evaluation.Reason = "设备的当前 MAC 身份已变化，已移除旧地址投影，等待重新确认身份。"
			evaluation.RemovedIPv4 = append(normalizeOrEmpty(terminal.IPv4, true), normalizeOrEmpty(member.LastIPv4, true)...)
			evaluation.RemovedIPv6 = append(normalizeOrEmpty(terminal.IPv6, false), normalizeOrEmpty(member.LastIPv6, false)...)
			result = append(result, evaluation)
			continue
		}

		trustedIPv4, trustedIPv6 := append([]string{}, member.LastIPv4...), append([]string{}, member.LastIPv6...)
		hasCurrent := len(currentIPv4)+len(currentIPv6) > 0
		hasTrusted := len(trustedIPv4)+len(trustedIPv6) > 0

		candidatesIPv4, candidatesIPv6 := currentIPv4, currentIPv6
		fallback := false
		if !hasCurrent {
			candidatesIPv4, candidatesIPv6 = trustedIPv4, trustedIPv6
			fallback = true
		}

		keptIPv4, removedIPv4 := splitConflicted(candidatesIPv4, anchorMAC, addressHolders)
		keptIPv6, removedIPv6 := splitConflicted(candidatesIPv6, anchorMAC, addressHolders)
		evaluation.IPv4, evaluation.IPv6 = keptIPv4, keptIPv6
		evaluation.RemovedIPv4, evaluation.RemovedIPv6 = removedIPv4, removedIPv6

		switch {
		case len(removedIPv4)+len(removedIPv6) > 0:
			evaluation.State = MemberConflicted
			evaluation.Reason = "地址 " + strings.Join(append(append([]string{}, removedIPv4...), removedIPv6...), "、") +
				" 已被其他设备使用，已移除该成员的错误地址投影，等待设备重新解析。"
		case fallback && hasTrusted:
			evaluation.State = MemberUnresolved
			evaluation.Reason = "设备暂时不在线或未被监控看到，暂时保留最后已知地址继续阻断。"
		case fallback:
			evaluation.State = MemberUnresolved
			evaluation.Reason = "设备当前不在线且没有可用的历史地址，该成员暂未投影任何地址。"
		default:
			evaluation.State = MemberResolved
		}
		result = append(result, evaluation)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Member.TerminalID < result[j].Member.TerminalID })
	return result
}

// splitConflicted withholds every address that is currently observed on a
// different non-empty MAC identity than the member's own anchor.
func splitConflicted(addresses []string, anchorMAC string, holders map[string]map[string]bool) (kept, removed []string) {
	kept = []string{}
	removed = []string{}
	for _, address := range addresses {
		parsed, err := normalizeAddressForHolder(address)
		if err != nil {
			continue
		}
		identityMACs := holders[parsed]
		conflicted := false
		for mac := range identityMACs {
			if mac != anchorMAC {
				conflicted = true
				break
			}
		}
		if conflicted {
			removed = append(removed, parsed)
			continue
		}
		kept = append(kept, parsed)
	}
	return kept, removed
}

func normalizeAddressForHolder(value string) (string, error) {
	address, err := netip.ParseAddr(strings.TrimSpace(value))
	if err != nil {
		return "", err
	}
	if address.Zone() != "" {
		return "", errors.New("scoped address is not supported")
	}
	return address.String(), nil
}

func normalizeOrEmpty(values []string, ipv4 bool) []string {
	normalized, err := normalizeAddresses(values, ipv4)
	if err != nil {
		return []string{}
	}
	return normalized
}
