package policyv2

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"rosboard/internal/routeros"
)

// crossDomainDNSConstraint is the physical-order contract for one overlapping
// Access and Routing DNS Static pair. It intentionally contains logical IDs,
// not RouterOS IDs, so a plan remains meaningful across create/recovery paths.
type crossDomainDNSConstraint struct {
	AccessLogicalID  string     `json:"accessLogicalID"`
	RoutingLogicalID string     `json:"routingLogicalID"`
	AccessRuleID     string     `json:"accessRuleID"`
	AccessTargetID   string     `json:"accessTargetID"`
	RoutingRuleID    string     `json:"routingRuleID"`
	RoutingEgressID  string     `json:"routingEgressID"`
	RoutingTargetID  string     `json:"routingTargetID"`
	AccessMatcher    SourceRule `json:"accessMatcher"`
	RoutingMatcher   SourceRule `json:"routingMatcher"`
}

type crossDomainDNSActual struct {
	LogicalID      string `json:"logicalID"`
	RouterID       string `json:"routerID"`
	Disabled       string `json:"disabled,omitempty"`
	Name           string `json:"name,omitempty"`
	MatchSubdomain string `json:"matchSubdomain,omitempty"`
	AddressList    string `json:"addressList,omitempty"`
	ForwardTo      string `json:"forwardTo,omitempty"`
}

func buildCrossDomainDNSConstraints(resolutions []CrossDomainProjectionResolution) []crossDomainDNSConstraint {
	constraints := make([]crossDomainDNSConstraint, 0)
	seen := make(map[string]bool)
	for _, resolution := range resolutions {
		for _, overlap := range resolution.Overlaps {
			accessLogicalID := domainDNSLogicalID("access", resolution.AccessTargetID, "", overlap[0])
			routingLogicalID := domainDNSLogicalID("routing", resolution.RoutingTargetID, resolution.RoutingEgressID, overlap[1])
			key := accessLogicalID + "\x00" + routingLogicalID
			if seen[key] {
				continue
			}
			seen[key] = true
			constraints = append(constraints, crossDomainDNSConstraint{
				AccessLogicalID: accessLogicalID, RoutingLogicalID: routingLogicalID,
				AccessRuleID: resolution.AccessRuleID, AccessTargetID: resolution.AccessTargetID,
				RoutingRuleID: resolution.RoutingRuleID, RoutingEgressID: resolution.RoutingEgressID,
				RoutingTargetID: resolution.RoutingTargetID, AccessMatcher: overlap[0], RoutingMatcher: overlap[1],
			})
		}
	}
	sort.Slice(constraints, func(i, j int) bool {
		left, right := constraints[i], constraints[j]
		if left.AccessLogicalID != right.AccessLogicalID {
			return left.AccessLogicalID < right.AccessLogicalID
		}
		return left.RoutingLogicalID < right.RoutingLogicalID
	})
	return constraints
}

func domainDNSLogicalID(domain, targetID, egressID string, matcher SourceRule) string {
	if domain == "access" {
		return "access-target-dns:" + targetID + ":" + matcher.RuleType + ":" + matcher.Domain
	}
	return "routing-dns:" + egressID + ":" + targetID + ":" + matcher.RuleType + ":" + matcher.Domain
}

