package policyv2

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"rosboard/internal/accesscontrol"
	"rosboard/internal/routeros"
)

func TestEquivalentRouterFieldHandlesRouterOSCanonicalValues(t *testing.T) {
	for _, test := range []struct {
		key, actual, desired string
		want                 bool
	}{
		{key: "fib", actual: "", desired: "yes", want: true},
		{key: "blackhole", actual: "", desired: "yes", want: true},
		{key: "match-subdomain", actual: "", desired: "no", want: true},
		{key: "disabled", actual: "", desired: "no", want: true},
		{key: "match-subdomain", actual: "", desired: "yes", want: false},
		{key: "address", actual: "139.59.210.98", desired: "139.59.210.98/32", want: true},
		{key: "address", actual: "2001:db8::1", desired: "2001:db8::1/128", want: true},
		{key: "address", actual: "192.0.2.1/24", desired: "192.0.2.0/24", want: true},
		{key: "address", actual: "192.0.2.1", desired: "2001:db8::1/128", want: false},
		{key: "name", actual: "139.59.210.98", desired: "139.59.210.98/32", want: false},
	} {
		if got := equivalentRouterField(test.key, test.actual, test.desired); got != test.want {
			t.Fatalf("%s actual=%q desired=%q got=%v want=%v", test.key, test.actual, test.desired, got, test.want)
		}
	}
}

func TestDiffDesiredTreatsHostAddressAndFullLengthCIDRAsEquivalent(t *testing.T) {
	desired := []DesiredObject{{
		LogicalID: "addr:source:IP-CIDR:139.59.210.98/32",
		Menu:      string(routeros.MenuIPFirewallAddressList),
		Fields: map[string]string{
			"list":    "manual_policy_lab",
			"address": "139.59.210.98/32",
			"comment": "rb_test | IP entry",
		},
	}}
	actual := []ActualObject{{
		LogicalID: desired[0].LogicalID,
		Menu:      desired[0].Menu,
		RouterID:  "*1",
		Fields: map[string]string{
			"list":    "manual_policy_lab",
			"address": "139.59.210.98",
			"comment": "rb_test | IP entry",
		},
	}}

	operations, blockers := DiffDesired(desired, actual)
	if len(operations) != 0 || len(blockers) != 0 {
		t.Fatalf("host address and full-length CIDR should converge: operations=%#v blockers=%#v", operations, blockers)
	}
}

func TestTrafficIngressCleanupDeletesAggregateListLast(t *testing.T) {
	actual := []ActualObject{
		{LogicalID: "traffic-ingress:list", Menu: string(routeros.MenuInterfaceList), RouterID: "*1"},
		{LogicalID: "traffic-ingress:member:wireguard1", Menu: string(routeros.MenuInterfaceListMember), RouterID: "*2"},
		{LogicalID: "lan-routing:one", Menu: string(routeros.MenuIPFirewallMangle), RouterID: "*3"},
	}
	operations, blockers := DiffDesired(nil, actual)
	if len(blockers) != 0 || len(operations) != 3 {
		t.Fatalf("unexpected cleanup diff: operations=%#v blockers=%#v", operations, blockers)
	}
	if operations[0].Menu != string(routeros.MenuInterfaceListMember) || operations[2].Menu != string(routeros.MenuInterfaceList) {
		t.Fatalf("unsafe traffic ingress cleanup order: %#v", operations)
	}
}

