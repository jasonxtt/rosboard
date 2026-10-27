package policyv2

import (
	"context"
	"testing"

	"rosboard/internal/routeros"
)

func TestRoutingIPTargetsMaterializeBothFamilies(t *testing.T) {
	for _, enabled := range []AddressFamily{FamilyIPv4, FamilyIPv6} {
		t.Run(string(enabled), func(t *testing.T) {
			repo := &keywordProjectionRepository{sourceRules: map[string][]SourceRule{"v": {{RuleType: "IP-CIDR", Domain: "192.0.2.0/24"}, {RuleType: "IP-CIDR6", Domain: "2001:db8::/32"}}}}
			source := Source{ID: "target", Kind: KindIP, ActiveVersionID: "v"}
			egress := Egress{ID: "wan", Enabled: true, Families: []EgressFamily{{Family: enabled, Enabled: true, Gateway: "192.0.2.1", RouteTable: "wan"}}}
			rule := RoutingRule{ID: "rule", EgressID: "wan", Enabled: true, TargetListIDs: []string{"target"}, SourceScope: &RoutingSourceScope{Kind: RoutingSourceInterface, Name: "ether1"}}
			result := DesiredResult{}
			add := func(id string, menu routeros.MutationMenu, phase, label string, fields map[string]string) {
				result.Objects = append(result.Objects, DesiredObject{LogicalID: id, Menu: string(menu), Fields: fields})
			}
			if err := buildRoutingDesired(context.Background(), &result, add, repo, nil, "manager", "device", TrafficIngressScope{}, false, []Egress{egress}, []Source{source}, []RoutingRule{rule}, nil); err != nil {
				t.Fatal(err)
			}
			found := map[string]bool{}
			for _, o := range result.Objects {
				if o.Menu == string(routeros.MenuIPFirewallAddressList) || o.Menu == string(routeros.MenuIPv6FirewallAddressList) {
					found[o.Fields["address"]] = true
				}
			}
			if !found["192.0.2.0/24"] || !found["2001:db8::/32"] {
				t.Fatalf("target members missing for enabled %s: %#v", enabled, found)
			}
		})
	}
}
