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

var managedMenus = []routeros.MutationMenu{
	routeros.MenuInterfaceList,
	routeros.MenuInterfaceListMember,
	routeros.MenuRoutingTable,
	routeros.MenuIPRoute,
	routeros.MenuIPv6Route,
	routeros.MenuRoutingRule,
	routeros.MenuIPDNSForwarders,
	routeros.MenuIPDNSStatic,
	routeros.MenuIPFirewallMangle,
	routeros.MenuIPv6FirewallMangle,
	routeros.MenuIPFirewallNAT,
	routeros.MenuIPv6FirewallNAT,
}

func ScanManaged(ctx context.Context, mutation PolicyMutation, repository Repository, desired []DesiredObject) ([]ActualObject, string, error) {
	managerID, err := repository.ManagerInstanceID(ctx)
	if err != nil {
		return nil, "", err
	}
	prefix := managedCommentPrefix(managerID, repository.DeviceID())
	tablePrefix := ManagedTablePrefix(managerID, repository.DeviceID())
	logicalByComment := make(map[string]string, len(desired))
	logicalByTable := make(map[string]string)
	for _, object := range desired {
		if object.Menu == string(routeros.MenuRoutingTable) {
			logicalByTable[object.Fields["name"]] = object.LogicalID
		} else {
			logicalByComment[managedCommentIdentity(object.Fields["comment"])] = object.LogicalID
		}
	}
	result := make([]ActualObject, 0)
	for _, menu := range managedMenus {
		objects, err := mutation.List(ctx, menu, routeros.MutationQuery{})
		if err != nil {
			return nil, "", fmt.Errorf("scan managed RouterOS menu %s: %w", menu, err)
		}
		for position, object := range objects {
			comment := strings.TrimSpace(object["comment"])
			commentIdentity := managedCommentIdentity(comment)
			if isForeignMasquerade(menu, object, prefix, commentIdentity) {
				continue
			}
			logicalID := ""
			if menu == routeros.MenuRoutingTable {
				name := strings.TrimSpace(object["name"])
				if !strings.HasPrefix(name, tablePrefix) {
					continue
				}
				logicalID = logicalByTable[name]
				if logicalID == "" {
					logicalID = "stale-table:" + name
				}
			} else {
				if !strings.HasPrefix(commentIdentity, prefix) {
					continue
				}
				logicalID = logicalByComment[commentIdentity]
				if logicalID == "" {
					logicalID = "stale:" + strings.TrimPrefix(commentIdentity, prefix)
				}
			}
			fields := make(map[string]string, len(object))
			for key, value := range object {
				if structuralRouterField(key) {
					fields[key] = value
				}
			}
			result = append(result, ActualObject{LogicalID: logicalID, Menu: string(menu), RouterID: object.ID(), Position: position, Fields: fields})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Menu == result[j].Menu {
			return result[i].Position < result[j].Position
		}
		return result[i].Menu < result[j].Menu
	})
	payload, err := json.Marshal(result)
	if err != nil {
		return nil, "", err
	}
	digest := sha256.Sum256(payload)
	return result, hex.EncodeToString(digest[:]), nil
}

