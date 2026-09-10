package diagnostics

import "time"

// Status is the stable status vocabulary shared by the API and both web UIs.
type Status string

const (
	StatusOK       Status = "ok"
	StatusWarning  Status = "warning"
	StatusError    Status = "error"
	StatusDisabled Status = "disabled"
	StatusSkipped  Status = "skipped"
)

type OverallStatus string

const (
	OverallHealthy OverallStatus = "healthy"
	OverallWarning OverallStatus = "warning"
	OverallError   OverallStatus = "error"
)

const ModeQuick = "quick"

const ModeDeep = "deep"

// Finding is intentionally independent from the aggregate report status:
// optional modules may be unhealthy without failing rosboard as a whole.
type Finding struct {
	ID             string         `json:"id"`
	Group          string         `json:"group"`
	Status         Status         `json:"status"`
	Title          string         `json:"title"`
	Summary        string         `json:"summary"`
	Recommendation string         `json:"recommendation,omitempty"`
	AffectsOverall bool           `json:"affectsOverall"`
	Evidence       map[string]any `json:"evidence,omitempty"`
}

type DiagnosticReport struct {
	GeneratedAt time.Time     `json:"generatedAt"`
	Mode        string        `json:"mode"`
	DeviceID    string        `json:"deviceId"`
	Overall     OverallStatus `json:"overall"`
	Findings    []Finding     `json:"findings"`
}

// Report is kept as a concise internal spelling while DiagnosticReport is the
// public contract name used by callers and documentation.
type Report = DiagnosticReport

// DiagnosticFinding is the explicit contract spelling for Finding.
type DiagnosticFinding = Finding

// OverallFor aggregates only findings that are explicitly allowed to affect
// the overall state. disabled/skipped are always excluded even if a caller
// accidentally marks them as affecting the overall state.
func OverallFor(findings []Finding) OverallStatus {
	hasError := false
	hasWarning := false
	for _, finding := range findings {
		if !finding.AffectsOverall || finding.Status == StatusDisabled || finding.Status == StatusSkipped {
			continue
		}
		switch finding.Status {
		case StatusError:
			hasError = true
		case StatusWarning:
			hasWarning = true
		}
	}
	if hasError {
		return OverallError
	}
	if hasWarning {
		return OverallWarning
	}
	return OverallHealthy
}