func parseDomainDNSProjection(object DesiredObject) (crossDomainDNSConstraint, bool) {
	if object.Menu != string(routeros.MenuIPDNSStatic) || strings.TrimSpace(object.Fields["name"]) == "" {
		return crossDomainDNSConstraint{}, false
	}
	matcher := SourceRule{RuleType: "DOMAIN", Domain: strings.TrimSpace(object.Fields["name"])}
	if strings.EqualFold(strings.TrimSpace(object.Fields["match-subdomain"]), "yes") {
		matcher.RuleType = "DOMAIN-SUFFIX"
	}
	logicalID := object.LogicalID
	switch {
	case strings.HasPrefix(logicalID, "access-target-dns:"):
		parts := strings.SplitN(strings.TrimPrefix(logicalID, "access-target-dns:"), ":", 3)
		if len(parts) != 3 {
			return crossDomainDNSConstraint{}, false
		}
		matcher.RuleType, matcher.Domain = parts[1], parts[2]
		return crossDomainDNSConstraint{AccessLogicalID: logicalID, AccessTargetID: parts[0], AccessMatcher: matcher}, true
	case strings.HasPrefix(logicalID, "routing-dns:"):
		parts := strings.SplitN(strings.TrimPrefix(logicalID, "routing-dns:"), ":", 4)
		if len(parts) != 4 {
			return crossDomainDNSConstraint{}, false
		}
		matcher.RuleType, matcher.Domain = parts[2], parts[3]
		return crossDomainDNSConstraint{RoutingLogicalID: logicalID, RoutingEgressID: parts[0], RoutingTargetID: parts[1], RoutingMatcher: matcher}, true
	case strings.HasPrefix(logicalID, "dns:"):
		parts := strings.SplitN(strings.TrimPrefix(logicalID, "dns:"), ":", 4)
		if len(parts) != 4 {
			return crossDomainDNSConstraint{}, false
		}
		matcher.RuleType, matcher.Domain = parts[2], parts[3]
		return crossDomainDNSConstraint{RoutingLogicalID: logicalID, RoutingEgressID: parts[0], RoutingTargetID: parts[1], RoutingMatcher: matcher}, true
	default:
		return crossDomainDNSConstraint{}, false
	}
}

func scanManagedForPlan(ctx context.Context, mutation PolicyMutation, repository Repository, desired, crossDesired []DesiredObject, domain PolicyDomain, constraints []crossDomainDNSConstraint) ([]ActualObject, []ActualObject, string, error) {
	allDesired := append([]DesiredObject(nil), desired...)
	allDesired = append(allDesired, crossDesired...)
	actual, _, err := ScanManaged(ctx, mutation, repository, allDesired)
	if err != nil {
		return nil, nil, "", err
	}
	domainActual := actual
	if domain != PolicyDomainCombined {
		domainActual = make([]ActualObject, 0, len(actual))
		for _, object := range actual {
			if managedActualDomain(object) == domain {
				domainActual = append(domainActual, object)
			}
		}
	}
	domainFingerprint, err := fingerprintActualObjects(domainActual)
	if err != nil {
		return nil, nil, "", err
	}
	includeCrossDomainState := len(constraints) > 0
	if !includeCrossDomainState && (domain == PolicyDomainAccess || domain == PolicyDomainCombined) {
		for _, object := range crossDomainAccessDNSDesired(allDesired) {
			if desiredObjectActive(object) {
				includeCrossDomainState = true
				break
			}
		}
	}
	if !includeCrossDomainState {
		return domainActual, actual, domainFingerprint, nil
	}
	crossFingerprint, err := fingerprintCrossDomainActual(actual)
	if err != nil {
		return nil, nil, "", err
	}
	payload, err := json.Marshal(struct {
		DomainFingerprint      string
		CrossDomainFingerprint string
	}{domainFingerprint, crossFingerprint})
	if err != nil {
		return nil, nil, "", err
	}
	digest := sha256.Sum256(payload)
	return domainActual, actual, hex.EncodeToString(digest[:]), nil
}

