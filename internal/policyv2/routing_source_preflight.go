package policyv2

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"rosboard/internal/routeros"
)

const (
	routingSourceInterfaceNotFoundCode     = "routing_source_interface_not_found"
	routingSourceInterfaceListNotFoundCode = "routing_source_interface_list_not_found"
	routingSourcePreflightUnavailableCode  = "routing_source_preflight_unavailable"
)

// ValidateRoutingSources is the apply-time source authority for canonical
// routing rules. It reads the live RouterOS menus directly; discovery
// candidates and ingress recommendations are deliberately not consulted.
// Missing objects are blockers. Interface health and role hints are warnings
// because RouterOS may still legally match those interfaces as ingress.
func ValidateRoutingSources(ctx context.Context, reader PolicyReader, repository Repository) ([]PlanIssue, []PlanIssue, error) {
	routingRepository, ok := repository.(RoutingRuleRepository)
	if !ok {
		return nil, nil, nil
	}
	authority, err := routingRepository.RoutingAuthority(ctx)
	if err != nil {
		return nil, nil, err
	}
	if authority != RoutingRuleAuthorityV1 {
		return nil, nil, nil
	}
	rules, err := routingRepository.ListRoutingRules(ctx)
	if err != nil {
		return nil, nil, err
	}

	interfaceRefs := make([]routingSourceNameRef, 0)
	interfaceListRefs := make([]routingSourceNameRef, 0)
	blockers := make([]PlanIssue, 0)
	warnings := make([]PlanIssue, 0)
	for _, rule := range rules {
		if !rule.Enabled || rule.SourceScope == nil {
			continue
		}
		scope, normalizeErr := NormalizeRoutingSourceScope(rule.SourceScope)
		if normalizeErr != nil {
			blockers = append(blockers, routingSourcePreflightIssue(rule, "routing_source_invalid", normalizeErr.Error()))
			continue
		}
		if _, normalizeErr := NormalizeRoutingRule(rule); normalizeErr != nil {
			blockers = append(blockers, routingSourcePreflightIssue(rule, "routing_source_invalid", normalizeErr.Error()))
			continue
		}
		switch scope.Kind {
		case RoutingSourceAll:
			blockers = append(blockers, routingSourceAllDeferredIssue(rule.ID))
		case RoutingSourceInterface:
			for _, name := range scope.Interfaces {
				interfaceRefs = append(interfaceRefs, routingSourceNameRef{rule: rule, name: name})
			}
			for _, name := range scope.InterfaceLists {
				if IsDeferredRoutingSourceInterfaceListName(name) {
					blockers = append(blockers, routingSourceInterfaceListAllDeferredIssue(rule.ID))
				} else {
					interfaceListRefs = append(interfaceListRefs, routingSourceNameRef{rule: rule, name: name})
				}
			}
		case RoutingSourceInterfaceList:
			if IsDeferredRoutingSourceInterfaceListName(scope.Name) {
				blockers = append(blockers, routingSourceInterfaceListAllDeferredIssue(rule.ID))
			} else {
				interfaceListRefs = append(interfaceListRefs, routingSourceNameRef{rule: rule, name: scope.Name})
			}
		case RoutingSourceDevice, RoutingSourceIP:
			// Device and IP sources are self-contained address matchers. Their
			// identity/IP resolution is handled by the existing source compiler.
		}
	}
	if len(interfaceRefs) == 0 && len(interfaceListRefs) == 0 {
		return blockers, warnings, nil
	}
	if reader == nil {
		return nil, nil, errors.New("routing source preflight reader is unavailable")
	}

	interfaceByName := make(map[string]routeros.RouterOSObject)
	if len(interfaceRefs) > 0 {
		interfaces, err := reader.PolicyList(ctx, routeros.ReadMenuInterface, []string{"name", "type", "running", "disabled", "dynamic"})
		if err != nil {
			return nil, nil, fmt.Errorf("%s: scan RouterOS interfaces: %w", routingSourcePreflightUnavailableCode, err)
		}
		interfaceByName = make(map[string]routeros.RouterOSObject, len(interfaces))
		for _, object := range interfaces {
			if name := strings.TrimSpace(object["name"]); name != "" {
				interfaceByName[name] = object
			}
		}
	}
	interfaceListNames := make(map[string]bool)
	if len(interfaceListRefs) > 0 {
		lists, err := reader.PolicyList(ctx, routeros.ReadMenuInterfaceList, []string{"name"})
		if err != nil {
			return nil, nil, fmt.Errorf("%s: scan RouterOS interface lists: %w", routingSourcePreflightUnavailableCode, err)
		}
		interfaceListNames = make(map[string]bool, len(lists))
		for _, object := range lists {
			if name := strings.TrimSpace(object["name"]); name != "" {
				interfaceListNames[name] = true
			}
		}
	}

	wanInterfaces := make(map[string]bool)
	if len(interfaceRefs) > 0 {
		egresses, err := repository.ListEgresses(ctx)
		if err != nil {
			return nil, nil, err
		}
		for _, egress := range egresses {
			for _, family := range egress.Families {
				if family.Enabled {
					if name := strings.TrimSpace(family.WANInterface); name != "" {
						wanInterfaces[name] = true
					}
				}
			}
		}
	}

	for _, ref := range interfaceRefs {
		object, exists := interfaceByName[ref.name]
		if !exists {
			blockers = append(blockers, routingSourcePreflightIssue(ref.rule, routingSourceInterfaceNotFoundCode, "selected RouterOS interface does not exist: "+ref.name))
			continue
		}
		appendRoutingSourceInterfaceWarnings(&warnings, ref.rule, ref.name, object, wanInterfaces)
	}
	for _, ref := range interfaceListRefs {
		if !interfaceListNames[ref.name] {
			blockers = append(blockers, routingSourcePreflightIssue(ref.rule, routingSourceInterfaceListNotFoundCode, "selected RouterOS interface-list does not exist: "+ref.name))
		}
	}
	return blockers, warnings, nil
}

