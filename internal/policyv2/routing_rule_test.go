package policyv2

import (
	"errors"
	"strings"
	"testing"

	"rosboard/internal/accesscontrol"
	"rosboard/internal/ownership"
)

func TestNormalizeRoutingRuleIngressFollowsSubjectMode(t *testing.T) {
	base := RoutingRule{ID: "rule", Name: "Rule", EgressID: "egress", TargetListIDs: []string{"target"}, Enabled: true, Subject: Subject{Mode: SubjectModeAll}}
	if _, err := NormalizeRoutingRule(base); !errors.Is(err, ErrRoutingExcludedRequiresIngress) {
		t.Fatalf("all rule without ingress error=%v, want ingress-required", err)
	}
	base.Subject = Subject{Mode: SubjectModeExcluded, Prefixes: []string{"10.0.0.20/32"}}
	if _, err := NormalizeRoutingRule(base); !errors.Is(err, ErrRoutingExcludedRequiresIngress) {
		t.Fatalf("excluded rule without ingress error=%v, want ingress-required", err)
	}
	base.Subject = Subject{Mode: SubjectModeSelected, Prefixes: []string{"10.0.0.20/32"}}
	base.Ingress = TrafficIngressScope{InterfaceLists: []string{"LAN"}}
	normalized, err := NormalizeRoutingRule(base)
	if err != nil {
		t.Fatal(err)
	}
	if HasTrafficIngress(normalized.Ingress) {
		t.Fatalf("selected rule retained an unused ingress scope: %#v", normalized.Ingress)
	}
	base.Subject = Subject{Mode: SubjectModeAll}
	base.Ingress = TrafficIngressScope{InterfaceLists: []string{"LAN"}}
	normalized, err = NormalizeRoutingRule(base)
	if err != nil || !HasTrafficIngress(normalized.Ingress) {
		t.Fatalf("all rule with ingress was not accepted: normalized=%#v err=%v", normalized, err)
	}
}

func TestSubjectsOverlapUsesOnlyProvenSubjectEvidence(t *testing.T) {
	all := Subject{Mode: SubjectModeAll}
	selectedPrefix := Subject{Mode: SubjectModeSelected, Prefixes: []string{"192.0.2.10"}}
	if overlap, indeterminate := SubjectsOverlap(all, selectedPrefix); !overlap || indeterminate {
		t.Fatalf("all and selected subjects = overlap=%v indeterminate=%v", overlap, indeterminate)
	}

	sameTerminal := Subject{Mode: SubjectModeSelected, Members: []SubjectMember{{TerminalID: "terminal-a", Binding: "auto"}}}
	if overlap, indeterminate := SubjectsOverlap(sameTerminal, sameTerminal); !overlap || indeterminate {
		t.Fatalf("same terminal subjects = overlap=%v indeterminate=%v", overlap, indeterminate)
	}

	overlappingPrefix := Subject{Mode: SubjectModeSelected, Prefixes: []string{"192.0.2.0/24"}}
	if overlap, indeterminate := SubjectsOverlap(selectedPrefix, overlappingPrefix); !overlap || indeterminate {
		t.Fatalf("manual prefix subjects = overlap=%v indeterminate=%v", overlap, indeterminate)
	}
	disjointPrefix := Subject{Mode: SubjectModeSelected, Prefixes: []string{"198.51.100.0/24"}}
	if overlap, indeterminate := SubjectsOverlap(overlappingPrefix, disjointPrefix); overlap || indeterminate {
		t.Fatalf("disjoint manual prefixes = overlap=%v indeterminate=%v", overlap, indeterminate)
	}

	unknown := Subject{Mode: SubjectModeSelected, Members: []SubjectMember{{TerminalID: "terminal-a", Binding: "auto"}}}
	known := Subject{Mode: SubjectModeSelected, Prefixes: []string{"203.0.113.0/24"}}
	if overlap, indeterminate := SubjectsOverlap(unknown, known); overlap || !indeterminate {
		t.Fatalf("unresolved subject evidence = overlap=%v indeterminate=%v", overlap, indeterminate)
	}
}