func fingerprintActualObjects(actual []ActualObject) (string, error) {
	payload, err := json.Marshal(actual)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

func fingerprintCrossDomainActual(actual []ActualObject) (string, error) {
	ordered := make([]ActualObject, 0)
	for _, object := range actual {
		if object.Menu != string(routeros.MenuIPDNSStatic) || object.Ownership != "owned" {
			continue
		}
		ordered = append(ordered, object)
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Position != ordered[j].Position {
			return ordered[i].Position < ordered[j].Position
		}
		return ordered[i].LogicalID < ordered[j].LogicalID
	})
	entries := make([]crossDomainDNSActual, 0, len(ordered))
	for _, object := range ordered {
		entries = append(entries, crossDomainDNSActual{
			LogicalID: object.LogicalID, RouterID: object.RouterID, Disabled: object.Fields["disabled"],
			Name: object.Fields["name"], MatchSubdomain: object.Fields["match-subdomain"],
			AddressList: object.Fields["address-list"], ForwardTo: object.Fields["forward-to"],
		})
	}
	payload, err := json.Marshal(entries)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

func appendCrossDomainPrecedenceBlockers(domain PolicyDomain, desired, crossDesired []DesiredObject, constraints []crossDomainDNSConstraint, actual []ActualObject, result *DesiredResult) {
	if domain != PolicyDomainRouting && domain != PolicyDomainCombined {
		return
	}
	desiredByLogicalID := crossDomainDesiredByLogicalID(desired, crossDesired)
	actualByLogicalID := ownedDNSActualByLogicalID(actual)
	seen := make(map[string]bool)
	for _, constraint := range constraints {
		matches := actualByLogicalID[constraint.AccessLogicalID]
		expected, expectedOK := desiredByLogicalID[constraint.AccessLogicalID]
		available := expectedOK && desiredObjectActive(expected) && len(matches) == 1 && matches[0].RouterID != "" && actualObjectActive(matches[0]) && crossDomainAccessActualMatchesDesired(matches[0], expected)
		if available || seen[constraint.AccessLogicalID] {
			continue
		}
		seen[constraint.AccessLogicalID] = true
		result.Blockers = append(result.Blockers, PlanIssue{
			Code: "cross_domain_access_precedence_unavailable", Status: "blocker", LogicalID: constraint.AccessLogicalID, EgressID: constraint.RoutingEgressID,
			Reason: "启用的访问控制域名投影必须先于策略路由投影存在且处于启用状态，当前无法证明其 RouterOS DNS Static 顺序：" + constraint.AccessLogicalID + " → " + constraint.RoutingLogicalID,
		})
	}
}

func planCrossDomainDNSMoves(domain PolicyDomain, desired, crossDesired []DesiredObject, constraints []crossDomainDNSConstraint, actual []ActualObject) []PlanOperation {
	if len(constraints) == 0 && domain != PolicyDomainAccess {
		return nil
	}
	allDesired := append([]DesiredObject(nil), desired...)
	allDesired = append(allDesired, crossDesired...)
	desiredByLogicalID := make(map[string]DesiredObject, len(allDesired))
	maxOrder := 0
	for _, object := range allDesired {
		if object.Order > maxOrder {
			maxOrder = object.Order
		}
		if _, ok := parseDomainDNSProjection(object); ok {
			desiredByLogicalID[object.LogicalID] = object
		}
	}
	actualByLogicalID := ownedDNSActualByLogicalID(actual)
	type moveCandidate struct {
		accessActual  ActualObject
		accessPresent bool
		routingActual ActualObject
	}
	candidates := make(map[string]moveCandidate)
	for _, constraint := range constraints {
		accessDesired, accessDesiredOK := desiredByLogicalID[constraint.AccessLogicalID]
		routingDesired, routingDesiredOK := desiredByLogicalID[constraint.RoutingLogicalID]
		if !accessDesiredOK || !routingDesiredOK || !desiredObjectActive(accessDesired) || !desiredObjectActive(routingDesired) {
			continue
		}
		accessActual, accessActualOK := uniqueOwnedActual(actualByLogicalID[constraint.AccessLogicalID])
		routingActual, routingActualOK := uniqueOwnedActual(actualByLogicalID[constraint.RoutingLogicalID])
		if !routingActualOK || (domain != PolicyDomainAccess && !accessActualOK) {
			// A Routing plan must not move a missing Access object. The
			// precedence blocker handles that case; a newly created Routing
			// object is appended after existing Access statics and needs no
			// extra move when the Access object is already present.
			continue
		}
		candidate, exists := candidates[constraint.AccessLogicalID]
		if !exists || routingActual.Position < candidate.routingActual.Position ||
			(routingActual.Position == candidate.routingActual.Position && routingActual.LogicalID < candidate.routingActual.LogicalID) {
			candidates[constraint.AccessLogicalID] = moveCandidate{accessActual: accessActual, accessPresent: accessActualOK, routingActual: routingActual}
		}
	}
	if domain == PolicyDomainAccess || domain == PolicyDomainCombined {
		accessDesired := crossDomainAccessDNSDesired(allDesired)
		routingActual := crossDomainRoutingDNSActuals(actual, false)
		for _, accessObject := range accessDesired {
			accessProjection, ok := parseDomainDNSProjection(accessObject)
			if !ok || accessProjection.AccessLogicalID == "" || !desiredObjectActive(accessObject) {
				continue
			}
			accessActual, accessActualOK := uniqueOwnedActual(actualByLogicalID[accessObject.LogicalID])
			for _, routingObject := range routingActual {
				routingMatcher, routingOK := actualDNSMatcher(routingObject)
				if !routingOK || len(domainProjectionOverlaps([]SourceRule{accessProjection.AccessMatcher}, []SourceRule{routingMatcher})) == 0 {
					continue
				}
				candidate, exists := candidates[accessObject.LogicalID]
				if !exists || routingObject.Position < candidate.routingActual.Position ||
					(routingObject.Position == candidate.routingActual.Position && routingObject.LogicalID < candidate.routingActual.LogicalID) {
					candidates[accessObject.LogicalID] = moveCandidate{accessActual: accessActual, accessPresent: accessActualOK, routingActual: routingObject}
				}
			}
		}
	}
	accessLogicalIDs := make([]string, 0, len(candidates))
	for logicalID := range candidates {
		accessLogicalIDs = append(accessLogicalIDs, logicalID)
	}
	sort.Strings(accessLogicalIDs)
	result := make([]PlanOperation, 0, len(accessLogicalIDs))
	for _, accessLogicalID := range accessLogicalIDs {
		candidate := candidates[accessLogicalID]
		if candidate.accessPresent && candidate.accessActual.Position < candidate.routingActual.Position {
			continue
		}
		sourceID := ""
		if candidate.accessPresent {
			sourceID = candidate.accessActual.RouterID
		}
		result = append(result, PlanOperation{
			Order: maxOrder + len(result) + 1, Phase: "dns", Action: "move", Menu: string(routeros.MenuIPDNSStatic),
			LogicalID: accessLogicalID, RouterID: sourceID, Ownership: "owned",
			Anchor: &PlanAnchor{LogicalID: candidate.routingActual.LogicalID, RouterID: candidate.routingActual.RouterID, Relation: "before", Menu: string(routeros.MenuIPDNSStatic)},
		})
	}
	return result
}

func crossDomainDesiredByLogicalID(desired, crossDesired []DesiredObject) map[string]DesiredObject {
	result := make(map[string]DesiredObject, len(desired)+len(crossDesired))
	for _, object := range append(append([]DesiredObject(nil), desired...), crossDesired...) {
		if object.LogicalID != "" {
			result[object.LogicalID] = object
		}
	}
	return result
}

func crossDomainAccessDNSDesired(desired []DesiredObject) []DesiredObject {
	result := make([]DesiredObject, 0)
	for _, object := range desired {
		projection, ok := parseDomainDNSProjection(object)
		if ok && projection.AccessLogicalID != "" {
			result = append(result, object)
		}
	}
	return result
}

func crossDomainRoutingDNSActuals(actual []ActualObject, includeDisabled bool) []ActualObject {
	result := make([]ActualObject, 0)
	for _, object := range actual {
		if object.Menu != string(routeros.MenuIPDNSStatic) || object.Ownership != "owned" || object.RouterID == "" || managedActualDomain(object) != PolicyDomainRouting {
			continue
		}
		if !includeDisabled && !actualObjectActive(object) {
			continue
		}
		if _, ok := actualDNSMatcher(object); !ok {
			continue
		}
		result = append(result, object)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Position != result[j].Position {
			return result[i].Position < result[j].Position
		}
		return result[i].LogicalID < result[j].LogicalID
	})
	return result
}

