package store

import (
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"rosboard/internal/policy"
	"rosboard/internal/policyv2"
)

const keywordSourceRulesMigrationKey = "domain_keyword_source_rules_migrated"

// migrateKeywordSourceRules restores derived DOMAIN-KEYWORD rows that were
// skipped by the pre-keyword parser. The original compressed content and
// SHA-256 are immutable source identity; only derived rows/counts are rebuilt.
// A marker is written in the same transaction, so a failed migration can be
// retried safely on the next startup.
func (s *Store) migrateKeywordSourceRules(ctx context.Context) error {
	var marker string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM policy_v2_schema_meta WHERE key = ?`, keywordSourceRulesMigrationKey).Scan(&marker)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("inspect DOMAIN-KEYWORD source migration: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin DOMAIN-KEYWORD source migration: %w", err)
	}
	defer tx.Rollback()
	if err := tx.QueryRowContext(ctx, `SELECT value FROM policy_v2_schema_meta WHERE key = ?`, keywordSourceRulesMigrationKey).Scan(&marker); err == nil {
		return nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("recheck DOMAIN-KEYWORD source migration: %w", err)
	}

	rows, err := tx.QueryContext(ctx, `
SELECT v.id, v.source_id, v.sha256, v.compressed_yaml, s.kind
FROM policy_v2_source_versions v
JOIN policy_v2_sources s ON s.id = v.source_id
WHERE s.kind = '' OR lower(s.kind) = 'domain'`)
	if err != nil {
		return fmt.Errorf("list source versions for DOMAIN-KEYWORD migration: %w", err)
	}
	type sourceVersionRow struct {
		versionID  string
		sourceID   string
		sha256     string
		compressed []byte
		kind       string
	}
	versions := make([]sourceVersionRow, 0)
	for rows.Next() {
		var version sourceVersionRow
		if err := rows.Scan(&version.versionID, &version.sourceID, &version.sha256, &version.compressed, &version.kind); err != nil {
			rows.Close()
			return fmt.Errorf("scan source version for DOMAIN-KEYWORD migration: %w", err)
		}
		versions = append(versions, version)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("read source versions for DOMAIN-KEYWORD migration: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close source versions for DOMAIN-KEYWORD migration: %w", err)
	}

	for _, version := range versions {
		raw, ok, err := decompressStoredSource(version.compressed)
		if err != nil {
			return fmt.Errorf("decompress source version %s for DOMAIN-KEYWORD migration: %w", version.versionID, err)
		}
		if !ok {
			// A few very old test/fixture rows contain an opaque placeholder instead
			// of a gzip payload. They have no source bytes that can be reparsed.
			continue
		}
		prepared, err := policy.PrepareSourceContent(raw, policyv2.NormalizeSourceKind(version.kind))
		if err != nil {
			return fmt.Errorf("parse source version %s for DOMAIN-KEYWORD migration: %w", version.versionID, err)
		}
		if version.sha256 != "" && !strings.EqualFold(version.sha256, prepared.SHA256) {
			return fmt.Errorf("source version %s SHA-256 does not match its stored content", version.versionID)
		}
		keywordCount := prepared.Counts()[string(policy.RuleTypeKeyword)]
		if keywordCount == 0 {
			continue
		}
		for _, rule := range prepared.Rules {
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO policy_v2_source_rules (version_id, rule_type, domain) VALUES (?, ?, ?)`, version.versionID, string(rule.Type), rule.Domain); err != nil {
				return fmt.Errorf("insert migrated source rule %s: %w", version.versionID, err)
			}
		}
		countsJSON, err := json.Marshal(prepared.Counts())
		if err != nil {
			return fmt.Errorf("encode migrated source counts %s: %w", version.versionID, err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE policy_v2_source_versions SET counts_json = ? WHERE id = ?`, string(countsJSON), version.versionID); err != nil {
			return fmt.Errorf("update migrated source counts %s: %w", version.versionID, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO policy_v2_schema_meta (key, value) VALUES (?, 'v1')`, keywordSourceRulesMigrationKey); err != nil {
		return fmt.Errorf("write DOMAIN-KEYWORD source migration marker: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit DOMAIN-KEYWORD source migration: %w", err)
	}
	return nil
}

func decompressStoredSource(compressed []byte) ([]byte, bool, error) {
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if errors.Is(err, gzip.ErrHeader) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	defer reader.Close()
	raw, err := io.ReadAll(io.LimitReader(reader, int64(policy.MaxSourceBytes)+1))
	if err != nil {
		return nil, false, err
	}
	if len(raw) > policy.MaxSourceBytes {
		return nil, false, fmt.Errorf("decompressed source exceeds %d bytes", policy.MaxSourceBytes)
	}
	return raw, true, nil
}
