package accesscontrol

import (
	"context"
	"errors"
	"net/netip"
	"sort"
	"strings"
	"time"

	"rosboard/internal/subject"
)

// Target scopes of a logical access rule. "internet" is a first-class scope
// meaning "block internet but keep locally identified networks"; it must never
// be modelled as a fake source or a 0.0.0.0/0 address-list entry.
const (
	TargetScopeInternet     = "internet"
	TargetScopeTargets      = "targets"
	TargetScopeSources      = "sources"
	TargetScopeApplications = "applications"
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
	ErrRuleNotFound          = errors.New("访问规则不存在")
	ErrRevisionStale         = errors.New("访问规则已被其他修改更新，请刷新后重试")
	ErrMemberDuplicate       = errors.New("该设备已是此规则的成员")
	ErrMemberAnchorRequired  = errors.New("自动跟随成员需要稳定的 MAC 身份锚点")
	ErrMemberAnchorChanged   = errors.New("自动跟随成员的身份锚点已变化")
	ErrCanonicalRuleRequired = errors.New("需要规范格式的访问规则")
)

// AccessRule is the user-facing logical entity. Multi-client, multi-source,
// and schedule semantics live here; the RouterOS expansion layer is derived
// from it, which is why terminal/source pairs must not become the primary
// entity.
type AccessRule struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	Subject       subject.Subject `json:"subject"`
	TargetScope   string          `json:"targetScope"`
	TargetListIDs []string        `json:"targetListIds"`
	Schedule      AccessSchedule  `json:"schedule"`
	Enabled       bool            `json:"enabled"`
	Revision      int64           `json:"revision"`
	CreatedAt     time.Time       `json:"createdAt"`
	UpdatedAt     time.Time       `json:"updatedAt"`

	// These fields are deliberately retained only as a physical/read
	// compatibility seam for the Slice 3 migration. Canonical API and desired
	// state never use them as authority.
	SourceIDs       []string `json:"-"`
	ApplicationIDs  []string `json:"-"`
	MigrationIssues []string `json:"-"`
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
		return MemberResolution{}, errors.New("成员解析缺少规则或终端标识")
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
	return state.DesiredRevision == state.AppliedRevision
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
		return errors.New("规则缺少 ID")
	}
	if strings.TrimSpace(rule.Name) == "" {
		return errors.New("规则名称不能为空")
	}
	if rule.Subject.Mode == subject.ModeAll || rule.Subject.Mode == subject.ModeExcluded || len(rule.Subject.Members) != 0 || len(rule.Subject.Prefixes) != 0 {
		if _, err := subject.Normalize(rule.Subject); err != nil {
			return err
		}
	}
	if rule.Subject.Mode == subject.ModeExcluded {
		return errors.New("访问控制规则的来源模式仅支持「全部」或「指定」")
	}
	if err := ValidateSchedule(rule.Schedule); err != nil {
		return err
	}
	switch rule.TargetScope {
	case TargetScopeInternet:
		if len(rule.TargetListIDs) != 0 || len(rule.SourceIDs) != 0 || len(rule.ApplicationIDs) != 0 {
			return errors.New("「整个互联网」规则不能引用目标列表")
		}
		if rule.Subject.Mode != "" && rule.Subject.Mode != subject.ModeAll && rule.Subject.Mode != subject.ModeSelected {
			return errors.New("访问控制规则的来源模式仅支持「全部」或「指定」")
		}
	case TargetScopeTargets:
		if len(rule.TargetListIDs) == 0 && (rule.Enabled || len(rule.MigrationIssues) == 0) {
			return errors.New("目标列表范围的规则至少需要一个目标列表")
		}
		if len(rule.SourceIDs) != 0 || len(rule.ApplicationIDs) != 0 {
			return errors.New("目标列表范围的规则不能引用旧版来源或应用")
		}
	case TargetScopeSources:
		if len(rule.SourceIDs) == 0 {
			return errors.New("来源范围的规则至少需要一个来源")
		}
		if len(rule.ApplicationIDs) != 0 {
			return errors.New("来源范围的规则不能引用应用")
		}
	case TargetScopeApplications:
		if len(rule.ApplicationIDs) == 0 {
			return errors.New("应用范围的规则至少需要一个应用")
		}
		if len(rule.SourceIDs) != 0 {
			return errors.New("应用范围的规则不能引用来源")
		}
	default:
		return errors.New("targetScope 必须是 internet 或 targets")
	}
	return nil
}