func TestRoutingRuleConflictsRequireSubjectTargetAndEgressOverlap(t *testing.T) {
	targets := map[string][]SourceRule{
		"domain-a": {{RuleType: "DOMAIN-SUFFIX", Domain: "example.com"}},
		"domain-b": {{RuleType: "DOMAIN", Domain: "api.example.com"}},
		"ip-a":     {{RuleType: "IP-CIDR", Domain: "192.0.2.0/24"}},
		"ip-b":     {{RuleType: "IP-CIDR", Domain: "192.0.2.128/25"}},
		"ip-c":     {{RuleType: "IP-CIDR", Domain: "198.51.100.0/24"}},
	}
	kinds := map[string]string{
		"domain-a": KindDomain, "domain-b": KindDomain,
		"ip-a": KindIP, "ip-b": KindIP, "ip-c": KindIP,
	}
	all := Subject{Mode: SubjectModeAll}
	disjoint := Subject{Mode: SubjectModeSelected, Prefixes: []string{"198.51.100.0/24"}}
	cases := []struct {
		name         string
		leftTarget   string
		rightTarget  string
		leftEgress   string
		rightEgress  string
		leftSubject  Subject
		rightSubject Subject
		want         int
	}{
		{name: "same subject same domain different egress", leftTarget: "domain-a", rightTarget: "domain-a", leftEgress: "wan-a", rightEgress: "wan-b", leftSubject: all, rightSubject: all, want: 1},
		{name: "nested domain different target lists", leftTarget: "domain-a", rightTarget: "domain-b", leftEgress: "wan-a", rightEgress: "wan-b", leftSubject: all, rightSubject: all, want: 1},
		{name: "overlapping ip ranges", leftTarget: "ip-a", rightTarget: "ip-b", leftEgress: "wan-a", rightEgress: "wan-b", leftSubject: all, rightSubject: all, want: 1},
		{name: "same egress allowed", leftTarget: "domain-a", rightTarget: "domain-a", leftEgress: "wan-a", rightEgress: "wan-a", leftSubject: all, rightSubject: all, want: 0},
		{name: "disjoint subject allowed", leftTarget: "ip-a", rightTarget: "ip-a", leftEgress: "wan-a", rightEgress: "wan-b", leftSubject: disjoint, rightSubject: Subject{Mode: SubjectModeSelected, Prefixes: []string{"203.0.113.0/24"}}, want: 0},
		{name: "disjoint ip target allowed", leftTarget: "ip-a", rightTarget: "ip-c", leftEgress: "wan-a", rightEgress: "wan-b", leftSubject: all, rightSubject: all, want: 0},
		{name: "domain and ip are declaratively separate", leftTarget: "domain-a", rightTarget: "ip-a", leftEgress: "wan-a", rightEgress: "wan-b", leftSubject: all, rightSubject: all, want: 0},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			rules := []RoutingRule{
				{ID: "left", Subject: test.leftSubject, TargetListIDs: []string{test.leftTarget}, EgressID: test.leftEgress, Priority: 20, Enabled: true},
				{ID: "right", Subject: test.rightSubject, TargetListIDs: []string{test.rightTarget}, EgressID: test.rightEgress, Priority: 10, Enabled: true},
			}
			conflicts := RoutingRuleConflicts(rules, targets, kinds)
			if len(conflicts) != test.want {
				t.Fatalf("conflicts=%#v, want %d", conflicts, test.want)
			}
		})
	}
}

