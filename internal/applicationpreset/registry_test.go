package applicationpreset

import (
	"strings"
	"testing"
)

func TestDefaultRegistryCoversTheBM7ClashCatalog(t *testing.T) {
	registry := Default()
	presets := registry.List()
	if len(presets) < 600 {
		t.Fatalf("default registry size=%d, want the full bm7 Clash catalog", len(presets))
	}
	for i := 1; i < len(presets); i++ {
		if presets[i-1].ID >= presets[i].ID {
			t.Fatalf("default presets are not sorted and unique: %#v", presets)
		}
	}
	youtube, ok := registry.Get("youtube")
	if !ok || youtube.RulePath == "" || youtube.RuleURL == "" {
		t.Fatalf("YouTube preset is missing a generated relative path and URL: %#v", youtube)
	}
	if youtube.RuleURL != rawRuleBaseURL+youtube.RulePath {
		t.Fatalf("YouTube URL=%q, want fixed raw base plus relative path", youtube.RuleURL)
	}
	openAI, ok := registry.Get("openai")
	if !ok || !containsFold(openAI.Aliases, "ChatGPT") {
		t.Fatalf("OpenAI preset is missing the ChatGPT search alias: %#v", openAI)
	}
}

func containsFold(values []string, want string) bool {
	for _, value := range values {
		if strings.EqualFold(value, want) {
			return true
		}
	}
	return false
}

func TestRegistryMatchesMostSpecificRuleAndRejectsAmbiguity(t *testing.T) {
	registry := New([]ApplicationPreset{{ID: "one", Name: "One"}, {ID: "two", Name: "Two"}})
	entries := []DomainEntry{
		{PresetID: "one", RuleType: "DOMAIN-SUFFIX", Domain: "example.com"},
		{PresetID: "one", RuleType: "DOMAIN", Domain: "exact.example.com"},
		{PresetID: "two", RuleType: "DOMAIN-SUFFIX", Domain: "shared.example"},
		{PresetID: "one", RuleType: "DOMAIN-SUFFIX", Domain: "shared.example"},
		{PresetID: "one", RuleType: "DOMAIN-SUFFIX", Domain: "unsupported.example"},
	}
	match := registry.MatchDomain("exact.example.com", entries)
	if match.Ambiguous || match.Preset.ID != "one" || match.MatchedDomain != "exact.example.com" {
		t.Fatalf("exact rule did not win over a broader suffix: %#v", match)
	}
	match = registry.MatchDomain("api.example.com", entries)
	if match.Ambiguous || match.Preset.ID != "one" || match.MatchedDomain != "example.com" {
		t.Fatalf("suffix rule did not match: %#v", match)
	}
	match = registry.MatchDomain("api.shared.example", entries)
	if !match.Ambiguous || match.Preset.ID != "" || match.MatchedDomain != "shared.example" {
		t.Fatalf("same-specificity rules must remain ambiguous: %#v", match)
	}
	match = registry.MatchDomain("api.example.com", []DomainEntry{
		{PresetID: "one", RuleType: "DOMAIN-SUFFIX", Domain: "example.com"},
		{PresetID: "two", RuleType: "DOMAIN", Domain: "api.example.com"},
	})
	if !match.Ambiguous || match.Preset.ID != "" || match.MatchedDomain != "api.example.com" {
		t.Fatalf("different-specificity rules must remain ambiguous: %#v", match)
	}
	match = registry.MatchDomain("api.example.com", []DomainEntry{
		{PresetID: "one", RuleType: "DOMAIN-SUFFIX", Domain: "example.com"},
		{PresetID: "two", RuleType: "DOMAIN-SUFFIX", Domain: "api.example.com"},
	})
	if !match.Ambiguous || match.Preset.ID != "" || match.MatchedDomain != "api.example.com" {
		t.Fatalf("nested suffix rules must remain ambiguous: %#v", match)
	}
	match = registry.MatchDomain("api.unknown.example", []DomainEntry{{PresetID: "one", RuleType: "DOMAIN-KEYWORD", Domain: "unknown"}})
	if match.Preset.ID != "" || match.Ambiguous {
		t.Fatalf("unsupported rule type was treated as a domain match: %#v", match)
	}
}
