package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"rosboard/internal/policyv2"
)

func (s *Store) initRoutingRuleSchema() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS policy_v2_schema_meta (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS policy_v2_routing_rules (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    egress_id TEXT NOT NULL REFERENCES policy_v2_egresses(id) ON DELETE RESTRICT,
    subject_mode TEXT NOT NULL,
    ingress_interface_lists_json TEXT NOT NULL DEFAULT '[]',
    ingress_interfaces_json TEXT NOT NULL DEFAULT '[]',
    priority INTEGER NOT NULL,
    enabled INTEGER NOT NULL,
    revision INTEGER NOT NULL,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS policy_v2_routing_rules_egress_idx ON policy_v2_routing_rules(egress_id);
CREATE TABLE IF NOT EXISTS policy_v2_routing_rule_targets (
    rule_id TEXT NOT NULL REFERENCES policy_v2_routing_rules(id) ON DELETE CASCADE,
    target_id TEXT NOT NULL REFERENCES policy_v2_sources(id) ON DELETE RESTRICT,
    position INTEGER NOT NULL,
    PRIMARY KEY (rule_id, target_id)
);
CREATE INDEX IF NOT EXISTS policy_v2_routing_rule_targets_target_idx ON policy_v2_routing_rule_targets(target_id);
CREATE TABLE IF NOT EXISTS policy_v2_routing_rule_members (
    rule_id TEXT NOT NULL REFERENCES policy_v2_routing_rules(id) ON DELETE CASCADE,
    terminal_id TEXT NOT NULL,
    binding TEXT NOT NULL,
    anchor_mac TEXT NOT NULL,
    pinned_ipv4_json TEXT NOT NULL,
    pinned_ipv6_json TEXT NOT NULL,
    last_ipv4_json TEXT NOT NULL,
    last_ipv6_json TEXT NOT NULL,
    PRIMARY KEY (rule_id, terminal_id)
);
CREATE TABLE IF NOT EXISTS policy_v2_routing_rule_prefixes (
    rule_id TEXT NOT NULL REFERENCES policy_v2_routing_rules(id) ON DELETE CASCADE,
    prefix TEXT NOT NULL,
    position INTEGER NOT NULL,
    PRIMARY KEY (rule_id, prefix)
);`)
	if err != nil {
		return fmt.Errorf("init routing rule schema: %w", err)
	}
	if _, err := s.db.Exec(`ALTER TABLE policy_v2_routing_rules ADD COLUMN ingress_interface_lists_json TEXT NOT NULL DEFAULT '[]'`); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
		return fmt.Errorf("add routing rule ingress interface lists: %w", err)
	}
	if _, err := s.db.Exec(`ALTER TABLE policy_v2_routing_rules ADD COLUMN ingress_interfaces_json TEXT NOT NULL DEFAULT '[]'`); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
		return fmt.Errorf("add routing rule ingress interfaces: %w", err)
	}
	return nil
}

func (r *PolicyRepository) EnsureRoutingRulesMigrated(ctx context.Context) error {
	return r.store.migrateLegacyRoutingRules(ctx)
}

func (r *PolicyRepository) RoutingAuthority(ctx context.Context) (string, error) {
	var value string
	err := r.store.db.QueryRowContext(ctx, `SELECT value FROM policy_v2_schema_meta WHERE key = ?`, policyv2.RoutingRuleAuthorityKey).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read routing rule authority: %w", err)
	}
	return value, nil
}

// migrateLegacyRoutingRules converts the old Source.EgressID relation once.
// It intentionally leaves the marker absent on a brand-new empty database so
// fixture code can still populate legacy rows before the first desired build.
func (s *Store) migrateLegacyRoutingRules(ctx context.Context) error {
	var marker string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM policy_v2_schema_meta WHERE key = ?`, policyv2.RoutingRuleAuthorityKey).Scan(&marker)
	if err == nil {
		if marker != policyv2.RoutingRuleAuthorityV1 {
			return fmt.Errorf("unsupported routing rule authority %q", marker)
		}
		return s.migrateRoutingRuleIngress(ctx)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("inspect routing rule authority: %w", err)
	}
	var legacyCount int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM policy_v2_sources WHERE trim(egress_id) <> ''`).Scan(&legacyCount); err != nil {
		return fmt.Errorf("inspect legacy routing associations: %w", err)
	}
	if legacyCount == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin routing rule migration: %w", err)
	}
	defer tx.Rollback()
	var transactionMarker string
	err = tx.QueryRowContext(ctx, `SELECT value FROM policy_v2_schema_meta WHERE key = ?`, policyv2.RoutingRuleAuthorityKey).Scan(&transactionMarker)
	if err == nil {
		if transactionMarker != policyv2.RoutingRuleAuthorityV1 {
			return fmt.Errorf("unsupported routing rule authority %q", transactionMarker)
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("recheck routing rule authority: %w", err)
	}
	rows, err := tx.QueryContext(ctx, `
SELECT e.id, e.name, e.priority
FROM policy_v2_egresses e
JOIN policy_v2_sources s ON s.egress_id = e.id
WHERE e.pending_delete = 0 AND s.pending_delete = 0
GROUP BY e.id, e.name, e.priority
ORDER BY e.priority, e.name, e.id`)
	if err != nil {
		return fmt.Errorf("list legacy routing egresses: %w", err)
	}
	type legacyEgress struct {
		id       string
		name     string
		priority int
	}
	egresses := make([]legacyEgress, 0)
	for rows.Next() {
		var egress legacyEgress
		if err := rows.Scan(&egress.id, &egress.name, &egress.priority); err != nil {
			rows.Close()
			return fmt.Errorf("scan legacy routing egress: %w", err)
		}
		egresses = append(egresses, egress)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("read legacy routing egresses: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close legacy routing egresses: %w", err)
	}
	now := unixTime(time.Now().UTC())
	for _, egress := range egresses {
		ruleID := "routing-rule:legacy-egress:" + egress.id
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO policy_v2_routing_rules (id, name, egress_id, subject_mode, ingress_interface_lists_json, ingress_interfaces_json, priority, enabled, revision, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, 1, 1, ?, ?)`, ruleID, egress.name, egress.id, policyv2.SubjectModeAll, "[]", "[]", egress.priority, now, now); err != nil {
			return fmt.Errorf("insert migrated routing rule %s: %w", ruleID, err)
		}
		targetRows, err := tx.QueryContext(ctx, `SELECT id FROM policy_v2_sources WHERE egress_id = ? AND pending_delete = 0 ORDER BY id`, egress.id)
		if err != nil {
			return fmt.Errorf("list migrated targets for %s: %w", egress.id, err)
		}
		position := 0
		for targetRows.Next() {
			var targetID string
			if err := targetRows.Scan(&targetID); err != nil {
				targetRows.Close()
				return fmt.Errorf("scan migrated target for %s: %w", egress.id, err)
			}
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO policy_v2_routing_rule_targets (rule_id, target_id, position) VALUES (?, ?, ?)`, ruleID, targetID, position); err != nil {
				targetRows.Close()
				return fmt.Errorf("insert migrated target for %s: %w", egress.id, err)
			}
			position++
		}
		if err := targetRows.Err(); err != nil {
			targetRows.Close()
			return fmt.Errorf("read migrated targets for %s: %w", egress.id, err)
		}
		if err := targetRows.Close(); err != nil {
			return fmt.Errorf("close migrated targets for %s: %w", egress.id, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE policy_v2_sources SET egress_id = '' WHERE trim(egress_id) <> ''`); err != nil {
		return fmt.Errorf("clear legacy routing associations: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO policy_v2_schema_meta (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, policyv2.RoutingRuleAuthorityKey, policyv2.RoutingRuleAuthorityV1); err != nil {
		return fmt.Errorf("write routing rule authority: %w", err)
	}
	if err := bumpDesiredRevision(ctx, tx); err != nil {
		return fmt.Errorf("bump desired revision for routing migration: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit routing rule migration: %w", err)
	}
	return s.migrateRoutingRuleIngress(ctx)
}

