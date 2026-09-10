package diagnostics

import "testing"

func TestOverallForSeparatesFindingStatusAndImpact(t *testing.T) {
	findings := []Finding{
		{ID: "optional-error", Status: StatusError, AffectsOverall: false},
		{ID: "disabled", Status: StatusDisabled, AffectsOverall: true},
		{ID: "skipped", Status: StatusSkipped, AffectsOverall: true},
	}
	if got := OverallFor(findings); got != OverallHealthy {
		t.Fatalf("OverallFor() = %q, want %q", got, OverallHealthy)
	}

	findings = append(findings, Finding{ID: "core-warning", Status: StatusWarning, AffectsOverall: true})
	if got := OverallFor(findings); got != OverallWarning {
		t.Fatalf("OverallFor() = %q, want %q", got, OverallWarning)
	}

	findings = append(findings, Finding{ID: "core-error", Status: StatusError, AffectsOverall: true})
	if got := OverallFor(findings); got != OverallError {
		t.Fatalf("OverallFor() = %q, want %q", got, OverallError)
	}
}
