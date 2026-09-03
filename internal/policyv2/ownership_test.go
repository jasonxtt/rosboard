package policyv2

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"rosboard/internal/accesscontrol"
	"rosboard/internal/ownership"
	"rosboard/internal/routeros"
)

func TestAccessForwarderIdentityIsScopedByManagerAndDevice(t *testing.T) {
	logicalID := "access-forwarder"
	first := accesscontrol.ManagedComment("manager-a", "device-a", logicalID, "访问控制 DNS 转发器")
	otherManager := accesscontrol.ManagedComment("manager-b", "device-a", logicalID, "访问控制 DNS 转发器")
	otherDevice := accesscontrol.ManagedComment("manager-a", "device-b", logicalID, "访问控制 DNS 转发器")
	policyComment := managedComment("manager-a", "device-a", logicalID, "访问控制 DNS 转发器")

	if first == otherManager || first == otherDevice {
		t.Fatalf("access-forwarder identity is not isolated: first=%q otherManager=%q otherDevice=%q", first, otherManager, otherDevice)
	}
	if first != policyComment {
		t.Fatalf("policy-v2 and Access Control disagree on ownership identity: access=%q policy=%q", first, policyComment)
	}
	if !ownership.IsCanonicalFor("manager-a", "device-a", first) || ownership.IsCanonicalFor("manager-b", "device-a", first) {
		t.Fatalf("access-forwarder ownership scope was not enforced: %q", first)
	}
}

func TestScopedManagersCoexistWithoutTouchingEachOther(t *testing.T) {
	logicalID := "access-forwarder"
	desiredA := ownershipTestForwarder("manager-a", "device-a", logicalID)
	desiredB := ownershipTestForwarder("manager-b", "device-a", logicalID)
	mutation := &fakeScanMutation{objects: map[routeros.MutationMenu][]routeros.RouterOSObject{}}
	repositoryA := &fakeScanRepository{managerID: "manager-a", deviceID: "device-a"}
	repositoryB := &fakeScanRepository{managerID: "manager-b", deviceID: "device-a"}

	operations, blockers := DiffDesired([]DesiredObject{desiredA}, nil)
	if len(blockers) != 0 || len(operations) != 1 || operations[0].Action != "create" {
		t.Fatalf("manager A initial apply was not a single create: operations=%#v blockers=%#v", operations, blockers)
	}
	addOwnershipTestObject(mutation, desiredA, "*a")

	actualBEmpty, _, err := ScanManaged(context.Background(), mutation, repositoryB, nil)
	if err != nil {
		t.Fatal(err)
	}
	operations, blockers = DiffDesired(nil, actualBEmpty)
	if len(blockers) != 0 || len(operations) != 0 {
		t.Fatalf("manager B empty desired state touched A's object: operations=%#v blockers=%#v actual=%#v", operations, blockers, actualBEmpty)
	}

	actualB, _, err := ScanManaged(context.Background(), mutation, repositoryB, []DesiredObject{desiredB})
	if err != nil {
		t.Fatal(err)
	}
	operations, blockers = DiffDesired([]DesiredObject{desiredB}, actualB)
	if len(blockers) != 0 || len(operations) != 1 || operations[0].Action != "create" || operations[0].RouterID != "" {
		t.Fatalf("manager B did not create its own object without touching A: operations=%#v blockers=%#v", operations, blockers)
	}
	addOwnershipTestObject(mutation, desiredB, "*b")

	for round := 0; round < 3; round++ {
		for _, test := range []struct {
			name       string
			repository Repository
			desired    DesiredObject
			foreignID  string
		}{
			{name: "A", repository: repositoryA, desired: desiredA, foreignID: "*b"},
			{name: "B", repository: repositoryB, desired: desiredB, foreignID: "*a"},
		} {
			t.Run(test.name+"/round"+strconv.Itoa(round), func(t *testing.T) {
				actual, _, err := ScanManaged(context.Background(), mutation, test.repository, []DesiredObject{test.desired})
				if err != nil {
					t.Fatal(err)
				}
				operations, blockers := DiffDesired([]DesiredObject{test.desired}, actual)
				if len(blockers) != 0 || len(operations) != 0 {
					t.Fatalf("coexisting scoped graph was not idempotent: operations=%#v blockers=%#v actual=%#v", operations, blockers, actual)
				}
				for _, object := range actual {
					if object.RouterID == test.foreignID && object.Ownership != "foreign" {
						t.Fatalf("foreign scoped object was not isolated: %#v", object)
					}
				}
			})
		}
	}
	if got := mutation.objects[routeros.MenuIPDNSForwarders]; len(got) != 2 || got[0][".id"] != "*a" || got[1][".id"] != "*b" {
		t.Fatalf("coexisting RouterOS IDs changed: %#v", got)
	}
}

