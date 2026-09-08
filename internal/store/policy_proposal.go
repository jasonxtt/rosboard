package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"rosboard/internal/policyv2"
)

// CommitPolicyProposal promotes the exact graph used to generate a reviewed
// plan. The dependency revision check and all row writes share one SQLite
// transaction; no intermediate egress, target, ingress, or rule state is
// visible to another desired-state reader.
func (r *PolicyRepository) CommitPolicyProposal(ctx context.Context, proposal policyv2.PolicyProposal, expectedRevision int64) (int64, error) {
	if proposal.Empty() {
		return 0, errors.New("policy proposal is empty")
	}
	tx, err := r.store.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var currentRevision int64
	if err := tx.QueryRowContext(ctx, `SELECT desired_revision FROM policy_v2_device_state WHERE id = 1`).Scan(&currentRevision); err != nil {
		return 0, err
	}
	if currentRevision != expectedRevision {
		return 0, policyv2.ErrRevisionStale
	}
	if err := checkProposalDependenciesTx(ctx, tx, proposal); err != nil {
		return 0, err
	}

	now := time.Now().UTC()
	accessTargetInvalidated := false
	if proposal.Egress != nil {
		if err := saveProposalEgressTx(ctx, tx, *proposal.Egress, now); err != nil {
			return 0, err
		}
	}
	seenTargets := make(map[string]bool, len(proposal.TargetLists))
	for _, target := range proposal.TargetLists {
		if seenTargets[target.Target.ID] {
			return 0, fmt.Errorf("duplicate proposed target list %q", target.Target.ID)
		}
		seenTargets[target.Target.ID] = true
		if err := saveProposalTargetListTx(ctx, tx, target, now); err != nil {
			return 0, err
		}
		domains, err := targetConsumerDomainsTx(ctx, tx, r.store.deviceID, target.Target.ID)
		if err != nil {
			return 0, err
		}
		accessTargetInvalidated = accessTargetInvalidated || domains.Access
	}
	trafficIngress, err := proposalTrafficIngress(ctx, tx, proposal.TrafficIngress)
	if err != nil {
		return 0, err
	}
	// Once RoutingRule is present, its ingress is the canonical authority. Keep
	// the old device-global field only for compatibility fallback in
	// saveProposalRoutingRuleTx; a policy edit must not silently rewrite it.
	if proposal.TrafficIngress != nil && proposal.RoutingRule == nil {
		if _, err := tx.ExecContext(ctx, `UPDATE policy_v2_device_state SET lan_scope_json = ? WHERE id = 1`, string(trafficIngress)); err != nil {
			return 0, err
		}
	}
	if proposal.RoutingRule != nil {
		if err := saveProposalRoutingRuleTx(ctx, tx, *proposal.RoutingRule, trafficIngress, now); err != nil {
			return 0, err
		}
	}
	if err := bumpDesiredRevision(ctx, tx); err != nil {
		return 0, err
	}
	if accessTargetInvalidated {
		if err := bumpAccessDesiredRevision(ctx, tx, r.store.deviceID); err != nil {
			return 0, err
		}
	}
	if err := tx.QueryRowContext(ctx, `SELECT desired_revision FROM policy_v2_device_state WHERE id = 1`).Scan(&currentRevision); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return currentRevision, nil
}