func TestDomainProjectionAmbiguityIsSeparateFromLogicalConflict(t *testing.T) {
	targets := map[string][]SourceRule{
		"video": {{RuleType: "DOMAIN-SUFFIX", Domain: "video.example"}},
		"ip":    {{RuleType: "IP-CIDR", Domain: "192.0.2.0/24"}},
	}
	kinds := map[string]string{"video": KindDomain, "ip": KindIP}
	rules := []RoutingRule{
		{ID: "ipad", Subject: Subject{Mode: SubjectModeSelected, Prefixes: []string{"192.0.2.0/25"}}, TargetListIDs: []string{"video"}, EgressID: "wan-a", Enabled: true},
		{ID: "tv", Subject: Subject{Mode: SubjectModeSelected, Prefixes: []string{"192.0.2.128/25"}}, TargetListIDs: []string{"video"}, EgressID: "wan-b", Enabled: true},
	}
	egresses := map[string]Egress{
		"wan-a": {ID: "wan-a", DNSUpstream: "1.1.1.1", FakeAlias: "192.0.2.53"},
		"wan-b": {ID: "wan-b", DNSUpstream: "8.8.8.8", FakeAlias: "192.0.2.54"},
	}
	if conflicts := RoutingRuleConflicts(rules, targets, kinds); len(conflicts) != 0 {
		t.Fatalf("disjoint subjects were reported as logical conflicts: %#v", conflicts)
	}
	ambiguities := DomainProjectionContextAmbiguities(rules, targets, kinds, egresses)
	if len(ambiguities) != 1 || ambiguities[0].Kind != "domain_projection_context_ambiguous" {
		t.Fatalf("domain ambiguity=%#v", ambiguities)
	}
	if got := DomainProjectionContextAmbiguities([]RoutingRule{
		{ID: "ip-a", Subject: Subject{Mode: SubjectModeAll}, TargetListIDs: []string{"ip"}, EgressID: "wan-a", Enabled: true},
		{ID: "ip-b", Subject: Subject{Mode: SubjectModeAll}, TargetListIDs: []string{"ip"}, EgressID: "wan-b", Enabled: true},
	}, targets, kinds, egresses); len(got) != 0 {
		t.Fatalf("IP-only rules were given a DNS ambiguity: %#v", got)
	}
}

func TestDomainProjectionAmbiguitiesUseDistinctPhysicalProjections(t *testing.T) {
	targets := map[string][]SourceRule{
		"youtube":       {{RuleType: "DOMAIN-SUFFIX", Domain: "youtube.com"}},
		"youtube-exact": {{RuleType: "DOMAIN", Domain: "youtube.com"}},
		"example":       {{RuleType: "DOMAIN-SUFFIX", Domain: "example.com"}},
		"ip":            {{RuleType: "IP-CIDR", Domain: "192.0.2.0/24"}},
	}
	kinds := map[string]string{
		"youtube": KindDomain, "youtube-exact": KindDomain, "example": KindDomain, "ip": KindIP,
	}
	egresses := map[string]Egress{
		"wan-a": {ID: "wan-a", DNSUpstream: "1.1.1.1", FakeAlias: "192.0.2.53"},
		"wan-b": {ID: "wan-b", DNSUpstream: "1.1.1.1", FakeAlias: "192.0.2.53"},
	}
	all := Subject{Mode: SubjectModeAll}
	tests := []struct {
		name        string
		rules       []RoutingRule
		wantBlocker bool
	}{
		{
			name: "same egress and target share one projection",
			rules: []RoutingRule{
				{ID: "rule-a", Subject: all, TargetListIDs: []string{"youtube"}, EgressID: "wan-a", Enabled: true},
				{ID: "rule-b", Subject: all, TargetListIDs: []string{"youtube"}, EgressID: "wan-a", Enabled: true},
			},
		},
		{
			name: "same egress and non-overlap targets are allowed",
			rules: []RoutingRule{
				{ID: "rule-a", Subject: all, TargetListIDs: []string{"youtube"}, EgressID: "wan-a", Enabled: true},
				{ID: "rule-b", Subject: all, TargetListIDs: []string{"example"}, EgressID: "wan-a", Enabled: true},
			},
		},
		{
			name: "same egress and overlap targets need separate projections",
			rules: []RoutingRule{
				{ID: "rule-a", Subject: all, TargetListIDs: []string{"youtube"}, EgressID: "wan-a", Enabled: true},
				{ID: "rule-b", Subject: all, TargetListIDs: []string{"youtube-exact"}, EgressID: "wan-a", Enabled: true},
			},
			wantBlocker: true,
		},
		{
			name: "different egress overlap blocks despite equal dns context",
			rules: []RoutingRule{
				{ID: "rule-a", Subject: all, TargetListIDs: []string{"youtube"}, EgressID: "wan-a", Enabled: true},
				{ID: "rule-b", Subject: all, TargetListIDs: []string{"youtube-exact"}, EgressID: "wan-b", Enabled: true},
			},
			wantBlocker: true,
		},
		{
			name: "ip projections have no dns blocker",
			rules: []RoutingRule{
				{ID: "rule-a", Subject: all, TargetListIDs: []string{"ip"}, EgressID: "wan-a", Enabled: true},
				{ID: "rule-b", Subject: all, TargetListIDs: []string{"ip"}, EgressID: "wan-b", Enabled: true},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := DomainProjectionContextAmbiguities(test.rules, targets, kinds, egresses)
			if (len(got) != 0) != test.wantBlocker {
				t.Fatalf("domain projection blockers=%#v, wantBlocker=%v", got, test.wantBlocker)
			}
		})
	}
}

