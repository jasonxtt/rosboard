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

func TestDeferredInterfaceListAllBlocksIngressProjection(t *testing.T) {
	result := DesiredResult{}
	lists, ready := buildRoutingIngressProjections(&result, nil, "manager", "device", TrafficIngressScope{}, false, []RoutingRule{{
		ID: "rule-all-list", Enabled: true,
		SourceScope: &RoutingSourceScope{Kind: RoutingSourceInterfaceList, Name: " ALL "},
		Subject:     Subject{Mode: SubjectModeAll},
		Ingress:     TrafficIngressScope{InterfaceLists: []string{"all"}},
	}})
	if len(result.Blockers) != 1 || result.Blockers[0].Code != RoutingSourceInterfaceListAllDeferredCode || result.Blockers[0].LogicalID != "rule-all-list" {
		t.Fatalf("deferred source blockers=%#v, want one stable blocker", result.Blockers)
	}
	if len(lists) != 0 || len(ready) != 0 {
		t.Fatalf("deferred source created executable ingress projection: lists=%#v ready=%#v", lists, ready)
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

func TestNormalizeRoutingSourceScopeInterfaceMultiSelectorAndExclusions(t *testing.T) {
	// The legacy single-name payload folds into the multi-selector form.
	folded, err := NormalizeRoutingSourceScope(&RoutingSourceScope{Kind: RoutingSourceInterface, Name: " wg1 "})
	if err != nil {
		t.Fatal(err)
	}
	if folded.Name != "" || len(folded.Interfaces) != 1 || folded.Interfaces[0] != "wg1" {
		t.Fatalf("legacy interface name was not folded into interfaces: %#v", folded)
	}

	normalized, err := NormalizeRoutingSourceScope(&RoutingSourceScope{
		Kind: RoutingSourceInterface, Interfaces: []string{" wg1 ", "bridge"}, InterfaceLists: []string{"LAN"},
		ExcludePrefixes: []string{"10.0.0.2", "10.0.0.0/24", "10.0.0.2", "fd86::/64"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(normalized.Interfaces) != 2 || normalized.Interfaces[0] != "bridge" || normalized.Interfaces[1] != "wg1" {
		t.Fatalf("interfaces were not normalized: %#v", normalized.Interfaces)
	}
	if len(normalized.InterfaceLists) != 1 || normalized.InterfaceLists[0] != "LAN" {
		t.Fatalf("interface lists were not normalized: %#v", normalized.InterfaceLists)
	}
	wantExclusions := []string{"10.0.0.0/24", "10.0.0.2/32", "fd86::/64"}
	if len(normalized.ExcludePrefixes) != len(wantExclusions) {
		t.Fatalf("exclusions were not canonicalized: %#v", normalized.ExcludePrefixes)
	}
	for index, want := range wantExclusions {
		if normalized.ExcludePrefixes[index] != want {
			t.Fatalf("exclusions were not canonicalized: %#v, want %#v", normalized.ExcludePrefixes, wantExclusions)
		}
	}

	for _, scope := range []*RoutingSourceScope{
		{Kind: RoutingSourceInterface},
		{Kind: RoutingSourceInterface, ExcludePrefixes: []string{"10.0.0.2"}},
		{Kind: RoutingSourceInterface, Interfaces: []string{"wg1"}, ExcludePrefixes: []string{"not-an-ip"}},
		{Kind: RoutingSourceInterfaceList, Name: "LAN", ExcludePrefixes: []string{"10.0.0.2"}},
		{Kind: RoutingSourceDevice, Interfaces: []string{"wg1"}},
		{Kind: RoutingSourceIP, ExcludePrefixes: []string{"10.0.0.2"}},
		{Kind: RoutingSourceAll, InterfaceLists: []string{"LAN"}},
	} {
		if _, err := NormalizeRoutingSourceScope(scope); !errors.Is(err, ErrRoutingSourceScopeInvalid) {
			t.Fatalf("scope %#v error=%v, want invalid source scope", scope, err)
		}
	}
}

func TestInterfaceSourceExclusionProjection(t *testing.T) {
	scope := &RoutingSourceScope{Kind: RoutingSourceInterface, Interfaces: []string{"wg1"}, InterfaceLists: []string{"LAN"}, ExcludePrefixes: []string{"10.0.0.2"}}
	subjectProjection, ingress, err := RoutingSourceLegacyProjection(*scope, Subject{Mode: SubjectModeAll})
	if err != nil {
		t.Fatal(err)
	}
	if subjectProjection.Mode != SubjectModeExcluded || len(subjectProjection.Prefixes) != 1 || subjectProjection.Prefixes[0] != "10.0.0.2/32" {
		t.Fatalf("exclusion projection subject=%#v, want excluded 10.0.0.2/32", subjectProjection)
	}
	if len(ingress.Interfaces) != 1 || ingress.Interfaces[0] != "wg1" || len(ingress.InterfaceLists) != 1 || ingress.InterfaceLists[0] != "LAN" {
		t.Fatalf("exclusion projection ingress=%#v, want wg1 + LAN", ingress)
	}

	rule, err := NormalizeRoutingRule(RoutingRule{
		ID: "rule", Name: "Rule", EgressID: "egress", TargetListIDs: []string{"target"}, Enabled: true,
		SourceScope: &RoutingSourceScope{Kind: RoutingSourceInterface, Interfaces: []string{"wg1"}, ExcludePrefixes: []string{"10.0.0.2"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if rule.Subject.Mode != SubjectModeExcluded || len(rule.Subject.Prefixes) != 1 {
		t.Fatalf("interface exclusion rule did not derive the excluded projection: %#v", rule)
	}

	if _, err := NormalizeRoutingRule(RoutingRule{
		ID: "rule", Name: "Rule", EgressID: "egress", TargetListIDs: []string{"target"},
		SourceScope: &RoutingSourceScope{Kind: RoutingSourceInterface, Interfaces: []string{"wg1"}, ExcludePrefixes: []string{"10.0.0.2"}},
		Subject:     Subject{Mode: SubjectModeExcluded, Prefixes: []string{"10.0.0.3"}},
	}); !errors.Is(err, ErrRoutingSourceScopeInvalid) {
		t.Fatalf("mismatched exclusion echo error=%v, want invalid source scope", err)
	}
}

func TestRoutingSourceDirectSelector(t *testing.T) {
	if kind, name, ok := RoutingSourceDirectSelector(&RoutingSourceScope{Kind: RoutingSourceInterface, Interfaces: []string{"wg1"}}); !ok || kind != RoutingSourceInterface || name != "wg1" {
		t.Fatalf("single interface selector=%q %q %v", kind, name, ok)
	}
	if kind, name, ok := RoutingSourceDirectSelector(&RoutingSourceScope{Kind: RoutingSourceInterface, InterfaceLists: []string{"LAN"}}); !ok || kind != RoutingSourceInterfaceList || name != "LAN" {
		t.Fatalf("single interface-list selector=%q %q %v", kind, name, ok)
	}
	for _, scope := range []*RoutingSourceScope{
		{Kind: RoutingSourceInterface, Interfaces: []string{"wg1", "wg2"}},
		{Kind: RoutingSourceInterface, Interfaces: []string{"wg1"}, InterfaceLists: []string{"LAN"}},
		{Kind: RoutingSourceInterface, Interfaces: []string{"wg1"}, ExcludePrefixes: []string{"10.0.0.2/32"}},
	} {
		if _, _, ok := RoutingSourceDirectSelector(scope); ok {
			t.Fatalf("multi-selector or excluding scope %#v compiled to a direct matcher", scope)
		}
	}
}

func TestUpgradeLegacyRoutingSource(t *testing.T) {
	typed := UpgradeLegacyRoutingSource(RoutingRule{
		Subject: Subject{Mode: SubjectModeAll},
		Ingress: TrafficIngressScope{InterfaceLists: []string{"LAN"}, Interfaces: []string{"bridge"}},
	})
	if typed.SourceScope == nil || typed.SourceScope.Kind != RoutingSourceInterface || len(typed.SourceScope.Interfaces) != 1 || len(typed.SourceScope.InterfaceLists) != 1 {
		t.Fatalf("all+ingress did not upgrade to an interface source: %#v", typed.SourceScope)
	}

	excluded := UpgradeLegacyRoutingSource(RoutingRule{
		Subject: Subject{Mode: SubjectModeExcluded, Prefixes: []string{"10.0.0.0/24"}},
		Ingress: TrafficIngressScope{InterfaceLists: []string{"LAN"}},
	})
	if excluded.SourceScope == nil || excluded.SourceScope.Kind != RoutingSourceInterface || len(excluded.SourceScope.ExcludePrefixes) != 1 {
		t.Fatalf("excluded prefixes did not upgrade to an interface source with exclusions: %#v", excluded.SourceScope)
	}

	device := UpgradeLegacyRoutingSource(RoutingRule{Subject: Subject{Mode: SubjectModeSelected, Members: []SubjectMember{{TerminalID: "terminal-a", Binding: "auto"}}}})
	if device.SourceScope == nil || device.SourceScope.Kind != RoutingSourceDevice {
		t.Fatalf("selected terminals did not upgrade to a device source: %#v", device.SourceScope)
	}

	ip := UpgradeLegacyRoutingSource(RoutingRule{Subject: Subject{Mode: SubjectModeSelected, Prefixes: []string{"10.0.0.0/24"}}})
	if ip.SourceScope == nil || ip.SourceScope.Kind != RoutingSourceIP {
		t.Fatalf("selected prefixes did not upgrade to an ip source: %#v", ip.SourceScope)
	}

	for _, rule := range []RoutingRule{
		{Subject: Subject{Mode: SubjectModeAll}},
		{Subject: Subject{Mode: SubjectModeExcluded, Members: []SubjectMember{{TerminalID: "terminal-a", Binding: "auto"}}, Prefixes: []string{"10.0.0.0/24"}}, Ingress: TrafficIngressScope{InterfaceLists: []string{"LAN"}}},
		{Subject: Subject{Mode: SubjectModeSelected, Members: []SubjectMember{{TerminalID: "terminal-a", Binding: "auto"}}, Prefixes: []string{"10.0.0.0/24"}}},
		{SourceScope: &RoutingSourceScope{Kind: RoutingSourceDevice}},
	} {
		if upgraded := UpgradeLegacyRoutingSource(rule); upgraded.SourceScope != rule.SourceScope {
			t.Fatalf("unconvertible or typed rule changed its source scope: %#v -> %#v", rule.SourceScope, upgraded.SourceScope)
		}
	}
}
