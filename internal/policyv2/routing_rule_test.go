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
		{name: "same subject same domain different egress resolves by DNS priority", leftTarget: "domain-a", rightTarget: "domain-a", leftEgress: "wan-a", rightEgress: "wan-b", leftSubject: all, rightSubject: all, want: 0},
		{name: "nested domain different target lists resolves by DNS priority", leftTarget: "domain-a", rightTarget: "domain-b", leftEgress: "wan-a", rightEgress: "wan-b", leftSubject: all, rightSubject: all, want: 0},
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

func TestDomainProjectionResolutionIsSeparateFromLogicalConflict(t *testing.T) {
	targets := map[string][]SourceRule{
		"video": {{RuleType: "DOMAIN-SUFFIX", Domain: "video.example"}},
		"ip":    {{RuleType: "IP-CIDR", Domain: "192.0.2.0/24"}},
	}
	kinds := map[string]string{"video": KindDomain, "ip": KindIP}
	rules := []RoutingRule{
		{ID: "ipad", Name: "iPad", Subject: Subject{Mode: SubjectModeSelected, Prefixes: []string{"192.0.2.0/25"}}, TargetListIDs: []string{"video"}, EgressID: "wan-a", Enabled: true},
		{ID: "tv", Name: "TV", Subject: Subject{Mode: SubjectModeSelected, Prefixes: []string{"192.0.2.128/25"}}, TargetListIDs: []string{"video"}, EgressID: "wan-b", Enabled: true},
	}
	egresses := map[string]Egress{
		"wan-a": {ID: "wan-a", Enabled: true, DNSUpstream: "1.1.1.1", FakeAlias: "192.0.2.53"},
		"wan-b": {ID: "wan-b", Enabled: true, DNSUpstream: "8.8.8.8", FakeAlias: "192.0.2.54"},
	}
	if conflicts := RoutingRuleConflicts(rules, targets, kinds); len(conflicts) != 0 {
		t.Fatalf("domain overlap across egresses must not be a logical conflict: %#v", conflicts)
	}
	// Domain overlap across physical projections is decided by Priority, not
	// by the subject model: both rules default to Priority 0, so the overlap
	// blocks with a user-readable reason instead of a silent winner.
	resolutions := DomainProjectionResolutions(rules, targets, kinds, egresses)
	if len(resolutions) != 1 || resolutions[0].Severity != "blocker" || resolutions[0].Code != "domain_projection_context_ambiguous" {
		t.Fatalf("equal-priority domain overlap resolutions=%#v", resolutions)
	}
	if !strings.Contains(resolutions[0].Reason, "video.example") || !strings.Contains(resolutions[0].Reason, "iPad") || !strings.Contains(resolutions[0].Reason, "TV") {
		t.Fatalf("blocker reason must name matcher and rule display names: %q", resolutions[0].Reason)
	}
	if strings.Contains(resolutions[0].Reason, "物理投影") || strings.Contains(resolutions[0].Reason, "ipad / tv") {
		t.Fatalf("blocker reason exposes internal projection jargon or raw IDs: %q", resolutions[0].Reason)
	}
	if got := DomainProjectionResolutions([]RoutingRule{
		{ID: "ip-a", Subject: Subject{Mode: SubjectModeAll}, TargetListIDs: []string{"ip"}, EgressID: "wan-a", Enabled: true},
		{ID: "ip-b", Subject: Subject{Mode: SubjectModeAll}, TargetListIDs: []string{"ip"}, EgressID: "wan-b", Enabled: true},
	}, targets, kinds, egresses); len(got) != 0 {
		t.Fatalf("IP-only rules were given a DNS projection resolution: %#v", got)
	}
}