func saveProposalEgressTx(ctx context.Context, tx *sql.Tx, egress policyv2.Egress, now time.Time) error {
	if egress.ID == "" {
		return errors.New("proposed egress id is required")
	}
	var currentRevision, applied, pendingDelete, createdAt int64
	var currentOrigin string
	err := tx.QueryRowContext(ctx, `SELECT revision, applied, pending_delete, created_at, origin FROM policy_v2_egresses WHERE id = ?`, egress.ID).Scan(&currentRevision, &applied, &pendingDelete, &createdAt, &currentOrigin)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if egress.Revision != 0 {
			return policyv2.ErrRevisionStale
		}
		egress.Revision = 1
		egress.CreatedAt = now
		egress.Applied = false
		if egress.Origin == "" {
			egress.Origin = policyv2.EgressOriginPolicy
		}
	case err != nil:
		return err
	default:
		if egress.Revision != currentRevision || pendingDelete != 0 {
			return policyv2.ErrRevisionStale
		}
		egress.Revision = currentRevision + 1
		egress.CreatedAt = timeFromUnix(createdAt)
		egress.Applied = applied != 0
		egress.Origin = policyv2.EgressOrigin(currentOrigin)
	}
	egress.PendingDeletion = false
	egress.UpdatedAt = now
	if _, err := tx.ExecContext(ctx, `INSERT INTO policy_v2_egresses (id, origin, name, priority, list_mode, list_name, dns_upstream, fake_alias, failure_mode, router_output, enabled, revision, pending_delete, applied, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET name=excluded.name, priority=excluded.priority, list_mode=excluded.list_mode, list_name=excluded.list_name, dns_upstream=excluded.dns_upstream, fake_alias=excluded.fake_alias, failure_mode=excluded.failure_mode, router_output=excluded.router_output, enabled=excluded.enabled, revision=excluded.revision, updated_at=excluded.updated_at`,
		egress.ID, egress.Origin, egress.Name, egress.Priority, egress.ListMode, egress.ListName, egress.DNSUpstream, egress.FakeAlias, egress.FailureMode, boolToInt(egress.RouterOutput), boolToInt(egress.Enabled), egress.Revision, boolToInt(egress.Applied), unixTime(egress.CreatedAt), unixTime(egress.UpdatedAt)); err != nil {
		return fmt.Errorf("save proposed egress: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM policy_v2_egress_families WHERE egress_id = ?`, egress.ID); err != nil {
		return err
	}
	for _, family := range egress.Families {
		if _, err := tx.ExecContext(ctx, `INSERT INTO policy_v2_egress_families (egress_id, family, enabled, wan_interface, gateway, route_table, route_mode, nat_mode, wan_source) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, egress.ID, family.Family, boolToInt(family.Enabled), family.WANInterface, family.Gateway, family.RouteTable, family.RouteMode, family.NATMode, family.WANSource); err != nil {
			return fmt.Errorf("save proposed egress family: %w", err)
		}
	}
	return nil
}

func saveProposalTargetListTx(ctx context.Context, tx *sql.Tx, proposed policyv2.ProposedTargetList, now time.Time) error {
	target := proposed.Target
	if target.ID == "" || proposed.Version.ID == "" {
		return errors.New("proposed target list identity is incomplete")
	}
	if proposed.Version.TargetListID != target.ID {
		return errors.New("proposed target list version belongs to a different target list")
	}
	if err := policyv2.ValidateTargetListKind(target.Kind); err != nil {
		return err
	}
	if err := policyv2.ValidateTargetListSourceType(target.SourceType); err != nil {
		return err
	}
	if err := policyv2.ValidateTargetListPreset(target.SourceType, target.PresetID); err != nil {
		return err
	}
	if proposed.Version.SHA256 == "" || len(proposed.Version.CompressedYAML) == 0 || len(proposed.Rules) == 0 {
		return errors.New("proposed target list content is incomplete")
	}
	for _, rule := range proposed.Rules {
		if rule.VersionID != proposed.Version.ID {
			return errors.New("proposed target list rule belongs to a different version")
		}
	}

	var current sourceRow
	err := tx.QueryRowContext(ctx, `SELECT egress_id, type, kind, preset_id, name, url, schedule, enabled, active_version_id, pending_version_id, last_good_version_id, etag, last_modified, next_run_at, revision, pending_delete, applied, created_at FROM policy_v2_sources WHERE id = ?`, target.ID).Scan(
		&current.EgressID, &current.Type, &current.Kind, &current.PresetID, &current.Name, &current.URL, &current.Schedule, &current.Enabled, &current.ActiveVersionID, &current.PendingVersionID, &current.LastGoodVersionID, &current.ETag, &current.LastModified, &current.NextRunAt, &current.Revision, &current.PendingDelete, &current.Applied, &current.CreatedAt)
	source := target.ToSource()
	source.PendingVersionID = proposed.Version.ID
	source.UpdatedAt = now
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if target.Revision != 0 {
			return policyv2.ErrRevisionStale
		}
		source.Revision = 1
		source.CreatedAt = now
		source.Applied = false
	case err != nil:
		return err
	default:
		if target.Revision != current.Revision || (current.PendingDelete != 0 && (target.PendingDeletion || target.SourceType != policyv2.TargetSourceTypePreset)) {
			return policyv2.ErrRevisionStale
		}
		if target.SourceType != current.Type || target.Kind != policyv2.NormalizeSourceKind(current.Kind) || target.PresetID != current.PresetID {
			return policyv2.ErrTargetListKindImmutable
		}
		source.Revision = current.Revision + 1
		source.EgressID = current.EgressID
		source.CreatedAt = timeFromUnix(current.CreatedAt)
		source.ActiveVersionID = current.ActiveVersionID
		source.LastGoodVersionID = current.LastGoodVersionID
		source.Applied = current.Applied != 0
		if source.ETag == "" {
			source.ETag = current.ETag
		}
		if source.LastModified == "" {
			source.LastModified = current.LastModified
		}
		if source.NextRunAt.IsZero() {
			source.NextRunAt = timeFromUnix(current.NextRunAt)
		}
	}
	source.PendingDeletion = false
	if _, err := tx.ExecContext(ctx, `INSERT INTO policy_v2_sources (id, egress_id, type, kind, preset_id, name, url, schedule, enabled, active_version_id, pending_version_id, last_good_version_id, etag, last_modified, next_run_at, revision, pending_delete, applied, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET egress_id=excluded.egress_id, type=excluded.type, kind=excluded.kind, preset_id=excluded.preset_id, name=excluded.name, url=excluded.url, schedule=excluded.schedule, enabled=excluded.enabled, active_version_id=excluded.active_version_id, pending_version_id=excluded.pending_version_id, last_good_version_id=excluded.last_good_version_id, etag=excluded.etag, last_modified=excluded.last_modified, next_run_at=excluded.next_run_at, revision=excluded.revision, pending_delete=excluded.pending_delete, applied=excluded.applied, updated_at=excluded.updated_at`,
		source.ID, source.EgressID, source.Type, source.Kind, source.PresetID, source.Name, source.URL, source.Schedule, boolToInt(source.Enabled), source.ActiveVersionID, source.PendingVersionID, source.LastGoodVersionID, source.ETag, source.LastModified, unixTime(source.NextRunAt), source.Revision, boolToInt(source.Applied), unixTime(source.CreatedAt), unixTime(source.UpdatedAt)); err != nil {
		return fmt.Errorf("save proposed target list: %w", err)
	}

	counts := map[string]int{"valid": len(proposed.Rules)}
	if proposed.Version.Counts != nil {
		counts = proposed.Version.Counts
	}
	countsJSON, err := json.Marshal(counts)
	if err != nil {
		return err
	}
	diffJSON, err := json.Marshal(proposed.Version.Diff)
	if err != nil {
		return err
	}
	versionState := proposed.Version.State
	if versionState == "" {
		versionState = "pending"
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO policy_v2_source_versions (id, source_id, sha256, compressed_yaml, state, error, http_status, counts_json, diff_json, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, proposed.Version.ID, source.ID, proposed.Version.SHA256, proposed.Version.CompressedYAML, versionState, proposed.Version.Error, proposed.Version.HTTPStatus, string(countsJSON), string(diffJSON), unixTime(proposed.Version.CreatedAt)); err != nil {
		return fmt.Errorf("save proposed target list version: %w", err)
	}
	for _, rule := range proposed.Rules {
		if _, err := tx.ExecContext(ctx, `INSERT INTO policy_v2_source_rules (version_id, rule_type, domain) VALUES (?, ?, ?)`, proposed.Version.ID, rule.RuleType, rule.Domain); err != nil {
			return fmt.Errorf("save proposed target list rule: %w", err)
		}
	}
	return nil
}

type sourceRow struct {
	EgressID, Type, Kind, PresetID, Name, URL, Schedule  string
	Enabled, PendingDelete, Applied                      int
	ActiveVersionID, PendingVersionID, LastGoodVersionID string
	ETag, LastModified                                   string
	NextRunAt, Revision, CreatedAt                       int64
}

func checkProposalDependenciesTx(ctx context.Context, tx *sql.Tx, proposal policyv2.PolicyProposal) error {
	for id, expectedRevision := range proposal.EgressRevisions {
		var revision int64
		err := tx.QueryRowContext(ctx, `SELECT revision FROM policy_v2_egresses WHERE id = ?`, id).Scan(&revision)
		if errors.Is(err, sql.ErrNoRows) || err == nil && revision != expectedRevision {
			return policyv2.ErrRevisionStale
		}
		if err != nil {
			return err
		}
	}
	for id, expectedRevision := range proposal.TargetListRevisions {
		var revision int64
		err := tx.QueryRowContext(ctx, `SELECT revision FROM policy_v2_sources WHERE id = ?`, id).Scan(&revision)
		if errors.Is(err, sql.ErrNoRows) || err == nil && revision != expectedRevision {
			return policyv2.ErrRevisionStale
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func proposalTrafficIngress(ctx context.Context, tx *sql.Tx, proposed *policyv2.TrafficIngressScope) ([]byte, error) {
	if proposed != nil {
		return policyv2.MarshalTrafficIngressScope(*proposed)
	}
	var payload string
	if err := tx.QueryRowContext(ctx, `SELECT lan_scope_json FROM policy_v2_device_state WHERE id = 1`).Scan(&payload); err != nil {
		return nil, err
	}
	return []byte(payload), nil
}

func saveProposalRoutingRuleTx(ctx context.Context, tx *sql.Tx, value policyv2.RoutingRule, trafficIngress []byte, now time.Time) error {
	var err error
	if (value.Subject.Mode == policyv2.SubjectModeAll || value.Subject.Mode == policyv2.SubjectModeExcluded) && !policyv2.HasTrafficIngress(value.Ingress) {
		if scope, scopeErr := policyv2.ParseTrafficIngressScope(trafficIngress); scopeErr == nil {
			value.Ingress = scope
		}
	}
	value, err = policyv2.NormalizeRoutingRule(value)
	if err != nil {
		return err
	}
	value.Ingress = policyv2.NormalizeTrafficIngressScopeUnvalidated(value.Ingress)
	var pending int
	if err := tx.QueryRowContext(ctx, `SELECT pending_delete FROM policy_v2_egresses WHERE id = ?`, value.EgressID).Scan(&pending); errors.Is(err, sql.ErrNoRows) {
		return policyv2.ErrEgressNotFound
	} else if err != nil {
		return err
	} else if pending != 0 {
		return policyv2.ErrEgressNotFound
	}
	for _, targetID := range value.TargetListIDs {
		if err := tx.QueryRowContext(ctx, `SELECT pending_delete FROM policy_v2_sources WHERE id = ?`, targetID).Scan(&pending); errors.Is(err, sql.ErrNoRows) {
			return policyv2.ErrTargetListNotFound
		} else if err != nil {
			return err
		} else if pending != 0 {
			return policyv2.ErrTargetListNotFound
		}
	}
	var currentRevision, currentCreatedAt int64
	err = tx.QueryRowContext(ctx, `SELECT revision, created_at FROM policy_v2_routing_rules WHERE id = ?`, value.ID).Scan(&currentRevision, &currentCreatedAt)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if value.Revision != 0 {
			return policyv2.ErrRevisionStale
		}
		value.Revision = 1
		value.CreatedAt = now
	case err != nil:
		return err
	default:
		if value.Revision != currentRevision {
			return policyv2.ErrRevisionStale
		}
		value.Revision = currentRevision + 1
		value.CreatedAt = timeFromUnix(currentCreatedAt)
	}
	value.UpdatedAt = now
	ingressLists, _ := json.Marshal(value.Ingress.InterfaceLists)
	ingressInterfaces, _ := json.Marshal(value.Ingress.Interfaces)
	if _, err := tx.ExecContext(ctx, `INSERT INTO policy_v2_routing_rules (id, name, egress_id, subject_mode, ingress_interface_lists_json, ingress_interfaces_json, priority, enabled, revision, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(id) DO UPDATE SET name=excluded.name, egress_id=excluded.egress_id, subject_mode=excluded.subject_mode, ingress_interface_lists_json=excluded.ingress_interface_lists_json, ingress_interfaces_json=excluded.ingress_interfaces_json, priority=excluded.priority, enabled=excluded.enabled, revision=excluded.revision, updated_at=excluded.updated_at`, value.ID, value.Name, value.EgressID, value.Subject.Mode, string(ingressLists), string(ingressInterfaces), value.Priority, boolToInt(value.Enabled), value.Revision, unixTime(value.CreatedAt), unixTime(value.UpdatedAt)); err != nil {
		return fmt.Errorf("save proposed routing rule: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM policy_v2_routing_rule_targets WHERE rule_id = ?`, value.ID); err != nil {
		return err
	}
	for position, targetID := range value.TargetListIDs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO policy_v2_routing_rule_targets (rule_id, target_id, position) VALUES (?, ?, ?)`, value.ID, targetID, position); err != nil {
			return fmt.Errorf("save proposed routing rule target: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM policy_v2_routing_rule_members WHERE rule_id = ?`, value.ID); err != nil {
		return err
	}
	for _, member := range value.Subject.Members {
		pinnedIPv4, _ := json.Marshal(nonNilStrings(member.PinnedIPv4))
		pinnedIPv6, _ := json.Marshal(nonNilStrings(member.PinnedIPv6))
		lastIPv4, _ := json.Marshal(nonNilStrings(member.LastIPv4))
		lastIPv6, _ := json.Marshal(nonNilStrings(member.LastIPv6))
		if _, err := tx.ExecContext(ctx, `INSERT INTO policy_v2_routing_rule_members (rule_id, terminal_id, binding, anchor_mac, pinned_ipv4_json, pinned_ipv6_json, last_ipv4_json, last_ipv6_json) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, value.ID, member.TerminalID, member.Binding, member.AnchorMAC, string(pinnedIPv4), string(pinnedIPv6), string(lastIPv4), string(lastIPv6)); err != nil {
			return fmt.Errorf("save proposed routing rule member: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM policy_v2_routing_rule_prefixes WHERE rule_id = ?`, value.ID); err != nil {
		return err
	}
	for position, prefix := range value.Subject.Prefixes {
		if _, err := tx.ExecContext(ctx, `INSERT INTO policy_v2_routing_rule_prefixes (rule_id, prefix, position) VALUES (?, ?, ?)`, value.ID, prefix, position); err != nil {
			return fmt.Errorf("save proposed routing rule prefix: %w", err)
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO policy_v2_schema_meta (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, policyv2.RoutingRuleAuthorityKey, policyv2.RoutingRuleAuthorityV1)
	return err
}
