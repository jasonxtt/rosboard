package policy

import (
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
	if _, err := PrepareSourceContent([]byte(nodes.String())); err == nil {
		t.Fatal("YAML node-count limit was not enforced")
	}

	var scalar strings.Builder
	scalar.WriteString("payload:\n  - \"")
	scalar.WriteString(strings.Repeat("a", maxYAMLScalarBytes+1))
	scalar.WriteString("\"\n")
	if _, err := PrepareSourceContent([]byte(scalar.String())); err == nil {
		t.Fatal("YAML scalar-size limit was not enforced")
	}
}

func TestPrepareSourceContentProducesSharedPendingMaterialization(t *testing.T) {
	body := []byte("payload:\n  - DOMAIN,example.com\n")
	prepared, err := PrepareSourceContent(body)
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
