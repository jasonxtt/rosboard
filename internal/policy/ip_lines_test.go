package policy

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"strings"
	"testing"
)

func TestParseIPLinesMixedFormats(t *testing.T) {
	text := strings.Join([]string{
		"IP-CIDR,91.108.0.0/16",
		"IP-CIDR6,2001:67c:4e8::/48",
		"91.108.0.0/16",
		"1.1.1.1",
		"2001:4860:4860::8888",
		"- IP-CIDR,8.8.8.0/24,no-resolve",
		"- IP-CIDR6,2606:4700::/32,no-resolve",
		"1.1.1.0/24,PROXY",
	}, "\n")
	result, err := ParseIPLines(text)
	if err != nil {
		t.Fatal(err)
	}
	want := []ParsedRule{
		{Type: RuleTypeIPCIDR, Domain: "91.108.0.0/16"},
		{Type: RuleTypeIP6, Domain: "2001:67c:4e8::/48"},
		{Type: RuleTypeIPCIDR, Domain: "1.1.1.1"},
		{Type: RuleTypeIP6, Domain: "2001:4860:4860::8888"},
		{Type: RuleTypeIPCIDR, Domain: "8.8.8.0/24"},
		{Type: RuleTypeIP6, Domain: "2606:4700::/32"},
	}
	if len(result.Rules) != len(want) {
		t.Fatalf("rules=%d want %d: %#v", len(result.Rules), len(want), result.Rules)
	}
	for i, rule := range want {
		if result.Rules[i] != rule {
			t.Fatalf("rule %d = %#v, want %#v", i, result.Rules[i], rule)
		}
	}
	if result.Ignored["duplicate"] != 1 {
		t.Fatalf("duplicate count = %d, want 1: %#v", result.Ignored["duplicate"], result.Ignored)
	}
}

func TestParseIPLinesCanonicalizesHostBits(t *testing.T) {
	result, err := ParseIPLines("91.108.5.5/16\n2001:67c:4e8::ffff/48")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rules) != 2 {
		t.Fatalf("rules=%#v", result.Rules)
	}
	if result.Rules[0].Domain != "91.108.0.0/16" || result.Rules[0].Type != RuleTypeIPCIDR {
		t.Fatalf("ipv4 rule = %#v", result.Rules[0])
	}
	if result.Rules[1].Domain != "2001:67c:4e8::/48" || result.Rules[1].Type != RuleTypeIP6 {
		t.Fatalf("ipv6 rule = %#v", result.Rules[1])
	}
}

func TestParseIPLinesFamilyMismatchesAndInvalid(t *testing.T) {
	text := strings.Join([]string{
		"IP-CIDR,2001:67c:4e8::/48",
		"IP-CIDR6,91.108.0.0/16",
		"999.1.2.3",
		"91.108.0.0/99",
		"example.com",
		"DOMAIN,example.com",
		"1.2.3.4:53",
	}, "\n")
	result, err := ParseIPLines(text)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rules) != 0 {
		t.Fatalf("expected no valid rules, got %#v", result.Rules)
	}
	if result.Ignored["IP-CIDR"] != 1 || result.Ignored["IP-CIDR6"] != 1 {
		t.Fatalf("family mismatches not reported: %#v", result.Ignored)
	}
	if result.Ignored["DOMAIN"] != 1 || result.Ignored["invalid"] != 4 {
		t.Fatalf("invalid categories wrong: %#v", result.Ignored)
	}
	if len(result.ErrorSamples) == 0 {
		t.Fatal("expected error samples")
	}
}

func TestParseIPLinesRejectsStructuralProblems(t *testing.T) {
	var many strings.Builder
	for i := 0; i <= MaxSourceRules; i++ {
		fmt.Fprintf(&many, "10.%d.%d.%d\n", i/65536, (i/256)%256, i%256)
	}
	if _, err := ParseIPLines(many.String()); err == nil {
		t.Fatal("expected too-many-rules error")
	}
	if _, err := ParseIPLines(strings.Repeat("a", MaxSourceBytes+1)); err == nil {
		t.Fatal("expected oversize error")
	}
}

