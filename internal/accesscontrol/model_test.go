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
	if err := ValidateRule(rule); err == nil || !strings.Contains(err.Error(), "「全部」或「指定」") {
		t.Fatalf("excluded access subject was not rejected: %v", err)
	}
	if _, err := NormalizeRule(rule); err == nil || !strings.Contains(err.Error(), "「全部」或「指定」") {
		t.Fatalf("excluded access subject was not rejected during normalization: %v", err)
	}
}

func TestStateApplied(t *testing.T) {
	cases := []struct {
		name            string
		desiredRevision int64
		appliedRevision int64
		want            bool
	}{
		{name: "fresh state is in sync", desiredRevision: 0, appliedRevision: 0, want: true},
		{name: "desired saved but not applied", desiredRevision: 1, appliedRevision: 0, want: false},
		{name: "desired applied", desiredRevision: 1, appliedRevision: 1, want: true},
		{name: "apply ahead of desired is inconsistent", desiredRevision: 1, appliedRevision: 2, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state := State{DesiredRevision: tc.desiredRevision, AppliedRevision: tc.appliedRevision}
			if got := state.Applied(); got != tc.want {
				t.Fatalf("Applied() = %v, want %v", got, tc.want)
			}
		})
	}
}