func TestScanManagedLeavesForeignPhysicalNamespaceWithoutComment(t *testing.T) {
	foreignList := ownership.Namespace("manager-a", "device-a") + "target_deadbeef"
	mutation := &fakeScanMutation{objects: map[routeros.MutationMenu][]routeros.RouterOSObject{
		routeros.MenuIPFirewallAddressList: {{".id": "*foreign", "list": foreignList, "address": "203.0.113.0/24", "disabled": "false"}},
	}}
	repository := &fakeScanRepository{managerID: "manager-b", deviceID: "device-a"}
	actual, _, err := ScanManaged(context.Background(), mutation, repository, nil)
	if err != nil {
		t.Fatal(err)
	}
	operations, blockers := DiffDesired(nil, actual)
	if len(actual) != 1 || actual[0].Ownership != "foreign" || len(operations) != 0 || len(blockers) != 0 {
		t.Fatalf("foreign physical namespace without a comment was not isolated: actual=%#v operations=%#v blockers=%#v", actual, operations, blockers)
	}
}

func TestScanManagedDoesNotInferOwnershipFromLegacyAccessNamespaces(t *testing.T) {
	tests := []struct {
		name   string
		menu   routeros.MutationMenu
		object routeros.RouterOSObject
	}{
		{name: "rb_ac", menu: routeros.MenuIPFirewallAddressList, object: routeros.RouterOSObject{".id": "*rb-ac", "list": "rb_ac_foreign", "address": "203.0.113.10"}},
		{name: "rbac", menu: routeros.MenuInterfaceList, object: routeros.RouterOSObject{".id": "*rbac", "name": "rbac_foreign"}},
		{name: "rosboard_access", menu: routeros.MenuIPDNSForwarders, object: routeros.RouterOSObject{".id": "*rosboard-access", "name": "rosboard_access_foreign", "dns-servers": "192.0.2.53"}},
		{name: "label-access-rule", menu: routeros.MenuIPFirewallFilter, object: routeros.RouterOSObject{".id": "*label-rule", "chain": "forward", "action": "drop", "comment": "我的访问规则"}},
		{name: "label-access-control", menu: routeros.MenuIPFirewallFilter, object: routeros.RouterOSObject{".id": "*label-control", "chain": "forward", "action": "drop", "comment": "访问控制测试"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mutation := &fakeScanMutation{objects: map[routeros.MutationMenu][]routeros.RouterOSObject{test.menu: {test.object}}}
			actual, _, err := ScanManaged(context.Background(), mutation, &fakeScanRepository{managerID: "manager", deviceID: "edge"}, nil)
			if err != nil {
				t.Fatal(err)
			}
			operations, blockers := DiffDesired(nil, actual)
			if len(actual) != 0 || len(operations) != 0 || len(blockers) != 0 {
				t.Fatalf("unproven legacy namespace or readable label became ownership: actual=%#v operations=%#v blockers=%#v", actual, operations, blockers)
			}
		})
	}
}

