package policy

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/idna"
	"gopkg.in/yaml.v3"
)

const (
	MaxSourceBytes = 5 << 20
	MaxSourceRules = 20_000

	maxYAMLNodes       = 100_000
	maxYAMLScalarBytes = 64 << 10
	maxErrorSamples    = 20
	maxErrorSampleSize = 256
)

type RuleType string

const (
	RuleTypeExact  RuleType = "DOMAIN"
	RuleTypeSuffix RuleType = "DOMAIN-SUFFIX"
	RuleTypeIPCIDR RuleType = "IP-CIDR"
	RuleTypeIP6    RuleType = "IP-CIDR6"
)

type ParsedRule struct {
	Type   RuleType
	Domain string
}

type ParseResult struct {
	Rules        []ParsedRule
	Ignored      map[string]int
	ErrorSamples []string
	SHA256       string
}

// ParseClashYAML accepts only the top-level Clash payload form used by policy
// sources. Unsupported and malformed entries are reported in bounded summary
// fields; structural errors fail the entire source.
func ParseClashYAML(payload []byte) (ParseResult, error) {
	return parseClashYAML(payload, KindDomain)
}

// ParseClashYAMLIP is the IP-list variant of ParseClashYAML: it extracts only
// IP-CIDR / IP-CIDR6 rules from the same safe payload reading.
func ParseClashYAMLIP(payload []byte) (ParseResult, error) {
	return parseClashYAML(payload, KindIP)
}

func parseClashYAML(payload []byte, kind string) (ParseResult, error) {
	result := ParseResult{Ignored: make(map[string]int)}
	hash := sha256.Sum256(payload)
	result.SHA256 = hex.EncodeToString(hash[:])
	if len(payload) > MaxSourceBytes {
		return result, fmt.Errorf("source exceeds %d bytes", MaxSourceBytes)
	}
	if !utf8.Valid(payload) {
		return result, errors.New("source is not valid UTF-8")
	}

	decoder := yaml.NewDecoder(bytes.NewReader(payload))
	var document yaml.Node
	if err := decoder.Decode(&document); err != nil {
		return result, fmt.Errorf("decode source YAML: %w", err)
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return result, errors.New("source must contain exactly one YAML document")
		}
		return result, fmt.Errorf("decode extra YAML document: %w", err)
	}
	if document.Kind == yaml.DocumentNode {
		if len(document.Content) != 1 {
			return result, errors.New("source YAML document is empty")
		}
		document = *document.Content[0]
	}
	if err := validateYAMLTree(&document); err != nil {
		return result, err
	}
	if document.Kind != yaml.MappingNode {
		return result, errors.New("source YAML root must be a mapping")
	}

	var payloadNode *yaml.Node
	for i := 0; i+1 < len(document.Content); i += 2 {
		key, value := document.Content[i], document.Content[i+1]
		if key.Kind == yaml.ScalarNode && key.Value == "payload" {
			payloadNode = value
			break
		}
	}
	if payloadNode == nil || payloadNode.Kind != yaml.SequenceNode {
		return result, errors.New("source YAML payload must be a sequence")
	}

	seen := make(map[string]struct{}, len(payloadNode.Content))
	for _, item := range payloadNode.Content {
		if item.Kind != yaml.ScalarNode || item.Tag != "!!str" {
			addIgnored(&result, "invalid", "payload item is not a string")
			continue
		}
		var ruleType RuleType
		var value string
		var err error
		if kind == KindIP {
			ruleType, value, err = parseIPRule(item.Value)
		} else {
			ruleType, value, err = parseRule(item.Value)
		}
		if err != nil {
			if ruleType != "" {
				addIgnored(&result, string(ruleType), err.Error())
			} else {
				addIgnored(&result, "invalid", err.Error())
			}
			continue
		}
		key := string(ruleType) + "\x00" + value
		if _, exists := seen[key]; exists {
			addIgnored(&result, "duplicate", "duplicate rule")
			continue
		}
		seen[key] = struct{}{}
		result.Rules = append(result.Rules, ParsedRule{Type: ruleType, Domain: value})
		if len(result.Rules) > MaxSourceRules {
			return result, fmt.Errorf("source contains more than %d valid rules", MaxSourceRules)
		}
	}
	if len(result.Rules) == 0 {
		if kind == KindIP {
			return result, errors.New("source contains no valid IP rules")
		}
		return result, errors.New("source contains no valid domain rules")
	}
	return result, nil
}

func validateYAMLTree(root *yaml.Node) error {
	seen := make(map[*yaml.Node]struct{})
	count := 0
	var walk func(*yaml.Node) error
	walk = func(node *yaml.Node) error {
		if node == nil {
			return nil
		}
		if _, ok := seen[node]; ok {
			return nil
		}
		seen[node] = struct{}{}
		count++
		if count > maxYAMLNodes {
			return fmt.Errorf("source YAML contains more than %d nodes", maxYAMLNodes)
		}
		if node.Kind == yaml.ScalarNode && len(node.Value) > maxYAMLScalarBytes {
			return fmt.Errorf("source YAML scalar exceeds %d bytes", maxYAMLScalarBytes)
		}
		for _, child := range node.Content {
			if err := walk(child); err != nil {
				return err
			}
		}
		return nil
	}
	return walk(root)
}

func parseRule(raw string) (RuleType, string, error) {
	separator := strings.IndexByte(raw, ',')
	if separator < 0 {
		return "", "", errors.New("rule has no type separator")
	}
	ruleType := strings.TrimSpace(raw[:separator])
	domainText := strings.TrimSpace(raw[separator+1:])
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

func normalizeDomain(domain string) (string, error) {
	if domain == "" {
		return "", errors.New("domain is empty")
	}
	if strings.HasSuffix(domain, ".") {
		domain = strings.TrimSuffix(domain, ".")
	}
	if domain == "" || strings.HasSuffix(domain, ".") || strings.Contains(domain, "..") {
		return "", errors.New("domain has an empty label")
	}
	for _, r := range domain {
		if unicode.IsControl(r) || unicode.IsSpace(r) || strings.ContainsRune("/*?@", r) {
			return "", errors.New("domain contains an invalid character")
		}
	}
	ascii, err := idna.Lookup.ToASCII(strings.ToLower(domain))
	if err != nil {
		return "", fmt.Errorf("domain is not valid IDNA: %w", err)
	}
	ascii = strings.TrimSuffix(ascii, ".")
	if ascii == "" || len(ascii) > 253 || net.ParseIP(ascii) != nil {
		return "", errors.New("domain is not a hostname")
	}
	for _, label := range strings.Split(ascii, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", errors.New("domain label is invalid")
		}
	}
	return ascii, nil
}

func addIgnored(result *ParseResult, category, sample string) {
	category = boundedErrorText(category)
	result.Ignored[category]++
	if len(result.ErrorSamples) < maxErrorSamples {
		result.ErrorSamples = append(result.ErrorSamples, category+": "+boundedErrorText(sample))
	}
}

func boundedErrorText(value string) string {
	if len(value) <= maxErrorSampleSize {
		return value
	}
	end := maxErrorSampleSize
	for end > 0 && !utf8.ValidString(value[:end]) {
		end--
	}
	return value[:end] + "..."
}
