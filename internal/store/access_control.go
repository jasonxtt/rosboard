package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"rosboard/internal/accesscontrol"
	"rosboard/internal/policyv2"
	"rosboard/internal/subject"
)

type AccessRepository struct {
	store *Store
}

const (
	accessSchemaVersion       = "v2"
	legacyAccessSchemaVersion = "v1"
)

func (s *Store) AccessRepository() *AccessRepository {
	return &AccessRepository{store: s}
}

// initAccessControlSchema 建立逻辑规则（AccessRule）持久化结构。
// 项目尚未正式上线、没有存量用户：旧的"1 terminal + 1 source"MVP schema
// 在首次升级时整体删除，不做长期兼容层。
func (s *Store) initAccessControlSchema() error {
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("begin access-control schema initialization: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS access_schema_meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		return fmt.Errorf("init access-control meta: %w", err)
	}
	var marker string
	err = tx.QueryRow(`SELECT value FROM access_schema_meta WHERE key = 'logical_rules'`).Scan(&marker)
	fresh := errors.Is(err, sql.ErrNoRows)
	switch {
	case fresh:
		if err := prepareLegacyAccessSchema(tx); err != nil {
			return err
		}
	case err != nil:
		return fmt.Errorf("inspect access-control schema marker: %w", err)
	case marker != legacyAccessSchemaVersion && marker != accessSchemaVersion:
		return fmt.Errorf("unsupported access-control schema marker %q", marker)
	case marker == legacyAccessSchemaVersion:
		// v1 has all of the original logical-rule tables, but not the additive
		// application relation. Validate the committed baseline before adding it.
		if err := validateLegacyAccessControlSchema(tx); err != nil {
			return fmt.Errorf("validate legacy access-control schema: %w", err)
		}
	case marker == accessSchemaVersion:
		// A committed marker is the boundary between the destructive first-run
		// migration and normal startup. Do not silently recreate a missing or
		// damaged table after that boundary; failing closed preserves the
		// operator's chance to restore the database instead of resetting access
		// desired/applied state to an empty schema.
		if err := validateAccessControlSchema(tx); err != nil {
			return fmt.Errorf("validate marked access-control schema: %w", err)
		}
	}
	_, err = tx.Exec(`
CREATE TABLE IF NOT EXISTS access_rules (
    device_id TEXT NOT NULL,
    id TEXT NOT NULL,
    name TEXT NOT NULL,
    target_scope TEXT NOT NULL,
    action TEXT NOT NULL DEFAULT 'deny',
    enabled INTEGER NOT NULL,
    revision INTEGER NOT NULL,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    subject_mode TEXT NOT NULL DEFAULT 'selected',
    PRIMARY KEY (device_id, id)
);
CREATE TABLE IF NOT EXISTS access_rule_sources (
    device_id TEXT NOT NULL,
    rule_id TEXT NOT NULL,
    source_id TEXT NOT NULL,
    position INTEGER NOT NULL,
    PRIMARY KEY (device_id, rule_id, source_id)
);
CREATE INDEX IF NOT EXISTS access_rule_sources_source_idx ON access_rule_sources(device_id, source_id);
CREATE TABLE IF NOT EXISTS access_rule_prefixes (
    device_id TEXT NOT NULL,
    rule_id TEXT NOT NULL,
    prefix TEXT NOT NULL,
    position INTEGER NOT NULL,
    PRIMARY KEY (device_id, rule_id, prefix)
);
CREATE TABLE IF NOT EXISTS access_rule_applications (
    device_id TEXT NOT NULL,
    rule_id TEXT NOT NULL,
    application_id TEXT NOT NULL,
    position INTEGER NOT NULL,
    PRIMARY KEY (device_id, rule_id, application_id)
);
CREATE INDEX IF NOT EXISTS access_rule_applications_application_idx ON access_rule_applications(device_id, application_id);
CREATE TABLE IF NOT EXISTS access_rule_members (
    device_id TEXT NOT NULL,
    rule_id TEXT NOT NULL,
    terminal_id TEXT NOT NULL,
    binding TEXT NOT NULL,
    anchor_mac TEXT NOT NULL DEFAULT '',
    pinned_ipv4_json TEXT NOT NULL,
    pinned_ipv6_json TEXT NOT NULL,
    last_ipv4_json TEXT NOT NULL DEFAULT '[]',
    last_ipv6_json TEXT NOT NULL DEFAULT '[]',
    PRIMARY KEY (device_id, rule_id, terminal_id)
);
CREATE INDEX IF NOT EXISTS access_rule_members_terminal_idx ON access_rule_members(device_id, terminal_id);
CREATE TABLE IF NOT EXISTS access_control_state (
    device_id TEXT PRIMARY KEY,
    desired_revision INTEGER NOT NULL,
    applied_revision INTEGER NOT NULL,
    applied_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS access_audit (
    device_id TEXT NOT NULL,
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    actor TEXT NOT NULL,
    action TEXT NOT NULL,
    rule_id TEXT NOT NULL,
    before_json TEXT NOT NULL,
    after_json TEXT NOT NULL,
    result TEXT NOT NULL,
    created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS access_audit_device_created_idx ON access_audit(device_id, created_at DESC);
CREATE TABLE IF NOT EXISTS access_rule_migration_issues (
    device_id TEXT NOT NULL,
    rule_id TEXT NOT NULL,
    code TEXT NOT NULL,
    value TEXT NOT NULL,
    message TEXT NOT NULL,
    PRIMARY KEY (device_id, rule_id, code, value)
);
`)
	if err != nil {
		return fmt.Errorf("init access-control schema: %w", err)
	}
	if _, err := tx.Exec(`ALTER TABLE access_rules ADD COLUMN subject_mode TEXT NOT NULL DEFAULT 'selected'`); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
		return fmt.Errorf("add access rule subject mode: %w", err)
	}
	if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS access_rule_prefixes (device_id TEXT NOT NULL, rule_id TEXT NOT NULL, prefix TEXT NOT NULL, position INTEGER NOT NULL, PRIMARY KEY (device_id, rule_id, prefix))`); err != nil {
		return fmt.Errorf("init access rule prefixes: %w", err)
	}
	if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS access_rule_migration_issues (device_id TEXT NOT NULL, rule_id TEXT NOT NULL, code TEXT NOT NULL, value TEXT NOT NULL, message TEXT NOT NULL, PRIMARY KEY (device_id, rule_id, code, value))`); err != nil {
		return fmt.Errorf("init access rule migration issues: %w", err)
	}
	if err := validateAccessControlSchema(tx); err != nil {
		return err
	}
	if fresh {
		if _, err := tx.Exec(`INSERT INTO access_schema_meta (key, value) VALUES ('logical_rules', ?)`, accessSchemaVersion); err != nil {
			return fmt.Errorf("record access-control schema marker: %w", err)
		}
	} else if marker == legacyAccessSchemaVersion {
		if _, err := tx.Exec(`UPDATE access_schema_meta SET value = ? WHERE key = 'logical_rules'`, accessSchemaVersion); err != nil {
			return fmt.Errorf("upgrade access-control schema marker: %w", err)
		}
	}
	if _, err := tx.Exec(`INSERT OR IGNORE INTO access_control_state (device_id, desired_revision, applied_revision, applied_at) VALUES (?, 0, 0, 0)`, s.deviceID); err != nil {
		return fmt.Errorf("init access-control state: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit access-control schema initialization: %w", err)
	}
	return nil
}

