package policyv2

import (
	"context"
	"testing"

	"rosboard/internal/routeros"
)

type routingSourcePreflightReader struct {
	objects map[routeros.ReadMenu][]routeros.RouterOSObject
}

func (r *routingSourcePreflightReader) PolicyList(_ context.Context, menu routeros.ReadMenu, _ []string) ([]routeros.RouterOSObject, error) {
	return r.objects[menu], nil
}

type routingSourcePreflightRepository struct {
	Repository
	RoutingRuleRepository
	rules    []RoutingRule
	egresses []Egress
}

func (r *routingSourcePreflightRepository) DeviceID() string { return "device" }

func (r *routingSourcePreflightRepository) ManagerInstanceID(context.Context) (string, error) {
	return "manager", nil
}

func (r *routingSourcePreflightRepository) ListEgresses(context.Context) ([]Egress, error) {
	return r.egresses, nil
}

func (r *routingSourcePreflightRepository) EnsureRoutingRulesMigrated(context.Context) error {
	return nil
}

func (r *routingSourcePreflightRepository) RoutingAuthority(context.Context) (string, error) {
	return RoutingRuleAuthorityV1, nil
}

func (r *routingSourcePreflightRepository) ListRoutingRules(context.Context) ([]RoutingRule, error) {
	return r.rules, nil
}

func (r *routingSourcePreflightRepository) GetRoutingRule(_ context.Context, id string) (RoutingRule, error) {
	for _, rule := range r.rules {
		if rule.ID == id {
			return rule, nil
		}
	}
	return RoutingRule{}, ErrRoutingRuleNotFound
}

func (r *routingSourcePreflightRepository) SaveRoutingRule(context.Context, RoutingRule) (RoutingRule, error) {
	return RoutingRule{}, nil
}

func (r *routingSourcePreflightRepository) DeleteRoutingRule(context.Context, string, int64) error {
	return nil
}

func TestValidateRoutingSourcesUsesLiveObjectsAndWarningsOnlyForInterfaceHealth(t *testing.T) {
	repository := &routingSourcePreflightRepository{
		rules: []RoutingRule{
			{ID: "interface-rule", Name: "Interface", EgressID: "wan", TargetListIDs: []string{"target"}, Enabled: true, SourceScope: &RoutingSourceScope{Kind: RoutingSourceInterface, Name: "wg1"}, Subject: Subject{Mode: SubjectModeAll}},
			{ID: "list-rule", Name: "List", EgressID: "wan", TargetListIDs: []string{"target"}, Enabled: true, SourceScope: &RoutingSourceScope{Kind: RoutingSourceInterfaceList, Name: "LAN"}, Subject: Subject{Mode: SubjectModeAll}},
		},
		egresses: []Egress{{ID: "wan", Families: []EgressFamily{{Enabled: true, WANInterface: "pppoe-out1"}}}},
	}
	reader := &routingSourcePreflightReader{objects: map[routeros.ReadMenu][]routeros.RouterOSObject{
		routeros.ReadMenuInterface: {
			{"name": "wg1", "type": "wireguard", "running": "true"},
			{"name": "pppoe-out1", "type": "pppoe-out", "running": "true"},
		},
		routeros.ReadMenuInterfaceList: {{"name": "LAN"}},
	}}
	blockers, warnings, err := ValidateRoutingSources(context.Background(), reader, repository)
	if err != nil {
		t.Fatal(err)
	}
	if len(blockers) != 0 {
		t.Fatalf("existing live source objects unexpectedly blocked: %#v", blockers)
	}
	if len(warnings) == 0 {
		t.Fatal("expected a role warning for the WireGuard interface")
	}

	reader.objects[routeros.ReadMenuInterface] = []routeros.RouterOSObject{{"name": "pppoe-out1", "type": "pppoe-out", "running": "true"}}
	blockers, _, err = ValidateRoutingSources(context.Background(), reader, repository)
	if err != nil {
		t.Fatal(err)
	}
	if len(blockers) != 1 || blockers[0].Code != routingSourceInterfaceNotFoundCode || blockers[0].LogicalID != "interface-rule" {
		t.Fatalf("missing live interface blockers=%#v, want stable source preflight blocker", blockers)
	}
}

func TestValidateRoutingSourcesKeepsAllDeferredWithoutRouterOSRead(t *testing.T) {
	repository := &routingSourcePreflightRepository{rules: []RoutingRule{{
		ID: "all-rule", Name: "All", EgressID: "wan", TargetListIDs: []string{"target"}, Enabled: true,
		SourceScope: &RoutingSourceScope{Kind: RoutingSourceAll}, Subject: Subject{Mode: SubjectModeAll},
	}}}
	blockers, warnings, err := ValidateRoutingSources(context.Background(), nil, repository)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 || len(blockers) != 1 || blockers[0].Code != RoutingSourceAllDeferredCode {
		t.Fatalf("all source preflight=%#v warnings=%#v, want one stable deferred blocker", blockers, warnings)
	}
}
