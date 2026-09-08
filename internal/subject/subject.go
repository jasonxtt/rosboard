// Package subject contains the small, policy-agnostic part of client subject
// handling shared by routing and access-control rules.
package subject

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sort"
	"strings"
)

const (
	ModeAll      = "all"
	ModeSelected = "selected"
	ModeExcluded = "excluded"

	BindingAuto  = "auto"
	BindingFixed = "fixed"

	FamilyIPv4 = "ipv4"
	FamilyIPv6 = "ipv6"
)

// Subject is deliberately narrow. Rule ownership, target selection and
// policy actions stay in their respective packages.
type Subject struct {
	Mode     string   `json:"mode"`
	Members  []Member `json:"members,omitempty"`
	Prefixes []string `json:"prefixes,omitempty"`
}

type Member struct {
	TerminalID string   `json:"terminalId"`
	Binding    string   `json:"binding"`
	AnchorMAC  string   `json:"-"`
	PinnedIPv4 []string `json:"pinnedIpv4,omitempty"`
	PinnedIPv6 []string `json:"pinnedIpv6,omitempty"`
	LastIPv4   []string `json:"-"`
	LastIPv6   []string `json:"-"`
}

// Normalize validates and canonicalizes the shared subject fields.
func Normalize(value Subject) (Subject, error) {
	value.Mode = strings.TrimSpace(value.Mode)
	if value.Mode != ModeAll && value.Mode != ModeSelected && value.Mode != ModeExcluded {
		return Subject{}, errors.New("subject mode must be all, selected or excluded")
	}
	if value.Mode == ModeAll && (len(value.Members) != 0 || len(value.Prefixes) != 0) {
		return Subject{}, errors.New("all subjects must not contain members or prefixes")
	}

	result := Subject{Mode: value.Mode, Members: make([]Member, 0, len(value.Members)), Prefixes: make([]string, 0, len(value.Prefixes))}
	seenMembers := make(map[string]bool, len(value.Members))
	for _, member := range value.Members {
		member.TerminalID = strings.TrimSpace(member.TerminalID)
		member.Binding = strings.TrimSpace(member.Binding)
		if member.TerminalID == "" {
			return Subject{}, errors.New("subject member terminal id is required")
		}
		if seenMembers[member.TerminalID] {
			return Subject{}, fmt.Errorf("subject member terminal %q is duplicated", member.TerminalID)
		}
		seenMembers[member.TerminalID] = true
		var err error
		member.AnchorMAC, err = NormalizeMAC(member.AnchorMAC)
		if err != nil {
			return Subject{}, err
		}
		member.PinnedIPv4, err = NormalizeAddresses(member.PinnedIPv4, true)
		if err != nil {
			return Subject{}, err
		}
		member.PinnedIPv6, err = NormalizeAddresses(member.PinnedIPv6, false)
		if err != nil {
			return Subject{}, err
		}
		member.LastIPv4, err = NormalizeAddresses(member.LastIPv4, true)
		if err != nil {
			return Subject{}, err
		}
		member.LastIPv6, err = NormalizeAddresses(member.LastIPv6, false)
		if err != nil {
			return Subject{}, err
		}
		switch member.Binding {
		case BindingAuto:
			if len(member.PinnedIPv4) != 0 || len(member.PinnedIPv6) != 0 {
				return Subject{}, errors.New("auto-follow member cannot pin addresses")
			}
			member.PinnedIPv4 = []string{}
			member.PinnedIPv6 = []string{}
		case BindingFixed:
			if len(member.PinnedIPv4)+len(member.PinnedIPv6) == 0 {
				return Subject{}, errors.New("fixed member requires at least one pinned address")
			}
			member.AnchorMAC = ""
			member.LastIPv4 = []string{}
			member.LastIPv6 = []string{}
		default:
			return Subject{}, errors.New("subject member binding must be auto or fixed")
		}
		result.Members = append(result.Members, member)
	}
	sort.Slice(result.Members, func(i, j int) bool { return result.Members[i].TerminalID < result.Members[j].TerminalID })
	for _, prefix := range value.Prefixes {
		canonical, err := NormalizePrefix(prefix)
		if err != nil {
			return Subject{}, err
		}
		result.Prefixes = append(result.Prefixes, canonical)
	}
	sort.Strings(result.Prefixes)
	result.Prefixes = unique(result.Prefixes)
	if (result.Mode == ModeSelected || result.Mode == ModeExcluded) && len(result.Members) == 0 && len(result.Prefixes) == 0 {
		return Subject{}, errors.New("selected or excluded subjects require at least one member or prefix")
	}
	return result, nil
}