func actualDNSMatcher(object ActualObject) (SourceRule, bool) {
	if object.Menu != string(routeros.MenuIPDNSStatic) {
		return SourceRule{}, false
	}
	domain := strings.TrimSpace(object.Fields["name"])
	if domain == "" {
		return SourceRule{}, false
	}
	ruleType := "DOMAIN"
	if strings.EqualFold(strings.TrimSpace(object.Fields["match-subdomain"]), "yes") || strings.EqualFold(strings.TrimSpace(object.Fields["match-subdomain"]), "true") {
		ruleType = "DOMAIN-SUFFIX"
	}
	return SourceRule{RuleType: ruleType, Domain: domain}, true
}

func crossDomainAccessActualMatchesDesired(actual ActualObject, desired DesiredObject) bool {
	for _, key := range []string{"name", "type", "match-subdomain", "address-list", "forward-to"} {
		if !equivalentRouterField(key, actual.Fields[key], desired.Fields[key]) {
			return false
		}
	}
	return true
}

func ownedDNSActualByLogicalID(actual []ActualObject) map[string][]ActualObject {
	result := make(map[string][]ActualObject)
	for _, object := range actual {
		if object.Menu != string(routeros.MenuIPDNSStatic) || object.Ownership != "owned" || object.LogicalID == "" {
			continue
		}
		result[object.LogicalID] = append(result[object.LogicalID], object)
	}
	return result
}

