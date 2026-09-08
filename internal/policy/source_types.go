package policy

import "time"

type SourceVersion struct {
	DeviceID        string
	ID              string
	SourceID        string
	SHA256          string
	CompressedYAML  []byte
	CountsJSON      []byte
	DiffSummaryJSON []byte
	State           string
	Error           string
	HTTPStatus      int
	CreatedAt       time.Time
}

type SourceRule struct {
	DeviceID  string
	VersionID string
	RuleType  string
	Domain    string
}
