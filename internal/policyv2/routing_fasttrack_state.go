package policyv2

import (
	"context"
	"errors"
	"fmt"
	"time"

	"rosboard/internal/routeros"
)

func readFastTrackRule(ctx context.Context, mutation PolicyMutation, menu routeros.MutationMenu, id string) (map[string]string, error) {
	// Reading the whole menu distinguishes a missing ID from transport/auth errors.
	objects, err := mutation.List(ctx, menu, routeros.MutationQuery{})
	if err != nil {
		return nil, err
	}
	for _, obj := range objects {
		if obj.ID() == id {
			return fastTrackFields(obj), nil
		}
	}
	return nil, nil
}
func fastTrackEvent(state *FastTrackState, code string, record FastTrackRecord, reason string) {
	state.Events = append(state.Events, PlanIssue{Code: code, LogicalID: fastTrackKey(record.Menu, record.ID), Reason: fmt.Sprintf("FastTrack %s: %s", record.ID, reason)})
	if len(state.Events) > 20 {
		state.Events = state.Events[len(state.Events)-20:]
	}
}
func fastTrackDiverged(state *FastTrackState, index int) {
	if state.Records[index].Status == "diverged" {
		return
	}
	state.Records[index].Status = "diverged"
	fastTrackEvent(state, "fasttrack_external_change_preserved", state.Records[index], "检测到外部修改，已保留当前配置，本次持有期不再自动修改或恢复。")
}

// ensureFastTrack runs under DeviceWriteGate, after the proposal has persisted.
// A routing failure retains these records because the saved consumer still exists.
func ensureFastTrack(ctx context.Context, applier *Applier, report *FastTrackReport) error {
	if report == nil || report.Consumers == 0 || report.RetainOnly {
		return nil
	}
	store, ok := applier.Repo.(FastTrackRepository)
	if !ok {
		return nil
	}
	state, err := store.LoadFastTrackState(ctx)
	if err != nil {
		return err
	}
	hasAdjustment := false
	for _, rule := range report.Rules {
		if rule.Status == "auto_fixable" {
			hasAdjustment = true
		}
	}
	if len(state.Records) == 0 && !hasAdjustment {
		return nil
	}
	for i, record := range state.Records {
		if record.Status == "diverged" {
			continue
		}
		actual, err := readFastTrackRule(ctx, applier.Mutation, record.Menu, record.ID)
		if err != nil {
			return err
		}
		switch {
		case actual == nil:
			fastTrackDiverged(&state, i)
		case fastTrackHash(actual) == fastTrackHash(record.Applied):
			state.Records[i].Status = "applied"
		case record.Status == "intent" && fastTrackHash(actual) == fastTrackHash(record.Before): // Resume only via a reviewed adjustment below.
		default:
			fastTrackDiverged(&state, i)
		}
	}
	if err := store.SaveFastTrackState(ctx, state); err != nil {
		return err
	}
	for _, rule := range report.Rules {
		if rule.Status != "auto_fixable" {
			continue
		}
		index := -1
		for i, record := range state.Records {
			if record.Menu == rule.Menu && record.ID == rule.ID {
				index = i
				break
			}
		}
		if index >= 0 && state.Records[index].Status != "intent" {
			continue
		}
		actual, err := readFastTrackRule(ctx, applier.Mutation, rule.Menu, rule.ID)
		if err != nil {
			return err
		}
		if actual == nil || fastTrackHash(actual) != fastTrackHash(rule.Before) {
			return ErrPlanStale
		}
		if index < 0 {
			applied := cloneStrings(actual)
			applied["connection-mark"] = "no-mark"
			state.Records = append(state.Records, FastTrackRecord{Menu: rule.Menu, ID: rule.ID, Before: actual, Applied: applied, Status: "intent"})
			index = len(state.Records) - 1
			// Durable intent precedes any RouterOS mutation.
			if err := store.SaveFastTrackState(ctx, state); err != nil {
				return err
			}
		}
		record := state.Records[index]
		if _, err := applier.Mutation.Patch(ctx, rule.Menu, rule.ID, routeros.RouterOSFields{"connection-mark": "no-mark"}); err != nil {
			return err
		}
		actual, err = readFastTrackRule(ctx, applier.Mutation, rule.Menu, rule.ID)
		if err != nil {
			return err
		}
		if actual == nil || fastTrackHash(actual) != fastTrackHash(record.Applied) {
			fastTrackDiverged(&state, index)
			if err := store.SaveFastTrackState(ctx, state); err != nil {
				return err
			}
			return errors.New("FastTrack 调整后的回读与预期配置不一致")
		}
		state.Records[index].Status = "applied"
		if err := store.SaveFastTrackState(ctx, state); err != nil {
			return err
		}
	}
	return nil
}

