package policy

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

func TestPrepareSourceContentEnforcesYAMLNodeAndScalarLimits(t *testing.T) {
	var nodes strings.Builder
	nodes.WriteString("root:\n")
	for i := 0; i < maxYAMLNodes/2+1; i++ {
		fmt.Fprintf(&nodes, "  key-%d: value\n", i)
	}
	if _, err := PrepareSourceContent([]byte(nodes.String()), KindDomain); err == nil {
		t.Fatal("YAML node-count limit was not enforced")
	}

	var scalar strings.Builder
	scalar.WriteString("payload:\n  - \"")
	scalar.WriteString(strings.Repeat("a", maxYAMLScalarBytes+1))
	scalar.WriteString("\"\n")
	if _, err := PrepareSourceContent([]byte(scalar.String()), KindDomain); err == nil {
		t.Fatal("YAML scalar-size limit was not enforced")
	}
}

func TestPrepareSourceContentProducesSharedPendingMaterialization(t *testing.T) {
	body := []byte("payload:\n  - DOMAIN,example.com\n")
	prepared, err := PrepareSourceContent(body, KindDomain)
	if err != nil {
		t.Fatalf("PrepareSourceContent() error = %v", err)
	}
	if prepared.Size != int64(len(body)) || prepared.SHA256 == "" || len(prepared.CompressedYAML) == 0 || len(prepared.Rules) != 1 {
		t.Fatalf("incomplete prepared source: %#v", prepared)
	}
	version, rules, err := prepared.PendingVersion("router-a", "source-a", "version-a")
	if err != nil {
		t.Fatalf("PendingVersion() error = %v", err)
	}
	if version.SHA256 != prepared.SHA256 || version.State != "pending" || len(rules) != 1 || rules[0].Domain != "example.com" {
		t.Fatalf("shared pending materialization mismatch: %#v %#v", version, rules)
	}
}

func TestPrepareSourceContentAcceptsClashAndPlainLineListsByKind(t *testing.T) {
	tests := []struct {
		name          string
		kind          string
		body          string
		wantRules     []ParsedRule
		wantIgnored   map[string]int
		wantRawSHA256 bool
	}{
		{
			name: "domain clash list",
			kind: KindDomain,
			body: strings.Join([]string{
				"# NAME: Netflix",
				"DOMAIN,e13252.dscg.akamaiedge.net",
				"DOMAIN-SUFFIX,Netflix.COM,REJECT",
				"IP-CIDR,103.87.204.0/22,no-resolve",
				"DOMAIN-KEYWORD,netflix",
				"plain.example",
			}, "\n"),
			wantRules: []ParsedRule{
				{Type: RuleTypeExact, Domain: "e13252.dscg.akamaiedge.net"},
				{Type: RuleTypeSuffix, Domain: "netflix.com"},
				{Type: RuleTypeSuffix, Domain: "plain.example"},
			},
			wantIgnored:   map[string]int{"IP-CIDR": 1, "DOMAIN-KEYWORD": 1},
			wantRawSHA256: true,
		},
		{
			name: "ip plain list",
			kind: KindIP,
			body: strings.Join([]string{
				"# IP-CIDR: 2",
				"103.87.205.9/22",
				"2001:db8:1::123/48",
				"DOMAIN-SUFFIX,netflix.com",
				"PROCESS-NAME,Netflix.exe",
			}, "\n"),
			wantRules: []ParsedRule{
				{Type: RuleTypeIPCIDR, Domain: "103.87.204.0/22"},
				{Type: RuleTypeIP6, Domain: "2001:db8:1::/48"},
			},
			wantIgnored:   map[string]int{"DOMAIN-SUFFIX": 1, "PROCESS-NAME": 1},
			wantRawSHA256: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prepared, err := PrepareSourceContent([]byte(tt.body), tt.kind)
			if err != nil {
				t.Fatalf("PrepareSourceContent() error = %v", err)
			}
			if len(prepared.Rules) != len(tt.wantRules) {
				t.Fatalf("rules = %#v, want %#v", prepared.Rules, tt.wantRules)
			}
			for i, want := range tt.wantRules {
				if prepared.Rules[i] != want {
					t.Errorf("rule %d = %#v, want %#v", i, prepared.Rules[i], want)
				}
			}
			for category, want := range tt.wantIgnored {
				if prepared.Ignored[category] != want {
					t.Errorf("ignored[%s] = %d, want %d: %#v", category, prepared.Ignored[category], want, prepared.Ignored)
				}
			}
			if prepared.Ignored["invalid"] != 0 {
				t.Errorf("comments were treated as invalid: %#v", prepared.Ignored)
			}
			if tt.wantRawSHA256 {
				digest := sha256.Sum256([]byte(tt.body))
				if prepared.SHA256 != hex.EncodeToString(digest[:]) {
					t.Fatalf("SHA-256 = %q, want raw content hash", prepared.SHA256)
				}
			}
		})
	}
}

func TestPrepareSourceContentRejectsLineListsWithoutApplicableRules(t *testing.T) {
	for _, tt := range []struct {
		kind string
		want string
	}{
		{kind: KindDomain, want: "no valid domain rules"},
		{kind: KindIP, want: "no valid IP rules"},
	} {
		_, err := PrepareSourceContent([]byte("# only comments\nPROCESS-NAME,app\n"), tt.kind)
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Fatalf("kind %q error = %v, want %q", tt.kind, err, tt.want)
		}
	}
}