func TestDiffDesiredOrdersDNSForwarderBeforeStaticRules(t *testing.T) {
	forwarder := DesiredObject{
		LogicalID: "forwarder:egress",
		Menu:      string(routeros.MenuIPDNSForwarders),
		Phase:     "dns",
		Fields:    map[string]string{"name": "rosboard_egress", "dns-servers": "192.0.2.1"},
	}
	staticRule := DesiredObject{
		LogicalID: "dns:egress:source:DOMAIN-SUFFIX:example.com",
		Menu:      string(routeros.MenuIPDNSStatic),
		Phase:     "dns",
		Fields:    map[string]string{"name": "example.com", "type": "FWD", "forward-to": "rosboard_egress"},
	}

	for _, test := range []struct {
		name   string
		actual []ActualObject
	}{
		{name: "create", actual: nil},
		{name: "patch", actual: []ActualObject{
			{LogicalID: staticRule.LogicalID, Menu: staticRule.Menu, RouterID: "*1", Fields: map[string]string{"name": "example.com", "type": "FWD", "forward-to": "old_forwarder"}},
			{LogicalID: forwarder.LogicalID, Menu: forwarder.Menu, RouterID: "*2", Fields: map[string]string{"name": "rosboard_egress", "dns-servers": "192.0.2.2"}},
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			operations, blockers := DiffDesired([]DesiredObject{staticRule, forwarder}, test.actual)
			if len(blockers) != 0 || len(operations) != 2 {
				t.Fatalf("operations=%#v blockers=%#v", operations, blockers)
			}
			if operations[0].Menu != string(routeros.MenuIPDNSForwarders) {
				t.Fatalf("DNS forwarder must precede static FWD rule: %#v", operations)
			}
		})
	}
}

func TestDiffDesiredClearsRemovedManagedFields(t *testing.T) {
	desired := []DesiredObject{{
		LogicalID: "rule",
		Menu:      string(routeros.MenuIPFirewallMangle),
		Phase:     "activation",
		Order:     1,
		Fields:    map[string]string{"chain": "prerouting", "action": "mark-routing"},
	}}
	actual := []ActualObject{{
		LogicalID: "rule",
		Menu:      string(routeros.MenuIPFirewallMangle),
		RouterID:  "*1",
		Fields:    map[string]string{"chain": "prerouting", "action": "mark-routing", "in-interface": "ether1", "scope": "30"},
	}}

	operations, blockers := DiffDesired(desired, actual)
	if len(blockers) != 0 || len(operations) != 1 {
		t.Fatalf("unexpected field-clear diff: operations=%#v blockers=%#v", operations, blockers)
	}
	if operations[0].Action != "patch" || operations[0].After["in-interface"] != "" {
		t.Fatalf("removed field was not cleared: %#v", operations[0])
	}
	if _, ok := operations[0].After["scope"]; ok {
		t.Fatalf("unmanaged RouterOS field was cleared: %#v", operations[0])
	}
}

func TestDiffDesiredDoesNotClearImplicitDisabled(t *testing.T) {
	for _, test := range []struct {
		name   string
		menu   routeros.MutationMenu
		fields map[string]string
	}{
		{
			name: "interface list member",
			menu: routeros.MenuInterfaceListMember,
			fields: map[string]string{
				"list":      "rb_ingress",
				"interface": "br-container-test",
				"comment":   "rosboard:managed member",
			},
		},
		{
			name: "dns forwarder",
			menu: routeros.MenuIPDNSForwarders,
			fields: map[string]string{
				"name":        "rosboard_forwarder",
				"dns-servers": "192.0.2.1",
				"comment":     "rosboard:managed forwarder",
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			desired := []DesiredObject{{LogicalID: "object", Menu: string(test.menu), Fields: test.fields}}
			actual := []ActualObject{{
				LogicalID: "object", Menu: string(test.menu), RouterID: "*6",
				Fields: map[string]string{
					"disabled": "false",
					"comment":  test.fields["comment"],
					"name":     test.fields["name"],
				},
			}}
			for key, value := range test.fields {
				actual[0].Fields[key] = value
			}

			operations, blockers := DiffDesired(desired, actual)
			if len(blockers) != 0 || len(operations) != 0 {
				t.Fatalf("RouterOS default disabled field should not create a patch: operations=%#v blockers=%#v", operations, blockers)
			}
		})
	}
}