func prepareLegacyAccessSchema(tx *sql.Tx) error {
	if _, err := tx.Exec(`DROP TABLE IF EXISTS access_policies`); err != nil {
		return fmt.Errorf("drop legacy access_policies: %w", err)
	}
	// The old audit rows describe the discarded one-terminal MVP model. Drop
	// the table on the same first-run transaction regardless of its column
	// shape; the new table is recreated below and a missing marker means this
	// initialization has not committed yet.
	if _, err := tx.Exec(`DROP TABLE IF EXISTS access_audit`); err != nil {
		return fmt.Errorf("drop legacy access_audit: %w", err)
	}
	return nil
}

func validateAccessControlSchema(tx *sql.Tx) error {
	return validateAccessControlSchemaColumns(tx, true)
}

func validateLegacyAccessControlSchema(tx *sql.Tx) error {
	return validateAccessControlSchemaColumns(tx, false)
}

func validateAccessControlSchemaColumns(tx *sql.Tx, includeApplications bool) error {
	required := map[string][]string{
		"access_schema_meta":   {"key", "value"},
		"access_rules":         {"device_id", "id", "name", "target_scope", "action", "enabled", "revision", "created_at", "updated_at"},
		"access_rule_sources":  {"device_id", "rule_id", "source_id", "position"},
		"access_rule_members":  {"device_id", "rule_id", "terminal_id", "binding", "anchor_mac", "pinned_ipv4_json", "pinned_ipv6_json", "last_ipv4_json", "last_ipv6_json"},
		"access_control_state": {"device_id", "desired_revision", "applied_revision", "applied_at"},
		"access_audit":         {"device_id", "id", "actor", "action", "rule_id", "before_json", "after_json", "result", "created_at"},
	}
	if includeApplications {
		required["access_rule_applications"] = []string{"device_id", "rule_id", "application_id", "position"}
	}
	for table, columns := range required {
		ok, err := accessTableHasColumns(tx, table, columns)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("access-control table %s has an incomplete schema", table)
		}
	}
	return nil
}

func accessTableHasColumns(tx *sql.Tx, table string, required []string) (bool, error) {
	rows, err := tx.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return false, fmt.Errorf("inspect access-control table %s columns: %w", table, err)
	}
	defer rows.Close()
	columns := make(map[string]bool, len(required))
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull, primaryKey int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return false, fmt.Errorf("scan access-control table %s columns: %w", table, err)
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("read access-control table %s columns: %w", table, err)
	}
	for _, column := range required {
		if !columns[column] {
			return false, nil
		}
	}
	return true, nil
}

type accessRuleSnapshot struct {
	Rule    accesscontrol.AccessRule   `json:"rule"`
	Members []accesscontrol.RuleMember `json:"members"`
}

