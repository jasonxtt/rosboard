package policyv2

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
)

// PolicyDomain is the execution boundary for a desired graph and its
// applied state. Shared persistence does not make routing and access one
// apply domain.
type PolicyDomain string

const (
	PolicyDomainRouting  PolicyDomain = "routing"
	PolicyDomainAccess   PolicyDomain = "access"
	PolicyDomainCombined PolicyDomain = "combined"
)

type TargetVersionPromotion struct {
	TargetListID string `json:"targetListId"`
	VersionID    string `json:"versionId"`
}

type TargetConsumerDomains struct {
	Routing bool `json:"routing"`
	Access  bool `json:"access"`
}

type EgressOrigin string

const (
	EgressOriginLegacy EgressOrigin = "legacy"
	EgressOriginPolicy EgressOrigin = "policy"
)

// EgressExecutionSignature is deliberately independent of display and
// lifecycle fields. It is used only for safe reuse/copy-on-write decisions.
func EgressExecutionSignature(egress Egress) string {
	type familySignature struct {
		Family       AddressFamily `json:"family"`
		Enabled      bool          `json:"enabled"`
		WANInterface string        `json:"wanInterface"`
		Gateway      string        `json:"gateway"`
		RouteTable   string        `json:"routeTable"`
		RouteMode    string        `json:"routeMode"`
		NATMode      string        `json:"natMode"`
		WANSource    string        `json:"wanSource"`
	}
	type signature struct {
		Families     []familySignature `json:"families"`
		DNSUpstream  string            `json:"dnsUpstream"`
		FakeAlias    string            `json:"fakeAlias,omitempty"`
		FailureMode  string            `json:"failureMode"`
		RouterOutput bool              `json:"routerOutput"`
		Enabled      bool              `json:"enabled"`
	}
	families := make([]familySignature, 0, len(egress.Families))
	for _, family := range egress.Families {
		if !family.Enabled {
			continue
		}
		families = append(families, familySignature{
			Family: family.Family, Enabled: true,
			WANInterface: strings.TrimSpace(family.WANInterface), Gateway: strings.TrimSpace(family.Gateway),
			RouteTable: routeTableSignature(family.RouteTable), RouteMode: strings.TrimSpace(family.RouteMode),
			NATMode: strings.TrimSpace(family.NATMode), WANSource: strings.TrimSpace(family.WANSource),
		})
	}
	sort.Slice(families, func(i, j int) bool { return families[i].Family < families[j].Family })
	value := signature{
		Families: families, DNSUpstream: strings.TrimSpace(egress.DNSUpstream),
		FailureMode: strings.TrimSpace(egress.FailureMode), RouterOutput: egress.RouterOutput,
		Enabled: egress.Enabled,
	}
	// An empty alias means "allocate the deterministic alias for this Egress";
	// it is therefore not a semantic distinction between otherwise equal
	// configurations. An explicit alias is part of execution semantics.
	if alias := strings.TrimSpace(egress.FakeAlias); alias != "" {
		value.FakeAlias = alias
	}
	payload, _ := json.Marshal(value)
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

func EgressExecutionSignatureFor(egress Egress) string {
	return EgressExecutionSignature(egress)
}

// InternalEgressName returns a readable, server-owned identity for a newly
// created policy egress. It is presentation metadata only; execution reuse is
// decided by EgressExecutionSignature.
func InternalEgressName(egress Egress, policyName string) string {
	parts := make([]string, 0, len(egress.Families)+1)
	if policyName = strings.TrimSpace(policyName); policyName != "" {
		parts = append(parts, policyName)
	}
	for _, family := range enabledFamilies(egress.Families) {
		endpoint := strings.TrimSpace(family.WANInterface)
		if endpoint == "" || strings.TrimSpace(family.WANSource) == "next-hop" {
			endpoint = strings.TrimSpace(family.Gateway)
		}
		if gateway := strings.TrimSpace(family.Gateway); gateway != "" && endpoint != gateway {
			endpoint += "-" + gateway
		}
		parts = append(parts, string(family.Family)+"-"+endpoint)
	}
	label := readableNameKey(strings.Join(parts, "-"), "egress", 32)
	suffix := shortHash("egress-internal:"+egress.ID+":"+EgressExecutionSignature(egress), 8)
	return "rb_" + label + "_" + suffix
}

// ResolvePolicyEgress applies the policy-owned reuse/COW rule to a proposed
// execution configuration. The returned bool says whether the returned
// Egress must be persisted; a false value means the caller should only bind
// its policy relation to the returned existing Egress ID.
func ResolvePolicyEgress(proposed Egress, currentID string, existing []Egress, refCounts map[string]int, newID string) (Egress, bool) {
	currentID = strings.TrimSpace(currentID)
	proposedID := strings.TrimSpace(proposed.ID)
	byID := make(map[string]Egress, len(existing))
	for _, candidate := range existing {
		if strings.TrimSpace(candidate.ID) == "" || candidate.PendingDeletion {
			continue
		}
		byID[candidate.ID] = candidate
	}
	// A non-empty proposed ID without an explicit current relation is an
	// existing candidate selected by the caller, not proof that this policy
	// currently owns it. Never mutate that candidate in place.
	if currentID == "" && proposedID != "" {
		if candidate, ok := byID[proposedID]; ok && egressExecutionSignaturesEqual(proposed, candidate) {
			return candidate, false
		}
	}
	if current, ok := byID[currentID]; ok {
		proposed.ID = current.ID
		proposed.Revision = current.Revision
		proposed.Origin = current.Origin
		if egressExecutionSignaturesEqual(proposed, current) {
			return current, false
		}
		if current.Origin == EgressOriginPolicy && refCounts[current.ID] <= 1 {
			proposed.Name = current.Name
			return proposed, true
		}
	}
	for _, candidate := range existing {
		if strings.TrimSpace(candidate.ID) == "" || candidate.PendingDeletion || !egressExecutionSignaturesEqual(proposed, candidate) {
			continue
		}
		return candidate, false
	}
	proposed.ID = strings.TrimSpace(newID)
	if proposed.ID == "" {
		return proposed, true
	}
	proposed.Name = InternalEgressName(proposed, "")
	for index := range proposed.Families {
		if isGeneratedRouteTableName(proposed.Families[index].RouteTable) {
			// The proposal may have been normalized before its new identity was
			// allocated. Recompute the automatic table from the final Egress ID.
			proposed.Families[index].RouteTable = ""
		}
	}
	proposed.Revision = 0
	proposed.Origin = EgressOriginPolicy
	proposed.PendingDeletion = false
	proposed.Applied = false
	return proposed, true
}

func routeTableSignature(value string) string {
	value = strings.TrimSpace(value)
	if isGeneratedRouteTableName(value) {
		return ""
	}
	return value
}

func isGeneratedRouteTableName(value string) bool {
	if len(value) != 25 || !strings.HasPrefix(value, "rb_") || value[15] != '_' {
		return false
	}
	for index, character := range value {
		if index == 0 || index == 1 || index == 2 || index == 15 || index == 24 {
			continue
		}
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return value[24] == '4' || value[24] == '6'
}

func egressExecutionSignaturesEqual(left, right Egress) bool {
	if strings.TrimSpace(left.FakeAlias) == "" {
		// Persisted aliases allocated by the platform are an implementation
		// detail when the proposal leaves the alias empty.
		right.FakeAlias = ""
	}
	return EgressExecutionSignature(left) == EgressExecutionSignature(right)
}

func appendTargetPromotion(promotions *[]TargetVersionPromotion, targetID, versionID string) {
	if promotions == nil || strings.TrimSpace(targetID) == "" || strings.TrimSpace(versionID) == "" {
		return
	}
	for _, promotion := range *promotions {
		if promotion.TargetListID == targetID && promotion.VersionID == versionID {
			return
		}
	}
	*promotions = append(*promotions, TargetVersionPromotion{TargetListID: targetID, VersionID: versionID})
}
