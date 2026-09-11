package policyv2

import (
	"context"
	"reflect"
	"testing"

	"rosboard/internal/accesscontrol"
	"rosboard/internal/routeros"
)

func TestEscapeRouterOSDNSRegexLiteralUsesRouterOSLiteralEscapes(t *testing.T) {
	const value = `a.b\\c+d*e?f(g)h[i]j{k}l^m$n|o`
	const want = `a\.b\\\\c\+d\*e\?f\(g\)h\[i\]j\{k\}l\^m\$n\|o`
	if got := escapeRouterOSDNSRegexLiteral(value); got != want {
		t.Fatalf("escaped keyword = %q, want %q", got, want)
	}
	if got := routerOSDNSKeywordRegexp("video"); got != ".*video.*" {
		t.Fatalf("keyword regexp = %q", got)
	}
}

func TestRoutingKeywordProjectionUsesSeparateRegexpAndTargetList(t *testing.T) {
	allRules := []SourceRule{
		{RuleType: "DOMAIN-SUFFIX", Domain: "example.com"},
		{RuleType: "DOMAIN-KEYWORD", Domain: "video+"},
	}
	target := &routingTargetProjection{
		id: "target", source: Source{ID: "target", Name: "Target", Kind: KindDomain}, rules: allRules,
		plainRules: domainPlainRules(allRules), keywordRules: domainKeywordRules(allRules),
		list: "rb_rt_plain", keywordList: "rb_rtk_keyword", plainActive: true, keywordActive: true,
		keywordPriority: 100, keywordRuleID: "rule-keyword", keywordPrioritySet: true,
	}
	if got := routingTargetListsForRule(target, RoutingRule{}); !reflect.DeepEqual(got, []string{"rb_rt_plain"}) {
		t.Fatalf("keyword-disabled rule target lists = %#v, want plain list only", got)
	}
	if got := routingTargetListsForRule(target, RoutingRule{IncludeKeywordDomains: true}); !reflect.DeepEqual(got, []string{"rb_rt_plain", "rb_rtk_keyword"}) {
		t.Fatalf("keyword-enabled rule target lists = %#v, want both lists", got)
	}

	result := DesiredResult{}
	dnsStatics := make([]routingDNSStaticEntry, 0)
	add := func(logicalID string, menu routeros.MutationMenu, phase string, fields map[string]string) {
		result.Objects = append(result.Objects, DesiredObject{LogicalID: logicalID, Menu: string(menu), Phase: phase, Fields: fields})
	}
	buildRoutingDomainObjects(&result, add, Egress{ID: "wan", Name: "WAN", Enabled: true, DNSUpstream: "1.1.1.1", FakeAlias: "192.0.2.53"}, []EgressFamily{{Family: FamilyIPv4, Enabled: true, RouteTable: "wan-table"}}, []*routingTargetProjection{target}, "no", "manager", "device", map[string]int{"wan\x00target": 1}, &dnsStatics)
	if len(result.Blockers) != 0 {
		t.Fatalf("keyword projection produced blockers: %#v", result.Blockers)
	}
	if len(dnsStatics) != 2 {
		t.Fatalf("DNS static count = %d, want 2: %#v", len(dnsStatics), dnsStatics)
	}
	var plain, keyword *routingDNSStaticEntry
	for index := range dnsStatics {
		entry := &dnsStatics[index]
		if entry.keyword {
			keyword = entry
		} else {
			plain = entry
		}
	}
	if plain == nil || plain.fields["name"] != "example.com" || plain.fields["regexp"] != "" || plain.fields["address-list"] != "rb_rt_plain" {
		t.Fatalf("plain DNS static projection = %#v", plain)
	}
	if keyword == nil || keyword.fields["regexp"] != ".*video\\+.*" || keyword.fields["name"] != "" || keyword.fields["match-subdomain"] != "" || keyword.fields["address-list"] != "rb_rtk_keyword" {
		t.Fatalf("keyword DNS static projection = %#v", keyword)
	}

	// RouterOS evaluates regexp statics before ordinary name statics even when
	// the ordinary projection has a numerically higher-priority routing rule.
	sortRoutingDNSStaticEntries(dnsStatics)
	if !dnsStatics[0].keyword || dnsStatics[1].keyword {
		t.Fatalf("keyword DNS statics must be emitted before plain statics: %#v", dnsStatics)
	}
}

func TestRoutingKeywordProjectionAddsSeparateMangleMatchers(t *testing.T) {
	target := &routingTargetProjection{
		id: "target", source: Source{ID: "target", Kind: KindDomain},
		plainRules:   []SourceRule{{RuleType: "DOMAIN", Domain: "example.com"}},
		keywordRules: []SourceRule{{RuleType: "DOMAIN-KEYWORD", Domain: "video"}},
		list:         "rb_rt_plain", keywordList: "rb_rtk_keyword",
	}
	rule := RoutingRule{ID: "rule", EgressID: "wan", Enabled: true, IncludeKeywordDomains: true, TargetListIDs: []string{"target"}, Subject: Subject{Mode: SubjectModeAll}}
	result := DesiredResult{}
	add := func(logicalID string, menu routeros.MutationMenu, phase string, fields map[string]string) {
		result.Objects = append(result.Objects, DesiredObject{LogicalID: logicalID, Menu: string(menu), Phase: phase, Fields: fields})
	}
	buildRoutingMangleFamily(&result, add, Egress{ID: "wan", Enabled: true}, EgressFamily{Family: FamilyIPv4}, map[string]string{"rule": "ingress"}, map[string]bool{"rule": true}, "wan-table", []*routingTargetProjection{target}, []RoutingRule{rule}, nil, "no", "manager", "device")
	connections := make(map[string]string)
	for _, object := range result.Objects {
		if object.Fields["action"] == "mark-connection" && object.Fields["chain"] == "prerouting" {
			connections[object.LogicalID] = object.Fields["dst-address-list"]
		}
	}
	if connections["routing-rule-connection:rule:ipv4:target"] != "rb_rt_plain" || connections["routing-rule-connection:rule:ipv4:target:keyword"] != "rb_rtk_keyword" {
		t.Fatalf("keyword mangle matchers = %#v", connections)
	}
}

