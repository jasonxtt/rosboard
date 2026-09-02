package accesscontrol

import (
	"strings"
	"testing"

	"rosboard/internal/subject"
)

func TestAccessRuleRejectsRoutingOnlyExcludedSubject(t *testing.T) {
	rule := AccessRule{
		ID: "access-excluded", Name: "Excluded", TargetScope: TargetScopeInternet,
		Subject: subject.Subject{Mode: subject.ModeExcluded, Prefixes: []string{"192.0.2.0/24"}},
	}
	if err := ValidateRule(rule); err == nil || !strings.Contains(err.Error(), "all or selected") {
		t.Fatalf("excluded access subject was not rejected: %v", err)
	}
	if _, err := NormalizeRule(rule); err == nil || !strings.Contains(err.Error(), "all or selected") {
		t.Fatalf("excluded access subject was not rejected during normalization: %v", err)
	}
}
