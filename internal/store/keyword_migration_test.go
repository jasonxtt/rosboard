package store

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"rosboard/internal/policy"
	"rosboard/internal/policyv2"
)

func TestKeywordSourceMigrationReparsesWithoutChangingSourceIdentity(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()

	ctx := context.Background()
	repository := storage.PolicyRepository()
	source, err := repository.SaveSource(ctx, policyv2.Source{
		ID: "domain-source", Type: policyv2.TargetSourceTypeManual, Kind: policyv2.KindDomain,
		Name: "Domain source", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte("payload:\n  - DOMAIN-SUFFIX,example.com\n  - DOMAIN-KEYWORD,video.player+\n")
	prepared, err := policy.PrepareSourceContent(raw, policy.KindDomain)
	if err != nil {
		t.Fatal(err)
	}
	version := policyv2.SourceVersion{
		ID:             "domain-version",
		SourceID:       source.ID,
		SHA256:         prepared.SHA256,
		CompressedYAML: append([]byte(nil), prepared.CompressedYAML...),
		Counts:         prepared.Counts(),
		State:          "pending",
	}
	rules := make([]policyv2.SourceRule, len(prepared.Rules))
	for i, rule := range prepared.Rules {
		rules[i] = policyv2.SourceRule{VersionID: version.ID, RuleType: string(rule.Type), Domain: rule.Domain}
	}
	if err := repository.SavePendingSourceVersion(ctx, version, rules); err != nil {
		t.Fatal(err)
	}

	originalCompressed := append([]byte(nil), prepared.CompressedYAML...)
	if _, err := storage.db.ExecContext(ctx, `DELETE FROM policy_v2_source_rules WHERE version_id = ?`, version.ID); err != nil {
		t.Fatal(err)
	}
	legacyCounts, err := json.Marshal(map[string]int{"valid": 1, string(policy.RuleTypeSuffix): 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := storage.db.ExecContext(ctx, `UPDATE policy_v2_source_versions SET counts_json = ? WHERE id = ?`, string(legacyCounts), version.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.db.ExecContext(ctx, `DELETE FROM policy_v2_schema_meta WHERE key = ?`, keywordSourceRulesMigrationKey); err != nil {
		t.Fatal(err)
	}

	if err := storage.migrateKeywordSourceRules(ctx); err != nil {
		t.Fatalf("migrateKeywordSourceRules() error = %v", err)
	}
	var compressed []byte
	var sha256 string
	var countsJSON string
	if err := storage.db.QueryRowContext(ctx, `SELECT compressed_yaml, sha256, counts_json FROM policy_v2_source_versions WHERE id = ?`, version.ID).Scan(&compressed, &sha256, &countsJSON); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(compressed, originalCompressed) || sha256 != prepared.SHA256 {
		t.Fatalf("migration changed source identity: compressedChanged=%v sha256=%q want %q", !bytes.Equal(compressed, originalCompressed), sha256, prepared.SHA256)
	}
	var counts map[string]int
	if err := json.Unmarshal([]byte(countsJSON), &counts); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(counts, prepared.Counts()) {
		t.Fatalf("migrated counts = %#v, want %#v", counts, prepared.Counts())
	}
	var migratedRules int
	if err := storage.db.QueryRowContext(ctx, `SELECT count(*) FROM policy_v2_source_rules WHERE version_id = ?`, version.ID).Scan(&migratedRules); err != nil {
		t.Fatal(err)
	}
	if migratedRules != 2 {
		t.Fatalf("migrated source rule count = %d, want 2", migratedRules)
	}
	var keywordRules int
	if err := storage.db.QueryRowContext(ctx, `SELECT count(*) FROM policy_v2_source_rules WHERE version_id = ? AND rule_type = ?`, version.ID, string(policy.RuleTypeKeyword)).Scan(&keywordRules); err != nil {
		t.Fatal(err)
	}
	if keywordRules != 1 {
		t.Fatalf("migrated keyword rule count = %d, want 1", keywordRules)
	}

	if err := storage.migrateKeywordSourceRules(ctx); err != nil {
		t.Fatalf("second migrateKeywordSourceRules() error = %v", err)
	}
	var replayedRules int
	if err := storage.db.QueryRowContext(ctx, `SELECT count(*) FROM policy_v2_source_rules WHERE version_id = ?`, version.ID).Scan(&replayedRules); err != nil {
		t.Fatal(err)
	}
	if replayedRules != migratedRules {
		t.Fatalf("replayed source rule count = %d, want %d", replayedRules, migratedRules)
	}
	var marker string
	if err := storage.db.QueryRowContext(ctx, `SELECT value FROM policy_v2_schema_meta WHERE key = ?`, keywordSourceRulesMigrationKey).Scan(&marker); err != nil {
		t.Fatal(err)
	}
	if marker != "v1" {
		t.Fatalf("migration marker = %q, want v1", marker)
	}
}
