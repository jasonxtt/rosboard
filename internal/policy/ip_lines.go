package policy

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"unicode/utf8"
)

// Source content kinds. They mirror policyv2 source kinds; the parser layer
// only needs the plain values.
const (
	KindDomain = "domain"
	KindIP     = "ip"
)

// ParseIPLines parses manually entered IP rules, one entry per line. A line
// may be a bare IPv4/IPv6 address or CIDR, or the Clash single-line form with
// an optional YAML list marker and trailing policy fields
// ("- IP-CIDR,91.108.0.0/16,no-resolve"). IP-CIDR only accepts IPv4 and
// IP-CIDR6 only accepts IPv6; family mismatches, duplicates, and invalid lines
// are reported as ignored entries without affecting other rules. Structural
// errors (oversize input, too many valid rules) fail the whole result.
func ParseIPLines(text string) (ParseResult, error) {
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
		if line == "" {
			continue
		}
		ruleType, address, err := parseIPLine(line)
		if err != nil {
			if ruleType != "" {
				addIgnored(&result, string(ruleType), err.Error())
			} else {
				addIgnored(&result, "invalid", err.Error())
			}
			continue
		}
		key := string(ruleType) + "\x00" + address
		if _, exists := seen[key]; exists {
			addIgnored(&result, "duplicate", "duplicate rule")
			continue
		}
		seen[key] = struct{}{}
		result.Rules = append(result.Rules, ParsedRule{Type: ruleType, Domain: address})
		if len(result.Rules) > MaxSourceRules {
			return result, fmt.Errorf("source contains more than %d valid rules", MaxSourceRules)
		}
	}
	return result, nil
}

// PrepareIPLines parses manually entered IP rules and packages them as a
// prepared source whose stored content is an equivalent Clash YAML payload,
// sharing version storage and the RouterOS sync pipeline with the other
// source kinds.
func PrepareIPLines(text string) (PreparedSourceContent, error) {
	parsed, err := ParseIPLines(text)
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

func parseIPLine(line string) (RuleType, string, error) {
	if strings.Contains(line, ",") {
		return parseIPRule(line)
	}
	return bareIPRule(line)
}

// bareIPRule classifies a bare address/prefix by its actual family.
func bareIPRule(text string) (RuleType, string, error) {
	value, isIPv4, err := normalizeIPValue(text)
	if err != nil {
		return "", "", err
	}
	if isIPv4 {
		return RuleTypeIPCIDR, value, nil
	}
	return RuleTypeIP6, value, nil
}

func parseIPRule(raw string) (RuleType, string, error) {
	separator := strings.IndexByte(raw, ',')
	if separator < 0 {
		return "", "", errors.New("rule has no type separator")
	}
	ruleType := strings.ToUpper(strings.TrimSpace(raw[:separator]))
	var typ RuleType
	switch ruleType {
	case string(RuleTypeIPCIDR):
		typ = RuleTypeIPCIDR
	case string(RuleTypeIP6):
		typ = RuleTypeIP6
	default:
		if ruleType == "" {
			return "", "", errors.New("rule type is empty")
		}
		return RuleType(ruleType), "", errors.New("unsupported rule type")
	}
	// Everything past the address (policy name, no-resolve, ...) is ignored.
	text := strings.TrimSpace(raw[separator+1:])
	if next := strings.IndexByte(text, ','); next >= 0 {
		text = strings.TrimSpace(text[:next])
	}
	value, isIPv4, err := normalizeIPValue(text)
	if err != nil {
		return typ, "", err
	}
	if typ == RuleTypeIPCIDR && !isIPv4 {
		return typ, "", errors.New("IP-CIDR requires an IPv4 address")
	}
	if typ == RuleTypeIP6 && isIPv4 {
		return typ, "", errors.New("IP-CIDR6 requires an IPv6 address")
	}
	return typ, value, nil
}

// normalizeIPValue canonicalizes a bare IP or a masked CIDR prefix and reports
// whether it belongs to the IPv4 family.
func normalizeIPValue(text string) (string, bool, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", false, errors.New("address is empty")
	}
	if strings.Contains(text, "/") {
		prefix, err := netip.ParsePrefix(text)
		if err != nil {
			return "", false, errors.New("address is not a valid IP or CIDR")
		}
		masked := prefix.Masked()
		return masked.String(), masked.Addr().Is4(), nil
	}
	address, err := netip.ParseAddr(text)
	if err != nil {
		return "", false, errors.New("address is not a valid IP or CIDR")
	}
	if address.Zone() != "" {
		return "", false, errors.New("address must not carry a zone")
	}
	return address.String(), address.Is4(), nil
}