func (r *AccessRepository) ListRules(ctx context.Context) ([]accesscontrol.AccessRule, error) {
	rows, err := r.store.db.QueryContext(ctx, `SELECT id, name, target_scope, subject_mode, enabled, revision, created_at, updated_at
		FROM access_rules WHERE device_id = ? ORDER BY created_at, id`, r.store.deviceID)
	if err != nil {
		return nil, fmt.Errorf("list access rules: %w", err)
	}
	rules := make([]accesscontrol.AccessRule, 0)
	byID := make(map[string]int)
	for rows.Next() {
		var rule accesscontrol.AccessRule
		var enabled int
		var createdAt, updatedAt int64
		if err := rows.Scan(&rule.ID, &rule.Name, &rule.TargetScope, &rule.Subject.Mode, &enabled, &rule.Revision, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan access rule: %w", err)
		}
		rule.Enabled = enabled != 0
		rule.CreatedAt = timeFromUnix(createdAt)
		rule.UpdatedAt = timeFromUnix(updatedAt)
		rule.SourceIDs = []string{}
		rule.ApplicationIDs = []string{}
		rule.TargetListIDs = []string{}
		rule.MigrationIssues = []string{}
		byID[rule.ID] = len(rules)
		rules = append(rules, rule)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	sourceRows, err := r.store.db.QueryContext(ctx, `SELECT rule_id, source_id FROM access_rule_sources WHERE device_id = ? ORDER BY rule_id, position`, r.store.deviceID)
	if err != nil {
		return nil, fmt.Errorf("list access rule sources: %w", err)
	}
	for sourceRows.Next() {
		var ruleID, sourceID string
		if err := sourceRows.Scan(&ruleID, &sourceID); err != nil {
			return nil, fmt.Errorf("scan access rule source: %w", err)
		}
		if index, ok := byID[ruleID]; ok {
			rules[index].SourceIDs = append(rules[index].SourceIDs, sourceID)
		}
	}
	if err := sourceRows.Err(); err != nil {
		return nil, err
	}
	if err := sourceRows.Close(); err != nil {
		return nil, err
	}
	applicationRows, err := r.store.db.QueryContext(ctx, `SELECT rule_id, application_id FROM access_rule_applications WHERE device_id = ? ORDER BY rule_id, position`, r.store.deviceID)
	if err != nil {
		return nil, fmt.Errorf("list access rule applications: %w", err)
	}
	for applicationRows.Next() {
		var ruleID, applicationID string
		if err := applicationRows.Scan(&ruleID, &applicationID); err != nil {
			return nil, fmt.Errorf("scan access rule application: %w", err)
		}
		if index, ok := byID[ruleID]; ok {
			rules[index].ApplicationIDs = append(rules[index].ApplicationIDs, applicationID)
		}
	}
	if err := applicationRows.Err(); err != nil {
		return nil, err
	}
	if err := applicationRows.Close(); err != nil {
		return nil, err
	}
	prefixRows, err := r.store.db.QueryContext(ctx, `SELECT rule_id, prefix FROM access_rule_prefixes WHERE device_id = ? ORDER BY rule_id, position`, r.store.deviceID)
	if err != nil {
		return nil, fmt.Errorf("list access rule subject prefixes: %w", err)
	}
	for prefixRows.Next() {
		var ruleID, prefix string
		if err := prefixRows.Scan(&ruleID, &prefix); err != nil {
			prefixRows.Close()
			return nil, err
		}
		if index, ok := byID[ruleID]; ok {
			rules[index].Subject.Prefixes = append(rules[index].Subject.Prefixes, prefix)
		}
	}
	if err := prefixRows.Err(); err != nil {
		prefixRows.Close()
		return nil, err
	}
	if err := prefixRows.Close(); err != nil {
		return nil, err
	}
	memberRows, err := r.store.db.QueryContext(ctx, `SELECT rule_id, terminal_id, binding, anchor_mac, pinned_ipv4_json, pinned_ipv6_json, last_ipv4_json, last_ipv6_json FROM access_rule_members WHERE device_id = ? ORDER BY rule_id, terminal_id`, r.store.deviceID)
	if err != nil {
		return nil, fmt.Errorf("list access rule subject members: %w", err)
	}
	for memberRows.Next() {
		member, err := scanAccessMember(memberRows)
		if err != nil {
			memberRows.Close()
			return nil, err
		}
		if index, ok := byID[member.RuleID]; ok {
			rules[index].Subject.Members = append(rules[index].Subject.Members, accessMemberSubject(member))
		}
	}
	if err := memberRows.Err(); err != nil {
		memberRows.Close()
		return nil, err
	}
	if err := memberRows.Close(); err != nil {
		return nil, err
	}
	issueRows, err := r.store.db.QueryContext(ctx, `SELECT rule_id, code, value FROM access_rule_migration_issues WHERE device_id = ? ORDER BY rule_id, code, value`, r.store.deviceID)
	if err != nil {
		return nil, fmt.Errorf("list access rule migration issues: %w", err)
	}
	for issueRows.Next() {
		var ruleID, code, value string
		if err := issueRows.Scan(&ruleID, &code, &value); err != nil {
			issueRows.Close()
			return nil, err
		}
		if index, ok := byID[ruleID]; ok {
			rules[index].MigrationIssues = append(rules[index].MigrationIssues, code+": "+value)
		}
	}
	if err := issueRows.Err(); err != nil {
		issueRows.Close()
		return nil, err
	}
	if err := issueRows.Close(); err != nil {
		return nil, err
	}
	for index := range rules {
		if rules[index].Subject.Mode == "" {
			rules[index].Subject.Mode = "selected"
		}
		if rules[index].TargetScope == accesscontrol.TargetScopeTargets {
			rules[index].TargetListIDs = append([]string{}, rules[index].SourceIDs...)
			rules[index].SourceIDs = []string{}
		}
	}
	return rules, nil
}

func accessMemberSubject(member accesscontrol.RuleMember) subject.Member {
	return subject.Member{TerminalID: member.TerminalID, Binding: member.Binding, AnchorMAC: member.AnchorMAC,
		PinnedIPv4: append([]string{}, member.PinnedIPv4...), PinnedIPv6: append([]string{}, member.PinnedIPv6...),
		LastIPv4: append([]string{}, member.LastIPv4...), LastIPv6: append([]string{}, member.LastIPv6...)}
}

func (r *AccessRepository) ListMembers(ctx context.Context) ([]accesscontrol.RuleMember, error) {
	rows, err := r.store.db.QueryContext(ctx, `SELECT rule_id, terminal_id, binding, anchor_mac, pinned_ipv4_json, pinned_ipv6_json, last_ipv4_json, last_ipv6_json
		FROM access_rule_members WHERE device_id = ? ORDER BY rule_id, terminal_id`, r.store.deviceID)
	if err != nil {
		return nil, fmt.Errorf("list access rule members: %w", err)
	}
	defer rows.Close()
	members := make([]accesscontrol.RuleMember, 0)
	for rows.Next() {
		member, err := scanAccessMember(rows)
		if err != nil {
			return nil, err
		}
		members = append(members, member)
	}
	return members, rows.Err()
}

func (r *AccessRepository) SaveRule(ctx context.Context, rule accesscontrol.AccessRule, members []accesscontrol.RuleMember, actor string) (accesscontrol.AccessRule, error) {
	if len(members) == 0 && len(rule.Subject.Members) > 0 {
		members = make([]accesscontrol.RuleMember, 0, len(rule.Subject.Members))
		for _, member := range rule.Subject.Members {
			members = append(members, accesscontrol.RuleMember{
				TerminalID: member.TerminalID, Binding: member.Binding, AnchorMAC: member.AnchorMAC,
				PinnedIPv4: append([]string{}, member.PinnedIPv4...), PinnedIPv6: append([]string{}, member.PinnedIPv6...),
				LastIPv4: append([]string{}, member.LastIPv4...), LastIPv6: append([]string{}, member.LastIPv6...),
			})
		}
	}
	rule, err := accesscontrol.NormalizeRule(rule)
	if err != nil {
		return accesscontrol.AccessRule{}, err
	}
	var canonicalMarker string
	if markerErr := r.store.db.QueryRowContext(ctx, `SELECT value FROM access_schema_meta WHERE key = ?`, canonicalAccessMarkerKey).Scan(&canonicalMarker); markerErr == nil && canonicalMarker == "v1" {
		if rule.TargetScope == accesscontrol.TargetScopeSources || rule.TargetScope == accesscontrol.TargetScopeApplications || len(rule.SourceIDs) != 0 || len(rule.ApplicationIDs) != 0 {
			return accesscontrol.AccessRule{}, accesscontrol.ErrCanonicalRuleRequired
		}
	}
	normalizedMembers := make([]accesscontrol.RuleMember, 0, len(members))
	seenTerminals := make(map[string]bool, len(members))
	for _, member := range members {
		member.RuleID = rule.ID
		member, err := accesscontrol.NormalizeMember(member)
		if err != nil {
			return accesscontrol.AccessRule{}, err
		}
		if seenTerminals[member.TerminalID] {
			return accesscontrol.AccessRule{}, accesscontrol.ErrMemberDuplicate
		}
		seenTerminals[member.TerminalID] = true
		normalizedMembers = append(normalizedMembers, member)
	}
	if rule.Subject.Mode != "all" && len(normalizedMembers) == 0 && len(rule.Subject.Prefixes) == 0 {
		return accesscontrol.AccessRule{}, errors.New("access rule requires at least one member")
	}

	tx, err := r.store.db.BeginTx(ctx, nil)
	if err != nil {
		return accesscontrol.AccessRule{}, fmt.Errorf("begin save access rule: %w", err)
	}
	defer tx.Rollback()
	// Take the SQLite write lock before checking source ownership. Otherwise a
	// concurrent source deletion could pass the read and leave a dangling
	// access-rule reference before this transaction's first write.
	lockResult, err := tx.ExecContext(ctx, `UPDATE access_control_state SET desired_revision = desired_revision WHERE device_id = ?`, r.store.deviceID)
	if err != nil {
		return accesscontrol.AccessRule{}, fmt.Errorf("lock access-control state for save: %w", err)
	}
	if rows, err := lockResult.RowsAffected(); err != nil || rows != 1 {
		if err != nil {
			return accesscontrol.AccessRule{}, fmt.Errorf("read access-control save lock result: %w", err)
		}
		return accesscontrol.AccessRule{}, errors.New("access-control state is missing")
	}

	now := time.Now().UTC()
	current, err := loadAccessRuleTx(ctx, tx, r.store.deviceID, rule.ID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if rule.Revision != 0 {
			return accesscontrol.AccessRule{}, accesscontrol.ErrRevisionStale
		}
		rule.Revision = 1
		rule.CreatedAt = now
	case err != nil:
		return accesscontrol.AccessRule{}, fmt.Errorf("load access rule before save: %w", err)
	default:
		if rule.Revision != current.Rule.Revision {
			return accesscontrol.AccessRule{}, accesscontrol.ErrRevisionStale
		}
		rule.Revision = current.Rule.Revision + 1
		rule.CreatedAt = current.Rule.CreatedAt
	}
	rule.UpdatedAt = now
	if rule.TargetScope == accesscontrol.TargetScopeSources {
		currentSourceIDs := make(map[string]bool, len(current.Rule.SourceIDs))
		for _, sourceID := range current.Rule.SourceIDs {
			currentSourceIDs[sourceID] = true
		}
		for _, sourceID := range rule.SourceIDs {
			var pendingDeletion int
			if err := tx.QueryRowContext(ctx, `SELECT pending_delete FROM policy_v2_sources WHERE id = ?`, sourceID).Scan(&pendingDeletion); errors.Is(err, sql.ErrNoRows) {
				return accesscontrol.AccessRule{}, policyv2.ErrSourceNotFound
			} else if err != nil {
				return accesscontrol.AccessRule{}, fmt.Errorf("check access rule source %s: %w", sourceID, err)
			} else if pendingDeletion != 0 && !currentSourceIDs[sourceID] {
				return accesscontrol.AccessRule{}, policyv2.ErrSourceNotFound
			}
		}
	}
	if rule.TargetScope == accesscontrol.TargetScopeTargets {
		for _, targetID := range rule.TargetListIDs {
			var pendingDeletion int
			if err := tx.QueryRowContext(ctx, `SELECT pending_delete FROM policy_v2_sources WHERE id = ?`, targetID).Scan(&pendingDeletion); errors.Is(err, sql.ErrNoRows) {
				return accesscontrol.AccessRule{}, policyv2.ErrTargetListNotFound
			} else if err != nil {
				return accesscontrol.AccessRule{}, fmt.Errorf("check access rule target %s: %w", targetID, err)
			} else if pendingDeletion != 0 {
				return accesscontrol.AccessRule{}, policyv2.ErrTargetListNotFound
			}
		}
	}
	if rule.Subject.Mode == "all" && len(normalizedMembers) != 0 {
		return accesscontrol.AccessRule{}, errors.New("all subjects must not contain members")
	}
	rule.Subject.Members = make([]subject.Member, 0, len(normalizedMembers))
	for _, member := range normalizedMembers {
		rule.Subject.Members = append(rule.Subject.Members, accessMemberSubject(member))
	}

	// 同一终端再次加入时保留其最后可信地址投影。
	lastByTerminal := make(map[string]accesscontrol.RuleMember, len(current.Members))
	for _, member := range current.Members {
		lastByTerminal[member.TerminalID] = member
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO access_rules (device_id, id, name, target_scope, action, enabled, revision, created_at, updated_at, subject_mode)
		VALUES (?, ?, ?, ?, 'deny', ?, ?, ?, ?, ?)
		ON CONFLICT(device_id, id) DO UPDATE SET name=excluded.name, target_scope=excluded.target_scope, enabled=excluded.enabled, revision=excluded.revision, updated_at=excluded.updated_at, subject_mode=excluded.subject_mode`,
		r.store.deviceID, rule.ID, rule.Name, rule.TargetScope, boolToInt(rule.Enabled), rule.Revision, unixTime(rule.CreatedAt), unixTime(rule.UpdatedAt), rule.Subject.Mode)
	if err != nil {
		return accesscontrol.AccessRule{}, fmt.Errorf("save access rule: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM access_rule_sources WHERE device_id = ? AND rule_id = ?`, r.store.deviceID, rule.ID); err != nil {
		return accesscontrol.AccessRule{}, fmt.Errorf("replace access rule sources: %w", err)
	}
	targetIDs := rule.SourceIDs
	if rule.TargetScope == accesscontrol.TargetScopeTargets {
		targetIDs = rule.TargetListIDs
	}
	for position, sourceID := range targetIDs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO access_rule_sources (device_id, rule_id, source_id, position) VALUES (?, ?, ?, ?)`,
			r.store.deviceID, rule.ID, sourceID, position); err != nil {
			return accesscontrol.AccessRule{}, fmt.Errorf("save access rule source: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM access_rule_prefixes WHERE device_id = ? AND rule_id = ?`, r.store.deviceID, rule.ID); err != nil {
		return accesscontrol.AccessRule{}, fmt.Errorf("replace access rule subject prefixes: %w", err)
	}
	for position, prefix := range rule.Subject.Prefixes {
		if _, err := tx.ExecContext(ctx, `INSERT INTO access_rule_prefixes (device_id, rule_id, prefix, position) VALUES (?, ?, ?, ?)`, r.store.deviceID, rule.ID, prefix, position); err != nil {
			return accesscontrol.AccessRule{}, fmt.Errorf("save access rule subject prefix: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM access_rule_applications WHERE device_id = ? AND rule_id = ?`, r.store.deviceID, rule.ID); err != nil {
		return accesscontrol.AccessRule{}, fmt.Errorf("replace access rule applications: %w", err)
	}
	for position, applicationID := range rule.ApplicationIDs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO access_rule_applications (device_id, rule_id, application_id, position) VALUES (?, ?, ?, ?)`,
			r.store.deviceID, rule.ID, applicationID, position); err != nil {
			return accesscontrol.AccessRule{}, fmt.Errorf("save access rule application: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM access_rule_members WHERE device_id = ? AND rule_id = ?`, r.store.deviceID, rule.ID); err != nil {
		return accesscontrol.AccessRule{}, fmt.Errorf("replace access rule members: %w", err)
	}
	persistedMembers := make([]accesscontrol.RuleMember, 0, len(normalizedMembers))
	for _, member := range normalizedMembers {
		previous, hasPrevious := lastByTerminal[member.TerminalID]
		if member.Binding == accesscontrol.BindingAuto {
			previousAnchor := ""
			if hasPrevious && previous.Binding == accesscontrol.BindingAuto && strings.TrimSpace(previous.AnchorMAC) != "" {
				previousAnchor, err = accesscontrol.NormalizeMAC(previous.AnchorMAC)
				if err != nil {
					return accesscontrol.AccessRule{}, accesscontrol.ErrMemberAnchorRequired
				}
			}
			if member.AnchorMAC == "" && hasPrevious {
				member.AnchorMAC = previousAnchor
			}
			if member.AnchorMAC == "" {
				return accesscontrol.AccessRule{}, accesscontrol.ErrMemberAnchorRequired
			}
			member, err = accesscontrol.NormalizeMember(member)
			if err != nil {
				return accesscontrol.AccessRule{}, err
			}
			if hasPrevious && previous.Binding == accesscontrol.BindingAuto && previousAnchor != "" && member.AnchorMAC != previousAnchor {
				return accesscontrol.AccessRule{}, accesscontrol.ErrMemberAnchorChanged
			}
		}
		lastIPv4, lastIPv6 := "[]", "[]"
		// A trusted projection belongs only to an unchanged auto-follow
		// identity. Fixed bindings are address-owned; carrying their old
		// auto-follow cache across a binding switch could later reintroduce a
		// stale address if the member is switched back to auto while offline.
		previousAnchor := ""
		if hasPrevious && previous.Binding == accesscontrol.BindingAuto && strings.TrimSpace(previous.AnchorMAC) != "" {
			previousAnchor, err = accesscontrol.NormalizeMAC(previous.AnchorMAC)
			if err != nil {
				return accesscontrol.AccessRule{}, accesscontrol.ErrMemberAnchorRequired
			}
		}
		if hasPrevious && previous.Binding == accesscontrol.BindingAuto && member.Binding == accesscontrol.BindingAuto && previousAnchor != "" && previousAnchor == member.AnchorMAC {
			if previousJSON, err := json.Marshal(firstNonNil(previous.LastIPv4)); err == nil {
				lastIPv4 = string(previousJSON)
				member.LastIPv4 = firstNonNil(previous.LastIPv4)
			}
			if previousJSON, err := json.Marshal(firstNonNil(previous.LastIPv6)); err == nil {
				lastIPv6 = string(previousJSON)
				member.LastIPv6 = firstNonNil(previous.LastIPv6)
			}
		}
		pinnedIPv4, _ := json.Marshal(firstNonNil(member.PinnedIPv4))
		pinnedIPv6, _ := json.Marshal(firstNonNil(member.PinnedIPv6))
		if _, err := tx.ExecContext(ctx, `INSERT INTO access_rule_members (device_id, rule_id, terminal_id, binding, anchor_mac, pinned_ipv4_json, pinned_ipv6_json, last_ipv4_json, last_ipv6_json)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			r.store.deviceID, rule.ID, member.TerminalID, member.Binding, member.AnchorMAC, string(pinnedIPv4), string(pinnedIPv6), lastIPv4, lastIPv6); err != nil {
			return accesscontrol.AccessRule{}, fmt.Errorf("save access rule member: %w", err)
		}
		persistedMembers = append(persistedMembers, member)
	}
	if err := bumpAccessDesiredRevision(ctx, tx, r.store.deviceID); err != nil {
		return accesscontrol.AccessRule{}, err
	}
	if err := writeAccessAudit(ctx, tx, r.store.deviceID, actor, "save", rule.ID, current, accessRuleSnapshot{Rule: rule, Members: persistedMembers}, "success"); err != nil {
		return accesscontrol.AccessRule{}, err
	}
	if err := tx.Commit(); err != nil {
		return accesscontrol.AccessRule{}, fmt.Errorf("commit access rule: %w", err)
	}
	return rule, nil
}

func (r *AccessRepository) DeleteRule(ctx context.Context, id string, revision int64, actor string) error {
	tx, err := r.store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin delete access rule: %w", err)
	}
	defer tx.Rollback()
	current, err := loadAccessRuleTx(ctx, tx, r.store.deviceID, strings.TrimSpace(id))
	if errors.Is(err, sql.ErrNoRows) {
		return accesscontrol.ErrRuleNotFound
	}
	if err != nil {
		return fmt.Errorf("load access rule before delete: %w", err)
	}
	if current.Rule.Revision != revision {
		return accesscontrol.ErrRevisionStale
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM access_rules WHERE device_id = ? AND id = ?`, r.store.deviceID, current.Rule.ID); err != nil {
		return fmt.Errorf("delete access rule: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM access_rule_sources WHERE device_id = ? AND rule_id = ?`, r.store.deviceID, current.Rule.ID); err != nil {
		return fmt.Errorf("delete access rule sources: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM access_rule_applications WHERE device_id = ? AND rule_id = ?`, r.store.deviceID, current.Rule.ID); err != nil {
		return fmt.Errorf("delete access rule applications: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM access_rule_members WHERE device_id = ? AND rule_id = ?`, r.store.deviceID, current.Rule.ID); err != nil {
		return fmt.Errorf("delete access rule members: %w", err)
	}
	if err := bumpAccessDesiredRevision(ctx, tx, r.store.deviceID); err != nil {
		return err
	}
	if err := writeAccessAudit(ctx, tx, r.store.deviceID, actor, "delete", current.Rule.ID, current, nil, "success"); err != nil {
		return err
	}
	return tx.Commit()
}

// SaveMemberResolutions records the last confirmed address projection of one
// auto-follow member. It deliberately does not bump the desired revision:
// monitor-driven address changes are picked up by the periodic reconcile.
func (r *AccessRepository) SaveMemberResolutions(ctx context.Context, ruleID, terminalID string, ipv4, ipv6 []string) error {
	var anchor string
	if err := r.store.db.QueryRowContext(ctx, `SELECT anchor_mac FROM access_rule_members
		WHERE device_id = ? AND rule_id = ? AND terminal_id = ? AND binding = 'auto' AND anchor_mac <> ''`,
		r.store.deviceID, strings.TrimSpace(ruleID), strings.TrimSpace(terminalID)).Scan(&anchor); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return accesscontrol.ErrMemberAnchorRequired
		}
		return fmt.Errorf("load access member anchor: %w", err)
	}
	resolution, err := accesscontrol.NormalizeMemberResolution(accesscontrol.MemberResolution{
		RuleID: ruleID, TerminalID: terminalID, AnchorMAC: anchor, IPv4: ipv4, IPv6: ipv6,
	})
	if err != nil {
		return err
	}
	ipv4JSON, _ := json.Marshal(resolution.IPv4)
	ipv6JSON, _ := json.Marshal(resolution.IPv6)
	result, err := r.store.db.ExecContext(ctx, `UPDATE access_rule_members SET last_ipv4_json = ?, last_ipv6_json = ?
		WHERE device_id = ? AND rule_id = ? AND terminal_id = ? AND binding = 'auto' AND anchor_mac <> ''`,
		string(ipv4JSON), string(ipv6JSON), r.store.deviceID, resolution.RuleID, resolution.TerminalID)
	if err != nil {
		return fmt.Errorf("save access member resolutions: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read access member resolution result: %w", err)
	}
	if rows != 1 {
		return accesscontrol.ErrMemberAnchorRequired
	}
	return nil
}

func saveMemberResolutionTx(ctx context.Context, tx *sql.Tx, deviceID string, resolution accesscontrol.MemberResolution) error {
	resolution, err := accesscontrol.NormalizeMemberResolution(resolution)
	if err != nil {
		return err
	}
	ipv4JSON, err := json.Marshal(resolution.IPv4)
	if err != nil {
		return fmt.Errorf("marshal IPv4 resolution: %w", err)
	}
	ipv6JSON, err := json.Marshal(resolution.IPv6)
	if err != nil {
		return fmt.Errorf("marshal IPv6 resolution: %w", err)
	}
	result, err := tx.ExecContext(ctx, `UPDATE access_rule_members
		SET anchor_mac = CASE WHEN anchor_mac = '' THEN ? ELSE anchor_mac END,
			last_ipv4_json = ?, last_ipv6_json = ?
		WHERE device_id = ? AND rule_id = ? AND terminal_id = ? AND binding = 'auto'
			AND (anchor_mac = '' OR anchor_mac = ?)`,
		resolution.AnchorMAC, string(ipv4JSON), string(ipv6JSON), deviceID, resolution.RuleID, resolution.TerminalID, resolution.AnchorMAC)
	if err != nil {
		return fmt.Errorf("save access member resolution: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return accesscontrol.ErrRevisionStale
	}
	return nil
}

func (r *AccessRepository) GetState(ctx context.Context) (accesscontrol.State, error) {
	var state accesscontrol.State
	var appliedAt int64
	err := r.store.db.QueryRowContext(ctx, `SELECT desired_revision, applied_revision, applied_at FROM access_control_state WHERE device_id = ?`, r.store.deviceID).Scan(&state.DesiredRevision, &state.AppliedRevision, &appliedAt)
	if err != nil {
		return accesscontrol.State{}, fmt.Errorf("get access-control state: %w", err)
	}
	state.DeviceID = r.store.deviceID
	state.AppliedAt = timeFromUnix(appliedAt)
	return state, nil
}

func loadAccessRuleTx(ctx context.Context, tx *sql.Tx, deviceID, id string) (accessRuleSnapshot, error) {
	snapshot := accessRuleSnapshot{Members: []accesscontrol.RuleMember{}}
	var enabled int
	var createdAt, updatedAt int64
	err := tx.QueryRowContext(ctx, `SELECT id, name, target_scope, subject_mode, enabled, revision, created_at, updated_at
		FROM access_rules WHERE device_id = ? AND id = ?`, deviceID, id).Scan(
		&snapshot.Rule.ID, &snapshot.Rule.Name, &snapshot.Rule.TargetScope, &snapshot.Rule.Subject.Mode, &enabled, &snapshot.Rule.Revision, &createdAt, &updatedAt)
	if err != nil {
		return accessRuleSnapshot{}, err
	}
	snapshot.Rule.Enabled = enabled != 0
	snapshot.Rule.CreatedAt = timeFromUnix(createdAt)
	snapshot.Rule.UpdatedAt = timeFromUnix(updatedAt)
	snapshot.Rule.SourceIDs = []string{}
	snapshot.Rule.ApplicationIDs = []string{}
	snapshot.Rule.TargetListIDs = []string{}
	sourceRows, err := tx.QueryContext(ctx, `SELECT source_id FROM access_rule_sources WHERE device_id = ? AND rule_id = ? ORDER BY position`, deviceID, id)
	if err != nil {
		return accessRuleSnapshot{}, err
	}
	defer sourceRows.Close()
	for sourceRows.Next() {
		var sourceID string
		if err := sourceRows.Scan(&sourceID); err != nil {
			return accessRuleSnapshot{}, err
		}
		snapshot.Rule.SourceIDs = append(snapshot.Rule.SourceIDs, sourceID)
	}
	if err := sourceRows.Err(); err != nil {
		return accessRuleSnapshot{}, err
	}
	if err := sourceRows.Close(); err != nil {
		return accessRuleSnapshot{}, err
	}
	applicationRows, err := tx.QueryContext(ctx, `SELECT application_id FROM access_rule_applications WHERE device_id = ? AND rule_id = ? ORDER BY position`, deviceID, id)
	if err != nil {
		return accessRuleSnapshot{}, err
	}
	defer applicationRows.Close()
	for applicationRows.Next() {
		var applicationID string
		if err := applicationRows.Scan(&applicationID); err != nil {
			return accessRuleSnapshot{}, err
		}
		snapshot.Rule.ApplicationIDs = append(snapshot.Rule.ApplicationIDs, applicationID)
	}
	if err := applicationRows.Err(); err != nil {
		return accessRuleSnapshot{}, err
	}
	if err := applicationRows.Close(); err != nil {
		return accessRuleSnapshot{}, err
	}
	prefixRows, err := tx.QueryContext(ctx, `SELECT prefix FROM access_rule_prefixes WHERE device_id = ? AND rule_id = ? ORDER BY position`, deviceID, id)
	if err != nil {
		return accessRuleSnapshot{}, err
	}
	for prefixRows.Next() {
		var prefix string
		if err := prefixRows.Scan(&prefix); err != nil {
			prefixRows.Close()
			return accessRuleSnapshot{}, err
		}
		snapshot.Rule.Subject.Prefixes = append(snapshot.Rule.Subject.Prefixes, prefix)
	}
	if err := prefixRows.Err(); err != nil {
		prefixRows.Close()
		return accessRuleSnapshot{}, err
	}
	if err := prefixRows.Close(); err != nil {
		return accessRuleSnapshot{}, err
	}
	memberRows, err := tx.QueryContext(ctx, `SELECT rule_id, terminal_id, binding, anchor_mac, pinned_ipv4_json, pinned_ipv6_json, last_ipv4_json, last_ipv6_json
		FROM access_rule_members WHERE device_id = ? AND rule_id = ? ORDER BY terminal_id`, deviceID, id)
	if err != nil {
		return accessRuleSnapshot{}, err
	}
	defer memberRows.Close()
	for memberRows.Next() {
		member, err := scanAccessMember(memberRows)
		if err != nil {
			return accessRuleSnapshot{}, err
		}
		snapshot.Members = append(snapshot.Members, member)
	}
	if err := memberRows.Err(); err != nil {
		return accessRuleSnapshot{}, err
	}
	if err := memberRows.Close(); err != nil {
		return accessRuleSnapshot{}, err
	}
	for _, member := range snapshot.Members {
		snapshot.Rule.Subject.Members = append(snapshot.Rule.Subject.Members, accessMemberSubject(member))
	}
	if snapshot.Rule.TargetScope == accesscontrol.TargetScopeTargets {
		snapshot.Rule.TargetListIDs = append([]string{}, snapshot.Rule.SourceIDs...)
		snapshot.Rule.SourceIDs = []string{}
	}
	if snapshot.Rule.Subject.Mode == "" {
		snapshot.Rule.Subject.Mode = "selected"
	}
	return snapshot, nil
}

func scanAccessMember(row rowScanner) (accesscontrol.RuleMember, error) {
	var member accesscontrol.RuleMember
	var pinnedIPv4, pinnedIPv6, lastIPv4, lastIPv6 string
	if err := row.Scan(&member.RuleID, &member.TerminalID, &member.Binding, &member.AnchorMAC, &pinnedIPv4, &pinnedIPv6, &lastIPv4, &lastIPv6); err != nil {
		return accesscontrol.RuleMember{}, err
	}
	for _, field := range []struct {
		name, encoded string
	}{
		{"pinned IPv4", pinnedIPv4}, {"pinned IPv6", pinnedIPv6}, {"last IPv4", lastIPv4}, {"last IPv6", lastIPv6},
	} {
		if strings.TrimSpace(field.encoded) == "" || strings.TrimSpace(field.encoded) == "null" {
			continue
		}
		var values []string
		if err := json.Unmarshal([]byte(field.encoded), &values); err != nil {
			return accesscontrol.RuleMember{}, fmt.Errorf("decode access member %s: %w", field.name, err)
		}
		switch field.name {
		case "pinned IPv4":
			member.PinnedIPv4 = values
		case "pinned IPv6":
			member.PinnedIPv6 = values
		case "last IPv4":
			member.LastIPv4 = values
		case "last IPv6":
			member.LastIPv6 = values
		}
	}
	member.PinnedIPv4 = nonNilStringSlice(member.PinnedIPv4)
	member.PinnedIPv6 = nonNilStringSlice(member.PinnedIPv6)
	member.LastIPv4 = nonNilStringSlice(member.LastIPv4)
	member.LastIPv6 = nonNilStringSlice(member.LastIPv6)
	return member, nil
}

func nonNilStringSlice(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func firstNonNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func bumpAccessDesiredRevision(ctx context.Context, tx *sql.Tx, deviceID string) error {
	result, err := tx.ExecContext(ctx, `UPDATE access_control_state SET desired_revision = desired_revision + 1 WHERE device_id = ?`, deviceID)
	if err != nil {
		return fmt.Errorf("bump access-control revision: %w", err)
	}
	if rows, err := result.RowsAffected(); err != nil || rows != 1 {
		if err != nil {
			return fmt.Errorf("read access-control revision result: %w", err)
		}
		return errors.New("access-control state is missing")
	}
	return nil
}

func writeAccessAudit(ctx context.Context, tx *sql.Tx, deviceID, actor, action, ruleID string, before, after any, result string) error {
	beforeJSON, err := json.Marshal(before)
	if err != nil {
		return err
	}
	afterJSON, err := json.Marshal(after)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO access_audit (device_id, actor, action, rule_id, before_json, after_json, result, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		deviceID, strings.TrimSpace(actor), action, ruleID, string(beforeJSON), string(afterJSON), result, unixTime(time.Now().UTC())); err != nil {
		return fmt.Errorf("write access-control audit: %w", err)
	}
	return nil
}
