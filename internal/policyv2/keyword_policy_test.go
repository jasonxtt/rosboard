package policyv2

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestKeywordImpactForProposalComputesCountsAndIntroducedKeywords(t *testing.T) {
	repository := &keywordProjectionRepository{
		sources: []Source{{ID: "target", Kind: KindDomain, ActiveVersionID: "version"}},
		sourceRules: map[string][]SourceRule{"version": {
			{RuleType: "DOMAIN-KEYWORD", Domain: "zeta"},
			{RuleType: "DOMAIN-KEYWORD", Domain: "alpha"},
		}},
	}
	proposal := &PolicyProposal{RoutingRule: &RoutingRule{ID: "new-rule", IncludeKeywordDomains: true, TargetListIDs: []string{"target"}}}
	impact, err := keywordImpactForProposal(context.Background(), repository, repository, proposal, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := &KeywordImpact{
		Enabled: true, AvailableCount: 2, ProjectedCount: 2,
		Keywords: []string{"alpha", "zeta"}, IntroducedKeywords: []string{"alpha", "zeta"},
		RequiresConfirmation: false, PrecedenceMode: "routeros-regexp-first",
	}
	if !reflect.DeepEqual(impact, want) {
		t.Fatalf("keyword impact = %#v, want %#v", impact, want)
	}
}

func TestKeywordImpactDoesNotRepeatConfirmationForAnUnchangedEnabledSet(t *testing.T) {
	repository := &keywordProjectionRepository{
		sources:      []Source{{ID: "target", Kind: KindDomain, ActiveVersionID: "version"}},
		routingRules: []RoutingRule{{ID: "rule", IncludeKeywordDomains: true, TargetListIDs: []string{"target"}}},
		sourceRules:  map[string][]SourceRule{"version": {{RuleType: "DOMAIN-KEYWORD", Domain: "video"}}},
	}
	proposal := &PolicyProposal{RoutingRule: &RoutingRule{ID: "rule", IncludeKeywordDomains: true, TargetListIDs: []string{"target"}}}
	impact, err := keywordImpactForProposal(context.Background(), repository, repository, proposal, nil)
	if err != nil {
		t.Fatal(err)
	}
	if impact.RequiresConfirmation || len(impact.IntroducedKeywords) != 0 {
		t.Fatalf("unchanged enabled keyword set should not repeat confirmation: %#v", impact)
	}

	plan := Plan{}
	appendKeywordImpactToPlan(&plan, &KeywordImpact{Enabled: true, ProjectedCount: 1, Keywords: []string{"video"}, RequiresConfirmation: true}, "rule")
	if plan.RequiresAcknowledgement || len(plan.Acknowledgements) != 0 || len(plan.Warnings) != 1 || plan.Warnings[0].RequiresAcknowledgement {
		t.Fatalf("keyword enable must add a warning without a separate acknowledgement: %#v", plan)
	}
	if !strings.Contains(plan.Warnings[0].Reason, "返回“高级设置”关闭“启用关键字域名规则”") {
		t.Fatalf("keyword warning should include the remediation: %#v", plan.Warnings[0])
	}
	plan = Plan{}
	appendKeywordImpactToPlan(&plan, impact, "rule")
	if plan.RequiresAcknowledgement || len(plan.Acknowledgements) != 0 || len(plan.Warnings) != 1 || plan.Warnings[0].RequiresAcknowledgement {
		t.Fatalf("unchanged keyword set must keep a warning without adding an acknowledgement: %#v", plan)
	}
}

func TestKeywordImpactDoesNotConfirmAutomaticContentAdditions(t *testing.T) {
	base := &keywordProjectionRepository{
		sources:      []Source{{ID: "target", Kind: KindDomain, ActiveVersionID: "old", PendingVersionID: "new"}},
		routingRules: []RoutingRule{{ID: "rule", IncludeKeywordDomains: true, TargetListIDs: []string{"target"}}},
		sourceRules:  map[string][]SourceRule{"old": {{RuleType: "DOMAIN-KEYWORD", Domain: "video"}}, "new": {{RuleType: "DOMAIN-KEYWORD", Domain: "video"}}},
	}
	planned := &keywordProjectionRepository{
		sources:     []Source{{ID: "target", Kind: KindDomain, ActiveVersionID: "old", PendingVersionID: "new"}},
		sourceRules: map[string][]SourceRule{"new": {{RuleType: "DOMAIN-KEYWORD", Domain: "video"}, {RuleType: "DOMAIN-KEYWORD", Domain: "youtube"}}},
	}
	proposal := &PolicyProposal{RoutingRule: &RoutingRule{ID: "rule", IncludeKeywordDomains: true, TargetListIDs: []string{"target"}}}
	impact, err := keywordImpactForProposal(context.Background(), base, planned, proposal, nil)
	if err != nil {
		t.Fatal(err)
	}
	if impact.RequiresConfirmation || len(impact.IntroducedKeywords) != 0 || !reflect.DeepEqual(impact.Keywords, []string{"video", "youtube"}) {
		t.Fatalf("automatic content additions should not reopen confirmation: %#v", impact)
	}
}

func TestKeywordImpactDisabledDoesNotProjectOrRequireAcknowledgement(t *testing.T) {
	impact, err := keywordImpactForProposal(context.Background(), &keywordProjectionRepository{
		sources:     []Source{{ID: "target", Kind: KindDomain, ActiveVersionID: "version"}},
		sourceRules: map[string][]SourceRule{"version": {{RuleType: "DOMAIN-KEYWORD", Domain: "video"}}},
	}, &keywordProjectionRepository{
		sources:     []Source{{ID: "target", Kind: KindDomain, ActiveVersionID: "version"}},
		sourceRules: map[string][]SourceRule{"version": {{RuleType: "DOMAIN-KEYWORD", Domain: "video"}}},
	}, &PolicyProposal{RoutingRule: &RoutingRule{ID: "rule", IncludeKeywordDomains: false, TargetListIDs: []string{"target"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if impact.Enabled || impact.ProjectedCount != 0 || impact.RequiresConfirmation {
		t.Fatalf("disabled keyword option must not project or require acknowledgement: %#v", impact)
	}
}

func TestKeywordImpactIntroducesOnlyNewKeywordsAfterTargetChange(t *testing.T) {
	base := &keywordProjectionRepository{
		sources:      []Source{{ID: "old-target", Kind: KindDomain, ActiveVersionID: "old-version"}},
		routingRules: []RoutingRule{{ID: "rule", IncludeKeywordDomains: true, TargetListIDs: []string{"old-target"}}},
		sourceRules:  map[string][]SourceRule{"old-version": {{RuleType: "DOMAIN-KEYWORD", Domain: "old"}}},
	}
	planned := &keywordProjectionRepository{
		sources: []Source{
			{ID: "old-target", Kind: KindDomain, ActiveVersionID: "old-version"},
			{ID: "new-target", Kind: KindDomain, ActiveVersionID: "new-version"},
		},
		sourceRules: map[string][]SourceRule{"old-version": {{RuleType: "DOMAIN-KEYWORD", Domain: "old"}}, "new-version": {{RuleType: "DOMAIN-KEYWORD", Domain: "new"}, {RuleType: "DOMAIN-KEYWORD", Domain: "old"}}},
	}
	proposal := &PolicyProposal{RoutingRule: &RoutingRule{ID: "rule", IncludeKeywordDomains: true, TargetListIDs: []string{"new-target"}}}
	impact, err := keywordImpactForProposal(context.Background(), base, planned, proposal, nil)
	if err != nil {
		t.Fatal(err)
	}
	if impact.RequiresConfirmation || !reflect.DeepEqual(impact.IntroducedKeywords, []string{"new"}) {
		t.Fatalf("target change should report introduced keywords without confirmation: %#v", impact)
	}

	// Turning the option back on is already the complete explicit opt-in,
	// even when the target keyword set itself is unchanged.
	base.routingRules[0].IncludeKeywordDomains = false
	impact, err = keywordImpactForProposal(context.Background(), base, planned, proposal, nil)
	if err != nil {
		t.Fatal(err)
	}
	if impact.RequiresConfirmation || !reflect.DeepEqual(impact.IntroducedKeywords, []string{"new", "old"}) {
		t.Fatalf("re-enabling keyword projection should not require confirmation: %#v", impact)
	}
}