func TestScanManagedClassifiesCurrentAndForeignRBSNamespaces(t *testing.T) {
	logicalID := "access-target:target-a:IP-CIDR:203.0.113.0/24"
	currentNamespace := ownership.Namespace("manager", "edge")
	desired := DesiredObject{
		LogicalID: logicalID,
		Menu:      string(routeros.MenuIPFirewallAddressList),
		Fields: map[string]string{
			"list":    currentNamespace + "target_current",
			"address": "203.0.113.0/24",
			"comment": managedComment("manager", "edge", logicalID, "访问规则目标列表"),
		},
	}
	foreign := DesiredObject{
		LogicalID: logicalID,
		Menu:      string(routeros.MenuIPFirewallAddressList),
		Fields: map[string]string{
			"list":    ownership.Namespace("other-manager", "edge") + "target_foreign",
			"address": "203.0.113.0/24",
			"comment": managedComment("other-manager", "edge", logicalID, "访问规则目标列表"),
		},
	}
	mutation := &fakeScanMutation{objects: map[routeros.MutationMenu][]routeros.RouterOSObject{
		routeros.MenuIPFirewallAddressList: {
			{".id": "*current", "list": desired.Fields["list"], "address": desired.Fields["address"], "comment": desired.Fields["comment"]},
			{".id": "*foreign", "list": foreign.Fields["list"], "address": foreign.Fields["address"], "comment": foreign.Fields["comment"]},
		},
	}}
	actual, _, err := ScanManaged(context.Background(), mutation, &fakeScanRepository{managerID: "manager", deviceID: "edge"}, []DesiredObject{desired})
	if err != nil {
		t.Fatal(err)
	}
	if len(actual) != 2 {
		t.Fatalf("expected current and foreign scoped objects: %#v", actual)
	}
	for _, object := range actual {
		if object.RouterID == "*current" && (object.Ownership != "owned" || object.LogicalID != logicalID) {
			t.Fatalf("current scoped object was not owned: %#v", object)
		}
		if object.RouterID == "*foreign" && object.Ownership != "foreign" {
			t.Fatalf("foreign scoped object was not isolated: %#v", object)
		}
	}
	operations, blockers := DiffDesired([]DesiredObject{desired}, actual)
	if len(operations) != 0 || len(blockers) != 0 {
		t.Fatalf("current/foreign scoped graph was not non-mutating: operations=%#v blockers=%#v", operations, blockers)
	}
}

func ownershipTestForwarder(managerID, deviceID, logicalID string) DesiredObject {
	return DesiredObject{
		LogicalID: logicalID,
		Menu:      string(routeros.MenuIPDNSForwarders),
		Phase:     "dns",
		Fields: map[string]string{
			"name":        "rosboard_access_" + ownership.Scope(managerID, deviceID),
			"dns-servers": "192.0.2.53",
			"comment":     accesscontrol.ManagedComment(managerID, deviceID, logicalID, "访问控制 DNS 转发器"),
		},
	}
}

func addOwnershipTestObject(mutation *fakeScanMutation, desired DesiredObject, id string) {
	object := routeros.RouterOSObject{".id": id}
	for key, value := range desired.Fields {
		object[key] = value
	}
	mutation.objects[routeros.MutationMenu(desired.Menu)] = append(mutation.objects[routeros.MutationMenu(desired.Menu)], object)
}

func TestScanManagedDoesNotAdoptUnscopedLegacyObjects(t *testing.T) {
	logicalID := "access-target:target-a:IP-CIDR:203.0.113.0/24"
	desired := DesiredObject{
		LogicalID: logicalID,
		Menu:      string(routeros.MenuIPFirewallAddressList),
		Fields: map[string]string{
			"list":     ownership.Namespace("manager", "edge") + "target_current",
			"address":  "203.0.113.0/24",
			"disabled": "no",
			"comment":  managedComment("manager", "edge", logicalID, "访问规则目标列表 · 地址条目"),
		},
	}
	legacy := "rb_" + ownership.Object(logicalID) + " | old"
	mutation := &fakeScanMutation{objects: map[routeros.MutationMenu][]routeros.RouterOSObject{
		routeros.MenuIPFirewallAddressList: {{".id": "*legacy", "list": desired.Fields["list"], "address": "203.0.113.0/24", "disabled": "false", "comment": legacy}},
	}}
	repository := &fakeScanRepository{managerID: "manager"}
	actual, _, err := ScanManaged(context.Background(), mutation, repository, []DesiredObject{desired})
	if err != nil {
		t.Fatal(err)
	}
	if len(actual) != 1 || actual[0].Ownership != "ambiguous" || actual[0].LogicalID != logicalID {
		t.Fatalf("unscoped legacy object was not kept ambiguous: %#v", actual)
	}
	operations, blockers := DiffDesired([]DesiredObject{desired}, actual)
	if len(operations) != 0 || len(blockers) != 1 || blockers[0].Code != "ambiguous_legacy_object" {
		t.Fatalf("unscoped legacy object must block without migration: operations=%#v blockers=%#v", operations, blockers)
	}
}

