package policy

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"strings"
	"testing"
)

func TestParseDomainLinesSupportsClashAndMosdnsForms(t *testing.T) {
	result, err := ParseDomainLines(strings.Join([]string{
		"- DOMAIN,ad.com,REJECT",
		"- DOMAIN-SUFFIX,google.com,auto",
		"DOMAIN,Example.COM.",
		"DOMAIN-SUFFIX, Example.COM.",
		"example.com",
		"domain:netbird.io",
		"full:www.dingtalkcs.com",
		"- example.org",
		"bücher.de",
	}, "\n"))
	if err != nil {
		t.Fatalf("ParseDomainLines() error = %v", err)
	}

	got := map[string]bool{}
	for _, rule := range result.Rules {
		got[string(rule.Type)+":"+rule.Domain] = true
	}
	for _, key := range []string{
		"DOMAIN:ad.com",
		"DOMAIN-SUFFIX:google.com",
		"DOMAIN:example.com",
		"DOMAIN-SUFFIX:example.com",
		"DOMAIN-SUFFIX:netbird.io",
		"DOMAIN:www.dingtalkcs.com",
		"DOMAIN-SUFFIX:example.org",
		"DOMAIN-SUFFIX:xn--bcher-kva.de",
	} {
		if !got[key] {
			t.Errorf("missing normalized rule %q, got %v", key, result.Rules)
		}
	}
	if len(result.Rules) != 8 {
		t.Fatalf("rule count = %d, want 8: %#v", len(result.Rules), result.Rules)
	}
	if result.Ignored["duplicate"] != 1 {
		t.Errorf("duplicate count = %d, want 1 (plain example.com repeats DOMAIN-SUFFIX)", result.Ignored["duplicate"])
	}
}

func TestParseDomainLinesIgnoresBlankLinesAndEmptyInput(t *testing.T) {
	result, err := ParseDomainLines("# NAME: source\n\n  \n\t\n# DOMAIN: 1\n")
	if err != nil {
		t.Fatalf("ParseDomainLines() error = %v", err)
	}
	if len(result.Rules) != 0 || len(result.Ignored) != 0 {
		t.Fatalf("blank input produced %#v", result)
	}
}

func TestParseDomainLinesCategorizesIgnoredLines(t *testing.T) {
	result, err := ParseDomainLines(strings.Join([]string{
		"keyword:ads.example.com",
		"regexp:^ads\\.",
		"include:other-list",
		"IP-CIDR,10.0.0.0/8",
		"DOMAIN-KEYWORD,ads",
		"DOMAIN,192.168.1.1",
		"not a domain",
		",missing-type.com",
	}, "\n"))
	if err != nil {
		t.Fatalf("ParseDomainLines() error = %v", err)
	}
	if len(result.Rules) != 0 {
		t.Fatalf("expected no valid rules, got %#v", result.Rules)
	}
	for category, want := range map[string]int{
		"keyword":        1,
		"regexp":         1,
		"include":        1,
		"IP-CIDR":        1,
		"DOMAIN-KEYWORD": 1,
		"DOMAIN":         1,
		"DOMAIN-SUFFIX":  1,
		"invalid":        1,
	} {
		if result.Ignored[category] != want {
			t.Errorf("ignored[%s] = %d, want %d (all: %v)", category, result.Ignored[category], want, result.Ignored)
		}
	}
	if len(result.ErrorSamples) == 0 {
		t.Fatal("expected bounded error samples")
	}
}

func TestParseDomainLinesIsCaseInsensitiveOnTypesAndPrefixes(t *testing.T) {
	result, err := ParseDomainLines("domain,Ad.COM\nFull:X.COM\nDomain:Y.COM\nDOMAIN-suffix,z.com")
	if err != nil {
		t.Fatalf("ParseDomainLines() error = %v", err)
	}
	got := map[string]bool{}
	for _, rule := range result.Rules {
		got[string(rule.Type)+":"+rule.Domain] = true
	}
	for _, key := range []string{
		"DOMAIN:ad.com",
		"DOMAIN:x.com",
		"DOMAIN-SUFFIX:y.com",
		"DOMAIN-SUFFIX:z.com",
	} {
		if !got[key] {
			t.Errorf("missing rule %q, got %v", key, result.Rules)
		}
	}
}

func TestParseDomainLinesRejectsOversizeAndTooManyRules(t *testing.T) {
	oversize := strings.Repeat("a.com\n", MaxSourceBytes/6+1)
	if _, err := ParseDomainLines(oversize); err == nil {
		t.Fatal("oversize text was accepted")
	}

	var builder strings.Builder
	for i := 0; i < MaxSourceRules+1; i++ {
		fmt.Fprintf(&builder, "host-%d.example.com\n", i)
	}
	if _, err := ParseDomainLines(builder.String()); err == nil {
		t.Fatal("too many valid rules were accepted")
	}
}

func TestPrepareDomainLinesRoundTripsThroughClashParser(t *testing.T) {
	prepared, err := PrepareDomainLines("- DOMAIN,ad.com,REJECT\n- DOMAIN-SUFFIX,google.com,auto\nfull:www.dingtalkcs.com\nexample.com\nnot a domain")
	if err != nil {
		t.Fatalf("PrepareDomainLines() error = %v", err)
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
	reparsed, err := ParseClashYAML(decompressed)
	if err != nil {
		t.Fatalf("ParseClashYAML(synthesized) error = %v: %s", err, decompressed)
	}
	if len(reparsed.Rules) != 4 {
		t.Fatalf("round-trip rule count = %d, want 4: %#v", len(reparsed.Rules), reparsed.Rules)
	}
	got := map[string]bool{}
	for _, rule := range reparsed.Rules {
		got[string(rule.Type)+":"+rule.Domain] = true
	}
	for _, key := range []string{
		"DOMAIN:ad.com",
		"DOMAIN-SUFFIX:google.com",
		"DOMAIN:www.dingtalkcs.com",
		"DOMAIN-SUFFIX:example.com",
	} {
		if !got[key] {
			t.Errorf("round-trip missing rule %q", key)
		}
	}
}