func TestActualDNSMatcherRecognizesKeywordIdentityWithColonScopedIDs(t *testing.T) {
	matcher, ok := actualDNSMatcher(ActualObject{
		LogicalID: "routing-dns:wan:preset:video:domain:DOMAIN-KEYWORD:video.player+",
		Menu:      string(routeros.MenuIPDNSStatic),
		Fields:    map[string]string{"regexp": ".*video\\.player\\+.*"},
	})
	if !ok || matcher.RuleType != "DOMAIN-KEYWORD" || matcher.Domain != "video.player+" {
		t.Fatalf("keyword actual matcher = %#v, ok=%v", matcher, ok)
	}
}

type keywordProjectionRepository struct {
	Repository
	RoutingRuleRepository
	sources      []Source
	egresses     []Egress
	routingRules []RoutingRule
	sourceRules  map[string][]SourceRule
}

func (r *keywordProjectionRepository) ListSources(context.Context, string) ([]Source, error) {
	return r.sources, nil
}

func (r *keywordProjectionRepository) ListEgresses(context.Context) ([]Egress, error) {
	return r.egresses, nil
}

func (r *keywordProjectionRepository) EnsureRoutingRulesMigrated(context.Context) error { return nil }
func (r *keywordProjectionRepository) RoutingAuthority(context.Context) (string, error) {
	return RoutingRuleAuthorityV1, nil
}
func (r *keywordProjectionRepository) ListRoutingRules(context.Context) ([]RoutingRule, error) {
	return r.routingRules, nil
}
func (r *keywordProjectionRepository) GetRoutingRule(_ context.Context, id string) (RoutingRule, error) {
	for _, rule := range r.routingRules {
		if rule.ID == id {
			return rule, nil
		}
	}
	return RoutingRule{}, ErrRoutingRuleNotFound
}
func (r *keywordProjectionRepository) ListSourceRules(_ context.Context, versionID string, _ RuleQuery) ([]SourceRule, bool, error) {
	return r.sourceRules[versionID], false, nil
}

type keywordProjectionAccessRepository struct {
	accesscontrol.Repository
	rules []accesscontrol.AccessRule
}

func (r *keywordProjectionAccessRepository) ListRules(context.Context) ([]accesscontrol.AccessRule, error) {
	return r.rules, nil
}

func TestRoutingKeywordAccessPrecedenceFailsClosedForPlainAccessMatchers(t *testing.T) {
	repository := &keywordProjectionRepository{
		sources:      []Source{{ID: "target", Kind: KindDomain, ActiveVersionID: "version"}},
		egresses:     []Egress{{ID: "wan", Enabled: true}},
		routingRules: []RoutingRule{{ID: "routing", Name: "Routing", EgressID: "wan", Enabled: true, IncludeKeywordDomains: true, TargetListIDs: []string{"target"}}},
		sourceRules:  map[string][]SourceRule{"version": {{RuleType: "DOMAIN-KEYWORD", Domain: "video"}, {RuleType: "DOMAIN", Domain: "video.example"}}},
	}
	access := &keywordProjectionAccessRepository{rules: []accesscontrol.AccessRule{{ID: "access", Name: "Access", Enabled: true, TargetScope: accesscontrol.TargetScopeTargets, TargetListIDs: []string{"target"}}}}
	result := DesiredResult{}
	if err := appendRoutingKeywordAccessPrecedenceBlockers(context.Background(), repository, access, nil, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Blockers) != 1 || result.Blockers[0].Code != routingKeywordAccessUnsafeCode {
		t.Fatalf("keyword/access overlap must fail closed: %#v", result.Blockers)
	}
}

func TestKeywordMayPrecedeAccessMatcherIsConservativeForSuffixRules(t *testing.T) {
	if !keywordMayPrecedeAccessMatcher([]string{"video"}, SourceRule{RuleType: "DOMAIN-SUFFIX", Domain: "example.com"}) {
		t.Fatal("any active keyword must conservatively block a suffix Access matcher")
	}
	if !keywordMayPrecedeAccessMatcher([]string{"video"}, SourceRule{RuleType: "DOMAIN", Domain: "video.example"}) {
		t.Fatal("matching keyword must block an exact Access matcher")
	}
	if keywordMayPrecedeAccessMatcher([]string{"video"}, SourceRule{RuleType: "DOMAIN", Domain: "example.com"}) {
		t.Fatal("nonmatching keyword must not block an exact Access matcher")
	}
}