func TestRoutingTargetListNameIsScopedByEgressAndTarget(t *testing.T) {
	first := RoutingTargetListName("manager", "device", "wan-a", "target-a")
	if first != RoutingTargetListName("manager", "device", "wan-a", "target-a") {
		t.Fatal("routing target list name is not deterministic")
	}
	if first == RoutingTargetListName("manager", "device", "wan-b", "target-a") || first == RoutingTargetListName("manager", "device", "wan-a", "target-b") {
		t.Fatal("routing target list identity is not scoped by egress and target")
	}
}

func TestPresetRoutingTargetListNameUsesStablePresetSlug(t *testing.T) {
	name := RoutingTargetListNameForSource("manager", "device", "wan-a", Source{
		ID: "preset:youtube:domain", Type: TargetSourceTypePreset, PresetID: "youtube", Kind: KindDomain,
	})
	if name != RoutingTargetListNameForSource("manager", "device", "wan-a", Source{ID: "preset:youtube:domain", Type: TargetSourceTypePreset, PresetID: "youtube", Kind: KindDomain}) {
		t.Fatal("preset routing target list name is not deterministic")
	}
	if !strings.HasPrefix(name, "rb_rt_") || !strings.Contains(name, "_youtube_d") {
		t.Fatalf("preset routing target list name=%q, want readable stable preset slug", name)
	}
	renamedPreset := RoutingTargetListNameForSource("manager", "device", "wan-a", Source{
		ID: "preset:youtube:domain", Type: TargetSourceTypePreset, PresetID: "youtube", Kind: KindDomain, Name: "YouTube renamed",
	})
	if name != renamedPreset {
		t.Fatalf("preset display-name rename changed stable routing name: %q != %q", name, renamedPreset)
	}
	custom := RoutingTargetListNameForSource("manager", "device", "wan-a", Source{ID: "manual-a", Type: TargetSourceTypeManual, Kind: KindDomain, Name: "Editable display name"})
	customRenamed := RoutingTargetListNameForSource("manager", "device", "wan-a", Source{ID: "manual-a", Type: TargetSourceTypeManual, Kind: KindDomain, Name: "Renamed display name"})
	if custom != customRenamed || strings.Contains(custom, "Editable") || len(custom) != len("rb_rt_")+12 {
		t.Fatalf("custom routing target list name=%q should remain hash-only", custom)
	}
}

