package policy

import (
	"fmt"
	"strings"
	"testing"
)

func TestParseClashYAMLNormalizesRulesAndReportsIgnoredEntries(t *testing.T) {
	result, err := ParseClashYAML([]byte(`
payload:
  - "DOMAIN,Example.COM."
  - "DOMAIN,example.com"
  - "DOMAIN-SUFFIX, Example.COM."
  - "DOMAIN-SUFFIX, bücher.de"
  - "IP-CIDR,192.0.2.0/24"
  - "DOMAIN,*.example.com"
  - "DOMAIN,not a domain"
`))
	if err != nil {
		t.Fatalf("ParseClashYAML() error = %v", err)
	}

	if got, want := len(result.Rules), 3; got != want {
		t.Fatalf("rule count = %d, want %d: %#v", got, want, result.Rules)
	}
	got := map[string]bool{}
	for _, rule := range result.Rules {
		got[string(rule.Type)+":"+rule.Domain] = true
	}
	for _, key := range []string{
		"DOMAIN:example.com",
		"DOMAIN-SUFFIX:example.com",
		"DOMAIN-SUFFIX:xn--bcher-kva.de",
	} {
		if !got[key] {
			t.Errorf("missing normalized rule %q", key)
		}
	}
	if result.Ignored["duplicate"] != 1 {
		t.Errorf("duplicate count = %d, want 1", result.Ignored["duplicate"])
	}
	if result.Ignored["IP-CIDR"] != 1 {
		t.Errorf("unsupported count = %d, want 1", result.Ignored["IP-CIDR"])
	}
	if result.Ignored["DOMAIN"] != 2 {
		t.Errorf("invalid DOMAIN count = %d, want 2", result.Ignored["DOMAIN"])
	}
	if result.SHA256 == "" || len(result.ErrorSamples) == 0 {
		t.Fatalf("parser result did not include hash and bounded error samples: %#v", result)
	}
}

func TestParseClashYAMLRejectsInvalidRootAndPayload(t *testing.T) {
	tests := []struct {
		name string
		yaml string
	}{
		{name: "root sequence", yaml: "- DOMAIN,example.com\n"},
		{name: "missing payload", yaml: "proxies: []\n"},
		{name: "payload scalar", yaml: "payload: DOMAIN,example.com\n"},
		{name: "payload non-string", yaml: "payload:\n  - 42\n"},
		{name: "zero valid", yaml: "payload:\n  - IP-CIDR,192.0.2.0/24\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ParseClashYAML([]byte(tt.yaml)); err == nil {
				t.Fatal("ParseClashYAML() error = nil, want error")
			}
		})
	}
}

func TestParseClashYAMLRejectsTooManyRulesAndOversizeInput(t *testing.T) {
	var builder strings.Builder
	builder.WriteString("payload:\n")
	for i := 0; i < MaxSourceRules+1; i++ {
		fmt.Fprintf(&builder, "  - DOMAIN,host-%d.example.com\n", i)
	}
	if _, err := ParseClashYAML([]byte(builder.String())); err == nil {
		t.Fatal("too many rules were accepted")
	}

	if _, err := ParseClashYAML(make([]byte, MaxSourceBytes+1)); err == nil {
		t.Fatal("oversize YAML was accepted")
	}
}

func TestParseClashYAMLRejectsInvalidUTF8AndMultipleDocuments(t *testing.T) {
	if _, err := ParseClashYAML([]byte{0xff}); err == nil {
		t.Fatal("invalid UTF-8 was accepted")
	}
	if _, err := ParseClashYAML([]byte("payload:\n  - DOMAIN,example.com\n---\npayload: []\n")); err == nil {
		t.Fatal("multiple YAML documents were accepted")
	}
}

func TestParseClashYAMLRejectsDoubleTrailingDot(t *testing.T) {
	if _, err := ParseClashYAML([]byte("payload:\n  - DOMAIN,example.com..\n")); err == nil {
		t.Fatal("domain with more than one trailing dot was accepted")
	}
}