func TestDomainProjectionResolutionsUsePriorityArbitration(t *testing.T) {
	targets := map[string][]SourceRule{
		"youtube":       {{RuleType: "DOMAIN-SUFFIX", Domain: "youtube.com"}},
		"youtube-exact": {{RuleType: "DOMAIN", Domain: "youtube.com"}},
		"example":       {{RuleType: "DOMAIN-SUFFIX", Domain: "example.com"}},
		"api":           {{RuleType: "DOMAIN", Domain: "api.example.com"}},
		"google":        {{RuleType: "DOMAIN-SUFFIX", Domain: "google.com"}},
		"video-google":  {{RuleType: "DOMAIN-SUFFIX", Domain: "video.google.com"}},
		"ip":            {{RuleType: "IP-CIDR", Domain: "192.0.2.0/24"}},
	}
	kinds := map[string]string{
		"youtube": KindDomain, "youtube-exact": KindDomain, "example": KindDomain, "api": KindDomain,
		"google": KindDomain, "video-google": KindDomain, "ip": KindIP,
	}
	egresses := map[string]Egress{
		"wan-a": {ID: "wan-a", Enabled: true, DNSUpstream: "1.1.1.1", FakeAlias: "192.0.2.53"},
		"wan-b": {ID: "wan-b", Enabled: true, DNSUpstream: "1.1.1.1", FakeAlias: "192.0.2.53"},
	}
	all := Subject{Mode: SubjectModeAll}
	tests := []struct {
		name        string
		rules       []RoutingRule
		wantBlocker bool
		wantWarning bool
		wantWinner  string
		wantLoser   string
	}{
		{
			// Case 1: overlapping TargetLists inside one RoutingRule are an OR
			// set behind the same egress decision — always allowed.
			name: "case 1 same rule overlapping target lists allowed",
			rules: []RoutingRule{
				{ID: "rule-a", Name: "A", Subject: all, TargetListIDs: []string{"youtube", "youtube-exact"}, EgressID: "wan-a", Priority: 10, Enabled: true},
			},
		},
		{
			// Two rules sharing one (egress, target) projection never compare
			// against themselves even with different priorities.
			name: "same egress and target share one projection",
			rules: []RoutingRule{
				{ID: "rule-a", Name: "A", Subject: all, TargetListIDs: []string{"youtube"}, EgressID: "wan-a", Priority: 10, Enabled: true},
				{ID: "rule-b", Name: "B", Subject: all, TargetListIDs: []string{"youtube"}, EgressID: "wan-a", Priority: 50, Enabled: true},
			},
		},
		{
			name: "same egress and non-overlap targets are allowed",
			rules: []RoutingRule{
				{ID: "rule-a", Name: "A", Subject: all, TargetListIDs: []string{"youtube"}, EgressID: "wan-a", Priority: 10, Enabled: true},
				{ID: "rule-b", Name: "B", Subject: all, TargetListIDs: []string{"example"}, EgressID: "wan-a", Priority: 20, Enabled: true},
			},
		},
		{
			// Case 2: different rules, different Priority, different egress,
			// exact/exact overlap → warning and the higher rule wins.
			name: "case 2 different priority exact overlap warns with winner",
			rules: []RoutingRule{
				{ID: "rule-a", Name: "香港出口", Subject: all, TargetListIDs: []string{"youtube"}, EgressID: "wan-a", Priority: 10, Enabled: true},
				{ID: "rule-b", Name: "美国出口", Subject: all, TargetListIDs: []string{"youtube-exact"}, EgressID: "wan-b", Priority: 20, Enabled: true},
			},
			wantWarning: true, wantWinner: "rule-a", wantLoser: "rule-b",
		},
		{
			// Case 3: equal Priority → deterministic winner is impossible.
			name: "case 3 equal priority overlap blocks",
			rules: []RoutingRule{
				{ID: "rule-a", Name: "香港出口", Subject: all, TargetListIDs: []string{"youtube"}, EgressID: "wan-a", Priority: 10, Enabled: true},
				{ID: "rule-b", Name: "美国出口", Subject: all, TargetListIDs: []string{"youtube-exact"}, EgressID: "wan-b", Priority: 10, Enabled: true},
			},
			wantBlocker: true,
		},
		{
			// Case 4: high-priority exact + low-priority suffix overlap; the
			// lower suffix keeps its non-overlapping space via first-match.
			name: "case 4 high exact low suffix warns",
			rules: []RoutingRule{
				{ID: "rule-a", Name: "A", Subject: all, TargetListIDs: []string{"api"}, EgressID: "wan-a", Priority: 10, Enabled: true},
				{ID: "rule-b", Name: "B", Subject: all, TargetListIDs: []string{"example"}, EgressID: "wan-b", Priority: 20, Enabled: true},
			},
			wantWarning: true, wantWinner: "rule-a", wantLoser: "rule-b",
		},
		{
			// Case 5: high-priority suffix shadows the low-priority exact
			// inside it — Priority beats matcher specificity.
			name: "case 5 high suffix low exact warns with suffix winning",
			rules: []RoutingRule{
				{ID: "rule-a", Name: "A", Subject: all, TargetListIDs: []string{"example"}, EgressID: "wan-a", Priority: 10, Enabled: true},
				{ID: "rule-b", Name: "B", Subject: all, TargetListIDs: []string{"api"}, EgressID: "wan-b", Priority: 20, Enabled: true},
			},
			wantWarning: true, wantWinner: "rule-a", wantLoser: "rule-b",
		},
		{
			// Case 6: nested suffix — the outer suffix wins the inner subtree
			// when its Priority is higher.
			name: "case 6 nested suffix high outer wins",
			rules: []RoutingRule{
				{ID: "rule-a", Name: "A", Subject: all, TargetListIDs: []string{"google"}, EgressID: "wan-a", Priority: 10, Enabled: true},
				{ID: "rule-b", Name: "B", Subject: all, TargetListIDs: []string{"video-google"}, EgressID: "wan-b", Priority: 20, Enabled: true},
			},
			wantWarning: true, wantWinner: "rule-a", wantLoser: "rule-b",
		},
		{
			// Case 6 reversed: the inner suffix wins its subtree when its
			// Priority is higher.
			name: "case 6 nested suffix high inner wins",
			rules: []RoutingRule{
				{ID: "rule-a", Name: "A", Subject: all, TargetListIDs: []string{"google"}, EgressID: "wan-a", Priority: 20, Enabled: true},
				{ID: "rule-b", Name: "B", Subject: all, TargetListIDs: []string{"video-google"}, EgressID: "wan-b", Priority: 10, Enabled: true},
			},
			wantWarning: true, wantWinner: "rule-b", wantLoser: "rule-a",
		},
		{
			// Case 8: same egress, different overlapping targets, different
			// Priority → still needs a winner because the physical address
			// lists differ.
			name: "case 8 same egress different targets overlap warns",
			rules: []RoutingRule{
				{ID: "rule-a", Name: "A", Subject: all, TargetListIDs: []string{"youtube"}, EgressID: "wan-a", Priority: 10, Enabled: true},
				{ID: "rule-b", Name: "B", Subject: all, TargetListIDs: []string{"youtube-exact"}, EgressID: "wan-a", Priority: 20, Enabled: true},
			},
			wantWarning: true, wantWinner: "rule-a", wantLoser: "rule-b",
		},
		{
			// Case 9: same egress, different targets, equal Priority → blocker.
			name: "case 9 same egress different targets equal priority blocks",
			rules: []RoutingRule{
				{ID: "rule-a", Name: "A", Subject: all, TargetListIDs: []string{"youtube"}, EgressID: "wan-a", Priority: 10, Enabled: true},
				{ID: "rule-b", Name: "B", Subject: all, TargetListIDs: []string{"youtube-exact"}, EgressID: "wan-a", Priority: 10, Enabled: true},
			},
			wantBlocker: true,
		},
		{
			name: "disjoint domains are allowed",
			rules: []RoutingRule{
				{ID: "rule-a", Name: "A", Subject: all, TargetListIDs: []string{"example"}, EgressID: "wan-a", Priority: 10, Enabled: true},
				{ID: "rule-b", Name: "B", Subject: all, TargetListIDs: []string{"google"}, EgressID: "wan-b", Priority: 20, Enabled: true},
			},
		},
		{
			name: "ip projections have no dns resolution",
			rules: []RoutingRule{
				{ID: "rule-a", Name: "A", Subject: all, TargetListIDs: []string{"ip"}, EgressID: "wan-a", Priority: 10, Enabled: true},
				{ID: "rule-b", Name: "B", Subject: all, TargetListIDs: []string{"ip"}, EgressID: "wan-b", Priority: 10, Enabled: true},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := DomainProjectionResolutions(test.rules, targets, kinds, egresses)
			blockers, warnings := 0, 0
			for _, resolution := range got {
				switch resolution.Severity {
				case "blocker":
					blockers++
					if !strings.Contains(resolution.Reason, "Priority") {
						t.Fatalf("blocker reason must mention Priority: %q", resolution.Reason)
					}
				case "warning":
					warnings++
					if resolution.RuleAID != test.wantWinner || resolution.RuleBID != test.wantLoser {
						t.Fatalf("warning winner/loser attribution=%s/%s, want %s/%s", resolution.RuleAID, resolution.RuleBID, test.wantWinner, test.wantLoser)
					}
					if !strings.Contains(resolution.Reason, "优先") || !strings.Contains(resolution.Reason, "不会生效") {
						t.Fatalf("warning reason must explain shadowing: %q", resolution.Reason)
					}
				}
			}
			if (blockers > 0) != test.wantBlocker {
				t.Fatalf("blockers=%d, wantBlocker=%v: %#v", blockers, test.wantBlocker, got)
			}
			if (warnings > 0) != test.wantWarning {
				t.Fatalf("warnings=%d, wantWarning=%v: %#v", warnings, test.wantWarning, got)
			}
			if !test.wantBlocker && !test.wantWarning && len(got) != 0 {
				t.Fatalf("unexpected resolutions=%#v", got)
			}
		})
	}
}