func ValidateMember(member RuleMember) error {
	if strings.TrimSpace(member.RuleID) == "" {
		return errors.New("成员缺少规则 ID")
	}
	if strings.TrimSpace(member.TerminalID) == "" {
		return errors.New("成员缺少终端标识")
	}
	switch member.Binding {
	case BindingAuto:
		if len(member.PinnedIPv4) != 0 || len(member.PinnedIPv6) != 0 {
			return errors.New("自动跟随成员不能固定地址")
		}
		if strings.TrimSpace(member.AnchorMAC) != "" {
			if _, err := NormalizeMAC(member.AnchorMAC); err != nil {
				return err
			}
		}
	case BindingFixed:
		if len(member.PinnedIPv4)+len(member.PinnedIPv6) == 0 {
			return errors.New("固定成员至少需要一个固定地址")
		}
		if _, err := normalizeAddresses(member.PinnedIPv4, true); err != nil {
			return err
		}
		if _, err := normalizeAddresses(member.PinnedIPv6, false); err != nil {
			return err
		}
	default:
		return errors.New("绑定方式必须是 auto 或 fixed")
	}
	return nil
}

func NormalizeRule(rule AccessRule) (AccessRule, error) {
	rule.ID = strings.TrimSpace(rule.ID)
	rule.Name = strings.TrimSpace(rule.Name)
	rule.TargetScope = strings.TrimSpace(rule.TargetScope)
	if strings.TrimSpace(rule.Subject.Mode) == "" {
		rule.Subject.Mode = subject.ModeSelected
	}
	if rule.TargetScope == TargetScopeInternet && rule.Subject.Mode == "" {
		rule.Subject.Mode = subject.ModeAll
	}
	if rule.Subject.Mode != subject.ModeSelected || len(rule.Subject.Members) != 0 || len(rule.Subject.Prefixes) != 0 {
		var err error
		rule.Subject, err = subject.Normalize(rule.Subject)
		if err != nil {
			return AccessRule{}, err
		}
	}
	if rule.Subject.Mode == subject.ModeExcluded {
		return AccessRule{}, errors.New("访问控制规则的来源模式仅支持「全部」或「指定」")
	}
	var err error
	rule.Schedule, err = NormalizeSchedule(rule.Schedule)
	if err != nil {
		return AccessRule{}, err
	}
	targetIDs := make([]string, 0, len(rule.TargetListIDs))
	seenTargets := make(map[string]bool, len(rule.TargetListIDs))
	for _, targetID := range rule.TargetListIDs {
		targetID = strings.TrimSpace(targetID)
		if targetID == "" || seenTargets[targetID] {
			continue
		}
		seenTargets[targetID] = true
		targetIDs = append(targetIDs, targetID)
	}
	sort.Strings(targetIDs)
	rule.TargetListIDs = targetIDs
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
	applicationIDs := make([]string, 0, len(rule.ApplicationIDs))
	seenApplications := make(map[string]bool, len(rule.ApplicationIDs))
	for _, applicationID := range rule.ApplicationIDs {
		applicationID = strings.TrimSpace(applicationID)
		if applicationID == "" || seenApplications[applicationID] {
			continue
		}
		seenApplications[applicationID] = true
		applicationIDs = append(applicationIDs, applicationID)
	}
	sort.Strings(applicationIDs)
	rule.ApplicationIDs = applicationIDs
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
	return subject.NormalizeMAC(value)
}

func IsReliableMAC(value string) bool {
	_, err := NormalizeMAC(value)
	return err == nil && strings.TrimSpace(value) != ""
}

func normalizeAddresses(values []string, ipv4 bool) ([]string, error) {
	return subject.NormalizeAddresses(values, ipv4)
}
