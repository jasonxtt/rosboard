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

// CommitAccessProposal writes the access rule and any preset target-list
// backing rows in one transaction. The target versions remain pending until
// CommitAccessApply promotes the exact versions reviewed by the plan.
func (r *PolicyRepository) CommitAccessProposal(ctx context.Context, proposal policyv2.AccessProposal, expectedRevision int64, actor string) (int64, error) {
	if proposal.Empty() || strings.TrimSpace(proposal.Rule.ID) == "" {
		return 0, errors.New("access proposal is empty")
	}
	tx, err := r.store.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var currentRevision int64
	if err := tx.QueryRowContext(ctx, `SELECT desired_revision FROM access_control_state WHERE device_id = ?`, r.store.deviceID).Scan(&currentRevision); err != nil {
		return 0, err
	}
	if currentRevision != expectedRevision {
		return 0, policyv2.ErrRevisionStale
	}
	if err := checkAccessProposalTargetDependenciesTx(ctx, tx, proposal); err != nil {
		return 0, err
	}
	now := time.Now().UTC()
	routingTargetInvalidated := false
	for _, target := range proposal.TargetLists {
		if err := saveProposalTargetListTx(ctx, tx, target, now); err != nil {
			return 0, err
		}
		domains, err := targetConsumerDomainsTx(ctx, tx, r.store.deviceID, target.Target.ID)
		if err != nil {
			return 0, err
		}
		routingTargetInvalidated = routingTargetInvalidated || domains.Routing
	}
	if err := saveAccessProposalRuleTx(ctx, tx, r.store.deviceID, proposal.Rule, proposal.Members, actor); err != nil {
		return 0, err
	}
	if routingTargetInvalidated {
		if err := bumpDesiredRevision(ctx, tx); err != nil {
			return 0, err
		}
	}
	if err := tx.QueryRowContext(ctx, `SELECT desired_revision FROM access_control_state WHERE device_id = ?`, r.store.deviceID).Scan(&currentRevision); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return currentRevision, nil
}