func TestScanManagedMigratesCurrentScopedV1CommentByPatch(t *testing.T) {
	logicalID := "route:wan-a:ipv4"
	desired := DesiredObject{
		LogicalID: logicalID,
		Menu:      string(routeros.MenuIPRoute),
		Fields: map[string]string{
			"gateway": "192.0.2.1",
			"comment": managedComment("manager", "edge", logicalID, "策略 主线"),
		},
	}
	legacy := legacyManagedCommentPrefixV1("manager", "edge") + shortHash(logicalID, 8) + " | old"
	mutation := &fakeScanMutation{objects: map[routeros.MutationMenu][]routeros.RouterOSObject{
		routeros.MenuIPRoute: {{".id": "*legacy", "gateway": "192.0.2.1", "comment": legacy}},
	}}
	actual, _, err := ScanManaged(context.Background(), mutation, &fakeScanRepository{managerID: "manager"}, []DesiredObject{desired})
	if err != nil {
		t.Fatal(err)
	}
	operations, blockers := DiffDesired([]DesiredObject{desired}, actual)
	if len(blockers) != 0 || len(operations) != 1 || operations[0].Action != "patch" || operations[0].RouterID != "*legacy" || len(operations[0].After) != 1 || operations[0].After["comment"] != desired.Fields["comment"] {
		t.Fatalf("current scoped V1 comment must migrate in place: operations=%#v blockers=%#v actual=%#v", operations, blockers, actual)
	}
}

func TestScanManagedLeavesForeignScopedV1Untouched(t *testing.T) {
	logicalID := "route:wan-a:ipv4"
	foreignComment := legacyManagedCommentPrefixV1("other-manager", "edge") + shortHash(logicalID, 8)
	mutation := &fakeScanMutation{objects: map[routeros.MutationMenu][]routeros.RouterOSObject{
		routeros.MenuIPRoute: {{".id": "*foreign", "gateway": "192.0.2.1", "comment": foreignComment}},
	}}
	actual, _, err := ScanManaged(context.Background(), mutation, &fakeScanRepository{managerID: "manager"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	operations, blockers := DiffDesired(nil, actual)
	if len(operations) != 0 || len(blockers) != 0 {
		t.Fatalf("foreign scoped V1 object must be ignored: operations=%#v blockers=%#v actual=%#v", operations, blockers, actual)
	}
	if len(actual) != 1 || actual[0].Ownership != "foreign" || actual[0].RouterID != "*foreign" {
		t.Fatalf("foreign scoped V1 object lost its safety classification: %#v", actual)
	}
}

func TestScanManagedRejectsForeignUnscopedNamespaceEvenWhenHashMatches(t *testing.T) {
	logicalID := "access-target:target-a:IP-CIDR:203.0.113.0/24"
	desired := DesiredObject{
		LogicalID: logicalID,
		Menu:      string(routeros.MenuIPFirewallAddressList),
		Fields: map[string]string{
			"list":    "rb_ac_current",
			"address": "203.0.113.0/24",
			"comment": managedComment("manager", "edge", logicalID, "访问控制目标列表"),
		},
	}
	mutation := &fakeScanMutation{objects: map[routeros.MutationMenu][]routeros.RouterOSObject{
		routeros.MenuIPFirewallAddressList: {{".id": "*foreign", "list": "rb_ac_other", "address": "203.0.113.0/24", "comment": "rb_" + ownership.Object(logicalID)}},
	}}
	actual, _, err := ScanManaged(context.Background(), mutation, &fakeScanRepository{managerID: "manager"}, []DesiredObject{desired})
	if err != nil {
		t.Fatal(err)
	}
	operations, blockers := DiffDesired([]DesiredObject{desired}, actual)
	if len(operations) != 0 || len(blockers) != 1 || blockers[0].Code != "ambiguous_legacy_object" {
		t.Fatalf("foreign legacy namespace must fail closed without patch/delete: operations=%#v blockers=%#v actual=%#v", operations, blockers, actual)
	}
}

func TestScanManagedDoesNotBroadDeleteUnknownUnscopedLegacy(t *testing.T) {
	mutation := &fakeScanMutation{objects: map[routeros.MutationMenu][]routeros.RouterOSObject{
		routeros.MenuIPDNSStatic: {{".id": "*legacy", "name": "foreign.example", "comment": "rb_deadbeef"}},
	}}
	actual, _, err := ScanManaged(context.Background(), mutation, &fakeScanRepository{managerID: "manager"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	operations, blockers := DiffDesired(nil, actual)
	if len(operations) != 0 || len(blockers) != 0 {
		t.Fatalf("unknown unscoped legacy object was broadly acted on: operations=%#v blockers=%#v actual=%#v", operations, blockers, actual)
	}
	if len(actual) != 1 || actual[0].Ownership != "ambiguous" || !strings.HasPrefix(actual[0].LogicalID, "ambiguous-legacy:") {
		t.Fatalf("unknown legacy object was not represented conservatively: %#v", actual)
	}
}
