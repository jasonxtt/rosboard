package policyv2

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	TargetSourceTypeURL    = "url"
	TargetSourceTypeUpload = "upload"
	TargetSourceTypeManual = "manual"
	TargetSourceTypePreset = "preset"
)

var (
	ErrInvalidTargetListKind       = errors.New("target list kind must be domain or ip")
	ErrInvalidTargetListSourceType = errors.New("target list source type must be url, upload, manual or preset")
	ErrPresetTargetListProtected   = errors.New("preset target lists are managed by application presets")
)

// ValidateTargetListKind is intentionally strict. Legacy Source callers must
// continue using NormalizeSourceKind because historical rows may contain
// values that predate the canonical TargetList contract.
func ValidateTargetListKind(kind string) error {
	if kind == KindDomain || kind == KindIP {
		return nil
	}
	return fmt.Errorf("%w: %q", ErrInvalidTargetListKind, kind)
}

func ValidateTargetListSourceType(sourceType string) error {
	switch sourceType {
	case TargetSourceTypeURL, TargetSourceTypeUpload, TargetSourceTypeManual, TargetSourceTypePreset:
		return nil
	default:
		return fmt.Errorf("%w: %q", ErrInvalidTargetListSourceType, sourceType)
	}
}

func ValidateTargetListPreset(sourceType, presetID string) error {
	if sourceType == TargetSourceTypePreset && strings.TrimSpace(presetID) == "" {
		return errors.New("preset target lists require presetId")
	}
	if sourceType != TargetSourceTypePreset && strings.TrimSpace(presetID) != "" {
		return errors.New("presetId is only valid for preset target lists")
	}
	return nil
}

// TargetList is the canonical library representation of reusable domain or IP
// content. The legacy Source.EgressID relationship is intentionally absent;
// policy routing continues to read that relationship through the temporary
// Source compatibility surface until RoutingRule becomes authoritative.
type TargetList struct {
	ID                string              `json:"id"`
	Name              string              `json:"name"`
	Kind              string              `json:"kind"`
	SourceType        string              `json:"sourceType"`
	PresetID          string              `json:"presetId,omitempty"`
	URL               string              `json:"url,omitempty"`
	Schedule          string              `json:"schedule"`
	Enabled           bool                `json:"enabled"`
	ActiveVersionID   string              `json:"activeVersionId"`
	PendingVersionID  string              `json:"pendingVersionId,omitempty"`
	LastGoodVersionID string              `json:"lastGoodVersionId"`
	ETag              string              `json:"etag,omitempty"`
	LastModified      string              `json:"lastModified,omitempty"`
	NextRunAt         time.Time           `json:"nextRunAt,omitempty"`
	Revision          int64               `json:"revision"`
	PendingDeletion   bool                `json:"pendingDeletion"`
	Applied           bool                `json:"-"`
	Versions          []TargetListVersion `json:"versions"`
	Counts            map[string]int      `json:"counts"`
	Usage             TargetListUsage     `json:"usage"`
	CreatedAt         time.Time           `json:"-"`
	UpdatedAt         time.Time           `json:"-"`
}

type TargetListVersion struct {
	ID             string         `json:"id"`
	TargetListID   string         `json:"targetListId"`
	SHA256         string         `json:"sha256"`
	CompressedYAML []byte         `json:"-"`
	State          string         `json:"state"`
	Error          string         `json:"error,omitempty"`
	HTTPStatus     int            `json:"httpStatus,omitempty"`
	Counts         map[string]int `json:"counts"`
	Diff           map[string]any `json:"diff"`
	CreatedAt      time.Time      `json:"createdAt"`
}

type TargetListRule struct {
	VersionID string `json:"-"`
	RuleType  string `json:"type"`
	Domain    string `json:"domain"`
}

type TargetListRefresh struct {
	NotModified  bool
	ETag         string
	LastModified string
	Version      *TargetListVersion
	Rules        []TargetListRule
}

type TargetListUsage struct {
	RoutingRuleCount int `json:"routingRuleCount"`
	AccessRuleCount  int `json:"accessRuleCount"`
}

