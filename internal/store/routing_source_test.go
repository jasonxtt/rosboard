package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"rosboard/internal/policyv2"
)

func TestRoutingSourceScopePersistenceReusesSubjectPayload(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	repository := storage.PolicyRepository()
	ctx := context.Background()
	if _, err := repository.SaveEgress(ctx, policyv2.Egress{ID: "wan-source", Name: "WAN source"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.SaveTargetList(ctx, policyv2.TargetList{ID: "target-source", Name: "Target", Kind: policyv2.KindIP, SourceType: policyv2.TargetSourceTypeManual, Enabled: true}); err != nil {
		t.Fatal(err)
	}

	device, err := repository.SaveRoutingRule(ctx, policyv2.RoutingRule{
		ID: "source-device", Name: "Device", EgressID: "wan-source", TargetListIDs: []string{"target-source"}, Enabled: true,
		SourceScope: &policyv2.RoutingSourceScope{Kind: policyv2.RoutingSourceDevice},
		Subject:     policyv2.Subject{Mode: policyv2.SubjectModeSelected, Members: []policyv2.SubjectMember{{TerminalID: "terminal-a", Binding: "auto", AnchorMAC: "AA:BB:CC:DD:EE:FF", LastIPv4: []string{"10.0.0.10"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ip, err := repository.SaveRoutingRule(ctx, policyv2.RoutingRule{
		ID: "source-ip", Name: "IP", EgressID: "wan-source", TargetListIDs: []string{"target-source"}, Enabled: true,
		SourceScope: &policyv2.RoutingSourceScope{Kind: policyv2.RoutingSourceIP},
		Subject:     policyv2.Subject{Mode: policyv2.SubjectModeSelected, Prefixes: []string{"10.0.0.0/24", "fd86::/64"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	interfaceRule, err := repository.SaveRoutingRule(ctx, policyv2.RoutingRule{
		ID: "source-interface", Name: "Interface", EgressID: "wan-source", TargetListIDs: []string{"target-source"}, Enabled: true,
		SourceScope: &policyv2.RoutingSourceScope{Kind: policyv2.RoutingSourceInterface, Name: "wg1"},
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		id          string
		wantJSON    string
		wantKind    policyv2.RoutingSourceKind
		wantSubject policyv2.Subject
		wantIngress policyv2.TrafficIngressScope
	}{
		{id: device.ID, wantJSON: `{"kind":"device"}`, wantKind: policyv2.RoutingSourceDevice, wantSubject: device.Subject, wantIngress: policyv2.TrafficIngressScope{InterfaceLists: []string{}, Interfaces: []string{}}},
		{id: ip.ID, wantJSON: `{"kind":"ip"}`, wantKind: policyv2.RoutingSourceIP, wantSubject: ip.Subject, wantIngress: policyv2.TrafficIngressScope{InterfaceLists: []string{}, Interfaces: []string{}}},
		{id: interfaceRule.ID, wantJSON: `{"kind":"interface","name":"wg1"}`, wantKind: policyv2.RoutingSourceInterface, wantSubject: policyv2.Subject{Mode: policyv2.SubjectModeAll, Members: []policyv2.SubjectMember{}, Prefixes: []string{}}, wantIngress: policyv2.TrafficIngressScope{InterfaceLists: []string{}, Interfaces: []string{"wg1"}}},
	} {
		var raw sql.NullString
		if err := storage.db.QueryRow(`SELECT source_scope_json FROM policy_v2_routing_rules WHERE id = ?`, test.id).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		if !raw.Valid || raw.String != test.wantJSON {
			t.Fatalf("%s source_scope_json=%q, want %q", test.id, raw.String, test.wantJSON)
		}
		loaded, err := repository.GetRoutingRule(ctx, test.id)
		if err != nil {
			t.Fatal(err)
		}
		if loaded.SourceScope == nil || loaded.SourceScope.Kind != test.wantKind {
			t.Fatalf("%s source scope=%#v", test.id, loaded.SourceScope)
		}
		if !reflect.DeepEqual(loaded.Subject, test.wantSubject) || !reflect.DeepEqual(loaded.Ingress, test.wantIngress) {
			t.Fatalf("%s projection changed: subject=%#v ingress=%#v", test.id, loaded.Subject, loaded.Ingress)
		}
	}
	if loaded, err := repository.GetRoutingRule(ctx, device.ID); err != nil || loaded.Subject.Members[0].AnchorMAC != "AA:BB:CC:DD:EE:FF" || !reflect.DeepEqual(loaded.Subject.Members[0].LastIPv4, []string{"10.0.0.10"}) {
		t.Fatalf("device identity state was not retained: rule=%#v err=%v", loaded, err)
	}
}

func TestCanonicalRoutingSourceOldClientCompatibilityAndConflict(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	repository := storage.PolicyRepository()
	ctx := context.Background()
	if _, err := repository.SaveEgress(ctx, policyv2.Egress{ID: "wan-compat", Name: "WAN compat"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.SaveTargetList(ctx, policyv2.TargetList{ID: "target-compat", Name: "Target", Kind: policyv2.KindIP, SourceType: policyv2.TargetSourceTypeManual, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.SaveTrafficIngress(ctx, []byte(`{"interfaceLists":["LAN"],"interfaces":[]}`)); err != nil {
		t.Fatal(err)
	}
	saved, err := repository.SaveRoutingRule(ctx, policyv2.RoutingRule{
		ID: "compat-rule", Name: "Compat", EgressID: "wan-compat", TargetListIDs: []string{"target-compat"}, Enabled: true,
		SourceScope: &policyv2.RoutingSourceScope{Kind: policyv2.RoutingSourceInterface, Name: "wg1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	beforeGlobal, err := repository.GetDeviceState(ctx)
	if err != nil {
		t.Fatal(err)
	}

	legacyEdit := saved
	legacyEdit.SourceScope = nil
	legacyEdit.Subject = policyv2.Subject{}
	legacyEdit.Ingress = policyv2.TrafficIngressScope{}
	legacyEdit.Name = "Compat renamed"
	updated, err := repository.SaveRoutingRule(ctx, legacyEdit)
	if err != nil {
		t.Fatalf("old-client non-source edit was rejected: %v", err)
	}
	if updated.Revision != saved.Revision+1 || updated.SourceScope == nil || updated.SourceScope.Name != "wg1" {
		t.Fatalf("old-client edit changed canonical source/revision unexpectedly: %#v", updated)
	}
	afterGlobal, err := repository.GetDeviceState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(beforeGlobal.TrafficIngress, afterGlobal.TrafficIngress) {
		t.Fatalf("canonical rule save rewrote global TrafficIngress: before=%s after=%s", beforeGlobal.TrafficIngress, afterGlobal.TrafficIngress)
	}

	changed := updated
	changed.SourceScope = nil
	changed.Subject = policyv2.Subject{Mode: policyv2.SubjectModeAll}
	changed.Ingress = policyv2.TrafficIngressScope{Interfaces: []string{"bridge1"}}
	if _, err := repository.SaveRoutingRule(ctx, changed); !errors.Is(err, policyv2.ErrRoutingSourceScopeConflict) {
		t.Fatalf("old-client changed source error=%v, want canonical source conflict", err)
	}
	current, err := repository.GetRoutingRule(ctx, updated.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Revision != updated.Revision || current.SourceScope == nil || current.SourceScope.Name != "wg1" {
		t.Fatalf("rejected source edit drifted persisted rule: %#v", current)
	}
}

func TestCanonicalDeviceOldClientJSONRoundTripPreservesIdentity(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	repository := storage.PolicyRepository()
	ctx := context.Background()
	if _, err := repository.SaveEgress(ctx, policyv2.Egress{ID: "wan-device-roundtrip", Name: "WAN device roundtrip"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.SaveTargetList(ctx, policyv2.TargetList{ID: "target-device-roundtrip", Name: "Target", Kind: policyv2.KindIP, SourceType: policyv2.TargetSourceTypeManual, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	saved, err := repository.SaveRoutingRule(ctx, policyv2.RoutingRule{
		ID: "device-roundtrip", Name: "Device", EgressID: "wan-device-roundtrip", TargetListIDs: []string{"target-device-roundtrip"}, Enabled: true,
		SourceScope: &policyv2.RoutingSourceScope{Kind: policyv2.RoutingSourceDevice},
		Subject: policyv2.Subject{Mode: policyv2.SubjectModeSelected, Members: []policyv2.SubjectMember{{
			TerminalID: "terminal-a", Binding: "auto", AnchorMAC: "AA:BB:CC:DD:EE:FF", LastIPv4: []string{"10.0.0.10"}, LastIPv6: []string{"2001:db8::10"},
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}

	payload, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	var legacy policyv2.RoutingRule
	if err := json.Unmarshal(payload, &legacy); err != nil {
		t.Fatal(err)
	}
	legacy.SourceScope = nil
	legacy.Name = "Device renamed"
	updated, err := repository.SaveRoutingRule(ctx, legacy)
	if err != nil {
		t.Fatalf("old-client device non-source edit was rejected: %v", err)
	}
	if updated.Revision != saved.Revision+1 || updated.Name != "Device renamed" || updated.SourceScope == nil {
		t.Fatalf("old-client device edit did not preserve canonical rule: %#v", updated)
	}
	if member := updated.Subject.Members[0]; member.AnchorMAC != "AA:BB:CC:DD:EE:FF" || !reflect.DeepEqual(member.LastIPv4, []string{"10.0.0.10"}) || !reflect.DeepEqual(member.LastIPv6, []string{"2001:db8::10"}) {
		t.Fatalf("old-client device edit lost hidden identity: %#v", member)
	}

	changed := updated
	changed.SourceScope = nil
	changed.Subject.Members = append([]policyv2.SubjectMember(nil), updated.Subject.Members...)
	changed.Subject.Members[0].TerminalID = "terminal-b"
	if _, err := repository.SaveRoutingRule(ctx, changed); !errors.Is(err, policyv2.ErrRoutingSourceScopeConflict) {
		t.Fatalf("changed device source error=%v, want canonical source conflict", err)
	}
	current, err := repository.GetRoutingRule(ctx, updated.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Revision != updated.Revision || current.Subject.Members[0].TerminalID != "terminal-a" || current.Subject.Members[0].AnchorMAC != "AA:BB:CC:DD:EE:FF" {
		t.Fatalf("rejected device source edit drifted persisted rule: %#v", current)
	}
}

func TestRoutingRuleSchemaAddsSourceScopeColumnIdempotently(t *testing.T) {
	dataDir := t.TempDir()
	first, err := Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	var count int
	if err := second.db.QueryRow(`SELECT count(*) FROM pragma_table_info('policy_v2_routing_rules') WHERE name = 'source_scope_json'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("source_scope_json columns=%d, want one", count)
	}
}

func TestPolicyProposalCommitsCanonicalRoutingSourceAtomically(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	repository := storage.PolicyRepository()
	ctx := context.Background()
	if _, err := repository.SaveEgress(ctx, policyv2.Egress{ID: "wan-proposal-source", Name: "WAN proposal source"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.SaveTargetList(ctx, policyv2.TargetList{ID: "target-proposal-source", Name: "Target", Kind: policyv2.KindIP, SourceType: policyv2.TargetSourceTypeManual, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	state, err := repository.GetDeviceState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.CommitPolicyProposal(ctx, policyv2.PolicyProposal{RoutingRule: &policyv2.RoutingRule{
		ID: "proposal-source-rule", Name: "Proposal source", EgressID: "wan-proposal-source", TargetListIDs: []string{"target-proposal-source"}, Enabled: true,
		SourceScope: &policyv2.RoutingSourceScope{Kind: policyv2.RoutingSourceInterfaceList, Name: "LAN"},
	}}, state.DesiredRevision); err != nil {
		t.Fatal(err)
	}
	loaded, err := repository.GetRoutingRule(ctx, "proposal-source-rule")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.SourceScope == nil || loaded.SourceScope.Kind != policyv2.RoutingSourceInterfaceList || loaded.SourceScope.Name != "LAN" || loaded.Subject.Mode != policyv2.SubjectModeAll || len(loaded.Ingress.InterfaceLists) != 1 || loaded.Ingress.InterfaceLists[0] != "LAN" {
		t.Fatalf("proposal lost canonical source or compatibility projection: %#v", loaded)
	}
}