type routingSourceNameRef struct {
	rule RoutingRule
	name string
}

func routingSourcePreflightIssue(rule RoutingRule, code, reason string) PlanIssue {
	return PlanIssue{Code: code, Status: "blocker", LogicalID: rule.ID, EgressID: rule.EgressID, Reason: reason}
}

func appendRoutingSourceInterfaceWarnings(warnings *[]PlanIssue, rule RoutingRule, name string, object routeros.RouterOSObject, wanInterfaces map[string]bool) {
	appendWarning := func(code, reason string) {
		*warnings = append(*warnings, PlanIssue{Code: code, Status: "warning", LogicalID: rule.ID, EgressID: rule.EgressID, Reason: reason})
	}
	if routerBool(object["disabled"], false) {
		appendWarning("routing_source_interface_disabled", "selected source interface is currently disabled: "+name)
	}
	if routerBool(object["dynamic"], false) {
		appendWarning("routing_source_interface_dynamic", "selected source interface is dynamic; verify that direct ingress matching is intentional: "+name)
	}
	if !routerBool(object["running"], true) {
		appendWarning("routing_source_interface_not_running", "selected source interface is not currently running: "+name)
	}
	if wanInterfaces[name] {
		appendWarning("routing_source_interface_wan", "selected source interface is also configured as a WAN egress; verify that matching traffic entering from it is intentional: "+name)
	}
	if hint := routingSourceInterfaceRoleHint(object["type"]); hint != "" {
		appendWarning("routing_source_interface_role_hint", "selected source interface is identified as "+hint+"; verify that using it as an ingress matcher is intentional: "+name)
	}
}

func routingSourceInterfaceRoleHint(kind string) string {
	kind = strings.ToLower(strings.TrimSpace(kind))
	switch {
	case strings.Contains(kind, "pppoe") || strings.Contains(kind, "lte"):
		return "a WAN-like interface"
	case strings.Contains(kind, "wireguard"):
		return "a WireGuard interface"
	case strings.Contains(kind, "bridge"):
		return "a bridge interface"
	case strings.Contains(kind, "tunnel") || strings.Contains(kind, "gre") || strings.Contains(kind, "eoip") || strings.Contains(kind, "ipip"):
		return "a tunnel interface"
	case strings.Contains(kind, "vlan"):
		return "a VLAN interface"
	default:
		return ""
	}
}

func appendUniquePlanIssues(existing *[]PlanIssue, additions ...[]PlanIssue) {
	seen := make(map[string]bool, len(*existing))
	for _, issue := range *existing {
		seen[planIssueKey(issue)] = true
	}
	for _, group := range additions {
		for _, issue := range group {
			key := planIssueKey(issue)
			if seen[key] {
				continue
			}
			seen[key] = true
			*existing = append(*existing, issue)
		}
	}
}

func planIssueKey(issue PlanIssue) string {
	return strings.Join([]string{issue.Code, issue.Status, issue.Family, issue.EgressID, issue.LogicalID, issue.Reason}, "\x00")
}