// migrateRoutingRuleIngress copies the former device-global scope into the
// all/excluded rules exactly once. Selected rules intentionally remain
// source-only and receive no ingress constraint.
func (s *Store) migrateRoutingRuleIngress(ctx context.Context) error {
	var marker string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM policy_v2_schema_meta WHERE key = 'routing_rule_ingress_migrated'`).Scan(&marker)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("inspect routing rule ingress migration: %w", err)
	}
	var payload string
	if err := s.db.QueryRowContext(ctx, `SELECT lan_scope_json FROM policy_v2_device_state WHERE id = 1`).Scan(&payload); err != nil {
		return err
	}
	scope, parseErr := policyv2.ParseTrafficIngressScope([]byte(payload))
	if parseErr != nil {
		scope = policyv2.TrafficIngressScope{}
	}
	lists, _ := json.Marshal(scope.InterfaceLists)
	interfaces, _ := json.Marshal(scope.Interfaces)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE policy_v2_routing_rules SET ingress_interface_lists_json = ?, ingress_interfaces_json = ? WHERE subject_mode IN (?, ?) AND ingress_interface_lists_json = '[]' AND ingress_interfaces_json = '[]'`, string(lists), string(interfaces), policyv2.SubjectModeAll, policyv2.SubjectModeExcluded); err != nil {
		return fmt.Errorf("migrate routing rule ingress: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO policy_v2_schema_meta (key, value) VALUES ('routing_rule_ingress_migrated', 'v1')`); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *PolicyRepository) ListRoutingRules(ctx context.Context) ([]policyv2.RoutingRule, error) {
	if err := r.EnsureRoutingRulesMigrated(ctx); err != nil {
		return nil, err
	}
	rows, err := r.store.db.QueryContext(ctx, `SELECT id, name, egress_id, subject_mode, ingress_interface_lists_json, ingress_interfaces_json, priority, enabled, revision, created_at, updated_at FROM policy_v2_routing_rules ORDER BY priority, name, id`)
	if err != nil {
		return nil, fmt.Errorf("list routing rules: %w", err)
	}
	result := make([]policyv2.RoutingRule, 0)
	for rows.Next() {
		var rule policyv2.RoutingRule
		var enabled int
		var ingressLists, ingressInterfaces string
		var createdAt, updatedAt int64
		if err := rows.Scan(&rule.ID, &rule.Name, &rule.EgressID, &rule.Subject.Mode, &ingressLists, &ingressInterfaces, &rule.Priority, &enabled, &rule.Revision, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan routing rule: %w", err)
		}
		rule.Enabled = enabled != 0
		rule.Ingress = decodeTrafficIngressScope(ingressLists, ingressInterfaces)
		rule.CreatedAt, rule.UpdatedAt = timeFromUnix(createdAt), timeFromUnix(updatedAt)
		result = append(result, rule)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("read routing rules: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close routing rules: %w", err)
	}
	for index := range result {
		rule := &result[index]
		rule.Subject.Members, err = r.listRoutingRuleMembers(ctx, rule.ID)
		if err != nil {
			return nil, err
		}
		rule.Subject.Prefixes, err = r.listRoutingRulePrefixes(ctx, rule.ID)
		if err != nil {
			return nil, err
		}
		rule.TargetListIDs, err = r.listRoutingRuleTargets(ctx, rule.ID)
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (r *PolicyRepository) GetRoutingRule(ctx context.Context, id string) (policyv2.RoutingRule, error) {
	if err := r.EnsureRoutingRulesMigrated(ctx); err != nil {
		return policyv2.RoutingRule{}, err
	}
	var rule policyv2.RoutingRule
	var enabled int
	var ingressLists, ingressInterfaces string
	var createdAt, updatedAt int64
	err := r.store.db.QueryRowContext(ctx, `SELECT id, name, egress_id, subject_mode, ingress_interface_lists_json, ingress_interfaces_json, priority, enabled, revision, created_at, updated_at FROM policy_v2_routing_rules WHERE id = ?`, id).Scan(&rule.ID, &rule.Name, &rule.EgressID, &rule.Subject.Mode, &ingressLists, &ingressInterfaces, &rule.Priority, &enabled, &rule.Revision, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return policyv2.RoutingRule{}, policyv2.ErrRoutingRuleNotFound
	}
	if err != nil {
		return policyv2.RoutingRule{}, fmt.Errorf("get routing rule: %w", err)
	}
	rule.Enabled = enabled != 0
	rule.Ingress = decodeTrafficIngressScope(ingressLists, ingressInterfaces)
	rule.CreatedAt, rule.UpdatedAt = timeFromUnix(createdAt), timeFromUnix(updatedAt)
	rule.Subject.Members, err = r.listRoutingRuleMembers(ctx, id)
	if err != nil {
		return policyv2.RoutingRule{}, err
	}
	rule.Subject.Prefixes, err = r.listRoutingRulePrefixes(ctx, id)
	if err != nil {
		return policyv2.RoutingRule{}, err
	}
	rule.TargetListIDs, err = r.listRoutingRuleTargets(ctx, id)
	if err != nil {
		return policyv2.RoutingRule{}, err
	}
	return rule, nil
}

func (r *PolicyRepository) SaveRoutingRule(ctx context.Context, value policyv2.RoutingRule) (policyv2.RoutingRule, error) {
	if (value.Subject.Mode == policyv2.SubjectModeAll || value.Subject.Mode == policyv2.SubjectModeExcluded) && !policyv2.HasTrafficIngress(value.Ingress) {
		// Accept the former global-ingress payload only at this compatibility
		// boundary; persisted rules are subsequently authoritative.
		if state, stateErr := r.GetDeviceState(ctx); stateErr == nil {
			if scope, scopeErr := policyv2.ParseTrafficIngressScope(state.TrafficIngress); scopeErr == nil && policyv2.HasTrafficIngress(scope) {
				value.Ingress = scope
			}
		}
	}
	value, err := policyv2.NormalizeRoutingRule(value)
	if err != nil {
		return policyv2.RoutingRule{}, err
	}
	if err := r.EnsureRoutingRulesMigrated(ctx); err != nil {
		return policyv2.RoutingRule{}, err
	}
	value.Ingress = policyv2.NormalizeTrafficIngressScopeUnvalidated(value.Ingress)
	tx, err := r.store.db.BeginTx(ctx, nil)
	if err != nil {
		return policyv2.RoutingRule{}, err
	}
	defer tx.Rollback()
	var pending int
	if err := tx.QueryRowContext(ctx, `SELECT pending_delete FROM policy_v2_egresses WHERE id = ?`, value.EgressID).Scan(&pending); errors.Is(err, sql.ErrNoRows) {
		return policyv2.RoutingRule{}, policyv2.ErrEgressNotFound
	} else if err != nil {
		return policyv2.RoutingRule{}, err
	} else if pending != 0 {
		return policyv2.RoutingRule{}, policyv2.ErrEgressNotFound
	}
	for _, targetID := range value.TargetListIDs {
		if err := tx.QueryRowContext(ctx, `SELECT pending_delete FROM policy_v2_sources WHERE id = ?`, targetID).Scan(&pending); errors.Is(err, sql.ErrNoRows) {
			return policyv2.RoutingRule{}, policyv2.ErrTargetListNotFound
		} else if err != nil {
			return policyv2.RoutingRule{}, err
		} else if pending != 0 {
			return policyv2.RoutingRule{}, policyv2.ErrTargetListNotFound
		}
	}
	now := time.Now().UTC()
	var currentRevision, currentCreatedAt int64
	err = tx.QueryRowContext(ctx, `SELECT revision, created_at FROM policy_v2_routing_rules WHERE id = ?`, value.ID).Scan(&currentRevision, &currentCreatedAt)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if value.Revision != 0 {
			return policyv2.RoutingRule{}, policyv2.ErrRevisionStale
		}
		value.Revision = 1
		value.CreatedAt = now
	case err != nil:
		return policyv2.RoutingRule{}, err
	default:
		if value.Revision != currentRevision {
			return policyv2.RoutingRule{}, policyv2.ErrRevisionStale
		}
		value.Revision = currentRevision + 1
		value.CreatedAt = timeFromUnix(currentCreatedAt)
	}
	value.UpdatedAt = now
	ingressLists, _ := json.Marshal(value.Ingress.InterfaceLists)
	ingressInterfaces, _ := json.Marshal(value.Ingress.Interfaces)
	if _, err := tx.ExecContext(ctx, `INSERT INTO policy_v2_routing_rules (id, name, egress_id, subject_mode, ingress_interface_lists_json, ingress_interfaces_json, priority, enabled, revision, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(id) DO UPDATE SET name=excluded.name, egress_id=excluded.egress_id, subject_mode=excluded.subject_mode, ingress_interface_lists_json=excluded.ingress_interface_lists_json, ingress_interfaces_json=excluded.ingress_interfaces_json, priority=excluded.priority, enabled=excluded.enabled, revision=excluded.revision, updated_at=excluded.updated_at`, value.ID, value.Name, value.EgressID, value.Subject.Mode, string(ingressLists), string(ingressInterfaces), value.Priority, boolToInt(value.Enabled), value.Revision, unixTime(value.CreatedAt), unixTime(value.UpdatedAt)); err != nil {
		return policyv2.RoutingRule{}, fmt.Errorf("save routing rule: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM policy_v2_routing_rule_targets WHERE rule_id = ?`, value.ID); err != nil {
		return policyv2.RoutingRule{}, err
	}
	for position, targetID := range value.TargetListIDs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO policy_v2_routing_rule_targets (rule_id, target_id, position) VALUES (?, ?, ?)`, value.ID, targetID, position); err != nil {
			return policyv2.RoutingRule{}, fmt.Errorf("save routing rule target: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM policy_v2_routing_rule_members WHERE rule_id = ?`, value.ID); err != nil {
		return policyv2.RoutingRule{}, err
	}
	for _, member := range value.Subject.Members {
		pinnedIPv4, _ := json.Marshal(nonNilStrings(member.PinnedIPv4))
		pinnedIPv6, _ := json.Marshal(nonNilStrings(member.PinnedIPv6))
		lastIPv4, _ := json.Marshal(nonNilStrings(member.LastIPv4))
		lastIPv6, _ := json.Marshal(nonNilStrings(member.LastIPv6))
		if _, err := tx.ExecContext(ctx, `INSERT INTO policy_v2_routing_rule_members (rule_id, terminal_id, binding, anchor_mac, pinned_ipv4_json, pinned_ipv6_json, last_ipv4_json, last_ipv6_json) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, value.ID, member.TerminalID, member.Binding, member.AnchorMAC, string(pinnedIPv4), string(pinnedIPv6), string(lastIPv4), string(lastIPv6)); err != nil {
			return policyv2.RoutingRule{}, fmt.Errorf("save routing rule member: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM policy_v2_routing_rule_prefixes WHERE rule_id = ?`, value.ID); err != nil {
		return policyv2.RoutingRule{}, err
	}
	for position, prefix := range value.Subject.Prefixes {
		if _, err := tx.ExecContext(ctx, `INSERT INTO policy_v2_routing_rule_prefixes (rule_id, prefix, position) VALUES (?, ?, ?)`, value.ID, prefix, position); err != nil {
			return policyv2.RoutingRule{}, fmt.Errorf("save routing rule prefix: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO policy_v2_schema_meta (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, policyv2.RoutingRuleAuthorityKey, policyv2.RoutingRuleAuthorityV1); err != nil {
		return policyv2.RoutingRule{}, err
	}
	if err := bumpDesiredRevision(ctx, tx); err != nil {
		return policyv2.RoutingRule{}, err
	}
	if err := tx.Commit(); err != nil {
		return policyv2.RoutingRule{}, err
	}
	return value, nil
}

func (r *PolicyRepository) DeleteRoutingRule(ctx context.Context, id string, revision int64) error {
	if err := r.EnsureRoutingRulesMigrated(ctx); err != nil {
		return err
	}
	tx, err := r.store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var currentRevision int64
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM policy_v2_routing_rules WHERE id = ?`, id).Scan(&currentRevision); errors.Is(err, sql.ErrNoRows) {
		return policyv2.ErrRoutingRuleNotFound
	} else if err != nil {
		return err
	}
	if revision != currentRevision {
		return policyv2.ErrRevisionStale
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM policy_v2_routing_rules WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete routing rule: %w", err)
	}
	if err := bumpDesiredRevision(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *PolicyRepository) listRoutingRuleTargets(ctx context.Context, ruleID string) ([]string, error) {
	rows, err := r.store.db.QueryContext(ctx, `SELECT target_id FROM policy_v2_routing_rule_targets WHERE rule_id = ? ORDER BY position, target_id`, ruleID)
	if err != nil {
		return nil, fmt.Errorf("list routing rule targets: %w", err)
	}
	defer rows.Close()
	result := make([]string, 0)
	for rows.Next() {
		var targetID string
		if err := rows.Scan(&targetID); err != nil {
			return nil, err
		}
		result = append(result, targetID)
	}
	return result, rows.Err()
}

func (r *PolicyRepository) listRoutingRuleMembers(ctx context.Context, ruleID string) ([]policyv2.SubjectMember, error) {
	rows, err := r.store.db.QueryContext(ctx, `SELECT terminal_id, binding, anchor_mac, pinned_ipv4_json, pinned_ipv6_json, last_ipv4_json, last_ipv6_json FROM policy_v2_routing_rule_members WHERE rule_id = ? ORDER BY terminal_id`, ruleID)
	if err != nil {
		return nil, fmt.Errorf("list routing rule members: %w", err)
	}
	defer rows.Close()
	result := make([]policyv2.SubjectMember, 0)
	for rows.Next() {
		var member policyv2.SubjectMember
		var pinnedIPv4, pinnedIPv6, lastIPv4, lastIPv6 string
		if err := rows.Scan(&member.TerminalID, &member.Binding, &member.AnchorMAC, &pinnedIPv4, &pinnedIPv6, &lastIPv4, &lastIPv6); err != nil {
			return nil, err
		}
		member.PinnedIPv4 = decodeStringSlice(pinnedIPv4)
		member.PinnedIPv6 = decodeStringSlice(pinnedIPv6)
		member.LastIPv4 = decodeStringSlice(lastIPv4)
		member.LastIPv6 = decodeStringSlice(lastIPv6)
		result = append(result, member)
	}
	return result, rows.Err()
}

func (r *PolicyRepository) listRoutingRulePrefixes(ctx context.Context, ruleID string) ([]string, error) {
	rows, err := r.store.db.QueryContext(ctx, `SELECT prefix FROM policy_v2_routing_rule_prefixes WHERE rule_id = ? ORDER BY position, prefix`, ruleID)
	if err != nil {
		return nil, fmt.Errorf("list routing rule prefixes: %w", err)
	}
	defer rows.Close()
	result := make([]string, 0)
	for rows.Next() {
		var prefix string
		if err := rows.Scan(&prefix); err != nil {
			return nil, err
		}
		result = append(result, prefix)
	}
	return result, rows.Err()
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func decodeStringSlice(value string) []string {
	result := []string{}
	if strings.TrimSpace(value) == "" {
		return result
	}
	if err := json.Unmarshal([]byte(value), &result); err != nil || result == nil {
		return []string{}
	}
	sort.Strings(result)
	return result
}

func decodeTrafficIngressScope(lists, interfaces string) policyv2.TrafficIngressScope {
	return policyv2.NormalizeTrafficIngressScopeUnvalidated(policyv2.TrafficIngressScope{
		InterfaceLists: decodeStringSlice(lists),
		Interfaces:     decodeStringSlice(interfaces),
	})
}