func DiffDesired(desired []DesiredObject, actual []ActualObject) ([]PlanOperation, []PlanIssue) {
	orderedDesired := append([]DesiredObject(nil), desired...)
	sort.SliceStable(orderedDesired, func(i, j int) bool {
		return orderedDesired[i].Order < orderedDesired[j].Order
	})
	actualByKey := make(map[string][]ActualObject)
	for _, object := range actual {
		key := object.Menu + "\x00" + object.LogicalID
		actualByKey[key] = append(actualByKey[key], object)
	}
	operations := make([]PlanOperation, 0)
	blockers := make([]PlanIssue, 0)
	used := make(map[string]bool)
	for _, object := range orderedDesired {
		key := object.Menu + "\x00" + object.LogicalID
		matches := actualByKey[key]
		if len(matches) > 1 {
			blockers = append(blockers, PlanIssue{Code: "duplicate_managed_object", Status: "blocker", LogicalID: object.LogicalID, Reason: "同一受管身份在 RouterOS 中出现多次"})
			continue
		}
		if len(matches) == 0 {
			operations = append(operations, PlanOperation{Order: object.Order, Phase: object.Phase, Action: "create", Menu: object.Menu, LogicalID: object.LogicalID, Ownership: "owned", After: cloneStrings(object.Fields)})
			continue
		}
		used[key] = true
		before, after := changedFields(matches[0].Fields, object.Fields)
		if len(after) > 0 {
			operations = append(operations, PlanOperation{Order: object.Order, Phase: object.Phase, Action: "patch", Menu: object.Menu, LogicalID: object.LogicalID, RouterID: matches[0].RouterID, Ownership: "owned", Before: before, After: after})
		}
	}
	for _, object := range actual {
		key := object.Menu + "\x00" + object.LogicalID
		if used[key] {
			continue
		}
		operations = append(operations, PlanOperation{Phase: "cleanup", Action: "delete", Menu: object.Menu, LogicalID: object.LogicalID, RouterID: object.RouterID, Ownership: "owned", Before: cloneStrings(object.Fields)})
	}
	operations = append(operations, desiredOrderMoves(orderedDesired, actual)...)
	phaseOrder := map[string]int{"foundation": 1, "routing": 2, "dns": 3, "activation": 4, "cleanup": 5}
	actionOrder := map[string]int{"create": 1, "patch": 2, "move": 3, "delete": 4}
	sort.SliceStable(operations, func(i, j int) bool {
		if phaseOrder[operations[i].Phase] != phaseOrder[operations[j].Phase] {
			return phaseOrder[operations[i].Phase] < phaseOrder[operations[j].Phase]
		}
		if left, right := dnsDependencyOrder(operations[i]), dnsDependencyOrder(operations[j]); left != right {
			return left < right
		}
		if actionOrder[operations[i].Action] != actionOrder[operations[j].Action] {
			return actionOrder[operations[i].Action] < actionOrder[operations[j].Action]
		}
		if operations[i].Phase == "cleanup" && operations[i].Action == "delete" {
			left, right := cleanupMenuOrder(operations[i].Menu), cleanupMenuOrder(operations[j].Menu)
			if left != right {
				return left < right
			}
		}
		if operations[i].Order != operations[j].Order {
			return operations[i].Order < operations[j].Order
		}
		return operations[i].LogicalID < operations[j].LogicalID
	})
	for index := range operations {
		operations[index].Seq = index + 1
	}
	return operations, blockers
}

func dnsDependencyOrder(operation PlanOperation) int {
	if operation.Phase != "dns" || operation.Action == "delete" {
		return 0
	}
	switch operation.Menu {
	case string(routeros.MenuIPDNSForwarders):
		return 1
	case string(routeros.MenuIPDNSStatic):
		return 2
	default:
		return 3
	}
}

func cleanupMenuOrder(menu string) int {
	switch menu {
	case string(routeros.MenuInterfaceListMember):
		return 1
	case string(routeros.MenuInterfaceList):
		return 3
	default:
		return 2
	}
}

func planSummary(operations []PlanOperation, blockers, warnings []PlanIssue) PlanSummary {
	summary := PlanSummary{Blockers: len(blockers), Warnings: len(warnings)}
	for _, operation := range operations {
		switch operation.Action {
		case "create":
			summary.Create++
		case "patch":
			summary.Patch++
		case "delete":
			summary.Delete++
		case "move":
			summary.Move++
		}
	}
	return summary
}

func structuralRouterField(key string) bool {
	switch key {
	case ".id", "bytes", "packets", "dynamic", "invalid", "active", "inactive", "last-logged-packet", "creation-time":
		return false
	default:
		return true
	}
}

func changedFields(actual, desired map[string]string) (map[string]string, map[string]string) {
	before := make(map[string]string)
	after := make(map[string]string)
	keys := make(map[string]bool, len(actual)+len(desired))
	for key := range actual {
		if managedRouterField(key) {
			keys[key] = true
		}
	}
	for key := range desired {
		keys[key] = true
	}
	for key := range keys {
		want := desired[key]
		got := actual[key]
		if equivalentRouterField(key, got, want) {
			continue
		}
		before[key] = got
		after[key] = want
	}
	return before, after
}

var managedRouterFields = map[string]bool{
	"action": true, "address": true, "address-list": true, "blackhole": true,
	"chain": true, "comment": true, "connection-mark": true, "connection-state": true,
	"disabled": true, "distance": true, "dns-servers": true, "dst-address": true, "dst-address-list": true,
	"dst-address-type": true, "dst-port": true, "exclude": true, "fib": true,
	"forward-to": true, "gateway": true, "in-interface": true, "in-interface-list": true,
	"include": true, "interface": true, "list": true, "match-subdomain": true,
	"name": true, "new-connection-mark": true, "new-routing-mark": true,
	"out-interface": true, "passthrough": true, "protocol": true, "routing-mark": true,
	"routing-table": true, "table": true, "to-address": true, "to-addresses": true,
	"to-ports": true, "type": true,
}