func uniqueOwnedActual(objects []ActualObject) (ActualObject, bool) {
	if len(objects) != 1 || objects[0].RouterID == "" {
		return ActualObject{}, false
	}
	return objects[0], true
}

func desiredObjectActive(object DesiredObject) bool {
	return !strings.EqualFold(strings.TrimSpace(object.Fields["disabled"]), "yes")
}

func actualObjectActive(object ActualObject) bool {
	disabled := strings.TrimSpace(object.Fields["disabled"])
	return !strings.EqualFold(disabled, "yes") && !strings.EqualFold(disabled, "true")
}

func convergeCrossDomainDNSOrder(ctx context.Context, mutation PolicyMutation, repository Repository, desired DesiredResult, domain PolicyDomain) error {
	if len(desired.crossDomainConstraints) == 0 && domain != PolicyDomainAccess {
		return nil
	}
	_, actual, _, err := scanManagedForPlan(ctx, mutation, repository, desired.Objects, desired.crossDomainDesired, domain, desired.crossDomainConstraints)
	if err != nil {
		return err
	}
	checked := DesiredResult{}
	appendCrossDomainPrecedenceBlockers(domain, desired.Objects, desired.crossDomainDesired, desired.crossDomainConstraints, actual, &checked)
	if len(checked.Blockers) > 0 {
		return fmt.Errorf("cross-domain DNS precedence is unavailable")
	}
	operations := planCrossDomainDNSMoves(domain, desired.Objects, desired.crossDomainDesired, desired.crossDomainConstraints, actual)
	for _, operation := range operations {
		if err := applyOperation(ctx, mutation, operation, map[string]string{}); err != nil {
			return fmt.Errorf("cross-domain DNS move %s: %w", operation.LogicalID, err)
		}
	}
	// All newly created or patched objects are still staged disabled at this
	// point. The post-activation verification is the gate that requires both
	// active projections; preactivation only needs the physical move to have
	// succeeded (or to be safely deferred until the other domain exists).
	return verifyCrossDomainDNSOrder(ctx, mutation, repository, desired, domain, false)
}

