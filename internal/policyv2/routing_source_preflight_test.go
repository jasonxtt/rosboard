package policyv2

import (
	"context"
	"errors"
	"testing"

	"rosboard/internal/routeros"
)

type routingSourcePreflightReader struct {
	objects map[routeros.ReadMenu][]routeros.RouterOSObject
	errors  map[routeros.ReadMenu]error
}

func (r *routingSourcePreflightReader) PolicyList(_ context.Context, menu routeros.ReadMenu, _ []string) ([]routeros.RouterOSObject, error) {
	if err := r.errors[menu]; err != nil {
		return nil, err
	}
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

func TestValidateRoutingSourcesReadsOnlySelectorMenusUsedByRules(t *testing.T) {
	tests := []struct {
		name       string
		rule       RoutingRule
		objects    map[routeros.ReadMenu][]routeros.RouterOSObject
		irrelevant routeros.ReadMenu
	}{
		{
			name: "interface source does not require interface-list read",
			rule: RoutingRule{
				ID: "interface-rule", Name: "Interface", EgressID: "wan", TargetListIDs: []string{"target"}, Enabled: true,
				SourceScope: &RoutingSourceScope{Kind: RoutingSourceInterface, Name: "wg1"}, Subject: Subject{Mode: SubjectModeAll},
			},
			objects: map[routeros.ReadMenu][]routeros.RouterOSObject{
				routeros.ReadMenuInterface: {{"name": "wg1", "type": "wireguard", "running": "true"}},
			},
			irrelevant: routeros.ReadMenuInterfaceList,
		},
		{
			name: "interface-list source does not require interface read",
			rule: RoutingRule{
				ID: "list-rule", Name: "List", EgressID: "wan", TargetListIDs: []string{"target"}, Enabled: true,
				SourceScope: &RoutingSourceScope{Kind: RoutingSourceInterfaceList, Name: "LAN"}, Subject: Subject{Mode: SubjectModeAll},
			},
			objects: map[routeros.ReadMenu][]routeros.RouterOSObject{
				routeros.ReadMenuInterfaceList: {{"name": "LAN"}},
			},
			irrelevant: routeros.ReadMenuInterface,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := &routingSourcePreflightRepository{rules: []RoutingRule{test.rule}}
			reader := &routingSourcePreflightReader{
				objects: test.objects,
				errors:  map[routeros.ReadMenu]error{test.irrelevant: errors.New("unrelated selector read failed")},
			}
			blockers, warnings, err := ValidateRoutingSources(context.Background(), reader, repository)
			if err != nil {
				t.Fatalf("unrelated selector read should not fail preflight: %v", err)
			}
			if len(blockers) != 0 {
				t.Fatalf("selector-only preflight blockers=%#v warnings=%#v", blockers, warnings)
			}
		})
	}
}

func TestValidateRoutingSourcesFailsClosedWhenUsedSelectorReadFails(t *testing.T) {
	rule := RoutingRule{
		ID: "interface-rule", Name: "Interface", EgressID: "wan", TargetListIDs: []string{"target"}, Enabled: true,
		SourceScope: &RoutingSourceScope{Kind: RoutingSourceInterface, Name: "wg1"}, Subject: Subject{Mode: SubjectModeAll},
	}
	repository := &routingSourcePreflightRepository{rules: []RoutingRule{rule}}
	reader := &routingSourcePreflightReader{errors: map[routeros.ReadMenu]error{
		routeros.ReadMenuInterface: errors.New("authoritative selector read failed"),
	}}
	if _, _, err := ValidateRoutingSources(context.Background(), reader, repository); err == nil {
		t.Fatal("used selector read failure must fail closed")
	}
}
