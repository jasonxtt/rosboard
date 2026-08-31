package accesscontrol

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sort"
	"strings"
	"time"
)

// Target scopes of a logical access rule. "internet" is a first-class scope
// meaning "block internet but keep locally identified networks"; it must never
// be modelled as a fake source or a 0.0.0.0/0 address-list entry.
const (
	TargetScopeInternet = "internet"
	TargetScopeSources  = "sources"
)

// Member bindings. Auto-follow projects the terminal's current addresses from
// the monitor snapshot; fixed pins user-chosen addresses.
const (
	BindingAuto  = "auto"
	BindingFixed = "fixed"
)

// Member projection states produced while building desired state.
const (
	MemberResolved   = "resolved"
	MemberUnresolved = "temporarily_unresolved"
	MemberConflicted = "conflicted"
)

var (
	ErrRuleNotFound         = errors.New("access rule not found")
	ErrRevisionStale        = errors.New("access rule revision is stale")
	ErrMemberDuplicate      = errors.New("terminal is already a member of this rule")
	ErrMemberAnchorRequired = errors.New("auto-follow member requires a stable MAC anchor")
	ErrMemberAnchorChanged  = errors.New("auto-follow member identity anchor changed")
)

// AccessRule is the user-facing logical entity. Multi-client and multi-source
// semantics live here; the RouterOS expansion layer is derived from it. Future
// schedule windows and shared daily quotas aggregate on this rule identity,
// which is why terminal/source pairs must not become the primary entity.
type AccessRule struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	TargetScope string    `json:"targetScope"`
	SourceIDs   []string  `json:"sourceIds"`
	Enabled     bool      `json:"enabled"`
	Revision    int64     `json:"revision"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// RuleMember is one controlled terminal inside a logical rule. LastIPv4/6 hold
// the last confirmed address resolution and are managed internally; they are
// not part of the user-facing API payload.
type RuleMember struct {
	RuleID     string `json:"ruleId"`
	TerminalID string `json:"terminalId"`
	Binding    string `json:"binding"`
	// AnchorMAC is the MAC identity observed when an auto-follow member was
	// created. It is deliberately omitted from the public JSON representation;
	// it is an internal ownership anchor, not a user-editable field.
	AnchorMAC  string   `json:"-"`
	PinnedIPv4 []string `json:"pinnedIpv4"`
	PinnedIPv6 []string `json:"pinnedIpv6"`
	LastIPv4   []string `json:"-"`
	LastIPv6   []string `json:"-"`
}

type Terminal struct {
	ID          string   `json:"id"`
	DisplayName string   `json:"displayName"`
	MACAddress  string   `json:"macAddress"`
	IPv4        []string `json:"ipv4"`
	IPv6        []string `json:"ipv6"`
}

// ScopePrefix is one locally trusted network prefix (from the monitor's
// TerminalScope). Interface is optional and is used as additional evidence
// when excluding local interfaces from internet egress discovery.
type ScopePrefix struct {
	CIDR      string `json:"cidr"`
	Family    string `json:"family"`
	Interface string `json:"interface,omitempty"`
}

// Scope carries local-network evidence from the monitor. Internet-scope rules
// use RouterOS default-route interfaces for enforcement; this evidence only
// prevents a local interface from being selected as an internet egress.
type Scope struct {
	Prefixes        []ScopePrefix `json:"prefixes"`
	LocalInterfaces []string      `json:"localInterfaces,omitempty"`
}

// InternetEgressCandidate is an interface that the operator may explicitly
// confirm when RouterOS does not expose a default route in a form rosboard can
// prove safe. The family is the map key in plan/API payloads.
type InternetEgressCandidate struct {
	Interface string `json:"interface"`
	Type      string `json:"type"`
	Running   bool   `json:"running"`
	Reason    string `json:"reason,omitempty"`
}

func (scope Scope) HasFamily(family string) bool {
	_, ok := scope.PrefixesForFamily(family)
	return ok
}

// PrefixesForFamily returns the canonical, trusted local prefixes for one
// address family. A malformed prefix only invalidates the family it claims;
// an unlabelled/malformed prefix is conservatively considered relevant to the
// requested family because its family cannot be established safely.
func (scope Scope) PrefixesForFamily(family string) ([]string, bool) {
	family = strings.ToLower(strings.TrimSpace(family))
	if family != FamilyIPv4 && family != FamilyIPv6 {
		return nil, false
	}
	prefixes := make([]string, 0, len(scope.Prefixes))
	seen := make(map[string]bool, len(scope.Prefixes))
	invalidForFamily := false
	for _, prefix := range scope.Prefixes {
		declaredFamily := strings.ToLower(strings.TrimSpace(prefix.Family))
		parsed, err := netip.ParsePrefix(strings.TrimSpace(prefix.CIDR))
		if err != nil {
			if declaredFamily == "" || declaredFamily == family || (declaredFamily != FamilyIPv4 && declaredFamily != FamilyIPv6) {
				invalidForFamily = true
			}
			continue
		}
		actualFamily := FamilyIPv4
		if parsed.Addr().Is6() {
			actualFamily = FamilyIPv6
		}
		if declaredFamily == "" {
			declaredFamily = actualFamily
		}
		if declaredFamily != FamilyIPv4 && declaredFamily != FamilyIPv6 {
			if actualFamily == family {
				invalidForFamily = true
			}
			continue
		}
		if declaredFamily != actualFamily {
			if declaredFamily == family || actualFamily == family {
				invalidForFamily = true
			}
			continue
		}
		if actualFamily != family {
			continue
		}
		canonical := parsed.Masked().String()
		if !seen[canonical] {
			seen[canonical] = true
			prefixes = append(prefixes, canonical)
		}
	}
	if invalidForFamily || len(prefixes) == 0 {
		return nil, false
	}
	sort.Strings(prefixes)
	return prefixes, true
}

// State is the device-level desired/applied revision pair for access control.
type State struct {
	DeviceID        string    `json:"deviceId"`
	DesiredRevision int64     `json:"desiredRevision"`
	AppliedRevision int64     `json:"appliedRevision"`
	AppliedAt       time.Time `json:"appliedAt,omitempty"`
}

// MemberResolution is a current, successful auto-follow observation. It is
// carried with a desired plan and persisted only after RouterOS read-back
// verification succeeds.
type MemberResolution struct {
	RuleID     string
	TerminalID string
	AnchorMAC  string
	IPv4       []string
	IPv6       []string
}

func NormalizeMemberResolution(resolution MemberResolution) (MemberResolution, error) {
	resolution.RuleID = strings.TrimSpace(resolution.RuleID)
	resolution.TerminalID = strings.TrimSpace(resolution.TerminalID)
	if resolution.RuleID == "" || resolution.TerminalID == "" {
		return MemberResolution{}, errors.New("member resolution identity is required")
	}
	anchor, err := NormalizeMAC(resolution.AnchorMAC)
	if err != nil || anchor == "" {
		return MemberResolution{}, ErrMemberAnchorRequired
	}
	resolution.AnchorMAC = anchor
	resolution.IPv4, err = normalizeAddresses(resolution.IPv4, true)
	if err != nil {
		return MemberResolution{}, err
	}
	resolution.IPv6, err = normalizeAddresses(resolution.IPv6, false)
	if err != nil {
		return MemberResolution{}, err
	}
	return resolution, nil
}

func (state State) Applied() bool {
	return state.DesiredRevision > 0 && state.DesiredRevision == state.AppliedRevision
}

type Repository interface {
	ListRules(context.Context) ([]AccessRule, error)
	ListMembers(context.Context) ([]RuleMember, error)
	GetState(context.Context) (State, error)
	// SaveMemberResolutions records the last confirmed address resolution of
	// one auto-follow member so a temporarily unseen terminal can keep its
	// last trusted projection.
	SaveMemberResolutions(ctx context.Context, ruleID, terminalID string, ipv4, ipv6 []string) error
}

func ValidateRule(rule AccessRule) error {
	if strings.TrimSpace(rule.ID) == "" {
		return errors.New("rule id is required")
	}
	if strings.TrimSpace(rule.Name) == "" {
		return errors.New("rule name is required")
	}
	switch rule.TargetScope {
	case TargetScopeInternet:
		if len(rule.SourceIDs) != 0 {
			return errors.New("internet scope rules must not reference sources")
		}
	case TargetScopeSources:
		if len(rule.SourceIDs) == 0 {
			return errors.New("sources scope rules require at least one source")
		}
	default:
		return errors.New("targetScope must be internet or sources")
	}
	return nil
}

func ValidateMember(member RuleMember) error {
	if strings.TrimSpace(member.RuleID) == "" {
		return errors.New("member rule id is required")
	}
	if strings.TrimSpace(member.TerminalID) == "" {
		return errors.New("member terminal id is required")
	}
	switch member.Binding {
	case BindingAuto:
		if len(member.PinnedIPv4) != 0 || len(member.PinnedIPv6) != 0 {
			return errors.New("auto-follow member cannot pin addresses")
		}
		if strings.TrimSpace(member.AnchorMAC) != "" {
			if _, err := NormalizeMAC(member.AnchorMAC); err != nil {
				return err
			}
		}
	case BindingFixed:
		if len(member.PinnedIPv4)+len(member.PinnedIPv6) == 0 {
			return errors.New("fixed member requires at least one pinned address")
		}
		if _, err := normalizeAddresses(member.PinnedIPv4, true); err != nil {
			return err
		}
		if _, err := normalizeAddresses(member.PinnedIPv6, false); err != nil {
			return err
		}
	default:
		return errors.New("binding must be auto or fixed")
	}
	return nil
}

func NormalizeRule(rule AccessRule) (AccessRule, error) {
	rule.ID = strings.TrimSpace(rule.ID)
	rule.Name = strings.TrimSpace(rule.Name)
	rule.TargetScope = strings.TrimSpace(rule.TargetScope)
	sourceIDs := make([]string, 0, len(rule.SourceIDs))
	seen := make(map[string]bool, len(rule.SourceIDs))
	for _, sourceID := range rule.SourceIDs {
		sourceID = strings.TrimSpace(sourceID)
		if sourceID == "" || seen[sourceID] {
			continue
		}
		seen[sourceID] = true
		sourceIDs = append(sourceIDs, sourceID)
	}
	sort.Strings(sourceIDs)
	rule.SourceIDs = sourceIDs
	if err := ValidateRule(rule); err != nil {
		return AccessRule{}, err
	}
	return rule, nil
}

func NormalizeMember(member RuleMember) (RuleMember, error) {
	member.RuleID = strings.TrimSpace(member.RuleID)
	member.TerminalID = strings.TrimSpace(member.TerminalID)
	member.Binding = strings.TrimSpace(member.Binding)
	if strings.TrimSpace(member.AnchorMAC) != "" {
		var err error
		member.AnchorMAC, err = NormalizeMAC(member.AnchorMAC)
		if err != nil {
			return RuleMember{}, err
		}
	}
	var err error
	member.PinnedIPv4, err = normalizeAddresses(member.PinnedIPv4, true)
	if err != nil {
		return RuleMember{}, err
	}
	member.PinnedIPv6, err = normalizeAddresses(member.PinnedIPv6, false)
	if err != nil {
		return RuleMember{}, err
	}
	if member.Binding == BindingAuto {
		// 保持空切片而不是 nil：JSON 序列化时输出 [] 而不是 null，
		// 前端直接对固定地址数组做展开与 .length 访问。
		member.PinnedIPv4 = []string{}
		member.PinnedIPv6 = []string{}
	} else if member.Binding == BindingFixed {
		// Fixed bindings are address-owned, not identity-following. Do not
		// retain a stale MAC anchor when a member changes binding.
		member.AnchorMAC = ""
	}
	if err := ValidateMember(member); err != nil {
		return RuleMember{}, err
	}
	return member, nil
}

// NormalizeMAC accepts only a six-byte Ethernet identity and returns the
// canonical upper-case colon-separated form. A non-empty but malformed value
// must never be treated as a reliable terminal identity.
func NormalizeMAC(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", nil
	}
	parsed, err := net.ParseMAC(trimmed)
	if err != nil || len(parsed) != 6 {
		return "", fmt.Errorf("invalid terminal MAC address")
	}
	if parsed[0]&1 != 0 {
		return "", fmt.Errorf("terminal MAC address must be unicast")
	}
	allZero := true
	for _, octet := range parsed {
		if octet != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		return "", fmt.Errorf("terminal MAC address must not be all zero")
	}
	parts := make([]string, len(parsed))
	for index, octet := range parsed {
		parts[index] = fmt.Sprintf("%02X", octet)
	}
	return strings.Join(parts, ":"), nil
}

func IsReliableMAC(value string) bool {
	_, err := NormalizeMAC(value)
	return err == nil && strings.TrimSpace(value) != ""
}

func normalizeAddresses(values []string, ipv4 bool) ([]string, error) {
	seen := make(map[string]bool)
	result := make([]string, 0, len(values))
	for _, value := range values {
		address, err := netip.ParseAddr(strings.TrimSpace(value))
		if err != nil || address.Zone() != "" || address.Is4() != ipv4 {
			family := "IPv6"
			if ipv4 {
				family = "IPv4"
			}
			return nil, errors.New("invalid " + family + " address")
		}
		// Link-local IPv6 addresses are interface-scoped and cannot be used
		// reliably in a forwarded RouterOS address-list rule. Keep all other
		// valid IPv6 addresses, including ULA addresses.
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