func TargetListFromSource(source Source) TargetList {
	versions := make([]TargetListVersion, len(source.Versions))
	for i, version := range source.Versions {
		versions[i] = TargetListVersionFromSource(version)
	}
	return TargetList{
		ID:         source.ID,
		Name:       source.Name,
		Kind:       source.Kind,
		SourceType: source.Type,
		PresetID:   source.PresetID,
		URL:        source.URL,
		Schedule:   source.Schedule,
		// Enabled remains on the wire for compatibility with legacy source
		// rows, but a canonical target list has no standalone activation
		// state. Consumer references decide whether it is projected.
		Enabled:           true,
		ActiveVersionID:   source.ActiveVersionID,
		PendingVersionID:  source.PendingVersionID,
		LastGoodVersionID: source.LastGoodVersionID,
		ETag:              source.ETag,
		LastModified:      source.LastModified,
		NextRunAt:         source.NextRunAt,
		Revision:          source.Revision,
		PendingDeletion:   source.PendingDeletion,
		Applied:           source.Applied,
		Versions:          versions,
		Counts:            cloneCounts(source.Counts),
		CreatedAt:         source.CreatedAt,
		UpdatedAt:         source.UpdatedAt,
	}
}

func (target TargetList) ToSource() Source {
	versions := make([]SourceVersion, len(target.Versions))
	for i, version := range target.Versions {
		versions[i] = version.ToSource()
	}
	return Source{
		ID:       target.ID,
		Type:     target.SourceType,
		PresetID: target.PresetID,
		Kind:     target.Kind,
		Name:     target.Name,
		URL:      target.URL,
		Schedule: target.Schedule,
		// Target lists are reusable library content. Keep the legacy source
		// column true on canonical writes; policy consumers own activation.
		Enabled:           true,
		ActiveVersionID:   target.ActiveVersionID,
		PendingVersionID:  target.PendingVersionID,
		LastGoodVersionID: target.LastGoodVersionID,
		ETag:              target.ETag,
		LastModified:      target.LastModified,
		NextRunAt:         target.NextRunAt,
		Revision:          target.Revision,
		PendingDeletion:   target.PendingDeletion,
		Applied:           target.Applied,
		Versions:          versions,
		Counts:            cloneCounts(target.Counts),
		CreatedAt:         target.CreatedAt,
		UpdatedAt:         target.UpdatedAt,
	}
}

func TargetListVersionFromSource(version SourceVersion) TargetListVersion {
	return TargetListVersion{
		ID:             version.ID,
		TargetListID:   version.SourceID,
		SHA256:         version.SHA256,
		CompressedYAML: append([]byte(nil), version.CompressedYAML...),
		State:          version.State,
		Error:          version.Error,
		HTTPStatus:     version.HTTPStatus,
		Counts:         cloneCounts(version.Counts),
		Diff:           cloneDiff(version.Diff),
		CreatedAt:      version.CreatedAt,
	}
}

func (version TargetListVersion) ToSource() SourceVersion {
	return SourceVersion{
		ID:             version.ID,
		SourceID:       version.TargetListID,
		SHA256:         version.SHA256,
		CompressedYAML: append([]byte(nil), version.CompressedYAML...),
		State:          version.State,
		Error:          version.Error,
		HTTPStatus:     version.HTTPStatus,
		Counts:         cloneCounts(version.Counts),
		Diff:           cloneDiff(version.Diff),
		CreatedAt:      version.CreatedAt,
	}
}

func TargetListRuleFromSource(rule SourceRule) TargetListRule {
	return TargetListRule{VersionID: rule.VersionID, RuleType: rule.RuleType, Domain: rule.Domain}
}

func (rule TargetListRule) ToSource() SourceRule {
	return SourceRule{VersionID: rule.VersionID, RuleType: rule.RuleType, Domain: rule.Domain}
}

func (refresh TargetListRefresh) ToSource() SourceRefresh {
	result := SourceRefresh{NotModified: refresh.NotModified, ETag: refresh.ETag, LastModified: refresh.LastModified}
	if refresh.Version != nil {
		version := refresh.Version.ToSource()
		result.Version = &version
	}
	result.Rules = make([]SourceRule, len(refresh.Rules))
	for i, rule := range refresh.Rules {
		result.Rules[i] = rule.ToSource()
	}
	return result
}

func cloneCounts(counts map[string]int) map[string]int {
	if counts == nil {
		return map[string]int{}
	}
	result := make(map[string]int, len(counts))
	for key, value := range counts {
		result[key] = value
	}
	return result
}

func cloneDiff(diff map[string]any) map[string]any {
	if diff == nil {
		return map[string]any{}
	}
	result := make(map[string]any, len(diff))
	for key, value := range diff {
		result[key] = value
	}
	return result
}