func TestRoutingCommentsKeepIdentityAndUseCleanTargetLabels(t *testing.T) {
	domainTarget := &routingTargetProjection{source: Source{Name: "YouTube", Kind: KindDomain}}
	ipTarget := &routingTargetProjection{source: Source{Name: "YouTube", Kind: KindIP}}
	cases := []struct {
		name, logicalID, label, want string
	}{
		{name: "domain matcher", logicalID: "routing-rule-connection:rule:ipv4:youtube", label: routingObjectCommentLabel("routing-rule-connection:rule:ipv4:youtube", "策略 xray", domainTarget), want: "入口连接标记 · 策略 xray · YouTube 域名"},
		{name: "ip matcher", logicalID: "routing-rule-connection:rule:ipv4:youtube-ip", label: routingObjectCommentLabel("routing-rule-connection:rule:ipv4:youtube-ip", "策略 xray", ipTarget), want: "入口连接标记 · 策略 xray · YouTube IP"},
		{name: "preset kind suffix is not repeated", logicalID: "routing-rule-connection:rule:ipv4:youtube-suffixed", label: routingObjectCommentLabel("routing-rule-connection:rule:ipv4:youtube-suffixed", "策略 xray", &routingTargetProjection{source: Source{Name: "YouTube · 域名", Kind: KindDomain}}), want: "入口连接标记 · 策略 xray · YouTube 域名"},
		{name: "ipv4 execution", logicalID: "routing-rule-routing:wan:ipv4:ingress:enabled", label: routingObjectCommentLabel("routing-rule-routing:wan:ipv4:ingress:enabled", "策略 xray", nil), want: "入口路由标记 · 策略 xray · IPv4"},
		{name: "ipv6 execution", logicalID: "routing-rule-routing:wan:ipv6:ingress:enabled", label: routingObjectCommentLabel("routing-rule-routing:wan:ipv6:ingress:enabled", "策略 xray", nil), want: "入口路由标记 · 策略 xray · IPv6"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			comment := managedComment("manager", "device", test.logicalID, test.label)
			wantIdentity := ownership.Identity("manager", "device", test.logicalID)
			if managedCommentIdentity(comment) != wantIdentity {
				t.Fatalf("comment identity=%q, want %q", managedCommentIdentity(comment), wantIdentity)
			}
			if comment != wantIdentity+" | "+test.want {
				t.Fatalf("comment=%q, want %q", comment, wantIdentity+" | "+test.want)
			}
			if strings.Contains(comment, "Domain") || strings.Contains(comment, "YouTube · 域名") || strings.Contains(comment, "YouTube · IP") {
				t.Fatalf("comment has repeated or English kind label: %q", comment)
			}
		})
	}
}

func TestRoutingSubjectReusesAutoFollowIdentitySemantics(t *testing.T) {
	rule := RoutingRule{ID: "rule-auto", Subject: Subject{Mode: SubjectModeSelected, Members: []SubjectMember{{
		TerminalID: "terminal-a", Binding: "auto", AnchorMAC: "AA:BB:CC:DD:EE:FF",
	}}}}
	terminals := []accesscontrol.Terminal{{ID: "terminal-a", MACAddress: "aa:bb:cc:dd:ee:ff", IPv4: []string{"192.0.2.20"}}}
	enriched := routingRulesWithTerminalEvidence([]RoutingRule{rule}, terminals)
	evaluation := accesscontrol.EvaluateMembers(RoutingRuleMembers(enriched[0]), terminals)
	if len(evaluation) != 1 || evaluation[0].State != accesscontrol.MemberResolved || len(evaluation[0].IPv4) != 1 || evaluation[0].IPv4[0] != "192.0.2.20" {
		t.Fatalf("auto-follow evaluation = %#v", evaluation)
	}

	terminals[0].MACAddress = "00:11:22:33:44:55"
	evaluation = accesscontrol.EvaluateMembers(RoutingRuleMembers(rule), terminals)
	if len(evaluation) != 1 || evaluation[0].State != accesscontrol.MemberConflicted || !evaluation[0].IdentityChanged {
		t.Fatalf("identity change evaluation = %#v", evaluation)
	}

	rule.Subject.Members[0].LastIPv4 = []string{"192.0.2.20"}
	evaluation = accesscontrol.EvaluateMembers(RoutingRuleMembers(rule), nil)
	if len(evaluation) != 1 || evaluation[0].State != accesscontrol.MemberUnresolved || len(evaluation[0].IPv4) != 1 || evaluation[0].IPv4[0] != "192.0.2.20" {
		t.Fatalf("last-known evaluation = %#v", evaluation)
	}
}
