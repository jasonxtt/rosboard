package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"rosboard/internal/accesscontrol"
)

const canonicalAccessMarkerKey = "canonical_targets"

// EnsureCanonicalAccessMigrated is intentionally lazy so an old database can
// be opened directly by the final binary. Opaque legacy application IDs are
// never guessed or materialized during migration.
func (r *AccessRepository) EnsureCanonicalAccessMigrated(ctx context.Context) error {
	var marker string
	err := r.store.db.QueryRowContext(ctx, `SELECT value FROM access_schema_meta WHERE key = ?`, canonicalAccessMarkerKey).Scan(&marker)
	if err == nil && marker == "v1" {
		return nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	tx, err := r.store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id, target_scope, enabled FROM access_rules WHERE device_id = ? ORDER BY id`, r.store.deviceID)
	if err != nil {
		return err
	}
	type legacyRule struct {
		id, scope string
		enabled   int
	}
	legacyRules := make([]legacyRule, 0)
	for rows.Next() {
		var value legacyRule
		if err := rows.Scan(&value.id, &value.scope, &value.enabled); err != nil {
			rows.Close()
			return err
		}
		legacyRules = append(legacyRules, value)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, legacy := range legacyRules {
		switch legacy.scope {
		case accesscontrol.TargetScopeSources:
			if _, err := tx.ExecContext(ctx, `UPDATE access_rules SET target_scope = ?, subject_mode = 'selected' WHERE device_id = ? AND id = ?`, accesscontrol.TargetScopeTargets, r.store.deviceID, legacy.id); err != nil {
				return err
			}
		case accesscontrol.TargetScopeApplications:
			unresolved, err := migrationApplicationIDs(ctx, tx, r.store.deviceID, legacy.id)
			if err != nil {
				return err
			}
			if err := replaceCanonicalTargetsTx(ctx, tx, r.store.deviceID, legacy.id, nil); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE access_rules SET target_scope = ?, enabled = CASE WHEN ? > 0 THEN 0 ELSE enabled END, subject_mode = 'selected' WHERE device_id = ? AND id = ?`, accesscontrol.TargetScopeTargets, len(unresolved), r.store.deviceID, legacy.id); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM access_rule_migration_issues WHERE device_id = ? AND rule_id = ?`, r.store.deviceID, legacy.id); err != nil {
				return err
			}
			for _, applicationID := range unresolved {
				message := "legacy application ID has no verified ApplicationPreset mapping"
				if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO access_rule_migration_issues (device_id, rule_id, code, value, message) VALUES (?, ?, 'legacy_application_unresolved', ?, ?)`, r.store.deviceID, legacy.id, applicationID, message); err != nil {
					return err
				}
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM access_rule_applications WHERE device_id = ? AND rule_id = ?`, r.store.deviceID, legacy.id); err != nil {
				return err
			}
		}
	}
	if len(legacyRules) > 0 {
		if _, err := tx.ExecContext(ctx, `UPDATE access_control_state SET desired_revision = desired_revision + 1 WHERE device_id = ?`, r.store.deviceID); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO access_schema_meta (key, value) VALUES (?, 'v1') ON CONFLICT(key) DO UPDATE SET value = excluded.value`, canonicalAccessMarkerKey); err != nil {
		return err
	}
	return tx.Commit()
}

func migrationApplicationIDs(ctx context.Context, tx *sql.Tx, deviceID, ruleID string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT application_id FROM access_rule_applications WHERE device_id = ? AND rule_id = ? ORDER BY position`, deviceID, ruleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		result = append(result, strings.TrimSpace(id))
	}
	return result, rows.Err()
}

func replaceCanonicalTargetsTx(ctx context.Context, tx *sql.Tx, deviceID, ruleID string, ids []string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM access_rule_sources WHERE device_id = ? AND rule_id = ?`, deviceID, ruleID); err != nil {
		return err
	}
	seen := make(map[string]bool, len(ids))
	position := 0
	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		if _, err := tx.ExecContext(ctx, `INSERT INTO access_rule_sources (device_id, rule_id, source_id, position) VALUES (?, ?, ?, ?)`, deviceID, ruleID, id, position); err != nil {
			return err
		}
		position++
	}
	return nil
}
