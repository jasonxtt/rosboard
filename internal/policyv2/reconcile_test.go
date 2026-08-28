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
