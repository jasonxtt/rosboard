package policy

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// ParseDomainLines parses domain rules from one-entry-per-line source content.
// Each line may use the Clash payload form with an optional YAML list marker
// and trailing policy name ("- DOMAIN,ad.com,REJECT"), or the mosdns form
// (plain domain, "domain:example.com", "full:example.com"). Plain and
// "domain:" entries match the domain and its subdomains (DOMAIN-SUFFIX);
// "full:" entries match exactly (DOMAIN). keyword:/regexp:/include: prefixes
// and unknown Clash rule types are reported as ignored entries. Structural
// errors (oversize input, too many valid rules) fail the whole result;
// per-line problems only populate the ignored summary.
func ParseDomainLines(text string) (ParseResult, error) {
	result := ParseResult{Ignored: make(map[string]int)}
	if len(text) > MaxSourceBytes {
		return result, fmt.Errorf("source exceeds %d bytes", MaxSourceBytes)
	}
	if !utf8.ValidString(text) {
		return result, errors.New("source is not valid UTF-8")
	}

	seen := make(map[string]struct{})
	for _, raw := range strings.Split(text, "\n") {
		line := stripDomainLineMarker(strings.TrimSpace(raw))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		ruleType, domain, err := parseDomainLine(line)
		if err != nil {
			if ruleType != "" {
				addIgnored(&result, string(ruleType), err.Error())
			} else {
				addIgnored(&result, "invalid", err.Error())
			}
			continue
		}
		key := string(ruleType) + "\x00" + domain
		if _, exists := seen[key]; exists {
			addIgnored(&result, "duplicate", "duplicate rule")
			continue
		}
		seen[key] = struct{}{}
		result.Rules = append(result.Rules, ParsedRule{Type: ruleType, Domain: domain})
		if len(result.Rules) > MaxSourceRules {
			return result, fmt.Errorf("source contains more than %d valid rules", MaxSourceRules)
		}
	}
	return result, nil
}

// PrepareDomainLines parses manually entered rules and packages them as a
// prepared source whose stored content is an equivalent Clash YAML payload,
// so manual sources share version storage and the RouterOS sync pipeline
// with URL and upload sources.
func PrepareDomainLines(text string) (PreparedSourceContent, error) {
	parsed, err := ParseDomainLines(text)
	if err != nil {
		return PreparedSourceContent{}, err
	}
	var builder strings.Builder
	builder.WriteString("payload:\n")
	for _, rule := range parsed.Rules {
		fmt.Fprintf(&builder, "  - \"%s,%s\"\n", rule.Type, rule.Domain)
	}
	yaml := []byte(builder.String())
	digest := sha256.Sum256(yaml)
	parsed.SHA256 = hex.EncodeToString(digest[:])
	compressed, err := gzipYAML(yaml)
	if err != nil {
		return PreparedSourceContent{}, err
	}
	return PreparedSourceContent{
		Size:           int64(len(yaml)),
		SHA256:         parsed.SHA256,
		CompressedYAML: compressed,
		ParseResult:    parsed,
	}, nil
}

// stripDomainLineMarker removes the YAML list marker ("- ") users paste in
// front of Clash payload lines. A hostname label can never start with "-",
// so stripping one leading dash is unambiguous.
func stripDomainLineMarker(line string) string {
	if !strings.HasPrefix(line, "-") {
		return line
	}
	line = strings.TrimSpace(line[1:])
	if len(line) >= 2 && strings.HasPrefix(line, "\"") && strings.HasSuffix(line, "\"") {
		return strings.TrimSpace(line[1 : len(line)-1])
	}
	return line
}

func parseDomainLine(line string) (RuleType, string, error) {
	lower := strings.ToLower(line)
	switch {
	case strings.HasPrefix(lower, "full:"):
		domain, err := normalizeDomain(strings.TrimSpace(line[len("full:"):]))
		if err != nil {
			return RuleTypeExact, "", err
		}
		return RuleTypeExact, domain, nil
	case strings.HasPrefix(lower, "domain:"):
		domain, err := normalizeDomain(strings.TrimSpace(line[len("domain:"):]))
		if err != nil {
			return RuleTypeSuffix, "", err
		}
		return RuleTypeSuffix, domain, nil
	case strings.HasPrefix(lower, "keyword:"), strings.HasPrefix(lower, "regexp:"), strings.HasPrefix(lower, "include:"):
		prefix, _, _ := strings.Cut(lower, ":")
		return RuleType(prefix), "", errors.New("unsupported rule type")
	}
	if !strings.Contains(line, ",") {
		domain, err := normalizeDomain(line)
		if err != nil {
			return RuleTypeSuffix, "", err
		}
		return RuleTypeSuffix, domain, nil
	}
	fields := strings.Split(line, ",")
	// Anything past the domain (e.g. ",REJECT" or ",auto") is the Clash
	// policy name and carries no meaning for DNS forwarding.
	ruleType := strings.ToUpper(strings.TrimSpace(fields[0]))
	domainText := strings.TrimSpace(fields[1])
	var typ RuleType
	switch ruleType {
	case string(RuleTypeExact):
		typ = RuleTypeExact
	case string(RuleTypeSuffix):
		typ = RuleTypeSuffix
	default:
		if ruleType == "" {
			return "", "", errors.New("rule type is empty")
		}
		return RuleType(ruleType), "", errors.New("unsupported rule type")
	}
	domain, err := normalizeDomain(domainText)
	if err != nil {
		return typ, "", err
	}
	return typ, domain, nil
}