func TestDomainProjectionDisabledEgressDoesNotArbitrateDNSConflicts(t *testing.T) {
	targets := map[string][]SourceRule{
		"youtube":       {{RuleType: "DOMAIN-SUFFIX", Domain: "youtube.com"}},
		"youtube-exact": {{RuleType: "DOMAIN", Domain: "youtube.com"}},
	}
	kinds := map[string]string{"youtube": KindDomain, "youtube-exact": KindDomain}
	all := Subject{Mode: SubjectModeAll}
	egressState := func(egressAEnabled bool) map[string]Egress {
		return map[string]Egress{
			"wan-a": {ID: "wan-a", Enabled: egressAEnabled, DNSUpstream: "1.1.1.1", FakeAlias: "192.0.2.53"},
			"wan-b": {ID: "wan-b", Enabled: true, DNSUpstream: "1.1.1.1", FakeAlias: "192.0.2.54"},
		}
	}
	ruleA := RoutingRule{ID: "rule-a", Name: "A", Subject: all, TargetListIDs: []string{"youtube"}, EgressID: "wan-a", Priority: 10, Enabled: true}
	ruleB := RoutingRule{ID: "rule-b", Name: "B", Subject: all, TargetListIDs: []string{"youtube-exact"}, EgressID: "wan-b", Priority: 20, Enabled: true}
	ruleBEqual := ruleB
	ruleBEqual.Priority = 10

	// A higher-Priority rule on a disabled Egress projects `disabled=yes` DNS
	// Statics only, so it must never shadow, warn about, or block the enabled
	// projection — even at equal Priority, where both active sides would
	// otherwise be an unresolvable tie. B is the only active projection.
	for _, against := range []RoutingRule{ruleB, ruleBEqual} {
		if got := DomainProjectionResolutions([]RoutingRule{ruleA, against}, targets, kinds, egressState(false)); len(got) != 0 {
			t.Fatalf("disabled-egress rule must not arbitrate against p%d: %#v", against.Priority, got)
		}
	}
	// A missing Egress reference is inactive for arbitration too (the rule
	// itself is blocked elsewhere as routing_rule_egress_unavailable).
	if got := DomainProjectionResolutions([]RoutingRule{ruleA, ruleB}, targets, kinds, map[string]Egress{"wan-b": {ID: "wan-b", Enabled: true}}); len(got) != 0 {
		t.Fatalf("missing-egress rule must not arbitrate: %#v", got)
	}

	// Re-enabling Egress A restores normal priority arbitration: A p10 wins
	// and B is described as the shadowed side.
	got := DomainProjectionResolutions([]RoutingRule{ruleA, ruleB}, targets, kinds, egressState(true))
	if len(got) != 1 || got[0].Severity != "warning" || got[0].RuleAID != "rule-a" || got[0].RuleBID != "rule-b" {
		t.Fatalf("re-enabled egress must restore priority arbitration: %#v", got)
	}
	if !strings.Contains(got[0].Reason, "优先") || !strings.Contains(got[0].Reason, "不会生效") {
		t.Fatalf("restored arbitration reason must explain shadowing: %q", got[0].Reason)
	}
	// Equal priority with both sides active returns to the blocker path.
	if got := DomainProjectionResolutions([]RoutingRule{ruleA, ruleBEqual}, targets, kinds, egressState(true)); len(got) != 1 || got[0].Severity != "blocker" {
		t.Fatalf("equal-priority active overlap must block: %#v", got)
	}
	// The disabled projection keeps a deterministic ordering priority for the
	// DNS Static sequence without joining conflict arbitration.
	priorities := DomainProjectionPriorities([]RoutingRule{ruleA, ruleB}, kinds, egressState(false))
	if priorities["wan-a\x00youtube"] != 10 {
		t.Fatalf("disabled projection lost its deterministic order priority: %#v", priorities)
	}
	if priorities["wan-b\x00youtube-exact"] != 20 {
		t.Fatalf("active projection priority=%d, want 20: %#v", priorities["wan-b\x00youtube-exact"], priorities)
	}
}

