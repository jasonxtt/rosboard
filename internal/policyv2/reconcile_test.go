package policyv2

import (
	"testing"

	"rosboard/internal/routeros"
)

func TestEquivalentRouterFieldHandlesRouterOSOmittedBooleans(t *testing.T) {
	for _, test := range []struct {
		key, actual, desired string
		want                 bool
	}{
		{key: "fib", actual: "", desired: "yes", want: true},
		{key: "blackhole", actual: "", desired: "yes", want: true},
		{key: "match-subdomain", actual: "", desired: "no", want: true},
		{key: "disabled", actual: "", desired: "no", want: true},
		{key: "match-subdomain", actual: "", desired: "yes", want: false},
	} {
		if got := equivalentRouterField(test.key, test.actual, test.desired); got != test.want {
			t.Fatalf("%s actual=%q desired=%q got=%v want=%v", test.key, test.actual, test.desired, got, test.want)
		}
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
	prefix := managedCommentPrefix("manager", "edge")
	foreign := routeros.RouterOSObject{"chain": "srcnat", "action": "masquerade"}
	if !isForeignMasquerade(routeros.MenuIPFirewallNAT, foreign, prefix, "") {
		t.Fatal("uncommented masquerade should remain outside policy ownership")
	}
	owned := routeros.RouterOSObject{"chain": "srcnat", "action": "masquerade"}
	ownedComment := managedComment(prefix, "masquerade:wan-a:ipv4")
	if isForeignMasquerade(routeros.MenuIPFirewallNAT, owned, prefix, ownedComment) {
		t.Fatal("legacy policy masquerade should remain discoverable for cleanup")
	}
	nonMasquerade := routeros.RouterOSObject{"chain": "srcnat", "action": "src-nat"}
	if isForeignMasquerade(routeros.MenuIPFirewallNAT, nonMasquerade, prefix, "") {
		t.Fatal("non-masquerade NAT rule should not be classified as foreign masquerade")
	}
}
