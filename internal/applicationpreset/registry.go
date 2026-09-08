// Package applicationpreset contains the source-controlled application
// catalog used by the canonical policy model. Presets are metadata; selected
// rules are fetched lazily and materialized as ordinary hidden TargetLists.
package applicationpreset

import (
	_ "embed"
	"encoding/json"
	"net/url"
	"sort"
	"strings"
)

const (
	rawRuleBaseURL = "https://raw.githubusercontent.com/blackmatrix7/ios_rule_script/master/"
	cdnRuleBaseURL = "https://cdn.jsdelivr.net/gh/blackmatrix7/ios_rule_script@master/"
)

// catalogJSON is generated from Blackmatrix7's Clash tree by
// tools/generate_application_catalog.go. Keeping relative paths in the
// catalog makes the runtime independent of the GitHub API and lets the
// selector fetch only the YAML chosen by the user.
//
//go:embed catalog.json
var catalogJSON []byte

type ApplicationPreset struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Category string   `json:"category,omitempty"`
	Aliases  []string `json:"aliases,omitempty"`
	RulePath string   `json:"rulePath,omitempty"`
	RuleURL  string   `json:"ruleURL"`
}

// CDNRuleURL returns the only supported transport fallback for a built-in
// preset. The canonical RuleURL remains the Blackmatrix7 GitHub URL; callers
// must use the returned URL only for a retry, never as provenance.
func CDNRuleURL(preset ApplicationPreset) (string, bool) {
	rulePath, ok := trustedRulePath(preset.RulePath)
	if !ok || preset.RuleURL != rawRuleURL(rulePath) {
		return "", false
	}
	return cdnRuleBaseURL + rulePath, true
}

func trustedRulePath(rawPath string) (string, bool) {
	rulePath := strings.TrimSpace(rawPath)
	if rulePath == "" || strings.HasPrefix(rulePath, "/") || !strings.HasPrefix(rulePath, "rule/Clash/") || !strings.HasSuffix(rulePath, ".yaml") {
		return "", false
	}
	for _, segment := range strings.Split(rulePath, "/") {
		decoded, err := url.PathUnescape(segment)
		if err != nil || decoded != segment || segment == "" || segment == "." || segment == ".." || strings.ContainsAny(segment, `\\?#`) {
			return "", false
		}
	}
	return rulePath, true
}

type DomainEntry struct {
	PresetID string
	Name     string
	Category string
	RuleType string
	Domain   string
}

type Match struct {
	Preset        ApplicationPreset
	MatchedDomain string
	Ambiguous     bool
}

type Registry struct {
	presets []ApplicationPreset
}

func Default() *Registry {
	var presets []ApplicationPreset
	if err := json.Unmarshal(catalogJSON, &presets); err != nil {
		panic("application preset catalog is invalid: " + err.Error())
	}
	return New(presets)
}

func New(presets []ApplicationPreset) *Registry {
	result := &Registry{presets: make([]ApplicationPreset, 0, len(presets))}
	seen := make(map[string]bool, len(presets))
	for _, preset := range presets {
		preset.ID = strings.TrimSpace(preset.ID)
		preset.Name = strings.TrimSpace(preset.Name)
		preset.Category = strings.TrimSpace(preset.Category)
		preset.RulePath = strings.TrimSpace(preset.RulePath)
		preset.RuleURL = strings.TrimSpace(preset.RuleURL)
		if preset.ID == "" || seen[preset.ID] {
			continue
		}
		if preset.Name == "" {
			preset.Name = preset.ID
		}
		if preset.Category == "" {
			preset.Category = "其他"
		}
		if preset.RuleURL == "" && preset.RulePath != "" {
			preset.RuleURL = rawRuleURL(preset.RulePath)
		}
		preset.Aliases = normalizeAliases(preset.Aliases, preset.Name, preset.ID)
		seen[preset.ID] = true
		result.presets = append(result.presets, preset)
	}
	sort.Slice(result.presets, func(i, j int) bool { return result.presets[i].ID < result.presets[j].ID })
	return result
}

func rawRuleURL(path string) string {
	return rawRuleBaseURL + strings.TrimPrefix(strings.TrimSpace(path), "/")
}

func normalizeAliases(values []string, name, id string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]bool, len(values)+2)
	for _, value := range append(append([]string{}, values...), name, id) {
		value = strings.TrimSpace(value)
		key := strings.ToLower(value)
		if value == "" || seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return strings.ToLower(result[i]) < strings.ToLower(result[j]) })
	return result
}

func (r *Registry) List() []ApplicationPreset {
	if r == nil {
		return []ApplicationPreset{}
	}
	return append([]ApplicationPreset(nil), r.presets...)
}

func (r *Registry) Get(id string) (ApplicationPreset, bool) {
	id = strings.TrimSpace(id)
	for _, preset := range r.List() {
		if preset.ID == id {
			return preset, true
		}
	}
	return ApplicationPreset{}, false
}

// MatchDomain follows TargetList DOMAIN/DOMAIN-SUFFIX semantics. Every
// matching preset participates in attribution; a match is ambiguous whenever
// more than one distinct preset matches, regardless of rule specificity.
func (r *Registry) MatchDomain(domain string, entries []DomainEntry) Match {
	domain = normalizeDomain(domain)
	if domain == "" {
		return Match{}
	}
	matchedDomains := make(map[string]string)
	for _, entry := range entries {
		if entry.PresetID == "" || entry.Domain == "" || (entry.RuleType != "DOMAIN" && entry.RuleType != "DOMAIN-SUFFIX") {
			continue
		}
		candidate := normalizeDomain(entry.Domain)
		if candidate == "" {
			continue
		}
		if entry.RuleType == "DOMAIN" && candidate != domain {
			continue
		}
		if entry.RuleType == "DOMAIN-SUFFIX" && candidate != domain && !strings.HasSuffix(domain, "."+candidate) {
			continue
		}
		if len(candidate) > len(matchedDomains[entry.PresetID]) {
			matchedDomains[entry.PresetID] = candidate
		}
	}
	if len(matchedDomains) == 0 {
		return Match{}
	}
	matchedDomain := ""
	for _, candidate := range matchedDomains {
		if len(candidate) > len(matchedDomain) {
			matchedDomain = candidate
		}
	}
	if len(matchedDomains) != 1 {
		return Match{MatchedDomain: matchedDomain, Ambiguous: true}
	}
	for presetID := range matchedDomains {
		preset, ok := r.Get(presetID)
		if !ok {
			return Match{}
		}
		return Match{Preset: preset, MatchedDomain: matchedDomain}
	}
	return Match{}
}

func normalizeDomain(value string) string {
	value = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(value, ".")))
	if value == "" || strings.Contains(value, "..") || strings.ContainsAny(value, "/ :") {
		return ""
	}
	for _, label := range strings.Split(value, ".") {
		if label == "" || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return ""
		}
	}
	return value
}
