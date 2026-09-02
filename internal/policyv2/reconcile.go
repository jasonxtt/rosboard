package policyv2

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/netip"
	"sort"
	"strings"

	"rosboard/internal/accesscontrol"
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
	routeros.MenuIPFirewallAddressList,
	routeros.MenuIPv6FirewallAddressList,
	routeros.MenuIPFirewallFilter,
	routeros.MenuIPv6FirewallFilter,
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
	legacyPrefix := legacyManagedCommentPrefix(managerID, repository.DeviceID())
	tablePrefix := ManagedTablePrefix(managerID, repository.DeviceID())
	logicalByComment := make(map[string]string, len(desired))
	logicalByTable := make(map[string]string)
	for _, object := range desired {
		if object.Menu == string(routeros.MenuRoutingTable) {
			logicalByTable[object.Fields["name"]] = object.LogicalID
		} else {
			commentIdentity := managedCommentIdentity(object.Fields["comment"])
			logicalByComment[commentIdentity] = object.LogicalID
			logicalByComment[accesscontrol.LegacyV1ManagedComment(managerID, repository.DeviceID(), object.LogicalID)] = object.LogicalID
			logicalByComment[accesscontrol.LegacyManagedComment(managerID, repository.DeviceID(), object.LogicalID)] = object.LogicalID
			logicalByComment[legacyManagedCommentPrefixV1(managerID, repository.DeviceID())+shortHash(object.LogicalID, 8)] = object.LogicalID
			// Also accept the pre-shortening identity so existing RouterOS
			// entries migrate by patching only the comment field.
			logicalByComment[legacyPrefix+shortHash(object.LogicalID, 16)] = object.LogicalID
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
			logicalID := ""
			if menu != routeros.MenuRoutingTable {
				logicalID = logicalByComment[commentIdentity]
				if logicalID == "" && accesscontrol.IsLegacyManagedComment(comment) && !accesscontrol.IsManagedCommentFor(managerID, repository.DeviceID(), comment) {
					fields := make(map[string]string, len(object))
					for key, value := range object {
						if structuralRouterField(key) {
							fields[key] = value
						}
					}
					result = append(result, ActualObject{
						LogicalID: "foreign-access:" + commentIdentity, Menu: string(menu), RouterID: object.ID(), Position: position,
						Fields: fields, Ownership: "foreign",
					})
					continue
				}
			}
			if isForeignMasquerade(menu, object, commentIdentity) {
				continue
			}
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
				logicalID = logicalByComment[commentIdentity]
				accessNamespace := hasAccessNamespaceFields(menu, object)
				if logicalID == "" && isManagedCommentFor(managerID, repository.DeviceID(), comment) {
					logicalID = "stale:" + commentIdentity
				}
				if logicalID == "" && accesscontrol.IsManagedCommentFor(managerID, repository.DeviceID(), comment) {
					logicalID = "stale:" + commentIdentity
				}
				if logicalID == "" && accessNamespace {
					logicalID = "stale-access:" + string(menu) + ":" + object.ID()
				}
				if logicalID == "" && !isManagedCommentFor(managerID, repository.DeviceID(), comment) && !accesscontrol.IsManagedCommentFor(managerID, repository.DeviceID(), comment) {
					continue
				}
				if logicalID == "" {
					logicalID = "stale:" + commentIdentity
				}
			}
			fields := make(map[string]string, len(object))
			for key, value := range object {
				if structuralRouterField(key) {
					fields[key] = value
				}
			}
			result = append(result, ActualObject{LogicalID: logicalID, Menu: string(menu), RouterID: object.ID(), Position: position, Fields: fields, Ownership: "owned"})
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

// ScanManagedForDomain keeps the shared RouterOS scanner but limits the
// returned actual graph to one ownership domain. This is important for an
// access plan: a routing object may be stale without becoming an access
// cleanup operation.
func ScanManagedForDomain(ctx context.Context, mutation PolicyMutation, repository Repository, desired []DesiredObject, domain PolicyDomain) ([]ActualObject, string, error) {
	actual, fingerprint, err := ScanManaged(ctx, mutation, repository, desired)
	if err != nil {
		return nil, "", err
	}
	if domain == PolicyDomainCombined {
		return actual, fingerprint, nil
	}
	filtered := make([]ActualObject, 0, len(actual))
	for _, object := range actual {
		if managedActualDomain(object) == domain {
			filtered = append(filtered, object)
		}
	}
	payload, err := json.Marshal(filtered)
	if err != nil {
		return nil, "", err
	}
	digest := sha256.Sum256(payload)
	return filtered, hex.EncodeToString(digest[:]), nil
}

func managedActualDomain(object ActualObject) PolicyDomain {
	if object.Ownership == "foreign" || strings.HasPrefix(object.LogicalID, "foreign-access:") {
		return PolicyDomainAccess
	}
	fields := object.Fields
	if strings.HasPrefix(strings.TrimSpace(fields["list"]), "rb_ac_") ||
		strings.HasPrefix(strings.TrimSpace(fields["address-list"]), "rb_ac_") ||
		strings.HasPrefix(strings.TrimSpace(fields["src-address-list"]), "rb_ac_") ||
		strings.HasPrefix(strings.TrimSpace(fields["dst-address-list"]), "rb_ac_") ||
		strings.HasPrefix(strings.TrimSpace(fields["list"]), applicationListPrefix) ||
		strings.HasPrefix(strings.TrimSpace(fields["address-list"]), applicationListPrefix) ||
		strings.HasPrefix(strings.TrimSpace(fields["forward-to"]), "rosboard_access_") ||
		strings.HasPrefix(strings.TrimSpace(fields["name"]), "rosboard_access_") ||
		hasAccessNamespaceValue(fields["name"]) ||
		hasAccessNamespaceValue(fields["list"]) ||
		hasAccessNamespaceValue(fields["chain"]) ||
		hasAccessNamespaceValue(fields["jump-target"]) ||
		hasAccessNamespaceValue(fields["interface-list"]) ||
		hasAccessNamespaceValue(fields["interface-list-name"]) ||
		hasAccessNamespaceValue(fields["list-name"]) ||
		strings.Contains(fields["comment"], "访问控制") ||
		isAccessFilterFields(object.Menu, fields) {
		return PolicyDomainAccess
	}
	return PolicyDomainRouting
}

func hasAccessNamespaceFields(menu routeros.MutationMenu, object routeros.RouterOSObject) bool {
	if menu == routeros.MenuRoutingTable {
		return false
	}
	for _, key := range []string{"name", "list", "chain", "jump-target", "interface-list", "interface-list-name", "list-name", "address-list", "src-address-list", "dst-address-list", "forward-to"} {
		if hasAccessNamespaceValue(object[key]) {
			return true
		}
	}
	if strings.Contains(object["comment"], "访问控制") || isAccessFilterFields(string(menu), map[string]string(object)) {
		return true
	}
	return false
}

func hasAccessNamespaceValue(value string) bool {
	value = strings.TrimSpace(value)
	return strings.HasPrefix(value, "rbac_") || strings.HasPrefix(value, "rb_ac_") || strings.HasPrefix(value, "rosboard_access_")
}

func DiffDesired(desired []DesiredObject, actual []ActualObject) ([]PlanOperation, []PlanIssue) {
	orderedDesired := append([]DesiredObject(nil), desired...)
	sort.SliceStable(orderedDesired, func(i, j int) bool {
		return orderedDesired[i].Order < orderedDesired[j].Order
	})
	actualByKey := make(map[string][]ActualObject)
	for _, object := range actual {
		if object.Ownership == "foreign" {
			continue
		}
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
		if object.Ownership == "foreign" {
			blockers = append(blockers, PlanIssue{Code: "foreign_access_object", Status: "blocker", LogicalID: object.LogicalID, Reason: "发现不属于当前设备实例的 AccessRule RouterOS 对象，已阻止继续同步"})
			continue
		}
		key := object.Menu + "\x00" + object.LogicalID
		if used[key] {
			continue
		}
		operations = append(operations, PlanOperation{Phase: "cleanup", Action: "delete", Menu: object.Menu, LogicalID: object.LogicalID, RouterID: object.RouterID, Ownership: "owned", Before: cloneStrings(object.Fields)})
	}
	operations = append(operations, desiredOrderMoves(orderedDesired, actual)...)
	sortPlanOperations(operations)
	return operations, blockers
}

func sortPlanOperations(operations []PlanOperation) {
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
		if managedRouterField(key) && shouldReconcileActualField(key, desired) {
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

func shouldReconcileActualField(key string, desired map[string]string) bool {
	if strings.ToLower(strings.TrimSpace(key)) == "disabled" {
		// RouterOS exposes disabled=false on menus whose desired model does
		// not manage that property. Treat an omitted default as unmanaged;
		// sending its empty value back is rejected by RouterOS.
		_, declared := desired["disabled"]
		return declared
	}
	return true
}

var managedRouterFields = map[string]bool{
	"action": true, "address": true, "address-list": true, "blackhole": true,
	"chain": true, "comment": true, "connection-mark": true, "connection-state": true,
	"disabled": true, "distance": true, "dns-servers": true, "dst-address": true, "dst-address-list": true,
	"dst-address-type": true, "dst-port": true, "exclude": true, "fib": true,
	"forward-to": true, "gateway": true, "in-interface": true, "in-interface-list": true,
	"include": true, "interface": true, "list": true, "match-subdomain": true,
	"name": true, "new-connection-mark": true, "new-routing-mark": true,
	"out-interface": true, "out-interface-list": true, "passthrough": true, "protocol": true, "routing-mark": true,
	"reject-with": true, "routing-table": true, "src-address-list": true, "jump-target": true,
	"table": true, "to-address": true, "to-addresses": true,
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
		if movablePolicyMenus[object.Menu] && !isAccessFilterFields(object.Menu, object.Fields) {
			actualByMenu[object.Menu] = append(actualByMenu[object.Menu], object)
		}
	}
	desiredByMenu := make(map[string][]DesiredObject)
	for _, object := range desired {
		if movablePolicyMenus[object.Menu] && !isAccessFilterFields(object.Menu, object.Fields) {
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
		desiredObjects := append([]DesiredObject(nil), desiredByMenu[menu]...)
		sort.SliceStable(desiredObjects, func(i, j int) bool {
			if desiredObjects[i].Order != desiredObjects[j].Order {
				return desiredObjects[i].Order < desiredObjects[j].Order
			}
			return desiredObjects[i].LogicalID < desiredObjects[j].LogicalID
		})
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

func isAccessJumpFields(menu string, fields map[string]string) bool {
	return isAccessFilterFields(menu, fields) && fields["action"] == "jump"
}

func isAccessFilterFields(menu string, fields map[string]string) bool {
	return (menu == string(routeros.MenuIPFirewallFilter) || menu == string(routeros.MenuIPv6FirewallFilter)) &&
		fields["chain"] == "forward" && accesscontrol.IsManagedComment(fields["comment"]) &&
		(fields["action"] == "jump" || strings.Contains(fields["comment"], "访问控制"))
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

func isForeignMasquerade(menu routeros.MutationMenu, object routeros.RouterOSObject, commentIdentity string) bool {
	if menu != routeros.MenuIPFirewallNAT && menu != routeros.MenuIPv6FirewallNAT {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(object["chain"]), "srcnat") || !strings.EqualFold(strings.TrimSpace(object["action"]), "masquerade") {
		return false
	}
	return !isManagedComment(commentIdentity)
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
	if strings.EqualFold(strings.TrimSpace(key), "address") && equivalentRouterAddress(left, right) {
		return true
	}
	return equivalentRouterValue(left, right)
}

func equivalentRouterAddress(left, right string) bool {
	leftPrefix, leftOK := routerAddressPrefix(left)
	rightPrefix, rightOK := routerAddressPrefix(right)
	return leftOK && rightOK && leftPrefix == rightPrefix
}

func routerAddressPrefix(value string) (netip.Prefix, bool) {
	value = strings.TrimSpace(value)
	if prefix, err := netip.ParsePrefix(value); err == nil {
		return prefix.Masked(), true
	}
	address, err := netip.ParseAddr(value)
	if err != nil || address.Zone() != "" {
		return netip.Prefix{}, false
	}
	return netip.PrefixFrom(address, address.BitLen()), true
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