// releaseFastTrack must only be called under DeviceWriteGate after full routing
// verification. A zero database count alone does not prove PBR was removed.
func releaseFastTrack(ctx context.Context, applier *Applier, automatic bool) error {
	store, ok := applier.Repo.(FastTrackRepository)
	if !ok {
		return nil
	}
	count, err := fastTrackConsumers(ctx, applier.Repo)
	if err != nil || count > 0 {
		return err
	}
	state, err := store.LoadFastTrackState(ctx)
	if err != nil || len(state.Records) == 0 {
		return err
	}
	desired, err := buildPlanDesired(ctx, applier, applier.Repo, PolicyDomainRouting, nil)
	if err != nil {
		return err
	}
	if len(desired.Blockers) > 0 {
		return errors.New("路由清理存在阻断项")
	}
	actual, _, err := ScanManagedForDomain(ctx, applier.Mutation, applier.Repo, desired.Objects, PolicyDomainRouting)
	if err != nil {
		return err
	}
	remaining, blockers := DiffDesired(desired.Objects, actual)
	if len(remaining) > 0 || len(blockers) > 0 {
		return errors.New("路由清理尚未验证完成")
	}
	for _, obj := range actual {
		if obj.Ownership == "owned" && (obj.Menu == string(routeros.MenuIPFirewallMangle) || obj.Menu == string(routeros.MenuIPv6FirewallMangle)) {
			return errors.New("受管的路由标记对象仍然存在")
		}
	}
	for i := 0; i < len(state.Records); {
		record := state.Records[i]
		if automatic && record.Status == "restore_blocked" {
			i++
			continue
		}
		current, err := readFastTrackRule(ctx, applier.Mutation, record.Menu, record.ID)
		if err != nil {
			return err
		}
		switch {
		case current == nil:
			fastTrackEvent(&state, "fasttrack_original_rule_missing", record, "原规则已不存在，不重建。")
		case record.Status == "diverged": // Ownership was permanently relinquished for this holding period.
		case (record.Status == "intent" || record.Status == "restoring") && fastTrackHash(current) == fastTrackHash(record.Before): // Unwritten intent, or restore completed before a crash.
		case fastTrackHash(current) != fastTrackHash(record.Applied):
			fastTrackEvent(&state, "fasttrack_external_change_preserved", record, "检测到外部修改，未恢复旧配置。")
		default:
			state.Records[i].Status = "restoring"
			if err := store.SaveFastTrackState(ctx, state); err != nil {
				return err
			}
			if value, present := record.Before["connection-mark"]; present {
				_, err = applier.Mutation.Patch(ctx, record.Menu, record.ID, routeros.RouterOSFields{"connection-mark": value})
			} else if unset, ok := applier.Mutation.(fastTrackUnset); ok {
				err = unset.UnsetFirewallConnectionMark(ctx, record.Menu, record.ID)
			} else {
				err = errors.New("FastTrack 取消设置不可用")
			}
			if err != nil {
				var rejected *routeros.HTTPError
				if errors.As(err, &rejected) && rejected.StatusCode >= 400 && rejected.StatusCode < 500 && rejected.StatusCode != 404 && rejected.StatusCode != 408 && rejected.StatusCode != 429 {
					state.Records[i].Status = "restore_blocked"
					fastTrackEvent(&state, "fasttrack_restore_requires_attention", record, "设备拒绝恢复请求，已停止后台重试；请检查权限或兼容性后手动同步。")
					if saveErr := store.SaveFastTrackState(ctx, state); saveErr != nil {
						return saveErr
					}
				}
				return err
			}
			current, err = readFastTrackRule(ctx, applier.Mutation, record.Menu, record.ID)
			if err != nil {
				return err
			}
			if current == nil || fastTrackHash(current) != fastTrackHash(record.Before) {
				fastTrackEvent(&state, "fasttrack_external_change_preserved", record, "恢复后的状态与预期不同，保留当前配置，不再自动写入。")
			}
		}
		state.Records = append(state.Records[:i], state.Records[i+1:]...)
		if err := store.SaveFastTrackState(ctx, state); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) retryFastTrackReleases(ctx context.Context) {
	m.mu.RLock()
	appliers := make(map[string]*Applier, len(m.appliers))
	for id, a := range m.appliers {
		appliers[id] = a
	}
	m.mu.RUnlock()
	for id, applier := range appliers {
		store, ok := applier.Repo.(FastTrackRepository)
		if !ok {
			continue
		}
		state, err := store.LoadFastTrackState(ctx)
		if err != nil || len(state.Records) == 0 {
			continue
		}
		pending := false
		for _, record := range state.Records {
			if record.Status != "restore_blocked" {
				pending = true
			}
		}
		if !pending {
			continue
		}
		release, ok := m.gate.TryAcquire(id)
		if !ok {
			continue
		}
		attempt, cancel := context.WithTimeout(ctx, 30*time.Second)
		err = releaseFastTrack(attempt, applier, true)
		cancel()
		release()
		if err != nil && m.logger != nil {
			m.logger.Printf("FastTrack restore pending for device %s: %v", id, err)
		}
	}
}

func finishFastTrack(ctx context.Context, applier *Applier, jobID string, report *FastTrackReport) error {
	restoreErr := releaseFastTrack(ctx, applier, false)
	store, ok := applier.Repo.(FastTrackRepository)
	if !ok {
		return restoreErr
	}
	state, err := store.LoadFastTrackState(ctx)
	if err != nil {
		return err
	}
	if len(state.Records) == 0 && len(state.Events) == 0 && state.JobID == "" && restoreErr == nil {
		return nil
	}
	state.JobID = jobID
	state.JobWarnings = nil
	seen := map[string]bool{}
	if report != nil {
		for _, event := range report.Events {
			seen[fastTrackHash(event)] = true
		}
	}
	for _, event := range state.Events {
		if !seen[fastTrackHash(event)] {
			state.JobWarnings = append(state.JobWarnings, event)
		}
	}
	blocked := false
	for _, record := range state.Records {
		if record.Status == "restore_blocked" {
			blocked = true
		}
	}
	if restoreErr != nil && !blocked {
		state.JobWarnings = append(state.JobWarnings, PlanIssue{Code: "fasttrack_restore_pending", Reason: "策略变更已完成，FastTrack 自动恢复暂未完成；后续同步将重新检查并重试。"})
	}
	return store.SaveFastTrackState(ctx, state)
}