// NormalizeSubject is an explicit alias for callers that want the operation
// name to remain self-describing at a policy boundary.
func NormalizeSubject(value Subject) (Subject, error) {
	return Normalize(value)
}

func NormalizeMAC(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", nil
	}
	parsed, err := net.ParseMAC(trimmed)
	if err != nil || len(parsed) != 6 {
		return "", errors.New("invalid terminal MAC address")
	}
	if parsed[0]&1 != 0 {
		return "", errors.New("terminal MAC address must be unicast")
	}
	allZero := true
	for _, octet := range parsed {
		if octet != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		return "", errors.New("terminal MAC address must not be all zero")
	}
	parts := make([]string, len(parsed))
	for index, octet := range parsed {
		parts[index] = fmt.Sprintf("%02X", octet)
	}
	return strings.Join(parts, ":"), nil
}

func NormalizeAddresses(values []string, ipv4 bool) ([]string, error) {
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		address, err := netip.ParseAddr(strings.TrimSpace(value))
		if err != nil || address.Zone() != "" || address.Is4() != ipv4 {
			family := FamilyIPv6
			if ipv4 {
				family = FamilyIPv4
			}
			return nil, errors.New("invalid " + family + " address")
		}
		if !ipv4 && address.IsLinkLocalUnicast() {
			continue
		}
		canonical := address.String()
		if !seen[canonical] {
			seen[canonical] = true
			result = append(result, canonical)
		}
	}
	sort.Strings(result)
	return result, nil
}

func NormalizePrefix(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if address, err := netip.ParseAddr(trimmed); err == nil {
		if address.Zone() != "" {
			return "", errors.New("invalid IP prefix")
		}
		bits := 128
		if address.Is4() {
			bits = 32
		}
		return netip.PrefixFrom(address, bits).String(), nil
	}
	prefix, err := netip.ParsePrefix(trimmed)
	if err != nil || prefix.Addr().Zone() != "" {
		return "", errors.New("invalid IP prefix")
	}
	return prefix.Masked().String(), nil
}

func PrefixFamily(prefix string) (string, error) {
	parsed, err := netip.ParsePrefix(strings.TrimSpace(prefix))
	if err != nil {
		return "", err
	}
	if parsed.Addr().Is4() {
		return FamilyIPv4, nil
	}
	return FamilyIPv6, nil
}

func PrefixesForFamily(prefixes []string, family string) []string {
	result := make([]string, 0, len(prefixes))
	for _, value := range prefixes {
		parsed, err := netip.ParsePrefix(strings.TrimSpace(value))
		if err != nil {
			continue
		}
		if parsed.Addr().Is4() == (family == FamilyIPv4) {
			result = append(result, parsed.Masked().String())
		}
	}
	sort.Strings(result)
	return unique(result)
}

func PrefixesOverlap(left, right string) bool {
	a, errA := netip.ParsePrefix(strings.TrimSpace(left))
	b, errB := netip.ParsePrefix(strings.TrimSpace(right))
	if errA != nil || errB != nil || a.Addr().Is4() != b.Addr().Is4() {
		return false
	}
	return a.Overlaps(b)
}

func unique(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}