func TestDiffDesiredStillReconcilesDeclaredDisabled(t *testing.T) {
	desired := []DesiredObject{{
		LogicalID: "rule",
		Menu:      string(routeros.MenuIPFirewallMangle),
		Fields:    map[string]string{"comment": "rosboard:managed rule", "disabled": "yes"},
	}}
	actual := []ActualObject{{
		LogicalID: "rule",
		Menu:      string(routeros.MenuIPFirewallMangle),
		RouterID:  "*1",
		Fields:    map[string]string{"comment": "rosboard:managed rule", "disabled": "false"},
	}}

	operations, blockers := DiffDesired(desired, actual)
	if len(blockers) != 0 || len(operations) != 1 || operations[0].After["disabled"] != "yes" {
		t.Fatalf("declared disabled field was not reconciled: operations=%#v blockers=%#v", operations, blockers)
	}
}

func TestDiffDesiredMovesManagedObjectsToDesiredOrder(t *testing.T) {
	desired := []DesiredObject{
		{LogicalID: "rule-a", Menu: string(routeros.MenuIPFirewallMangle), Phase: "activation", Order: 1, Fields: map[string]string{"comment": "a"}},
		{LogicalID: "rule-b", Menu: string(routeros.MenuIPFirewallMangle), Phase: "activation", Order: 2, Fields: map[string]string{"comment": "b"}},
	}
	actual := []ActualObject{
		{LogicalID: "rule-b", Menu: string(routeros.MenuIPFirewallMangle), RouterID: "*b", Position: 0, Fields: map[string]string{"comment": "b"}},
		{LogicalID: "rule-a", Menu: string(routeros.MenuIPFirewallMangle), RouterID: "*a", Position: 1, Fields: map[string]string{"comment": "a"}},
	}

	operations, blockers := DiffDesired(desired, actual)
	if len(blockers) != 0 || len(operations) != 1 {
		t.Fatalf("unexpected order diff: operations=%#v blockers=%#v", operations, blockers)
	}
	move := operations[0]
	if move.Action != "move" || move.RouterID != "*a" || move.Anchor == nil || move.Anchor.RouterID != "*b" || move.Anchor.Relation != "before" {
		t.Fatalf("unexpected move operation: %#v", move)
	}
}

func TestForeignMasqueradeIsNotManagedButLegacyOwnedMasqueradeIs(t *testing.T) {
	foreign := routeros.RouterOSObject{"chain": "srcnat", "action": "masquerade"}
	if !isForeignMasquerade(routeros.MenuIPFirewallNAT, foreign, "") {
		t.Fatal("uncommented masquerade should remain outside policy ownership")
	}
	owned := routeros.RouterOSObject{"chain": "srcnat", "action": "masquerade"}
	ownedComment := managedComment(managedCommentIdentityPrefix, "masquerade:wan-a:ipv4")
	if isForeignMasquerade(routeros.MenuIPFirewallNAT, owned, ownedComment) {
		t.Fatal("policy masquerade should remain discoverable for cleanup")
	}
	legacyComment := legacyManagedCommentPrefix("manager", "edge") + shortHash("masquerade:wan-a:ipv4", 16)
	if isForeignMasquerade(routeros.MenuIPFirewallNAT, owned, legacyComment) {
		t.Fatal("legacy policy masquerade should remain discoverable for cleanup")
	}
	nonMasquerade := routeros.RouterOSObject{"chain": "srcnat", "action": "src-nat"}
	if isForeignMasquerade(routeros.MenuIPFirewallNAT, nonMasquerade, "") {
		t.Fatal("non-masquerade NAT rule should not be classified as foreign masquerade")
	}
}

type fakeScanRepository struct {
	Repository
	managerID string
}

func (r *fakeScanRepository) DeviceID() string { return "edge" }

func (r *fakeScanRepository) ManagerInstanceID(context.Context) (string, error) {
	return r.managerID, nil
}

type fakeScanMutation struct {
	PolicyMutation
	objects map[routeros.MutationMenu][]routeros.RouterOSObject
}

func (m *fakeScanMutation) List(_ context.Context, menu routeros.MutationMenu, _ routeros.MutationQuery) ([]routeros.RouterOSObject, error) {
	return m.objects[menu], nil
}

