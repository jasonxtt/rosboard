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
		{id: interfaceRule.ID, wantJSON: `{"kind":"interface","interfaces":["wg1"]}`, wantKind: policyv2.RoutingSourceInterface, wantSubject: policyv2.Subject{Mode: policyv2.SubjectModeAll, Members: []policyv2.SubjectMember{}, Prefixes: []string{}}, wantIngress: policyv2.TrafficIngressScope{InterfaceLists: []string{}, Interfaces: []string{"wg1"}}},
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
	if updated.Revision != saved.Revision+1 || updated.SourceScope == nil || len(updated.SourceScope.Interfaces) != 1 || updated.SourceScope.Interfaces[0] != "wg1" {
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
	if current.Revision != updated.Revision || current.SourceScope == nil || len(current.SourceScope.Interfaces) != 1 || current.SourceScope.Interfaces[0] != "wg1" {
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

func TestMigrateRoutingRuleSourceScopeUpgradesLegacyRows(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	repository := storage.PolicyRepository()
	ctx := context.Background()
	if _, err := repository.SaveEgress(ctx, policyv2.Egress{ID: "wan-migrate", Name: "WAN migrate"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.SaveTargetList(ctx, policyv2.TargetList{ID: "target-migrate", Name: "Target migrate", Kind: policyv2.KindIP, SourceType: policyv2.TargetSourceTypeManual, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	// Seed the authority marker so the typed-source migration runs on the next
	// migration pass, then insert legacy rows directly the way a pre-typed
	// database stored them.
	if _, err := storage.db.Exec(`INSERT INTO policy_v2_schema_meta (key, value) VALUES (?, ?)`, policyv2.RoutingRuleAuthorityKey, policyv2.RoutingRuleAuthorityV1); err != nil {
		t.Fatal(err)
	}
	insertRule := func(id, subjectMode string, sourceScope any, ingressLists, ingressInterfaces string) {
		t.Helper()
		if _, err := storage.db.Exec(`INSERT INTO policy_v2_routing_rules (id, name, egress_id, subject_mode, source_scope_json, ingress_interface_lists_json, ingress_interfaces_json, priority, enabled, revision, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, 0, 1, 1, 1, 1)`,
			id, id, "wan-migrate", subjectMode, sourceScope, ingressLists, ingressInterfaces); err != nil {
			t.Fatal(err)
		}
		if _, err := storage.db.Exec(`INSERT INTO policy_v2_routing_rule_targets (rule_id, target_id, position) VALUES (?, ?, 0)`, id, "target-migrate"); err != nil {
			t.Fatal(err)
		}
	}
	insertRule("legacy-all", policyv2.SubjectModeAll, nil, `["LAN"]`, `["bridge-lan"]`)
	insertRule("legacy-excluded", policyv2.SubjectModeExcluded, nil, `["LAN"]`, `[]`)
	insertRule("legacy-device", policyv2.SubjectModeSelected, nil, `[]`, `[]`)
	insertRule("legacy-ip", policyv2.SubjectModeSelected, nil, `[]`, `[]`)
	insertRule("legacy-mixed", policyv2.SubjectModeSelected, nil, `[]`, `[]`)
	insertRule("legacy-no-ingress", policyv2.SubjectModeAll, nil, `[]`, `[]`)
	if _, err := storage.db.Exec(`INSERT INTO policy_v2_routing_rule_prefixes (rule_id, prefix, position) VALUES ('legacy-excluded', '10.0.0.0/24', 0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.db.Exec(`INSERT INTO policy_v2_routing_rule_members (rule_id, terminal_id, binding, anchor_mac, pinned_ipv4_json, pinned_ipv6_json, last_ipv4_json, last_ipv6_json) VALUES ('legacy-device', 'terminal-a', 'auto', '', '[]', '[]', '[]', '[]')`); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.db.Exec(`INSERT INTO policy_v2_routing_rule_prefixes (rule_id, prefix, position) VALUES ('legacy-ip', '192.0.2.0/24', 0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.db.Exec(`INSERT INTO policy_v2_routing_rule_members (rule_id, terminal_id, binding, anchor_mac, pinned_ipv4_json, pinned_ipv6_json, last_ipv4_json, last_ipv6_json) VALUES ('legacy-mixed', 'terminal-a', 'auto', '', '[]', '[]', '[]', '[]')`); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.db.Exec(`INSERT INTO policy_v2_routing_rule_prefixes (rule_id, prefix, position) VALUES ('legacy-mixed', '192.0.2.0/24', 0)`); err != nil {
		t.Fatal(err)
	}

	if err := repository.EnsureRoutingRulesMigrated(ctx); err != nil {
		t.Fatal(err)
	}
	if err := repository.EnsureRoutingRulesMigrated(ctx); err != nil {
		t.Fatalf("migration replay failed: %v", err)
	}
	rules, err := repository.ListRoutingRules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	byID := make(map[string]policyv2.RoutingRule, len(rules))
	for _, rule := range rules {
		byID[rule.ID] = rule
	}

	all := byID["legacy-all"]
	if all.SourceScope == nil || all.SourceScope.Kind != policyv2.RoutingSourceInterface || !reflect.DeepEqual(all.SourceScope.InterfaceLists, []string{"LAN"}) || !reflect.DeepEqual(all.SourceScope.Interfaces, []string{"bridge-lan"}) {
		t.Fatalf("legacy all+ingress was not upgraded to an interface source: %#v", all.SourceScope)
	}
	excluded := byID["legacy-excluded"]
	if excluded.SourceScope == nil || excluded.SourceScope.Kind != policyv2.RoutingSourceInterface || !reflect.DeepEqual(excluded.SourceScope.ExcludePrefixes, []string{"10.0.0.0/24"}) {
		t.Fatalf("legacy excluded prefixes were not upgraded to interface exclusions: %#v", excluded.SourceScope)
	}
	if excluded.Subject.Mode != policyv2.SubjectModeExcluded || len(excluded.Subject.Prefixes) != 1 {
		t.Fatalf("legacy excluded rule lost its compatibility projection: %#v", excluded.Subject)
	}
	device := byID["legacy-device"]
	if device.SourceScope == nil || device.SourceScope.Kind != policyv2.RoutingSourceDevice || len(device.Subject.Members) != 1 {
		t.Fatalf("legacy selected terminals were not upgraded to a device source: %#v", device.SourceScope)
	}
	ip := byID["legacy-ip"]
	if ip.SourceScope == nil || ip.SourceScope.Kind != policyv2.RoutingSourceIP || len(ip.Subject.Prefixes) != 1 {
		t.Fatalf("legacy selected prefixes were not upgraded to an ip source: %#v", ip.SourceScope)
	}
	if byID["legacy-mixed"].SourceScope != nil {
		t.Fatalf("mixed legacy rule must keep its legacy source representation: %#v", byID["legacy-mixed"].SourceScope)
	}
	if byID["legacy-no-ingress"].SourceScope != nil {
		t.Fatalf("ingress-less legacy all rule must keep its legacy source representation: %#v", byID["legacy-no-ingress"].SourceScope)
	}
}
