package policyv2

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestNormalizeRoutingSourceScopeAndTypedRuleProjection(t *testing.T) {
	valid := []struct {
		name  string
		scope *RoutingSourceScope
		want  RoutingSourceKind
	}{
		{name: "device", scope: &RoutingSourceScope{Kind: RoutingSourceDevice}, want: RoutingSourceDevice},
		{name: "ip", scope: &RoutingSourceScope{Kind: RoutingSourceIP}, want: RoutingSourceIP},
		{name: "interface", scope: &RoutingSourceScope{Kind: "INTERFACE", Name: "  wg1  "}, want: RoutingSourceInterface},
		{name: "interface-list", scope: &RoutingSourceScope{Kind: RoutingSourceInterfaceList, Name: "LAN"}, want: RoutingSourceInterfaceList},
		{name: "all", scope: &RoutingSourceScope{Kind: RoutingSourceAll}, want: RoutingSourceAll},
	}
	for _, test := range valid {
		t.Run(test.name, func(t *testing.T) {
			normalized, err := NormalizeRoutingSourceScope(test.scope)
			if err != nil || normalized == nil || normalized.Kind != test.want {
				t.Fatalf("normalized=%#v err=%v, want kind %q", normalized, err, test.want)
			}
		})
	}
	for _, scope := range []*RoutingSourceScope{
		{Kind: RoutingSourceDevice, Name: "wg1"},
		{Kind: RoutingSourceInterface},
		{Kind: RoutingSourceAll, Name: "unexpected"},
		{Kind: "unknown"},
	} {
		if _, err := NormalizeRoutingSourceScope(scope); !errors.Is(err, ErrRoutingSourceScopeInvalid) {
			t.Fatalf("scope %#v error=%v, want invalid source scope", scope, err)
		}
	}

	base := RoutingRule{ID: "rule", Name: "Rule", EgressID: "egress", TargetListIDs: []string{"target"}, Enabled: true}
	interfaceRule, err := NormalizeRoutingRule(RoutingRule{ID: base.ID, Name: base.Name, EgressID: base.EgressID, TargetListIDs: base.TargetListIDs, Enabled: true, SourceScope: &RoutingSourceScope{Kind: RoutingSourceInterface, Name: "wg1"}})
	if err != nil {
		t.Fatal(err)
	}
	if interfaceRule.Subject.Mode != SubjectModeAll || len(interfaceRule.Ingress.Interfaces) != 1 || interfaceRule.Ingress.Interfaces[0] != "wg1" {
		t.Fatalf("interface projection=%#v, want all + wg1", interfaceRule)
	}

	allRule, err := NormalizeRoutingRule(RoutingRule{ID: base.ID, Name: base.Name, EgressID: base.EgressID, TargetListIDs: base.TargetListIDs, Enabled: true, SourceScope: &RoutingSourceScope{Kind: RoutingSourceAll}})
	if err != nil {
		t.Fatal(err)
	}
	if allRule.Subject.Mode != SubjectModeAll || HasTrafficIngress(allRule.Ingress) {
		t.Fatalf("all projection=%#v, want an unconstrained compatibility projection", allRule)
	}

	deviceRule, err := NormalizeRoutingRule(RoutingRule{ID: base.ID, Name: base.Name, EgressID: base.EgressID, TargetListIDs: base.TargetListIDs, Enabled: true, SourceScope: &RoutingSourceScope{Kind: RoutingSourceDevice}, Subject: Subject{Mode: SubjectModeSelected, Members: []SubjectMember{{TerminalID: "terminal-a", Binding: "fixed", PinnedIPv4: []string{"10.0.0.10"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(deviceRule.Subject.Members) != 1 || HasTrafficIngress(deviceRule.Ingress) {
		t.Fatalf("device rule lost canonical Subject or retained ingress: %#v", deviceRule)
	}

	for _, rule := range []RoutingRule{
		{ID: base.ID, Name: base.Name, EgressID: base.EgressID, TargetListIDs: base.TargetListIDs, SourceScope: &RoutingSourceScope{Kind: RoutingSourceDevice}, Subject: Subject{Mode: SubjectModeSelected, Prefixes: []string{"10.0.0.0/24"}}},
		{ID: base.ID, Name: base.Name, EgressID: base.EgressID, TargetListIDs: base.TargetListIDs, SourceScope: &RoutingSourceScope{Kind: RoutingSourceIP}, Subject: Subject{Mode: SubjectModeSelected, Members: []SubjectMember{{TerminalID: "terminal-a", Binding: "fixed", PinnedIPv4: []string{"10.0.0.10"}}}}},
	} {
		if _, err := NormalizeRoutingRule(rule); !errors.Is(err, ErrRoutingSourceScopeInvalid) {
			t.Fatalf("invalid typed rule=%#v err=%v, want invalid source scope", rule, err)
		}
	}
}

func TestCanonicalRoutingSourceRejectsChangedLegacyProjection(t *testing.T) {
	canonical := RoutingRule{
		ID: "rule", SourceScope: &RoutingSourceScope{Kind: RoutingSourceInterface, Name: "wg1"},
		Subject: Subject{Mode: SubjectModeAll}, Ingress: TrafficIngressScope{Interfaces: []string{"wg1"}},
	}
	if matches, err := LegacyRoutingSourceProjectionMatches(canonical, RoutingRule{}); err != nil || !matches {
		t.Fatalf("omitted legacy projection matches=%v err=%v, want match", matches, err)
	}
	unchanged := canonical
	unchanged.SourceScope = nil
	unchanged.Name = "renamed only"
	if matches, err := LegacyRoutingSourceProjectionMatches(canonical, unchanged); err != nil || !matches {
		t.Fatalf("equivalent legacy projection matches=%v err=%v, want match", matches, err)
	}
	changed := unchanged
	changed.Ingress = TrafficIngressScope{Interfaces: []string{"bridge1"}}
	if matches, err := LegacyRoutingSourceProjectionMatches(canonical, changed); err != nil || matches {
		t.Fatalf("changed legacy projection matches=%v err=%v, want mismatch", matches, err)
	}
	if _, err := PrepareRoutingRuleWrite(changed, &canonical); !errors.Is(err, ErrRoutingSourceScopeConflict) {
		t.Fatalf("changed legacy source error=%v, want conflict", err)
	}
}

func TestCanonicalDeviceLegacyRoundTripIgnoresServerManagedIdentity(t *testing.T) {
	canonical := RoutingRule{
		ID: "device-rule", Name: "Device", EgressID: "egress", TargetListIDs: []string{"target"}, Enabled: true, Priority: 10,
		SourceScope: &RoutingSourceScope{Kind: RoutingSourceDevice},
		Subject: Subject{Mode: SubjectModeSelected, Members: []SubjectMember{{
			TerminalID: "terminal-a", Binding: "auto", AnchorMAC: "AA:BB:CC:DD:EE:FF", LastIPv4: []string{"10.0.0.10"}, LastIPv6: []string{"2001:db8::10"},
		}}},
	}

	payload, err := json.Marshal(canonical)
	if err != nil {
		t.Fatal(err)
	}
	var legacy RoutingRule
	if err := json.Unmarshal(payload, &legacy); err != nil {
		t.Fatal(err)
	}
	legacy.SourceScope = nil
	legacy.Name = "Device renamed"
	legacy.Priority = 20

	if matches, err := LegacyRoutingSourceProjectionMatches(canonical, legacy); err != nil || !matches {
		t.Fatalf("device JSON round-trip matches=%v err=%v, want match", matches, err)
	}
	prepared, err := PrepareRoutingRuleWrite(legacy, &canonical)
	if err != nil {
		t.Fatal(err)
	}
	member := prepared.Subject.Members[0]
	if prepared.Name != "Device renamed" || member.AnchorMAC != "AA:BB:CC:DD:EE:FF" || len(member.LastIPv4) != 1 || member.LastIPv4[0] != "10.0.0.10" || len(member.LastIPv6) != 1 || member.LastIPv6[0] != "2001:db8::10" {
		t.Fatalf("legacy write lost non-source edits or hidden identity: %#v", prepared)
	}

	changedTerminal := legacy
	changedTerminal.Subject.Members = append([]SubjectMember(nil), legacy.Subject.Members...)
	changedTerminal.Subject.Members[0].TerminalID = "terminal-b"
	if _, err := PrepareRoutingRuleWrite(changedTerminal, &canonical); !errors.Is(err, ErrRoutingSourceScopeConflict) {
		t.Fatalf("changed terminal source error=%v, want conflict", err)
	}

	changedBinding := legacy
	changedBinding.Subject.Members = append([]SubjectMember(nil), legacy.Subject.Members...)
	changedBinding.Subject.Members[0].Binding = "fixed"
	changedBinding.Subject.Members[0].PinnedIPv4 = []string{"10.0.0.10"}
	if _, err := PrepareRoutingRuleWrite(changedBinding, &canonical); !errors.Is(err, ErrRoutingSourceScopeConflict) {
		t.Fatalf("changed binding source error=%v, want conflict", err)
	}

	fixedCanonical := canonical
	fixedCanonical.Subject.Members = []SubjectMember{{TerminalID: "terminal-a", Binding: "fixed", PinnedIPv4: []string{"10.0.0.10"}}}
	fixedLegacy := fixedCanonical
	fixedLegacy.SourceScope = nil
	fixedLegacy.Subject.Members = append([]SubjectMember(nil), fixedCanonical.Subject.Members...)
	fixedLegacy.Subject.Members[0].PinnedIPv4 = []string{"10.0.0.11"}
	if _, err := PrepareRoutingRuleWrite(fixedLegacy, &fixedCanonical); !errors.Is(err, ErrRoutingSourceScopeConflict) {
		t.Fatalf("changed pinned source error=%v, want conflict", err)
	}
}

func TestRoutingSourcePayloadParticipatesInProposalAndDesiredHash(t *testing.T) {
	rule := RoutingRule{
		ID: "rule", SourceScope: &RoutingSourceScope{Kind: RoutingSourceDevice},
		Subject: Subject{Mode: SubjectModeSelected, Members: []SubjectMember{{TerminalID: "terminal-a", Binding: "auto", AnchorMAC: "AA:BB:CC:DD:EE:FF", LastIPv4: []string{"10.0.0.10"}}}},
	}
	proposal := PolicyProposal{RoutingRule: &rule}
	clone := clonePolicyProposal(&proposal)
	if clone == nil || clone.RoutingRule == nil || clone.RoutingRule.SourceScope == nil || clone.RoutingRule.SourceScope.Kind != RoutingSourceDevice || clone.RoutingRule.Subject.Members[0].AnchorMAC != "AA:BB:CC:DD:EE:FF" {
		t.Fatalf("proposal clone lost typed source or identity state: %#v", clone)
	}
	first, err := ProposalHash(proposal)
	if err != nil {
		t.Fatal(err)
	}
	changedAnchor := rule
	changedAnchor.Subject.Members = append([]SubjectMember(nil), rule.Subject.Members...)
	changedAnchor.Subject.Members[0].AnchorMAC = "AA:BB:CC:DD:EE:01"
	second, err := ProposalHash(PolicyProposal{RoutingRule: &changedAnchor})
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("proposal hash ignored hidden routing identity state")
	}

	result := DesiredResult{Objects: []DesiredObject{{LogicalID: "same"}}}
	result.routingSourceHashes = routingRuleSourceHashes([]RoutingRule{rule})
	if err := hashDesiredResult(&result); err != nil {
		t.Fatal(err)
	}
	first = result.Hash
	changedKind := rule
	changedKind.SourceScope = &RoutingSourceScope{Kind: RoutingSourceIP}
	changedKind.Subject = Subject{Mode: SubjectModeSelected, Prefixes: []string{"10.0.0.10/32"}}
	result.routingSourceHashes = routingRuleSourceHashes([]RoutingRule{changedKind})
	if err := hashDesiredResult(&result); err != nil {
		t.Fatal(err)
	}
	if first == result.Hash {
		t.Fatal("desired hash ignored canonical routing source kind/payload")
	}
}
