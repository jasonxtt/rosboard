package policyv2

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"rosboard/internal/routeros"
)

var reservedInterfaceLists = map[string]bool{
	"all": true, "none": true, "dynamic": true, "static": true, "wan": true,
}

type TrafficIngressScope struct {
	InterfaceLists []string `json:"interfaceLists"`
	Interfaces     []string `json:"interfaces"`
}

func ParseTrafficIngressScope(payload []byte) (TrafficIngressScope, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(payload, &raw); err != nil || raw == nil {
		return TrafficIngressScope{}, errors.New("traffic ingress must be a JSON object")
	}
	var scope TrafficIngressScope
	if value, ok := raw["interfaceLists"]; ok {
		if err := json.Unmarshal(value, &scope.InterfaceLists); err != nil {
			return TrafficIngressScope{}, errors.New("interfaceLists must be an array of strings")
		}
		if value := raw["interfaces"]; len(value) > 0 {
			if err := json.Unmarshal(value, &scope.Interfaces); err != nil {
				return TrafficIngressScope{}, errors.New("interfaces must be an array of strings")
			}
		}
	} else {
		// The first V2 frontend stored selected interface-list names under
		// `interfaces`. Treat that shape as lists during the in-place upgrade.
		if value := raw["interfaces"]; len(value) > 0 {
			if err := json.Unmarshal(value, &scope.InterfaceLists); err != nil {
				return TrafficIngressScope{}, errors.New("interfaces must be an array of strings")
			}
		} else {
			var legacy string
			for _, key := range []string{"interfaceList", "listName", "lanScope"} {
				if value := raw[key]; len(value) > 0 && json.Unmarshal(value, &legacy) == nil && strings.TrimSpace(legacy) != "" {
					scope.InterfaceLists = []string{legacy}
					break
				}
			}
		}
	}
	scope.InterfaceLists = normalizedNames(scope.InterfaceLists)
	scope.Interfaces = normalizedNames(scope.Interfaces)
	for _, name := range scope.InterfaceLists {
		if reservedInterfaceLists[strings.ToLower(name)] {
			return TrafficIngressScope{}, errors.New("built-in or WAN interface lists cannot be selected")
		}
	}
	return scope, nil
}

func MarshalTrafficIngressScope(scope TrafficIngressScope) ([]byte, error) {
	scope.InterfaceLists = normalizedNames(scope.InterfaceLists)
	scope.Interfaces = normalizedNames(scope.Interfaces)
	return json.Marshal(scope)
}

func NormalizeTrafficIngressScopeUnvalidated(scope TrafficIngressScope) TrafficIngressScope {
	return TrafficIngressScope{InterfaceLists: normalizedNames(scope.InterfaceLists), Interfaces: normalizedNames(scope.Interfaces)}
}

func HasTrafficIngress(scope TrafficIngressScope) bool {
	return len(scope.InterfaceLists) > 0 || len(scope.Interfaces) > 0
}

func TrafficIngressScopeKey(scope TrafficIngressScope) string {
	scope = NormalizeTrafficIngressScopeUnvalidated(scope)
	payload, _ := json.Marshal(scope)
	return string(payload)
}

func ManagedIngressListName(managerID, deviceID string) string {
	return ManagedTablePrefix(managerID, deviceID) + "ingress"
}

func NormalizeTrafficIngressScope(scope TrafficIngressScope, candidates []TrafficIngressCandidate) (TrafficIngressScope, error) {
	listNames := make(map[string]bool)
	interfaceNames := make(map[string]bool)
	for _, candidate := range candidates {
		if candidate.Kind == "interface-list" {
			listNames[candidate.Name] = true
		} else {
			interfaceNames[candidate.Name] = true
		}
	}
	result := TrafficIngressScope{}
	classify := func(name, preferredKind string) error {
		name = strings.TrimSpace(name)
		if name == "" {
			return nil
		}
		if preferredKind == "interface-list" && listNames[name] || preferredKind == "interface" && interfaceNames[name] {
			if preferredKind == "interface-list" {
				result.InterfaceLists = append(result.InterfaceLists, name)
			} else {
				result.Interfaces = append(result.Interfaces, name)
			}
			return nil
		}
		if listNames[name] {
			result.InterfaceLists = append(result.InterfaceLists, name)
			return nil
		}
		if interfaceNames[name] {
			result.Interfaces = append(result.Interfaces, name)
			return nil
		}
		return fmt.Errorf("traffic ingress candidate no longer exists: %s", name)
	}
	for _, name := range scope.InterfaceLists {
		if err := classify(name, "interface-list"); err != nil {
			return TrafficIngressScope{}, err
		}
	}
	for _, name := range scope.Interfaces {
		if err := classify(name, "interface"); err != nil {
			return TrafficIngressScope{}, err
		}
	}
	result.InterfaceLists = normalizedNames(result.InterfaceLists)
	result.Interfaces = normalizedNames(result.Interfaces)
	return result, nil
}