func TestParseClashYAMLIPExtractsOnlyIPRules(t *testing.T) {
	payload := []byte("payload:\n" +
		"  - IP-CIDR,91.108.0.0/16,no-resolve\n" +
		"  - IP-CIDR6,2001:67c:4e8::/48,no-resolve\n" +
		"  - DOMAIN-SUFFIX,example.com\n" +
		"  - IP-CIDR,91.108.0.0/16,DIRECT\n" +
		"  - not-an-ip-rule\n")
	result, err := ParseClashYAMLIP(payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rules) != 2 {
		t.Fatalf("rules=%#v", result.Rules)
	}
	if result.Rules[0].Type != RuleTypeIPCIDR || result.Rules[0].Domain != "91.108.0.0/16" {
		t.Fatalf("rule 0 = %#v", result.Rules[0])
	}
	if result.Rules[1].Type != RuleTypeIP6 || result.Rules[1].Domain != "2001:67c:4e8::/48" {
		t.Fatalf("rule 1 = %#v", result.Rules[1])
	}
	if result.Ignored["DOMAIN-SUFFIX"] != 1 || result.Ignored["invalid"] != 1 || result.Ignored["duplicate"] != 1 {
		t.Fatalf("ignored = %#v", result.Ignored)
	}

	// The domain parser must not start eating IP rules.
	domain, err := ParseClashYAML(payload)
	if err != nil || len(domain.Rules) != 1 || domain.Rules[0].Type != RuleTypeSuffix {
		t.Fatalf("domain parser changed on mixed payload: %#v err=%v", domain.Rules, err)
	}
}

func TestParseClashYAMLIPRequiresIPRules(t *testing.T) {
	if _, err := ParseClashYAMLIP([]byte("payload:\n  - DOMAIN-SUFFIX,example.com\n")); err == nil {
		t.Fatal("expected no-valid-IP-rules error")
	}
	if _, err := ParseClashYAMLIP([]byte("payload:\n  - IP-CIDR,91.108.0.0/16\n")); err != nil {
		t.Fatalf("valid payload rejected: %v", err)
	}
}

func TestPrepareIPLinesRoundTripsThroughClashParser(t *testing.T) {
	prepared, err := PrepareIPLines("1.1.1.1\n2001:4860:4860::8888\n91.108.0.0/16")
	if err != nil {
		t.Fatalf("PrepareIPLines() error = %v", err)
	}
	if prepared.Size == 0 || prepared.SHA256 == "" || len(prepared.CompressedYAML) == 0 {
		t.Fatalf("prepared content incomplete: %#v", prepared)
	}
	reader, err := gzip.NewReader(bytes.NewReader(prepared.CompressedYAML))
	if err != nil {
		t.Fatalf("gzip.NewReader() error = %v", err)
	}
	decompressed, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("gzip read error = %v", err)
	}
	reparsed, err := ParseClashYAMLIP(decompressed)
	if err != nil {
		t.Fatalf("ParseClashYAMLIP(synthesized) error = %v: %s", err, decompressed)
	}
	if len(reparsed.Rules) != 3 {
		t.Fatalf("round-trip rule count = %d, want 3: %#v", len(reparsed.Rules), reparsed.Rules)
	}
	got := map[string]bool{}
	for _, rule := range reparsed.Rules {
		got[string(rule.Type)+":"+rule.Domain] = true
	}
	for _, key := range []string{
		"IP-CIDR:1.1.1.1",
		"IP-CIDR6:2001:4860:4860::8888",
		"IP-CIDR:91.108.0.0/16",
	} {
		if !got[key] {
			t.Errorf("round-trip missing rule %q", key)
		}
	}
}
