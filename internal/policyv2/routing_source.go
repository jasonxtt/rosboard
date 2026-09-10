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

// RoutingSourceScope stores only the typed discriminator and the selector name
// needed by interface/interface-list sources. Device and IP payloads continue
// to use RoutingRule.Subject and its existing child-table persistence.
type RoutingSourceScope struct {
	Kind RoutingSourceKind `json:"kind"`
	Name string            `json:"name,omitempty"`
}

var (
	ErrRoutingSourceScopeInvalid  = errors.New("invalid routing source scope")
	ErrRoutingSourceScopeConflict = errors.New("routing source scope conflicts with legacy projection")
)

func IsDeferredRoutingSourceInterfaceListName(name string) bool {
	return strings.EqualFold(strings.TrimSpace(name), "all")
}

func IsDeferredRoutingSource(scope *RoutingSourceScope) bool {
	return scope != nil && scope.Kind == RoutingSourceInterfaceList && IsDeferredRoutingSourceInterfaceListName(scope.Name)
}

func routingSourceInterfaceListAllDeferredIssue(logicalID string) PlanIssue {
	return PlanIssue{
		Code:      RoutingSourceInterfaceListAllDeferredCode,
		Status:    "blocker",
		LogicalID: logicalID,
		Reason:    `InterfaceList("all") as a routing source is deferred until a safe matcher implementation is available`,
	}
}

func routingSourceAllDeferredIssue(logicalID string) PlanIssue {
	return PlanIssue{
		Code:      RoutingSourceAllDeferredCode,
		Status:    "blocker",
		LogicalID: logicalID,
		Reason:    `SourceScope("all") is deferred until RouterOS prerouting/output and loop-safety semantics are proven`,
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
		if normalized.Name != "" {
			return nil, fmt.Errorf("%w: %s source must not contain a name", ErrRoutingSourceScopeInvalid, normalized.Kind)
		}
	case RoutingSourceInterface, RoutingSourceInterfaceList:
		if normalized.Name == "" {
			return nil, fmt.Errorf("%w: %s source requires a name", ErrRoutingSourceScopeInvalid, normalized.Kind)
		}
	default:
		return nil, fmt.Errorf("%w: unsupported source kind %q", ErrRoutingSourceScopeInvalid, value.Kind)
	}
	return normalized, nil
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
		return Subject{Mode: SubjectModeAll}, NormalizeTrafficIngressScopeUnvalidated(TrafficIngressScope{Interfaces: []string{normalizedScope.Name}}), nil
	case RoutingSourceInterfaceList:
		return Subject{Mode: SubjectModeAll}, NormalizeTrafficIngressScopeUnvalidated(TrafficIngressScope{InterfaceLists: []string{normalizedScope.Name}}), nil
	case RoutingSourceAll:
		return Subject{Mode: SubjectModeAll}, NormalizeTrafficIngressScopeUnvalidated(TrafficIngressScope{}), nil
	default:
		return Subject{}, TrafficIngressScope{}, fmt.Errorf("%w: unsupported source kind %q", ErrRoutingSourceScopeInvalid, normalizedScope.Kind)
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
			return Subject{}, fmt.Errorf("%w: device source requires selected terminal members only", ErrRoutingSourceScopeInvalid)
		}
	case RoutingSourceIP:
		if normalized.Mode != SubjectModeSelected || len(normalized.Members) != 0 || len(normalized.Prefixes) == 0 {
			return Subject{}, fmt.Errorf("%w: ip source requires selected IP prefixes only", ErrRoutingSourceScopeInvalid)
		}
	case RoutingSourceInterface, RoutingSourceInterfaceList, RoutingSourceAll:
		if normalized.Mode != SubjectModeAll || len(normalized.Members) != 0 || len(normalized.Prefixes) != 0 {
			return Subject{}, fmt.Errorf("%w: %s source uses an all-subject compatibility projection", ErrRoutingSourceScopeInvalid, scope.Kind)
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
		return false, fmt.Errorf("%w: canonical source scope is absent", ErrRoutingSourceScopeConflict)
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
			return RoutingRule{}, fmt.Errorf("%w: legacy Subject/Ingress does not match the canonical source", ErrRoutingSourceScopeConflict)
		}
		// Preserve the canonical source payload wholesale for old-client
		// non-source edits. This also preserves hidden identity state such as
		// AnchorMAC and last trusted addresses when those fields were omitted.
		value.SourceScope = &RoutingSourceScope{Kind: current.SourceScope.Kind, Name: current.SourceScope.Name}
		value.Subject = current.Subject
		value.Ingress = current.Ingress
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
	ID          string              `json:"id"`
	SourceScope *RoutingSourceScope `json:"sourceScope,omitempty"`
	Subject     routingSubjectHash  `json:"subject"`
	Ingress     TrafficIngressScope `json:"ingress"`
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
		ID: rule.ID, SourceScope: rule.SourceScope,
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
