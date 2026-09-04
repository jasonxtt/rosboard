package policyv2

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestTargetListConversionPreservesIdentityWithoutEgressOwnership(t *testing.T) {
	createdAt := time.Unix(100, 0).UTC()
	updatedAt := time.Unix(200, 0).UTC()
	source := Source{
		ID:                "source-a",
		EgressID:          "wan-a",
		Type:              TargetSourceTypeURL,
		Kind:              KindDomain,
		Name:              "Domains",
		URL:               "https://example.test/rules.yaml",
		Schedule:          "1h",
		Enabled:           false,
		ActiveVersionID:   "version-active",
		PendingVersionID:  "version-pending",
		LastGoodVersionID: "version-good",
		ETag:              "etag-1",
		LastModified:      "Wed, 21 Oct 2015 07:28:00 GMT",
		NextRunAt:         updatedAt,
		Revision:          7,
		PendingDeletion:   false,
		Versions: []SourceVersion{{
			ID:             "version-active",
			SourceID:       "source-a",
			SHA256:         "sha-1",
			CompressedYAML: []byte("payload"),
			State:          "active",
			Counts:         map[string]int{"valid": 2},
			Diff:           map[string]any{"added": 2},
			CreatedAt:      createdAt,
		}},
		Counts:    map[string]int{"valid": 2},
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}

	target := TargetListFromSource(source)
	if target.ID != source.ID || target.SourceType != source.Type || target.Kind != source.Kind || target.Revision != source.Revision {
		t.Fatalf("target identity was not preserved: %#v", target)
	}
	if !target.Enabled {
		t.Fatal("canonical target list must not expose a standalone disabled state")
	}
	if target.ActiveVersionID != source.ActiveVersionID || target.PendingVersionID != source.PendingVersionID || target.LastGoodVersionID != source.LastGoodVersionID {
		t.Fatalf("target version state was not preserved: %#v", target)
	}
	if len(target.Versions) != 1 || target.Versions[0].TargetListID != source.ID || target.Versions[0].SHA256 != "sha-1" {
		t.Fatalf("target versions were not converted: %#v", target.Versions)
	}
	encoded, err := json.Marshal(target)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "egressId") || strings.Contains(string(encoded), "EgressID") {
		t.Fatalf("canonical target exposed legacy egress ownership: %s", encoded)
	}

	roundTrip := target.ToSource()
	if roundTrip.EgressID != "" {
		t.Fatalf("canonical target unexpectedly restored egress ownership: %#v", roundTrip)
	}
	if roundTrip.ID != source.ID || roundTrip.Type != source.Type || roundTrip.Kind != source.Kind || roundTrip.Revision != source.Revision || roundTrip.ActiveVersionID != source.ActiveVersionID || roundTrip.PendingVersionID != source.PendingVersionID || roundTrip.LastGoodVersionID != source.LastGoodVersionID {
		t.Fatalf("canonical round trip changed source identity/state: %#v", roundTrip)
	}
	invalidSource := (TargetList{Kind: "mixed", SourceType: TargetSourceTypeManual}).ToSource()
	if invalidSource.Kind != "mixed" {
		t.Fatalf("canonical conversion silently normalized invalid kind: %q", invalidSource.Kind)
	}
}

func TestTargetListValidationRejectsUnknownKindAndSourceType(t *testing.T) {
	for _, kind := range []string{"mixed", "unknown"} {
		if err := ValidateTargetListKind(kind); err == nil {
			t.Errorf("ValidateTargetListKind(%q) accepted invalid kind", kind)
		}
	}
	for _, sourceType := range []string{"bar"} {
		if err := ValidateTargetListSourceType(sourceType); err == nil {
			t.Errorf("ValidateTargetListSourceType(%q) accepted unsupported type", sourceType)
		}
	}
	if err := ValidateTargetListSourceType(TargetSourceTypePreset); err != nil {
		t.Fatalf("preset source type must be accepted: %v", err)
	}
	if err := ValidateTargetListPreset(TargetSourceTypePreset, "youtube"); err != nil {
		t.Fatalf("preset source type must accept a preset identity: %v", err)
	}
}

func TestNormalizeSourceKindKeepsOnlyDomainAndIPKinds(t *testing.T) {
	for _, test := range []struct {
		input string
		want  string
	}{
		{input: KindDomain, want: KindDomain},
		{input: KindIP, want: KindIP},
		{input: "mixed", want: KindDomain},
		{input: "", want: KindDomain},
	} {
		if got := NormalizeSourceKind(test.input); got != test.want {
			t.Errorf("NormalizeSourceKind(%q) = %q, want %q", test.input, got, test.want)
		}
	}
}
