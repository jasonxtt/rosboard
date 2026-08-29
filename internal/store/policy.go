package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"rosboard/internal/policyv2"
)

type PolicyRepository struct {
	store *Store
}

func (s *Store) PolicyRepository() *PolicyRepository {
	return &PolicyRepository{store: s}
}

func (r *PolicyRepository) DeviceID() string { return r.store.deviceID }

func (s *Store) initPolicySchema() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS policy_v2_egresses (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    priority INTEGER NOT NULL,
    list_mode TEXT NOT NULL,
    list_name TEXT NOT NULL,
    dns_upstream TEXT NOT NULL,
    fake_alias TEXT NOT NULL,
    failure_mode TEXT NOT NULL,
    router_output INTEGER NOT NULL,
    enabled INTEGER NOT NULL,
    revision INTEGER NOT NULL,
    pending_delete INTEGER NOT NULL,
    applied INTEGER NOT NULL,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS policy_v2_egress_families (
    egress_id TEXT NOT NULL REFERENCES policy_v2_egresses(id) ON DELETE CASCADE,
    family TEXT NOT NULL,
    enabled INTEGER NOT NULL,
    wan_interface TEXT NOT NULL,
    gateway TEXT NOT NULL,
    route_table TEXT NOT NULL,
    route_mode TEXT NOT NULL,
    nat_mode TEXT NOT NULL,
    wan_source TEXT NOT NULL,
    PRIMARY KEY (egress_id, family)
);
CREATE TABLE IF NOT EXISTS policy_v2_sources (
    id TEXT PRIMARY KEY,
    egress_id TEXT NOT NULL,
    type TEXT NOT NULL,
    kind TEXT NOT NULL DEFAULT 'domain',
    name TEXT NOT NULL,
    url TEXT NOT NULL,
    schedule TEXT NOT NULL,
    enabled INTEGER NOT NULL,
    active_version_id TEXT NOT NULL,
    pending_version_id TEXT NOT NULL,
    last_good_version_id TEXT NOT NULL,
    etag TEXT NOT NULL,
    last_modified TEXT NOT NULL,
    next_run_at INTEGER NOT NULL,
    revision INTEGER NOT NULL,
    pending_delete INTEGER NOT NULL,
    applied INTEGER NOT NULL,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS policy_v2_sources_egress_idx ON policy_v2_sources(egress_id);
CREATE TABLE IF NOT EXISTS policy_v2_source_versions (
    id TEXT PRIMARY KEY,
    source_id TEXT NOT NULL REFERENCES policy_v2_sources(id) ON DELETE CASCADE,
    sha256 TEXT NOT NULL,
    compressed_yaml BLOB NOT NULL,
    state TEXT NOT NULL,
    error TEXT NOT NULL,
    http_status INTEGER NOT NULL,
    counts_json TEXT NOT NULL,
    diff_json TEXT NOT NULL,
    created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS policy_v2_versions_source_idx ON policy_v2_source_versions(source_id, created_at DESC);
CREATE TABLE IF NOT EXISTS policy_v2_source_rules (
    version_id TEXT NOT NULL REFERENCES policy_v2_source_versions(id) ON DELETE CASCADE,
    rule_type TEXT NOT NULL,
    domain TEXT NOT NULL,
    PRIMARY KEY (version_id, rule_type, domain)
);
CREATE TABLE IF NOT EXISTS policy_v2_device_state (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    lan_scope_json TEXT NOT NULL,
    desired_revision INTEGER NOT NULL,
    applied_revision INTEGER NOT NULL,
    applied_hash TEXT NOT NULL,
    applied_at INTEGER NOT NULL,
    job_id TEXT NOT NULL,
    job_plan_id TEXT NOT NULL,
    job_state TEXT NOT NULL,
    job_phase TEXT NOT NULL,
    job_progress INTEGER NOT NULL,
    job_error TEXT NOT NULL,
    job_created_at INTEGER NOT NULL,
    job_started_at INTEGER NOT NULL,
    job_finished_at INTEGER NOT NULL
);
INSERT OR IGNORE INTO policy_v2_device_state (
    id, lan_scope_json, desired_revision, applied_revision, applied_hash, applied_at,
    job_id, job_plan_id, job_state, job_phase, job_progress, job_error,
    job_created_at, job_started_at, job_finished_at
) VALUES (1, '{}', 0, 0, '', 0, '', '', '', '', 0, '', 0, 0, 0);
`)
	if err != nil {
		return fmt.Errorf("init policy v2 schema: %w", err)
	}
	if _, err := s.db.Exec(`ALTER TABLE policy_v2_sources ADD COLUMN kind TEXT NOT NULL DEFAULT 'domain'`); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
		return fmt.Errorf("add policy_v2_sources.kind: %w", err)
	}
	return nil
}

func (r *PolicyRepository) ListEgresses(ctx context.Context) ([]policyv2.Egress, error) {
	rows, err := r.store.db.QueryContext(ctx, `SELECT id, name, priority, list_mode, list_name, dns_upstream, fake_alias, failure_mode, router_output, enabled, revision, pending_delete, applied, created_at, updated_at FROM policy_v2_egresses ORDER BY priority, name, id`)
	if err != nil {
		return nil, fmt.Errorf("list policy egresses: %w", err)
	}
	result := make([]policyv2.Egress, 0)
	for rows.Next() {
		egress, err := scanEgress(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		result = append(result, egress)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for i := range result {
		result[i].Families, err = listEgressFamilies(ctx, r.store.db, result[i].ID)
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (r *PolicyRepository) GetEgress(ctx context.Context, id string) (policyv2.Egress, error) {
	row := r.store.db.QueryRowContext(ctx, `SELECT id, name, priority, list_mode, list_name, dns_upstream, fake_alias, failure_mode, router_output, enabled, revision, pending_delete, applied, created_at, updated_at FROM policy_v2_egresses WHERE id = ?`, id)
	egress, err := scanEgress(row)
	if errors.Is(err, sql.ErrNoRows) {
		return policyv2.Egress{}, policyv2.ErrEgressNotFound
	}
	if err != nil {
		return policyv2.Egress{}, fmt.Errorf("get policy egress: %w", err)
	}
	egress.Families, err = listEgressFamilies(ctx, r.store.db, id)
	if err != nil {
		return policyv2.Egress{}, err
	}
	return egress, nil
}

func (r *PolicyRepository) SaveEgress(ctx context.Context, egress policyv2.Egress) (policyv2.Egress, error) {
	tx, err := r.store.db.BeginTx(ctx, nil)
	if err != nil {
		return policyv2.Egress{}, err
	}
	defer tx.Rollback()

	now := time.Now().UTC()
	var currentRevision int64
	var applied, pendingDelete int
	var createdAt int64
	err = tx.QueryRowContext(ctx, `SELECT revision, applied, pending_delete, created_at FROM policy_v2_egresses WHERE id = ?`, egress.ID).Scan(&currentRevision, &applied, &pendingDelete, &createdAt)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if egress.Revision != 0 {
			return policyv2.Egress{}, policyv2.ErrRevisionStale
		}
		egress.Revision = 1
		egress.CreatedAt = now
		egress.Applied = false
	case err != nil:
		return policyv2.Egress{}, err
	default:
		if egress.Revision != currentRevision || pendingDelete != 0 {
			return policyv2.Egress{}, policyv2.ErrRevisionStale
		}
		egress.Revision = currentRevision + 1
		egress.CreatedAt = timeFromUnix(createdAt)
		egress.Applied = applied != 0
	}
	egress.UpdatedAt = now
	egress.PendingDeletion = false

	_, err = tx.ExecContext(ctx, `INSERT INTO policy_v2_egresses (id, name, priority, list_mode, list_name, dns_upstream, fake_alias, failure_mode, router_output, enabled, revision, pending_delete, applied, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET name=excluded.name, priority=excluded.priority, list_mode=excluded.list_mode, list_name=excluded.list_name, dns_upstream=excluded.dns_upstream, fake_alias=excluded.fake_alias, failure_mode=excluded.failure_mode, router_output=excluded.router_output, enabled=excluded.enabled, revision=excluded.revision, updated_at=excluded.updated_at`,
		egress.ID, egress.Name, egress.Priority, egress.ListMode, egress.ListName, egress.DNSUpstream, egress.FakeAlias, egress.FailureMode, boolToInt(egress.RouterOutput), boolToInt(egress.Enabled), egress.Revision, boolToInt(egress.Applied), unixTime(egress.CreatedAt), unixTime(egress.UpdatedAt))
	if err != nil {
		return policyv2.Egress{}, fmt.Errorf("save policy egress: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM policy_v2_egress_families WHERE egress_id = ?`, egress.ID); err != nil {
		return policyv2.Egress{}, err
	}
	for _, family := range egress.Families {
		if _, err := tx.ExecContext(ctx, `INSERT INTO policy_v2_egress_families (egress_id, family, enabled, wan_interface, gateway, route_table, route_mode, nat_mode, wan_source) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, egress.ID, family.Family, boolToInt(family.Enabled), family.WANInterface, family.Gateway, family.RouteTable, family.RouteMode, family.NATMode, family.WANSource); err != nil {
			return policyv2.Egress{}, fmt.Errorf("save policy egress family: %w", err)
		}
	}
	if err := bumpDesiredRevision(ctx, tx); err != nil {
		return policyv2.Egress{}, err
	}
	if err := tx.Commit(); err != nil {
		return policyv2.Egress{}, err
	}
	return egress, nil
}

func (r *PolicyRepository) DeleteEgress(ctx context.Context, id string, revision int64) error {
	tx, err := r.store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var currentRevision int64
	var applied int
	if err := tx.QueryRowContext(ctx, `SELECT revision, applied FROM policy_v2_egresses WHERE id = ?`, id).Scan(&currentRevision, &applied); errors.Is(err, sql.ErrNoRows) {
		return policyv2.ErrEgressNotFound
	} else if err != nil {
		return err
	}
	if revision != currentRevision {
		return policyv2.ErrRevisionStale
	}
	if _, err := tx.ExecContext(ctx, `UPDATE policy_v2_sources SET egress_id = '', revision = revision + 1, updated_at = ? WHERE egress_id = ?`, unixTime(time.Now().UTC()), id); err != nil {
		return err
	}
	if applied != 0 {
		_, err = tx.ExecContext(ctx, `UPDATE policy_v2_egresses SET pending_delete = 1, enabled = 0, revision = revision + 1, updated_at = ? WHERE id = ?`, unixTime(time.Now().UTC()), id)
	} else {
		_, err = tx.ExecContext(ctx, `DELETE FROM policy_v2_egresses WHERE id = ?`, id)
	}
	if err != nil {
		return err
	}
	if err := bumpDesiredRevision(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *PolicyRepository) ListSources(ctx context.Context, egressID string) ([]policyv2.Source, error) {
	query := `SELECT id, egress_id, type, kind, name, url, schedule, enabled, active_version_id, pending_version_id, last_good_version_id, etag, last_modified, next_run_at, revision, pending_delete, applied, created_at, updated_at FROM policy_v2_sources`
	args := make([]any, 0, 1)
	if egressID != "" {
		query += ` WHERE egress_id = ?`
		args = append(args, egressID)
	}
	query += ` ORDER BY name, id`
	rows, err := r.store.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list policy sources: %w", err)
	}
	result := make([]policyv2.Source, 0)
	for rows.Next() {
		source, err := scanSource(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		result = append(result, source)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for i := range result {
		result[i].Versions, err = r.ListSourceVersions(ctx, result[i].ID)
		if err != nil {
			return nil, err
		}
		result[i].Counts = activeSourceCounts(result[i])
	}
	return result, nil
}

func (r *PolicyRepository) GetSource(ctx context.Context, id string) (policyv2.Source, error) {
	row := r.store.db.QueryRowContext(ctx, `SELECT id, egress_id, type, kind, name, url, schedule, enabled, active_version_id, pending_version_id, last_good_version_id, etag, last_modified, next_run_at, revision, pending_delete, applied, created_at, updated_at FROM policy_v2_sources WHERE id = ?`, id)
	source, err := scanSource(row)
	if errors.Is(err, sql.ErrNoRows) {
		return policyv2.Source{}, policyv2.ErrSourceNotFound
	}
	if err != nil {
		return policyv2.Source{}, fmt.Errorf("get policy source: %w", err)
	}
	source.Versions, err = r.ListSourceVersions(ctx, id)
	if err != nil {
		return policyv2.Source{}, err
	}
	source.Counts = activeSourceCounts(source)
	return source, nil
}

func (r *PolicyRepository) SaveSource(ctx context.Context, source policyv2.Source) (policyv2.Source, error) {
	tx, err := r.store.db.BeginTx(ctx, nil)
	if err != nil {
		return policyv2.Source{}, err
	}
	defer tx.Rollback()
	if source.EgressID != "" {
		var pending int
		if err := tx.QueryRowContext(ctx, `SELECT pending_delete FROM policy_v2_egresses WHERE id = ?`, source.EgressID).Scan(&pending); errors.Is(err, sql.ErrNoRows) {
			return policyv2.Source{}, policyv2.ErrEgressNotFound
		} else if err != nil {
			return policyv2.Source{}, err
		} else if pending != 0 {
			return policyv2.Source{}, policyv2.ErrEgressNotFound
		}
	}

	now := time.Now().UTC()
	var current policyv2.Source
	var currentRevision, currentNextRunAt, currentCreatedAt int64
	var enabled, pendingDelete, applied int
	err = tx.QueryRowContext(ctx, `SELECT egress_id, type, kind, name, url, schedule, enabled, active_version_id, pending_version_id, last_good_version_id, etag, last_modified, next_run_at, revision, pending_delete, applied, created_at FROM policy_v2_sources WHERE id = ?`, source.ID).Scan(
		&current.EgressID, &current.Type, &current.Kind, &current.Name, &current.URL, &current.Schedule, &enabled, &current.ActiveVersionID, &current.PendingVersionID, &current.LastGoodVersionID, &current.ETag, &current.LastModified, &currentNextRunAt, &currentRevision, &pendingDelete, &applied, &currentCreatedAt,
	)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if source.Revision != 0 {
			return policyv2.Source{}, policyv2.ErrRevisionStale
		}
		source.Revision = 1
		source.CreatedAt = now
		source.Kind = policyv2.NormalizeSourceKind(source.Kind)
	case err != nil:
		return policyv2.Source{}, err
	default:
		if source.Revision != currentRevision || pendingDelete != 0 {
			return policyv2.Source{}, policyv2.ErrRevisionStale
		}
		source.Revision = currentRevision + 1
		// Content kind is fixed at creation like the entry type; edits keep
		// the stored value so legacy clients cannot flip it accidentally.
		source.Kind = policyv2.NormalizeSourceKind(current.Kind)
		source.ActiveVersionID = current.ActiveVersionID
		source.PendingVersionID = current.PendingVersionID
		source.LastGoodVersionID = current.LastGoodVersionID
		if source.ETag == "" {
			source.ETag = current.ETag
		}
		if source.LastModified == "" {
			source.LastModified = current.LastModified
		}
		if source.NextRunAt.IsZero() {
			source.NextRunAt = timeFromUnix(currentNextRunAt)
		}
		source.Applied = applied != 0
		source.CreatedAt = timeFromUnix(currentCreatedAt)
	}
	source.UpdatedAt = now
	_, err = tx.ExecContext(ctx, `INSERT INTO policy_v2_sources (id, egress_id, type, kind, name, url, schedule, enabled, active_version_id, pending_version_id, last_good_version_id, etag, last_modified, next_run_at, revision, pending_delete, applied, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET egress_id=excluded.egress_id, type=excluded.type, kind=excluded.kind, name=excluded.name, url=excluded.url, schedule=excluded.schedule, enabled=excluded.enabled, active_version_id=excluded.active_version_id, pending_version_id=excluded.pending_version_id, last_good_version_id=excluded.last_good_version_id, etag=excluded.etag, last_modified=excluded.last_modified, next_run_at=excluded.next_run_at, revision=excluded.revision, updated_at=excluded.updated_at`,
		source.ID, source.EgressID, source.Type, source.Kind, source.Name, source.URL, source.Schedule, boolToInt(source.Enabled), source.ActiveVersionID, source.PendingVersionID, source.LastGoodVersionID, source.ETag, source.LastModified, unixTime(source.NextRunAt), source.Revision, boolToInt(source.Applied), unixTime(source.CreatedAt), unixTime(source.UpdatedAt))
	if err != nil {
		return policyv2.Source{}, fmt.Errorf("save policy source: %w", err)
	}
	if err := bumpDesiredRevision(ctx, tx); err != nil {
		return policyv2.Source{}, err
	}
	if err := tx.Commit(); err != nil {
		return policyv2.Source{}, err
	}
	return source, nil
}

func (r *PolicyRepository) DeleteSource(ctx context.Context, id string, revision int64) error {
	tx, err := r.store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var currentRevision int64
	var applied int
	var activeVersion string
	if err := tx.QueryRowContext(ctx, `SELECT revision, applied, active_version_id FROM policy_v2_sources WHERE id = ?`, id).Scan(&currentRevision, &applied, &activeVersion); errors.Is(err, sql.ErrNoRows) {
		return policyv2.ErrSourceNotFound
	} else if err != nil {
		return err
	}
	if revision != currentRevision {
		return policyv2.ErrRevisionStale
	}
	if applied != 0 || activeVersion != "" {
		_, err = tx.ExecContext(ctx, `UPDATE policy_v2_sources SET egress_id = '', enabled = 0, pending_delete = 1, revision = revision + 1, updated_at = ? WHERE id = ?`, unixTime(time.Now().UTC()), id)
	} else {
		_, err = tx.ExecContext(ctx, `DELETE FROM policy_v2_sources WHERE id = ?`, id)
	}
	if err != nil {
		return err
	}
	if err := bumpDesiredRevision(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *PolicyRepository) SavePendingSourceVersion(ctx context.Context, version policyv2.SourceVersion, rules []policyv2.SourceRule) error {
	tx, err := r.store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM policy_v2_sources WHERE id = ?`, version.SourceID).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
		return policyv2.ErrSourceNotFound
	} else if err != nil {
		return err
	}
	counts, err := json.Marshal(nonNilCounts(version.Counts))
	if err != nil {
		return err
	}
	diff, err := json.Marshal(nonNilDiff(version.Diff))
	if err != nil {
		return err
	}
	if version.CreatedAt.IsZero() {
		version.CreatedAt = time.Now().UTC()
	}
	if version.State == "" {
		version.State = "pending"
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO policy_v2_source_versions (id, source_id, sha256, compressed_yaml, state, error, http_status, counts_json, diff_json, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, version.ID, version.SourceID, version.SHA256, version.CompressedYAML, version.State, version.Error, version.HTTPStatus, string(counts), string(diff), unixTime(version.CreatedAt)); err != nil {
		return fmt.Errorf("save pending policy source version: %w", err)
	}
	for _, rule := range rules {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO policy_v2_source_rules (version_id, rule_type, domain) VALUES (?, ?, ?)`, version.ID, rule.RuleType, rule.Domain); err != nil {
			return fmt.Errorf("save policy source rule: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE policy_v2_sources SET pending_version_id = ?, revision = revision + 1, updated_at = ? WHERE id = ?`, version.ID, unixTime(time.Now().UTC()), version.SourceID); err != nil {
		return err
	}
	if err := bumpDesiredRevision(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *PolicyRepository) SaveSourceRefresh(ctx context.Context, source policyv2.Source, refresh policyv2.SourceRefresh, nextRunAt time.Time) error {
	tx, err := r.store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var revision int64
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM policy_v2_sources WHERE id = ?`, source.ID).Scan(&revision); errors.Is(err, sql.ErrNoRows) {
		return policyv2.ErrSourceNotFound
	} else if err != nil {
		return err
	}
	if revision != source.Revision {
		return policyv2.ErrRevisionStale
	}
	etag := refresh.ETag
	if etag == "" {
		etag = source.ETag
	}
	lastModified := refresh.LastModified
	if lastModified == "" {
		lastModified = source.LastModified
	}
	pendingID := ""
	if refresh.Version != nil {
		version := *refresh.Version
		counts, err := json.Marshal(nonNilCounts(version.Counts))
		if err != nil {
			return err
		}
		diff, err := json.Marshal(nonNilDiff(version.Diff))
		if err != nil {
			return err
		}
		if version.CreatedAt.IsZero() {
			version.CreatedAt = time.Now().UTC()
		}
		if version.State == "" {
			version.State = "pending"
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO policy_v2_source_versions (id, source_id, sha256, compressed_yaml, state, error, http_status, counts_json, diff_json, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, version.ID, source.ID, version.SHA256, version.CompressedYAML, version.State, version.Error, version.HTTPStatus, string(counts), string(diff), unixTime(version.CreatedAt)); err != nil {
			return fmt.Errorf("save refreshed policy source version: %w", err)
		}
		for _, rule := range refresh.Rules {
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO policy_v2_source_rules (version_id, rule_type, domain) VALUES (?, ?, ?)`, version.ID, rule.RuleType, rule.Domain); err != nil {
				return err
			}
		}
		if version.State == "pending" {
			pendingID = version.ID
		}
	}
	now := time.Now().UTC()
	if pendingID != "" {
		if _, err := tx.ExecContext(ctx, `UPDATE policy_v2_sources SET pending_version_id = ?, etag = ?, last_modified = ?, next_run_at = ?, revision = revision + 1, updated_at = ? WHERE id = ?`, pendingID, etag, lastModified, unixTime(nextRunAt), unixTime(now), source.ID); err != nil {
			return err
		}
		if err := bumpDesiredRevision(ctx, tx); err != nil {
			return err
		}
	} else {
		if _, err := tx.ExecContext(ctx, `UPDATE policy_v2_sources SET etag = ?, last_modified = ?, next_run_at = ?, revision = revision + 1, updated_at = ? WHERE id = ?`, etag, lastModified, unixTime(nextRunAt), unixTime(now), source.ID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *PolicyRepository) ListSourceVersions(ctx context.Context, sourceID string) ([]policyv2.SourceVersion, error) {
	rows, err := r.store.db.QueryContext(ctx, `SELECT id, source_id, sha256, compressed_yaml, state, error, http_status, counts_json, diff_json, created_at FROM policy_v2_source_versions WHERE source_id = ? ORDER BY created_at DESC, id DESC`, sourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]policyv2.SourceVersion, 0)
	for rows.Next() {
		var version policyv2.SourceVersion
		var counts, diff string
		var createdAt int64
		if err := rows.Scan(&version.ID, &version.SourceID, &version.SHA256, &version.CompressedYAML, &version.State, &version.Error, &version.HTTPStatus, &counts, &diff, &createdAt); err != nil {
			return nil, err
		}
		version.CreatedAt = timeFromUnix(createdAt)
		version.Counts = map[string]int{}
		version.Diff = map[string]any{}
		_ = json.Unmarshal([]byte(counts), &version.Counts)
		_ = json.Unmarshal([]byte(diff), &version.Diff)
		result = append(result, version)
	}
	return result, rows.Err()
}

func (r *PolicyRepository) ListSourceRules(ctx context.Context, versionID string, query policyv2.RuleQuery) ([]policyv2.SourceRule, bool, error) {
	if query.Limit <= 0 || query.Limit > 1000 {
		query.Limit = 100
	}
	statement := `SELECT version_id, rule_type, domain FROM policy_v2_source_rules WHERE version_id = ?`
	args := []any{versionID}
	if query.RuleType != "" {
		statement += ` AND rule_type = ?`
		args = append(args, query.RuleType)
	}
	if query.Query != "" {
		statement += ` AND domain LIKE ? ESCAPE '\'`
		args = append(args, "%"+escapeLike(query.Query)+"%")
	}
	if query.AfterType != "" || query.AfterDomain != "" {
		statement += ` AND (rule_type > ? OR (rule_type = ? AND domain > ?))`
		args = append(args, query.AfterType, query.AfterType, query.AfterDomain)
	}
	statement += ` ORDER BY rule_type, domain LIMIT ?`
	args = append(args, query.Limit+1)
	rows, err := r.store.db.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	result := make([]policyv2.SourceRule, 0, query.Limit+1)
	for rows.Next() {
		var rule policyv2.SourceRule
		if err := rows.Scan(&rule.VersionID, &rule.RuleType, &rule.Domain); err != nil {
			return nil, false, err
		}
		result = append(result, rule)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	hasNext := len(result) > query.Limit
	if hasNext {
		result = result[:query.Limit]
	}
	return result, hasNext, nil
}

func (r *PolicyRepository) GetDeviceState(ctx context.Context) (policyv2.DeviceState, error) {
	var state policyv2.DeviceState
	var trafficIngressJSON string
	var appliedAt, jobCreatedAt, jobStartedAt, jobFinishedAt int64
	err := r.store.db.QueryRowContext(ctx, `SELECT lan_scope_json, desired_revision, applied_revision, applied_hash, applied_at, job_id, job_plan_id, job_state, job_phase, job_progress, job_error, job_created_at, job_started_at, job_finished_at FROM policy_v2_device_state WHERE id = 1`).Scan(
		&trafficIngressJSON, &state.DesiredRevision, &state.AppliedRevision, &state.AppliedHash, &appliedAt,
		&state.Job.ID, &state.Job.PlanID, &state.Job.State, &state.Job.Phase, &state.Job.Progress, &state.Job.Error,
		&jobCreatedAt, &jobStartedAt, &jobFinishedAt,
	)
	if err != nil {
		return policyv2.DeviceState{}, fmt.Errorf("get policy device state: %w", err)
	}
	state.DeviceID = r.DeviceID()
	state.TrafficIngress = []byte(trafficIngressJSON)
	state.AppliedAt = timeFromUnix(appliedAt)
	state.Job.CreatedAt = timeFromUnix(jobCreatedAt)
	state.Job.StartedAt = timeFromUnix(jobStartedAt)
	state.Job.FinishedAt = timeFromUnix(jobFinishedAt)
	return state, nil
}

func (r *PolicyRepository) SaveTrafficIngress(ctx context.Context, payload []byte) (policyv2.DeviceState, error) {
	scope, err := policyv2.ParseTrafficIngressScope(payload)
	if err != nil {
		return policyv2.DeviceState{}, err
	}
	scopeJSON, err := policyv2.MarshalTrafficIngressScope(scope)
	if err != nil {
		return policyv2.DeviceState{}, err
	}
	tx, err := r.store.db.BeginTx(ctx, nil)
	if err != nil {
		return policyv2.DeviceState{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE policy_v2_device_state SET lan_scope_json = ? WHERE id = 1`, string(scopeJSON)); err != nil {
		return policyv2.DeviceState{}, err
	}
	if err := bumpDesiredRevision(ctx, tx); err != nil {
		return policyv2.DeviceState{}, err
	}
	if err := tx.Commit(); err != nil {
		return policyv2.DeviceState{}, err
	}
	return r.GetDeviceState(ctx)
}

func (r *PolicyRepository) SaveApplyJob(ctx context.Context, job policyv2.ApplyJob) error {
	result, err := r.store.db.ExecContext(ctx, `UPDATE policy_v2_device_state SET job_id = ?, job_plan_id = ?, job_state = ?, job_phase = ?, job_progress = ?, job_error = ?, job_created_at = ?, job_started_at = ?, job_finished_at = ? WHERE id = 1`,
		job.ID, job.PlanID, job.State, job.Phase, job.Progress, job.Error, unixTime(job.CreatedAt), unixTime(job.StartedAt), unixTime(job.FinishedAt))
	if err != nil {
		return fmt.Errorf("save policy apply job: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows != 1 {
		return errors.New("policy device state is missing")
	}
	return nil
}

func (r *PolicyRepository) GetApplyJob(ctx context.Context, id string) (policyv2.ApplyJob, error) {
	state, err := r.GetDeviceState(ctx)
	if err != nil {
		return policyv2.ApplyJob{}, err
	}
	if state.Job.ID == "" || state.Job.ID != id {
		return policyv2.ApplyJob{}, policyv2.ErrJobNotFound
	}
	return state.Job, nil
}

func (r *PolicyRepository) CommitApply(ctx context.Context, desiredRevision int64, appliedHash string, job policyv2.ApplyJob) error {
	tx, err := r.store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var currentRevision int64
	if err := tx.QueryRowContext(ctx, `SELECT desired_revision FROM policy_v2_device_state WHERE id = 1`).Scan(&currentRevision); err != nil {
		return err
	}
	if currentRevision != desiredRevision {
		return policyv2.ErrRevisionStale
	}
	now := time.Now().UTC()
	if _, err := tx.ExecContext(ctx, `UPDATE policy_v2_source_versions SET state = 'success' WHERE id IN (SELECT pending_version_id FROM policy_v2_sources WHERE pending_version_id <> '')`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE policy_v2_sources SET active_version_id = pending_version_id, last_good_version_id = pending_version_id, pending_version_id = '', applied = 1, updated_at = ? WHERE pending_version_id <> ''`, unixTime(now)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM policy_v2_sources WHERE pending_delete = 1`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM policy_v2_egresses WHERE pending_delete = 1`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE policy_v2_sources SET applied = 1`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE policy_v2_egresses SET applied = 1`); err != nil {
		return err
	}
	job.State = "committed"
	job.Phase = "committed"
	job.FinishedAt = now
	if _, err := tx.ExecContext(ctx, `UPDATE policy_v2_device_state SET applied_revision = ?, applied_hash = ?, applied_at = ?, job_id = ?, job_plan_id = ?, job_state = ?, job_phase = ?, job_progress = ?, job_error = ?, job_created_at = ?, job_started_at = ?, job_finished_at = ? WHERE id = 1`,
		desiredRevision, appliedHash, unixTime(now), job.ID, job.PlanID, job.State, job.Phase, job.Progress, job.Error, unixTime(job.CreatedAt), unixTime(job.StartedAt), unixTime(job.FinishedAt)); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *PolicyRepository) ManagerInstanceID(ctx context.Context) (string, error) {
	return r.store.ManagerInstanceID(ctx)
}

type rowScanner interface {
	Scan(...any) error
}

func scanEgress(row rowScanner) (policyv2.Egress, error) {
	var egress policyv2.Egress
	var routerOutput, enabled, pendingDelete, applied int
	var createdAt, updatedAt int64
	err := row.Scan(&egress.ID, &egress.Name, &egress.Priority, &egress.ListMode, &egress.ListName, &egress.DNSUpstream, &egress.FakeAlias, &egress.FailureMode, &routerOutput, &enabled, &egress.Revision, &pendingDelete, &applied, &createdAt, &updatedAt)
	if err != nil {
		return policyv2.Egress{}, err
	}
	egress.RouterOutput = routerOutput != 0
	egress.Enabled = enabled != 0
	egress.PendingDeletion = pendingDelete != 0
	egress.Applied = applied != 0
	egress.CreatedAt = timeFromUnix(createdAt)
	egress.UpdatedAt = timeFromUnix(updatedAt)
	egress.Families = []policyv2.EgressFamily{}
	return egress, nil
}

func listEgressFamilies(ctx context.Context, db interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, egressID string) ([]policyv2.EgressFamily, error) {
	rows, err := db.QueryContext(ctx, `SELECT family, enabled, wan_interface, gateway, route_table, route_mode, nat_mode, wan_source FROM policy_v2_egress_families WHERE egress_id = ? ORDER BY family`, egressID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]policyv2.EgressFamily, 0, 2)
	for rows.Next() {
		var family policyv2.EgressFamily
		var enabled int
		if err := rows.Scan(&family.Family, &enabled, &family.WANInterface, &family.Gateway, &family.RouteTable, &family.RouteMode, &family.NATMode, &family.WANSource); err != nil {
			return nil, err
		}
		family.Enabled = enabled != 0
		result = append(result, family)
	}
	return result, rows.Err()
}

func scanSource(row rowScanner) (policyv2.Source, error) {
	var source policyv2.Source
	var enabled, pendingDelete, applied int
	var nextRunAt, createdAt, updatedAt int64
	err := row.Scan(&source.ID, &source.EgressID, &source.Type, &source.Kind, &source.Name, &source.URL, &source.Schedule, &enabled, &source.ActiveVersionID, &source.PendingVersionID, &source.LastGoodVersionID, &source.ETag, &source.LastModified, &nextRunAt, &source.Revision, &pendingDelete, &applied, &createdAt, &updatedAt)
	if err != nil {
		return policyv2.Source{}, err
	}
	source.Kind = policyv2.NormalizeSourceKind(source.Kind)
	source.Enabled = enabled != 0
	source.PendingDeletion = pendingDelete != 0
	source.Applied = applied != 0
	source.NextRunAt = timeFromUnix(nextRunAt)
	source.CreatedAt = timeFromUnix(createdAt)
	source.UpdatedAt = timeFromUnix(updatedAt)
	source.Versions = []policyv2.SourceVersion{}
	source.Counts = map[string]int{}
	return source, nil
}

func activeSourceCounts(source policyv2.Source) map[string]int {
	for _, version := range source.Versions {
		if version.ID == source.ActiveVersionID {
			return nonNilCounts(version.Counts)
		}
	}
	return map[string]int{}
}

func bumpDesiredRevision(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `UPDATE policy_v2_device_state SET desired_revision = desired_revision + 1 WHERE id = 1`)
	return err
}

func nonNilCounts(counts map[string]int) map[string]int {
	if counts == nil {
		return map[string]int{}
	}
	return counts
}

func nonNilDiff(diff map[string]any) map[string]any {
	if diff == nil {
		return map[string]any{}
	}
	return diff
}

func escapeLike(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `%`, `\%`)
	return strings.ReplaceAll(value, `_`, `\_`)
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func unixTime(value time.Time) int64 {
	if value.IsZero() {
		return 0
	}
	return value.UnixNano()
}

func timeFromUnix(value int64) time.Time {
	if value == 0 {
		return time.Time{}
	}
	return time.Unix(0, value).UTC()
}