func ValidateTrafficIngress(ctx context.Context, reader PolicyReader, repository Repository) ([]PlanIssue, error) {
	scopes, err := trafficIngressScopesForValidation(ctx, repository)
	if err != nil {
		return nil, err
	}
	if len(scopes) == 0 {
		return nil, nil
	}
	lists, err := reader.PolicyList(ctx, routeros.ReadMenuInterfaceList, []string{"name"})
	if err != nil {
		return nil, fmt.Errorf("scan RouterOS interface lists: %w", err)
	}
	interfaces, err := reader.PolicyList(ctx, routeros.ReadMenuInterface, []string{"name", "disabled", "dynamic"})
	if err != nil {
		return nil, fmt.Errorf("scan RouterOS interfaces: %w", err)
	}
	existingLists := make(map[string]bool, len(lists))
	for _, object := range lists {
		existingLists[object["name"]] = true
	}
	existingInterfaces := make(map[string]routeros.RouterOSObject, len(interfaces))
	for _, object := range interfaces {
		existingInterfaces[object["name"]] = object
	}
	egresses, err := repository.ListEgresses(ctx)
	if err != nil {
		return nil, err
	}
	wanInterfaces := make(map[string]bool)
	for _, egress := range egresses {
		for _, family := range egress.Families {
			if family.Enabled && strings.TrimSpace(family.WANInterface) != "" {
				wanInterfaces[family.WANInterface] = true
			}
		}
	}
	issues := make([]PlanIssue, 0)
	for _, item := range scopes {
		for _, name := range item.Scope.InterfaceLists {
			if !existingLists[name] {
				issues = append(issues, PlanIssue{Code: "traffic_ingress_list_not_found", Status: "blocker", LogicalID: item.LogicalID, Reason: "策略流量入口列表不存在：" + name})
			}
		}
		for _, name := range item.Scope.Interfaces {
			object, ok := existingInterfaces[name]
			switch {
			case !ok:
				issues = append(issues, PlanIssue{Code: "traffic_ingress_interface_not_found", Status: "blocker", LogicalID: item.LogicalID, Reason: "策略流量入口接口不存在：" + name})
			case routerBool(object["disabled"], false):
				issues = append(issues, PlanIssue{Code: "traffic_ingress_interface_disabled", Status: "blocker", LogicalID: item.LogicalID, Reason: "策略流量入口接口已禁用：" + name})
			case routerBool(object["dynamic"], false):
				issues = append(issues, PlanIssue{Code: "traffic_ingress_dynamic_interface", Status: "blocker", LogicalID: item.LogicalID, Reason: "动态 VPN 接口不能直接选择，请通过专用 interface list 纳入：" + name})
			case wanInterfaces[name]:
				issues = append(issues, PlanIssue{Code: "traffic_ingress_is_wan", Status: "blocker", LogicalID: item.LogicalID, Reason: "策略出口接口不能同时作为流量入口：" + name})
			}
		}
	}
	return issues, nil
}

type trafficIngressValidationScope struct {
	LogicalID string
	Scope     TrafficIngressScope
}

func trafficIngressScopesForValidation(ctx context.Context, repository Repository) ([]trafficIngressValidationScope, error) {
	routingRepository, ok := repository.(RoutingRuleRepository)
	if !ok {
		state, err := repository.GetDeviceState(ctx)
		if err != nil {
			return nil, err
		}
		scope, err := ParseTrafficIngressScope(state.TrafficIngress)
		if err != nil {
			return nil, nil // BuildDesired reports the payload error as a blocker.
		}
		if !HasTrafficIngress(scope) {
			return nil, nil
		}
		return []trafficIngressValidationScope{{Scope: scope}}, nil
	}
	authority, err := routingRepository.RoutingAuthority(ctx)
	if err != nil {
		return nil, err
	}
	if authority != RoutingRuleAuthorityV1 {
		state, err := repository.GetDeviceState(ctx)
		if err != nil {
			return nil, err
		}
		scope, err := ParseTrafficIngressScope(state.TrafficIngress)
		if err != nil {
			return nil, nil
		}
		if !HasTrafficIngress(scope) {
			return nil, nil
		}
		return []trafficIngressValidationScope{{Scope: scope}}, nil
	}
	rules, err := routingRepository.ListRoutingRules(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]trafficIngressValidationScope, 0)
	seen := make(map[string]bool)
	for _, rule := range rules {
		if !rule.Enabled || (rule.Subject.Mode != SubjectModeAll && rule.Subject.Mode != SubjectModeExcluded) {
			continue
		}
		scope := NormalizeTrafficIngressScopeUnvalidated(rule.Ingress)
		if !HasTrafficIngress(scope) {
			continue // The desired builder reports the required-scope blocker.
		}
		key := TrafficIngressScopeKey(scope)
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, trafficIngressValidationScope{LogicalID: rule.ID, Scope: scope})
	}
	return result, nil
}

func routingRulesRequireTrafficIngress(ctx context.Context, repository Repository) (bool, error) {
	routingRepository, ok := repository.(RoutingRuleRepository)
	if !ok {
		return true, nil
	}
	authority, err := routingRepository.RoutingAuthority(ctx)
	if err != nil {
		return false, err
	}
	if authority != RoutingRuleAuthorityV1 {
		return true, nil
	}
	rules, err := routingRepository.ListRoutingRules(ctx)
	if err != nil {
		return false, err
	}
	for _, rule := range rules {
		if rule.Enabled && (rule.Subject.Mode == SubjectModeAll || rule.Subject.Mode == SubjectModeExcluded) {
			return true, nil
		}
	}
	return false, nil
}

func normalizedNames(values []string) []string {
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