func checkAccessProposalTargetDependenciesTx(ctx context.Context, tx *sql.Tx, proposal policyv2.AccessProposal) error {
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

func saveAccessProposalRuleTx(ctx context.Context, tx *sql.Tx, deviceID string, rule accesscontrol.AccessRule, members []accesscontrol.RuleMember, actor string) error {
	if len(members) == 0 && len(rule.Subject.Members) > 0 {
		members = make([]accesscontrol.RuleMember, 0, len(rule.Subject.Members))
		for _, member := range rule.Subject.Members {
			members = append(members, accesscontrol.RuleMember{
				RuleID: rule.ID, TerminalID: member.TerminalID, Binding: member.Binding, AnchorMAC: member.AnchorMAC,
				PinnedIPv4: append([]string(nil), member.PinnedIPv4...), PinnedIPv6: append([]string(nil), member.PinnedIPv6...),
				LastIPv4: append([]string(nil), member.LastIPv4...), LastIPv6: append([]string(nil), member.LastIPv6...),
			})
		}
	}
	if err := accesscontrol.ValidateRule(rule); err != nil {
		return err
	}
	normalizedMembers := make([]accesscontrol.RuleMember, 0, len(members))
	seenTerminals := make(map[string]bool, len(members))
	for _, member := range members {
		member.RuleID = rule.ID
		member, err := accesscontrol.NormalizeMember(member)
		if err != nil {
			return err
		}
		if seenTerminals[member.TerminalID] {
			return accesscontrol.ErrMemberDuplicate
		}
		seenTerminals[member.TerminalID] = true
		normalizedMembers = append(normalizedMembers, member)
	}
	if rule.Subject.Mode != "all" && len(normalizedMembers) == 0 && len(rule.Subject.Prefixes) == 0 {
		return errors.New("access rule requires at least one member")
	}
	if rule.Subject.Mode == "all" && len(normalizedMembers) != 0 {
		return errors.New("all subjects must not contain members")
	}

	lockResult, err := tx.ExecContext(ctx, `UPDATE access_control_state SET desired_revision = desired_revision WHERE device_id = ?`, deviceID)
	if err != nil {
		return fmt.Errorf("lock access-control state for proposal: %w", err)
	}
	if rows, err := lockResult.RowsAffected(); err != nil || rows != 1 {
		if err != nil {
			return err
		}
		return errors.New("access-control state is missing")
	}
	current, err := loadAccessRuleTx(ctx, tx, deviceID, rule.ID)
	if errors.Is(err, sql.ErrNoRows) {
		if rule.Revision != 0 {
			return accesscontrol.ErrRevisionStale
		}
		current = accessRuleSnapshot{Members: []accesscontrol.RuleMember{}}
		rule.Revision = 1
		rule.CreatedAt = time.Now().UTC()
	} else if err != nil {
		return err
	} else {
		if rule.Revision != current.Rule.Revision {
			return accesscontrol.ErrRevisionStale
		}
		rule.Revision = current.Rule.Revision + 1
		rule.CreatedAt = current.Rule.CreatedAt
	}
	rule.UpdatedAt = time.Now().UTC()
	if rule.TargetScope == accesscontrol.TargetScopeSources {
		currentSourceIDs := make(map[string]bool, len(current.Rule.SourceIDs))
		for _, sourceID := range current.Rule.SourceIDs {
			currentSourceIDs[sourceID] = true
		}
		for _, sourceID := range rule.SourceIDs {
			var pendingDeletion int
			if err := tx.QueryRowContext(ctx, `SELECT pending_delete FROM policy_v2_sources WHERE id = ?`, sourceID).Scan(&pendingDeletion); errors.Is(err, sql.ErrNoRows) {
				return policyv2.ErrSourceNotFound
			} else if err != nil {
				return err
			} else if pendingDeletion != 0 && !currentSourceIDs[sourceID] {
				return policyv2.ErrSourceNotFound
			}
		}
	}
	if rule.TargetScope == accesscontrol.TargetScopeTargets {
		for _, targetID := range rule.TargetListIDs {
			var pendingDeletion int
			if err := tx.QueryRowContext(ctx, `SELECT pending_delete FROM policy_v2_sources WHERE id = ?`, targetID).Scan(&pendingDeletion); errors.Is(err, sql.ErrNoRows) {
				return policyv2.ErrTargetListNotFound
			} else if err != nil {
				return err
			} else if pendingDeletion != 0 {
				return policyv2.ErrTargetListNotFound
			}
		}
	}
	rule.Subject.Members = make([]subject.Member, 0, len(normalizedMembers))
	for _, member := range normalizedMembers {
		rule.Subject.Members = append(rule.Subject.Members, accessMemberSubject(member))
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO access_rules (device_id, id, name, target_scope, action, enabled, revision, created_at, updated_at, subject_mode)
		VALUES (?, ?, ?, ?, 'deny', ?, ?, ?, ?, ?)
		ON CONFLICT(device_id, id) DO UPDATE SET name=excluded.name, target_scope=excluded.target_scope, enabled=excluded.enabled, revision=excluded.revision, updated_at=excluded.updated_at, subject_mode=excluded.subject_mode`,
		deviceID, rule.ID, rule.Name, rule.TargetScope, boolToInt(rule.Enabled), rule.Revision, unixTime(rule.CreatedAt), unixTime(rule.UpdatedAt), rule.Subject.Mode); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM access_rule_sources WHERE device_id = ? AND rule_id = ?`, deviceID, rule.ID); err != nil {
		return err
	}
	targetIDs := rule.SourceIDs
	if rule.TargetScope == accesscontrol.TargetScopeTargets {
		targetIDs = rule.TargetListIDs
	}
	for position, sourceID := range targetIDs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO access_rule_sources (device_id, rule_id, source_id, position) VALUES (?, ?, ?, ?)`, deviceID, rule.ID, sourceID, position); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM access_rule_prefixes WHERE device_id = ? AND rule_id = ?`, deviceID, rule.ID); err != nil {
		return err
	}
	for position, prefix := range rule.Subject.Prefixes {
		if _, err := tx.ExecContext(ctx, `INSERT INTO access_rule_prefixes (device_id, rule_id, prefix, position) VALUES (?, ?, ?, ?)`, deviceID, rule.ID, prefix, position); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM access_rule_applications WHERE device_id = ? AND rule_id = ?`, deviceID, rule.ID); err != nil {
		return err
	}
	for position, applicationID := range rule.ApplicationIDs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO access_rule_applications (device_id, rule_id, application_id, position) VALUES (?, ?, ?, ?)`, deviceID, rule.ID, applicationID, position); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM access_rule_members WHERE device_id = ? AND rule_id = ?`, deviceID, rule.ID); err != nil {
		return err
	}
	lastByTerminal := make(map[string]accesscontrol.RuleMember, len(current.Members))
	for _, member := range current.Members {
		lastByTerminal[member.TerminalID] = member
	}
	persistedMembers := make([]accesscontrol.RuleMember, 0, len(normalizedMembers))
	for _, member := range normalizedMembers {
		previous, hasPrevious := lastByTerminal[member.TerminalID]
		lastIPv4, lastIPv6 := "[]", "[]"
		previousAnchor := ""
		if hasPrevious && previous.Binding == accesscontrol.BindingAuto && strings.TrimSpace(previous.AnchorMAC) != "" {
			previousAnchor, err = accesscontrol.NormalizeMAC(previous.AnchorMAC)
			if err != nil {
				return accesscontrol.ErrMemberAnchorRequired
			}
		}
		if member.Binding == accesscontrol.BindingAuto {
			if member.AnchorMAC == "" && hasPrevious {
				member.AnchorMAC = previousAnchor
			}
			if member.AnchorMAC == "" {
				return accesscontrol.ErrMemberAnchorRequired
			}
			member, err = accesscontrol.NormalizeMember(member)
			if err != nil {
				return err
			}
			if hasPrevious && previous.Binding == accesscontrol.BindingAuto && previousAnchor != "" && member.AnchorMAC != previousAnchor {
				return accesscontrol.ErrMemberAnchorChanged
			}
		}
		if hasPrevious && previous.Binding == accesscontrol.BindingAuto && member.Binding == accesscontrol.BindingAuto && previousAnchor != "" && previousAnchor == member.AnchorMAC {
			lastIPv4JSON, _ := json.Marshal(firstNonNil(previous.LastIPv4))
			lastIPv6JSON, _ := json.Marshal(firstNonNil(previous.LastIPv6))
			lastIPv4, lastIPv6 = string(lastIPv4JSON), string(lastIPv6JSON)
			member.LastIPv4, member.LastIPv6 = firstNonNil(previous.LastIPv4), firstNonNil(previous.LastIPv6)
		}
		pinnedIPv4, _ := json.Marshal(firstNonNil(member.PinnedIPv4))
		pinnedIPv6, _ := json.Marshal(firstNonNil(member.PinnedIPv6))
		if _, err := tx.ExecContext(ctx, `INSERT INTO access_rule_members (device_id, rule_id, terminal_id, binding, anchor_mac, pinned_ipv4_json, pinned_ipv6_json, last_ipv4_json, last_ipv6_json)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, deviceID, rule.ID, member.TerminalID, member.Binding, member.AnchorMAC, string(pinnedIPv4), string(pinnedIPv6), lastIPv4, lastIPv6); err != nil {
			return err
		}
		persistedMembers = append(persistedMembers, member)
	}
	if err := bumpAccessDesiredRevision(ctx, tx, deviceID); err != nil {
		return err
	}
	if err := writeAccessAudit(ctx, tx, deviceID, actor, "save", rule.ID, current, accessRuleSnapshot{Rule: rule, Members: persistedMembers}, "success"); err != nil {
		return err
	}
	return nil
}