func managedRouterField(key string) bool {
	return managedRouterFields[strings.ToLower(strings.TrimSpace(key))]
}

var movablePolicyMenus = map[string]bool{
	string(routeros.MenuIPFirewallFilter):   true,
	string(routeros.MenuIPv6FirewallFilter): true,
	string(routeros.MenuIPFirewallMangle):   true,
	string(routeros.MenuIPv6FirewallMangle): true,
}

func desiredOrderMoves(desired []DesiredObject, actual []ActualObject) []PlanOperation {
	actualByMenu := make(map[string][]ActualObject)
	for _, object := range actual {
		if movablePolicyMenus[object.Menu] {
			actualByMenu[object.Menu] = append(actualByMenu[object.Menu], object)
		}
	}
	desiredByMenu := make(map[string][]DesiredObject)
	for _, object := range desired {
		if movablePolicyMenus[object.Menu] {
			desiredByMenu[object.Menu] = append(desiredByMenu[object.Menu], object)
		}
	}

	result := make([]PlanOperation, 0)
	menus := make([]string, 0, len(desiredByMenu))
	for menu := range desiredByMenu {
		menus = append(menus, menu)
	}
	sort.Strings(menus)
	for _, menu := range menus {
		desiredObjects := desiredByMenu[menu]
		if len(desiredObjects) < 2 {
			continue
		}
		current := make([]string, 0, len(actualByMenu[menu])+len(desiredObjects))
		routerIDs := make(map[string]string, len(actualByMenu[menu]))
		duplicate := false
		for _, object := range actualByMenu[menu] {
			if _, exists := routerIDs[object.LogicalID]; exists {
				duplicate = true
			}
			current = append(current, object.LogicalID)
			routerIDs[object.LogicalID] = object.RouterID
		}
		if duplicate {
			continue
		}
		for _, object := range desiredObjects {
			if !containsString(current, object.LogicalID) {
				current = append(current, object.LogicalID)
			}
		}

		for index := 0; index < len(desiredObjects)-1 && index < len(current); index++ {
			source := desiredObjects[index]
			if current[index] == source.LogicalID {
				continue
			}
			targetLogicalID := current[index]
			if !containsString(current, source.LogicalID) || targetLogicalID == source.LogicalID {
				continue
			}
			result = append(result, PlanOperation{
				Order: source.Order, Phase: source.Phase, Action: "move", Menu: source.Menu, LogicalID: source.LogicalID,
				RouterID: routerIDs[source.LogicalID], Ownership: "owned",
				Anchor: &PlanAnchor{LogicalID: targetLogicalID, RouterID: routerIDs[targetLogicalID], Relation: "before", Menu: source.Menu},
			})
			current = moveLogicalIDBefore(current, source.LogicalID, targetLogicalID)
		}
	}
	return result
}

func moveLogicalIDBefore(values []string, source, target string) []string {
	withoutSource := make([]string, 0, len(values))
	for _, value := range values {
		if value != source {
			withoutSource = append(withoutSource, value)
		}
	}
	for index, value := range withoutSource {
		if value == target {
			result := append([]string(nil), withoutSource[:index]...)
			result = append(result, source)
			return append(result, withoutSource[index:]...)
		}
	}
	return values
}

func isForeignMasquerade(menu routeros.MutationMenu, object routeros.RouterOSObject, managedPrefix, commentIdentity string) bool {
	if menu != routeros.MenuIPFirewallNAT && menu != routeros.MenuIPv6FirewallNAT {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(object["chain"]), "srcnat") || !strings.EqualFold(strings.TrimSpace(object["action"]), "masquerade") {
		return false
	}
	return !strings.HasPrefix(commentIdentity, managedPrefix)
}

func equivalentRouterField(key, left, right string) bool {
	if strings.TrimSpace(left) == "" {
		normalizedRight := normalizeRouterValue(right)
		if normalizedRight == "no" {
			return true
		}
		if normalizedRight == "yes" && (key == "fib" || key == "blackhole") {
			return true
		}
	}
	return equivalentRouterValue(left, right)
}

func equivalentRouterValue(left, right string) bool {
	return normalizeRouterValue(left) == normalizeRouterValue(right)
}

func normalizeRouterValue(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	switch value {
	case "true", "yes", "1", "on":
		return "yes"
	case "false", "no", "0", "off":
		return "no"
	default:
		return value
	}
}

func cloneStrings(values map[string]string) map[string]string {
	result := make(map[string]string, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}