func TestScanManagedMigratesLegacyCommentIdentityByPatch(t *testing.T) {
	desired := []DesiredObject{{
		LogicalID: "route:wan-a:ipv4",
		Menu:      string(routeros.MenuIPRoute),
		Phase:     "routing",
		Order:     1,
		Fields:    map[string]string{"comment": managedComment(managedCommentIdentityPrefix, "route:wan-a:ipv4", "策略 主线 · 默认路由"), "gateway": "192.0.2.1"},
	}}
	legacyComment := legacyManagedCommentPrefix("manager", "edge") + shortHash("route:wan-a:ipv4", 16) + " | 策略 主线 · 默认路由"
	mutation := &fakeScanMutation{objects: map[routeros.MutationMenu][]routeros.RouterOSObject{
		routeros.MenuIPRoute: {{"gateway": "192.0.2.1", "comment": legacyComment}},
	}}
	repository := &fakeScanRepository{managerID: "manager"}

	actual, _, err := ScanManaged(context.Background(), mutation, repository, desired)
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}
	if len(actual) != 1 || actual[0].LogicalID != "route:wan-a:ipv4" {
		t.Fatalf("legacy comment identity was not recognized: %#v", actual)
	}

	operations, blockers := DiffDesired(desired, actual)
	if len(blockers) != 0 || len(operations) != 1 {
		t.Fatalf("legacy identity should patch in place: operations=%#v blockers=%#v", operations, blockers)
	}
	if operations[0].Action != "patch" || len(operations[0].After) != 1 || operations[0].After["comment"] != desired[0].Fields["comment"] {
		t.Fatalf("migration must only rewrite the comment field: %#v", operations[0])
	}
}

func TestScanManagedCleansUnrecognizedLegacyAndSkipsForeignComments(t *testing.T) {
	desired := []DesiredObject{{
		LogicalID: "rule",
		Menu:      string(routeros.MenuIPFirewallMangle),
		Fields:    map[string]string{"comment": managedComment(managedCommentIdentityPrefix, "rule", "策略 主线"), "chain": "prerouting"},
	}}
	currentLegacyComment := legacyManagedCommentPrefix("manager", "edge") + "aaaaaaaaaaaaaaaa"
	mutation := &fakeScanMutation{objects: map[routeros.MutationMenu][]routeros.RouterOSObject{
		routeros.MenuIPFirewallMangle: {
			{"chain": "prerouting", "comment": currentLegacyComment},
			{"chain": "prerouting", "comment": "rb_ffffffff"},
			{"chain": "prerouting", "comment": "rb_custom"},
			{"chain": "prerouting", "comment": "我的自定义规则"},
		},
	}}
	repository := &fakeScanRepository{managerID: "manager"}

	actual, _, err := ScanManaged(context.Background(), mutation, repository, desired)
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}
	if len(actual) != 2 {
		t.Fatalf("expected two recognized stale objects and two skipped foreign objects: %#v", actual)
	}
	operations, blockers := DiffDesired(desired, actual)
	if len(blockers) != 0 {
		t.Fatalf("unexpected blockers: %#v", blockers)
	}
	creates, deletes := 0, 0
	for _, operation := range operations {
		switch operation.Action {
		case "create":
			creates++
		case "delete":
			deletes++
			if operation.Ownership != "owned" {
				t.Fatalf("stale object must be deleted as owned: %#v", operation)
			}
		}
	}
	if creates != 1 || deletes != 2 {
		t.Fatalf("expected one create and two stale deletes: operations=%#v", operations)
	}
}