func TestDomainProjectionEffectivePriorityUsesHighestConsumer(t *testing.T) {
	targets := map[string][]SourceRule{
		"youtube": {{RuleType: "DOMAIN-SUFFIX", Domain: "youtube.com"}},
	}
	kinds := map[string]string{"youtube": KindDomain}
	egresses := map[string]Egress{"wan-a": {ID: "wan-a", Enabled: true}, "wan-b": {ID: "wan-b", Enabled: true}}
	// Case 7: one physical projection shared by rules with different
	// Priorities has effective priority = min (highest) of its consumers and
	// never reports an internal conflict. wan-b's separate projection has its
	// own lower effective priority so the cross-pair resolves by priority.
	rules := []RoutingRule{
		{ID: "rule-a", Name: "A", Subject: Subject{Mode: SubjectModeAll}, TargetListIDs: []string{"youtube"}, EgressID: "wan-a", Priority: 10, Enabled: true},
		{ID: "rule-b", Name: "B", Subject: Subject{Mode: SubjectModeAll}, TargetListIDs: []string{"youtube"}, EgressID: "wan-a", Priority: 50, Enabled: true},
	}
	if got := DomainProjectionResolutions(rules, targets, kinds, egresses); len(got) != 0 {
		t.Fatalf("shared projection consumers must not conflict: %#v", got)
	}
	priorities := DomainProjectionPriorities(rules, kinds, egresses)
	if priorities["wan-a\x00youtube"] != 10 {
		t.Fatalf("shared projection effective priority=%d, want highest consumer 10", priorities["wan-a\x00youtube"])
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