func verifyCrossDomainDNSOrder(ctx context.Context, mutation PolicyMutation, repository Repository, desired DesiredResult, domain PolicyDomain, requireRoutingActive bool) error {
	if len(desired.crossDomainConstraints) == 0 && domain != PolicyDomainAccess {
		return nil
	}
	_, actual, _, err := scanManagedForPlan(ctx, mutation, repository, desired.Objects, desired.crossDomainDesired, domain, desired.crossDomainConstraints)
	if err != nil {
		return err
	}
	actualByLogicalID := ownedDNSActualByLogicalID(actual)
	desiredByLogicalID := crossDomainDesiredByLogicalID(desired.Objects, desired.crossDomainDesired)
	for _, constraint := range desired.crossDomainConstraints {
		accessActual, accessOK := uniqueOwnedActual(actualByLogicalID[constraint.AccessLogicalID])
		routingActual, routingOK := uniqueOwnedActual(actualByLogicalID[constraint.RoutingLogicalID])
		expectedAccess, expectedAccessOK := desiredByLogicalID[constraint.AccessLogicalID]
		if !routingOK {
			if requireRoutingActive {
				return fmt.Errorf("active Routing DNS static %s is missing or disabled", constraint.RoutingLogicalID)
			}
			continue
		}
		if !accessOK {
			return fmt.Errorf("Access DNS static %s is missing", constraint.AccessLogicalID)
		}
		if !expectedAccessOK || !crossDomainAccessActualMatchesDesired(accessActual, expectedAccess) {
			return fmt.Errorf("Access DNS static %s does not match the planned projection", constraint.AccessLogicalID)
		}
		if !requireRoutingActive {
			if accessActual.Position >= routingActual.Position {
				return fmt.Errorf("Access DNS static %s is not before Routing DNS static %s", constraint.AccessLogicalID, constraint.RoutingLogicalID)
			}
			continue
		}
		if !actualObjectActive(accessActual) {
			return fmt.Errorf("active Access DNS static %s is missing or disabled", constraint.AccessLogicalID)
		}
		if accessActual.Position >= routingActual.Position {
			return fmt.Errorf("Access DNS static %s is not before Routing DNS static %s", constraint.AccessLogicalID, constraint.RoutingLogicalID)
		}
	}
	if domain == PolicyDomainAccess || domain == PolicyDomainCombined {
		accessDesired := crossDomainAccessDNSDesired(append(append([]DesiredObject(nil), desired.Objects...), desired.crossDomainDesired...))
		routingActual := crossDomainRoutingDNSActuals(actual, false)
		for _, accessObject := range accessDesired {
			accessProjection, ok := parseDomainDNSProjection(accessObject)
			if !ok || !desiredObjectActive(accessObject) {
				continue
			}
			accessActual, accessOK := uniqueOwnedActual(actualByLogicalID[accessObject.LogicalID])
			for _, routingObject := range routingActual {
				routingMatcher, routingOK := actualDNSMatcher(routingObject)
				if !routingOK || len(domainProjectionOverlaps([]SourceRule{accessProjection.AccessMatcher}, []SourceRule{routingMatcher})) == 0 {
					continue
				}
				if !accessOK {
					return fmt.Errorf("Access DNS static %s is missing", accessObject.LogicalID)
				}
				if !crossDomainAccessActualMatchesDesired(accessActual, accessObject) {
					return fmt.Errorf("Access DNS static %s does not match the planned projection", accessObject.LogicalID)
				}
				if requireRoutingActive && !actualObjectActive(routingObject) {
					return fmt.Errorf("active Routing DNS static %s is missing or disabled", routingObject.LogicalID)
				}
				if requireRoutingActive && !actualObjectActive(accessActual) {
					return fmt.Errorf("active Access DNS static %s is missing or disabled", accessObject.LogicalID)
				}
				if accessActual.Position >= routingObject.Position {
					return fmt.Errorf("Access DNS static %s is not before Routing DNS static %s", accessObject.LogicalID, routingObject.LogicalID)
				}
			}
		}
	}
	return nil
}