func TestScanManagedClassifiesStaleAccessNamespacesForAccessOnlyCleanup(t *testing.T) {
	mutation := &fakeScanMutation{objects: map[routeros.MutationMenu][]routeros.RouterOSObject{
		routeros.MenuInterfaceList: {
			{".id": "*list", "name": "rbac_internet_123"},
		},
		routeros.MenuInterfaceListMember: {
			{".id": "*member", "list": "rbac_internet_123", "interface": "wan1"},
		},
		routeros.MenuIPDNSForwarders: {
			{".id": "*forwarder", "name": "rosboard_access_123", "dns-servers": "192.0.2.53"},
		},
		routeros.MenuIPFirewallFilter: {
			{".id": "*filter", "chain": "forward", "action": "jump", "jump-target": "rbac_rule_123", "src-address-list": "rbac_rule_123"},
		},
	}}
	repository := &fakeScanRepository{managerID: "manager"}

	access, _, err := ScanManagedForDomain(context.Background(), mutation, repository, nil, PolicyDomainAccess)
	if err != nil {
		t.Fatalf("access scan failed: %v", err)
	}
	if len(access) != 4 {
		t.Fatalf("expected all stale access namespace objects in the access scan: %#v", access)
	}
	for _, object := range access {
		if object.Ownership != "owned" || !strings.HasPrefix(object.LogicalID, "stale-access:") {
			t.Fatalf("stale access object was not classified as owned access: %#v", object)
		}
	}

	routing, _, err := ScanManagedForDomain(context.Background(), mutation, repository, nil, PolicyDomainRouting)
	if err != nil {
		t.Fatalf("routing scan failed: %v", err)
	}
	if len(routing) != 0 {
		t.Fatalf("routing scan must not include stale access objects: %#v", routing)
	}
}

func TestScanManagedBlocksForeignAccessNamespaces(t *testing.T) {
	foreignAccess := accesscontrol.LegacyV1ManagedComment("other-manager", "edge", "access:rule-a:ipv4:jump-out")
	currentAccess := accesscontrol.LegacyV1ManagedComment("manager", "other-device", "access:rule-b:ipv4:jump-out")
	migrationLike := "ra_12345678"
	migrationLikeV2 := "ra_v1_123456789abc_abcdefabcdef_1234567890abcdef"
	mutation := &fakeScanMutation{objects: map[routeros.MutationMenu][]routeros.RouterOSObject{
		routeros.MenuIPFirewallFilter: {
			{"chain": "forward", "action": "jump", "comment": foreignAccess},
			{"chain": "forward", "action": "jump", "comment": currentAccess},
			{"chain": "forward", "action": "jump", "comment": migrationLike},
			{"chain": "forward", "action": "jump", "comment": migrationLikeV2},
		},
	}}
	repository := &fakeScanRepository{managerID: "manager"}

	actual, _, err := ScanManaged(context.Background(), mutation, repository, nil)
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}
	if len(actual) != 4 {
		t.Fatalf("foreign or unowned access comments must be surfaced for a safety blocker: %#v", actual)
	}
	for _, object := range actual {
		if object.Ownership != "foreign" {
			t.Fatalf("foreign access object lost its ownership marker: %#v", object)
		}
	}
	operations, blockers := DiffDesired(nil, actual)
	if len(operations) != 0 || len(blockers) != 4 {
		t.Fatalf("foreign access objects must block without becoming deletable: operations=%#v blockers=%#v", operations, blockers)
	}
}

func TestManagedCommentShortFormat(t *testing.T) {
	comment := managedComment(managedCommentIdentityPrefix, "traffic-ingress:list", "策略流量入口聚合列表")
	if !regexp.MustCompile(`^rb_[0-9a-f]{8} \| 策略流量入口聚合列表$`).MatchString(comment) {
		t.Fatalf("unexpected short comment format: %q", comment)
	}
	if legacy := legacyManagedCommentPrefix("manager", "edge"); !strings.HasPrefix(legacy, "rosboard:v2:") {
		t.Fatalf("unexpected legacy prefix: %q", legacy)
	}
	for _, test := range []struct {
		comment string
		want    bool
	}{
		{comment: "rb_437614df | 标签", want: true},
		{comment: "rb_437614df", want: true},
		{comment: "rosboard:v2:deadbeefcafe:1234567890ab:437614df7720f810 | 标签", want: true},
		{comment: "rb_custom", want: false},
		{comment: "rosboard:v2:deadbeefcafe:1234567890ab:not-hex", want: false},
		{comment: "lan", want: false},
		{comment: "", want: false},
	} {
		if got := isManagedComment(test.comment); got != test.want {
			t.Fatalf("isManagedComment(%q) = %v, want %v", test.comment, got, test.want)
		}
	}
}
