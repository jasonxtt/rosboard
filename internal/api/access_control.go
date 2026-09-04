package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/google/uuid"

	"rosboard/internal/accesscontrol"
	"rosboard/internal/config"
	"rosboard/internal/policyv2"
	"rosboard/internal/store"
	"rosboard/internal/subject"
)

const accessControlBoundary = "访问控制基于目标库中的域名/IP 列表，通过 RouterOS DNS/address-list 进行尽力控制，并非 DPI；共享 CDN/IP、DNS 变化可能造成误控或漏控。域名/IP 访问控制依赖 RouterOS 可见的目标地址；未强制使用本机 DNS，DoH、DoT、VPN 或代理可能绕过规则。整个互联网规则按 RouterOS 默认路由实际出口接口逐个阻断，本地接口不会被选为互联网出口。"

func isAccessControlPath(path string) bool {
	return path == "/api/access-control" || strings.HasPrefix(path, "/api/access-control/")
}

type accessDeviceContext struct {
	device           config.DeviceConfig
	policyRepository *store.PolicyRepository
	accessRepository *store.AccessRepository
}

type accessRuleRequest struct {
	ID               string                      `json:"id"`
	Name             string                      `json:"name"`
	Subject          subject.Subject             `json:"subject"`
	TargetScope      string                      `json:"targetScope"`
	TargetListIDs    []string                    `json:"targetListIds"`
	Enabled          bool                        `json:"enabled"`
	Revision         int64                       `json:"revision"`
	PresetSelections []policyPlanPresetSelection `json:"presetSelections"`
}

type accessInternetEgressRequest struct {
	InternetEgresses map[string][]string `json:"internetEgresses"`
}

type accessRuleMemberResponse struct {
	TerminalID string   `json:"terminalId"`
	Binding    string   `json:"binding"`
	State      string   `json:"state"`
	IPv4       []string `json:"ipv4"`
	IPv6       []string `json:"ipv6"`
	Reason     string   `json:"reason,omitempty"`
}

type accessRuleResponse struct {
	ID            string                     `json:"id"`
	Name          string                     `json:"name"`
	Subject       subject.Subject            `json:"subject"`
	TargetScope   string                     `json:"targetScope"`
	TargetListIDs []string                   `json:"targetListIds"`
	Enabled       bool                       `json:"enabled"`
	Revision      int64                      `json:"revision"`
	CreatedAt     string                     `json:"createdAt"`
	UpdatedAt     string                     `json:"updatedAt"`
	Members       []accessRuleMemberResponse `json:"members"`
	Status        string                     `json:"status"`
	Issues        []string                   `json:"issues"`
}

type accessTerminalResponse struct {
	ID           string   `json:"id"`
	DisplayName  string   `json:"displayName"`
	MACAddress   string   `json:"macAddress"`
	IPv4         []string `json:"ipv4"`
	IPv6         []string `json:"ipv6"`
	RoutingIPv4  []string `json:"routingIpv4"`
	RoutingIPv6  []string `json:"routingIpv6"`
	AutoEligible bool     `json:"autoEligible"`
}

func (s *Server) serveAccessControlAPI(writer http.ResponseWriter, request *http.Request) {
	relative := strings.Trim(strings.TrimPrefix(request.URL.Path, "/api/access-control"), "/")
	parts := strings.Split(relative, "/")
	if len(parts) < 2 || parts[0] != "devices" || strings.TrimSpace(parts[1]) == "" {
		writeAccessError(writer, http.StatusNotFound, "not_found", "not found")
		return
	}
	device, ok := s.resolveAccessDevice(writer, strings.TrimSpace(parts[1]))
	if !ok {
		return
	}
	switch {
	case len(parts) == 2 && request.Method == http.MethodGet:
		s.serveAccessOverview(writer, request, device)
	case len(parts) == 3 && parts[2] == "rules" && request.Method == http.MethodPost:
		s.saveAccessRule(writer, request, device, "")
	case len(parts) == 4 && parts[2] == "rules" && request.Method == http.MethodPut:
		s.saveAccessRule(writer, request, device, parts[3])
	case len(parts) == 4 && parts[2] == "rules" && request.Method == http.MethodDelete:
		s.deleteAccessRule(writer, request, device, parts[3])
	case len(parts) == 3 && parts[2] == "sync" && request.Method == http.MethodPost:
		s.syncAccessControl(writer, request, device)
	case len(parts) == 4 && parts[2] == "jobs" && request.Method == http.MethodGet:
		s.serveAccessJob(writer, request, device, parts[3])
	default:
		writeAccessError(writer, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
}

func (s *Server) resolveAccessDevice(writer http.ResponseWriter, deviceID string) (accessDeviceContext, bool) {
	device, found := s.configSnapshot().Device(deviceID)
	if !found || device.Archived {
		writeAccessError(writer, http.StatusNotFound, "device_not_found", "device not found")
		return accessDeviceContext{}, false
	}
	if s.store == nil {
		writeAccessError(writer, http.StatusServiceUnavailable, "storage_unavailable", "device storage unavailable")
		return accessDeviceContext{}, false
	}
	deviceStore, err := s.store.OpenDevice(deviceID)
	if err != nil {
		writeAccessError(writer, http.StatusServiceUnavailable, "storage_unavailable", "device storage unavailable")
		return accessDeviceContext{}, false
	}
	return accessDeviceContext{device: device, policyRepository: deviceStore.PolicyRepository(), accessRepository: deviceStore.AccessRepository()}, true
}

func (s *Server) serveAccessOverview(writer http.ResponseWriter, request *http.Request, device accessDeviceContext) {
	ctx := request.Context()
	_ = device.accessRepository.EnsureCanonicalAccessMigrated(ctx)
	rules, members, err := s.loadAccessRules(ctx, device)
	if err != nil {
		writeAccessError(writer, http.StatusServiceUnavailable, "load_failed", "failed to load access rules")
		return
	}
	targetLists, err := device.policyRepository.ListTargetLists(ctx)
	if err != nil {
		writeAccessError(writer, http.StatusServiceUnavailable, "load_failed", "failed to load target lists")
		return
	}
	state, err := device.accessRepository.GetState(ctx)
	if err != nil {
		writeAccessError(writer, http.StatusServiceUnavailable, "load_failed", "failed to load access-control state")
		return
	}
	policyState, err := device.policyRepository.GetDeviceState(ctx)
	if err != nil {
		writeAccessError(writer, http.StatusServiceUnavailable, "load_failed", "failed to load policy state")
		return
	}
	terminals := s.accessTerminals(device.device.ID)
	terminalResponses := make([]accessTerminalResponse, 0, len(terminals))
	for _, terminal := range terminals {
		routingIPv4, routingIPv6 := policyv2.RoutingUsableTerminalAddresses(terminal)
		terminalResponses = append(terminalResponses, accessTerminalResponse{
			ID: terminal.ID, DisplayName: terminal.DisplayName, MACAddress: terminal.MACAddress, IPv4: nonNilStrings(terminal.IPv4), IPv6: nonNilStrings(terminal.IPv6), RoutingIPv4: nonNilStrings(routingIPv4), RoutingIPv6: nonNilStrings(routingIPv6),
			AutoEligible: accesscontrol.IsReliableMAC(terminal.MACAddress) && hasObservedAddress(terminal),
		})
	}
	ruleResponses := make([]accessRuleResponse, 0, len(rules))
	for _, rule := range rules {
		ruleResponses = append(ruleResponses, buildAccessRuleResponse(rule, members, terminals, state, policyState, targetLists))
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"device":      map[string]any{"id": device.device.ID, "name": device.device.Name, "enabled": device.device.Enabled},
		"rules":       ruleResponses,
		"targetLists": targetLists,
		"terminals":   terminalResponses,
		"state":       state,
		"job":         policyState.Job,
		"boundary":    accessControlBoundary,
	})
}

func (s *Server) loadAccessRules(ctx context.Context, device accessDeviceContext) ([]accesscontrol.AccessRule, []accesscontrol.RuleMember, error) {
	rules, err := device.accessRepository.ListRules(ctx)
	if err != nil {
		return nil, nil, err
	}
	members, err := device.accessRepository.ListMembers(ctx)
	if err != nil {
		return nil, nil, err
	}
	return rules, members, nil
}

// buildAccessRuleResponse combines the stored rule with member projection
// states and the device-level job so the UI can show 已应用 / 正在应用 /
// 待应用 / 已降级 / 应用失败 without understanding RouterOS reconcile.
func buildAccessRuleResponse(rule accesscontrol.AccessRule, members []accesscontrol.RuleMember, terminals []accesscontrol.Terminal, state accesscontrol.State, policyState policyv2.DeviceState, targetLists []policyv2.TargetList) accessRuleResponse {
	ruleMembers := make([]accesscontrol.RuleMember, 0, len(members))
	for _, member := range members {
		if member.RuleID == rule.ID {
			ruleMembers = append(ruleMembers, member)
		}
	}
	evaluations := accesscontrol.EvaluateMembers(ruleMembers, terminals)
	evaluationByTerminal := make(map[string]accesscontrol.MemberEvaluation, len(evaluations))
	for _, evaluation := range evaluations {
		evaluationByTerminal[evaluation.Member.TerminalID] = evaluation
	}
	memberResponses := make([]accessRuleMemberResponse, 0, len(ruleMembers))
	issues := make([]string, 0)
	monitorDrift := false
	for _, member := range ruleMembers {
		evaluation := evaluationByTerminal[member.TerminalID]
		if evaluation.State != accesscontrol.MemberResolved {
			issues = append(issues, evaluation.Reason)
		}
		if member.Binding == accesscontrol.BindingAuto && accesscontrol.IsReliableMAC(member.AnchorMAC) &&
			(!sameAccessAddresses(evaluation.IPv4, evaluation.Member.LastIPv4) || !sameAccessAddresses(evaluation.IPv6, evaluation.Member.LastIPv6)) {
			monitorDrift = true
		}
		memberResponses = append(memberResponses, accessRuleMemberResponse{
			TerminalID: member.TerminalID,
			Binding:    member.Binding,
			State:      evaluation.State,
			IPv4:       evaluation.IPv4,
			IPv6:       evaluation.IPv6,
			Reason:     evaluation.Reason,
		})
	}
	targetIssues := accessRuleTargetIssues(rule, targetLists)
	issues = append(issues, targetIssues...)
	statusIssues := append([]string{}, targetIssues...)
	issues = append(issues, rule.MigrationIssues...)
	statusIssues = append(statusIssues, rule.MigrationIssues...)
	status := accessRuleStatusWithDrift(rule, state, policyState, memberResponses, monitorDrift, statusIssues...)
	if status == "applied" && len(issues) > 0 {
		status = "degraded"
	}
	return accessRuleResponse{
		ID: rule.ID, Name: rule.Name, Subject: rule.Subject, TargetScope: rule.TargetScope, TargetListIDs: nonNilStrings(rule.TargetListIDs),
		Enabled: rule.Enabled, Revision: rule.Revision,
		CreatedAt: rule.CreatedAt.UTC().Format(time.RFC3339), UpdatedAt: rule.UpdatedAt.UTC().Format(time.RFC3339),
		Members: memberResponses, Status: status, Issues: issues,
	}
}

func accessRuleTargetIssues(rule accesscontrol.AccessRule, targets []policyv2.TargetList) []string {
	if rule.TargetScope != accesscontrol.TargetScopeTargets {
		return nil
	}
	targetByID := make(map[string]policyv2.TargetList, len(targets))
	for _, target := range targets {
		targetByID[target.ID] = target
	}
	issues := make([]string, 0)
	for _, targetID := range rule.TargetListIDs {
		target, ok := targetByID[targetID]
		if !ok {
			issues = append(issues, "目标列表 "+targetID+" 已不存在，规则当前未能完整生效。")
			continue
		}
		switch {
		case target.PendingDeletion:
			issues = append(issues, "目标列表 "+target.Name+" 正在清理，规则当前未能完整生效。")
		case strings.TrimSpace(target.ActiveVersionID) == "" && strings.TrimSpace(target.PendingVersionID) == "":
			issues = append(issues, "目标列表 "+target.Name+" 尚无已应用版本，规则当前未能完整生效。")
		}
	}
	return issues
}

func accessRuleStatus(rule accesscontrol.AccessRule, state accesscontrol.State, policyState policyv2.DeviceState, members []accessRuleMemberResponse, targetIssues ...string) string {
	return accessRuleStatusWithDrift(rule, state, policyState, members, false, targetIssues...)
}

func accessRuleStatusWithDrift(rule accesscontrol.AccessRule, state accesscontrol.State, policyState policyv2.DeviceState, members []accessRuleMemberResponse, monitorDrift bool, targetIssues ...string) string {
	job := policyState.Job
	if job.ID != "" && job.State != "" && job.State != "committed" && job.State != "failed" {
		return "applying"
	}
	if job.State == "failed" {
		return "failed"
	}
	if !rule.Enabled {
		return "disabled"
	}
	if !state.Applied() {
		return "pending"
	}
	if len(targetIssues) > 0 {
		return "degraded"
	}
	if monitorDrift {
		return "pending"
	}
	for _, member := range members {
		if member.State != accesscontrol.MemberResolved {
			return "degraded"
		}
	}
	return "applied"
}

func (s *Server) saveAccessRule(writer http.ResponseWriter, request *http.Request, device accessDeviceContext, pathID string) {
	if !s.requirePolicyWriteAccess(writer, request, device.device) {
		return
	}
	var payload accessRuleRequest
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		writeAccessError(writer, http.StatusBadRequest, "invalid_body", "invalid request body")
		return
	}
	if payload.TargetScope != accesscontrol.TargetScopeInternet && payload.TargetScope != accesscontrol.TargetScopeTargets {
		writeAccessError(writer, http.StatusUnprocessableEntity, "canonical_access_rule_required", "access rules must use subject, targetScope=internet|targets, and targetListIds")
		return
	}
	rule := accesscontrol.AccessRule{
		ID: payload.ID, Name: payload.Name, Subject: payload.Subject, TargetScope: payload.TargetScope, TargetListIDs: payload.TargetListIDs,
		Enabled: payload.Enabled, Revision: payload.Revision,
	}
	if pathID != "" {
		rule.ID = pathID
	}
	if rule.ID == "" {
		rule.ID = uuid.NewString()
	}
	if err := device.accessRepository.EnsureCanonicalAccessMigrated(request.Context()); err != nil {
		writeAccessError(writer, http.StatusConflict, "canonical_access_migration_required", err.Error())
		return
	}
	rule, err := accesscontrol.NormalizeRule(rule)
	if err != nil {
		writeAccessError(writer, http.StatusUnprocessableEntity, "invalid_rule", err.Error())
		return
	}
	existingMembers := make(map[string]accesscontrol.RuleMember)
	if pathID != "" {
		existingRules, loadErr := device.accessRepository.ListRules(request.Context())
		if loadErr != nil {
			writeAccessError(writer, http.StatusServiceUnavailable, "load_failed", "failed to load access rule")
			return
		}
		found := false
		for _, existingRule := range existingRules {
			if existingRule.ID == rule.ID {
				found = true
				break
			}
		}
		if !found {
			writeAccessError(writer, http.StatusNotFound, "rule_not_found", "access rule not found")
			return
		}
		storedMembers, loadErr := device.accessRepository.ListMembers(request.Context())
		if loadErr != nil {
			writeAccessError(writer, http.StatusServiceUnavailable, "load_failed", "failed to load access rule members")
			return
		}
		for _, storedMember := range storedMembers {
			if storedMember.RuleID == rule.ID {
				existingMembers[storedMember.TerminalID] = storedMember
			}
		}
	}
	terminals := s.accessTerminals(device.device.ID)
	memberRequests := payload.Subject.Members
	members := make([]accesscontrol.RuleMember, 0, len(memberRequests))
	for _, memberRequest := range memberRequests {
		terminalID := strings.TrimSpace(memberRequest.TerminalID)
		member := accesscontrol.RuleMember{
			RuleID: rule.ID, TerminalID: terminalID, Binding: strings.TrimSpace(memberRequest.Binding),
			PinnedIPv4: memberRequest.PinnedIPv4, PinnedIPv6: memberRequest.PinnedIPv6,
		}
		previous, hadPrevious := existingMembers[terminalID]
		terminal, found := accessTerminalByID(terminals, terminalID)
		if member.Binding == accesscontrol.BindingAuto {
			if found {
				currentAnchor, anchorErr := accesscontrol.NormalizeMAC(terminal.MACAddress)
				if anchorErr != nil || currentAnchor == "" {
					if hadPrevious && previous.Binding == accesscontrol.BindingAuto && previous.AnchorMAC != "" {
						// A transiently malformed monitor identity is treated like
						// an unseen terminal for an existing auto member: preserve
						// the trusted anchor and projection rather than accepting a
						// new, unverified identity.
						member.AnchorMAC = previous.AnchorMAC
					} else {
						writeAccessError(writer, http.StatusUnprocessableEntity, "terminal_unresolvable", "自动跟随设备需要可靠终端身份和至少一个当前地址")
						return
					}
				} else {
					member.AnchorMAC = currentAnchor
					if !hasObservedAddress(terminal) && (!hadPrevious || previous.Binding != accesscontrol.BindingAuto || previous.AnchorMAC != member.AnchorMAC) {
						writeAccessError(writer, http.StatusUnprocessableEntity, "terminal_unresolvable", "自动跟随设备需要可靠终端身份和至少一个当前地址")
						return
					}
				}
			} else if hadPrevious && previous.Binding == accesscontrol.BindingAuto && previous.AnchorMAC != "" {
				// Existing auto members may be edited while temporarily absent.
				// New members still require a current identity and address.
				member.AnchorMAC = previous.AnchorMAC
			} else {
				writeAccessError(writer, http.StatusUnprocessableEntity, "terminal_unavailable", "受控设备当前不在监控快照中，无法确认其身份或地址")
				return
			}
		} else if member.Binding != accesscontrol.BindingFixed || !found {
			if !found && hadPrevious && previous.Binding == accesscontrol.BindingFixed {
				// An unchanged fixed pin remains valid even while the terminal is
				// temporarily absent. The address is intentionally not migrated.
			} else if !found {
				writeAccessError(writer, http.StatusUnprocessableEntity, "terminal_unavailable", "受控设备当前不在监控快照中，无法确认其身份或地址")
				return
			}
		}
		member, err := accesscontrol.NormalizeMember(member)
		if err != nil {
			writeAccessError(writer, http.StatusUnprocessableEntity, "invalid_member", err.Error())
			return
		}
		if member.Binding == accesscontrol.BindingFixed {
			unchangedFixed := hadPrevious && previous.Binding == accesscontrol.BindingFixed &&
				sameAccessAddresses(member.PinnedIPv4, previous.PinnedIPv4) && sameAccessAddresses(member.PinnedIPv6, previous.PinnedIPv6)
			if !unchangedFixed && !found {
				writeAccessError(writer, http.StatusUnprocessableEntity, "terminal_unavailable", "受控设备当前不在监控快照中，无法确认其身份或地址")
				return
			}
			if found && !unchangedFixed {
				if err := validatePinnedAddresses(member, terminal); err != nil {
					writeAccessError(writer, http.StatusUnprocessableEntity, "invalid_pinned_address", err.Error())
					return
				}
			}
		}
		if member.Binding == accesscontrol.BindingAuto && found && hadPrevious && previous.Binding == accesscontrol.BindingAuto && previous.AnchorMAC != "" && member.AnchorMAC != previous.AnchorMAC {
			writeAccessError(writer, http.StatusConflict, "terminal_identity_changed", "自动跟随设备的身份已变化，请重新确认终端后再保存")
			return
		}
		members = append(members, member)
	}
	if len(members) == 0 && rule.Subject.Mode != subject.ModeAll && len(rule.Subject.Prefixes) == 0 {
		writeAccessError(writer, http.StatusUnprocessableEntity, "members_required", "请至少选择一台设备")
		return
	}
	if s.policy == nil || s.policy.ApplierFor(device.device.ID) == nil {
		writeAccessError(writer, http.StatusServiceUnavailable, "runtime_unavailable", "policy runtime is unavailable")
		return
	}
	proposal := policyv2.AccessProposal{Rule: rule, Members: members}
	if rule.TargetScope == accesscontrol.TargetScopeTargets && len(payload.PresetSelections) > 0 {
		policyDevice := policyDeviceContext{device: device.device, repository: device.policyRepository, accessRepository: device.accessRepository}
		presetProposal := &policyv2.PolicyProposal{}
		for _, selection := range payload.PresetSelections {
			if err := s.appendProposedPresetTargets(request.Context(), policyDevice, selection, presetProposal); err != nil {
				writeAccessError(writer, http.StatusUnprocessableEntity, "invalid_preset_selection", err.Error())
				return
			}
		}
		selectedTargetIDs := make(map[string]bool, len(rule.TargetListIDs))
		for _, targetID := range rule.TargetListIDs {
			selectedTargetIDs[targetID] = true
		}
		for _, target := range presetProposal.TargetLists {
			if selectedTargetIDs[target.Target.ID] {
				proposal.TargetLists = append(proposal.TargetLists, target)
			}
		}
	}
	plan, err := s.policy.GeneratePlanWithOptions(request.Context(), device.device.ID, "access-rule-save", policyv2.PlanOptions{AccessProposal: &proposal})
	if err != nil {
		status, code := accessRulePlanApplyError(err)
		writeJSON(writer, status, accessPlanErrorPayload(err, map[string]any{"code": code, "error": err.Error()}))
		return
	}
	if len(plan.Plan.Blockers) > 0 {
		err := accessPlanBlockedError{blockers: plan.Plan.Blockers, candidates: plan.Plan.InternetEgressCandidates}
		status, code := accessRulePlanApplyError(err)
		writeJSON(writer, status, accessPlanErrorPayload(err, map[string]any{"code": code, "error": err.Error()}))
		return
	}
	job, err := s.policy.ApplyPlanWithHash(request.Context(), device.device.ID, plan.PlanID, plan.PlanHash)
	if err != nil {
		status, code := accessRulePlanApplyError(err)
		writeJSON(writer, status, accessPlanErrorPayload(err, map[string]any{"code": code, "error": err.Error()}))
		return
	}
	if storedRules, loadErr := device.accessRepository.ListRules(request.Context()); loadErr == nil {
		for _, storedRule := range storedRules {
			if storedRule.ID == rule.ID {
				rule = storedRule
				break
			}
		}
	}
	writeJSON(writer, http.StatusAccepted, map[string]any{"rule": rule, "job": job, "jobId": job.ID})
}

func sameAccessAddresses(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

// validatePinnedAddresses keeps fixed members honest: pinned addresses must be
// currently observed on the chosen terminal so a typo cannot block a stranger.
func validatePinnedAddresses(member accesscontrol.RuleMember, terminal accesscontrol.Terminal) error {
	observed := make(map[string]bool)
	for _, address := range append(append([]string{}, terminal.IPv4...), terminal.IPv6...) {
		parsed, err := netip.ParseAddr(strings.TrimSpace(address))
		if err == nil {
			observed[parsed.String()] = true
		}
	}
	for _, address := range append(append([]string{}, member.PinnedIPv4...), member.PinnedIPv6...) {
		if !observed[strings.TrimSpace(address)] {
			return fmt.Errorf("固定地址 %s 不是设备当前观察到的地址", address)
		}
	}
	return nil
}

func hasObservedAddress(terminal accesscontrol.Terminal) bool {
	for _, values := range [][]string{terminal.IPv4, terminal.IPv6} {
		for _, value := range values {
			address, err := netip.ParseAddr(strings.TrimSpace(value))
			if err == nil && address.Zone() == "" {
				return true
			}
		}
	}
	return false
}

func (s *Server) deleteAccessRule(writer http.ResponseWriter, request *http.Request, device accessDeviceContext, ruleID string) {
	if !s.requirePolicyWriteAccess(writer, request, device.device) {
		return
	}
	revision := queryRevision(request)
	if revision < 0 {
		writeAccessError(writer, http.StatusBadRequest, "invalid_revision", "revision must be non-negative")
		return
	}
	if err := device.accessRepository.DeleteRule(request.Context(), ruleID, revision, accessActor(request)); err != nil {
		switch {
		case errors.Is(err, accesscontrol.ErrRuleNotFound):
			writeAccessError(writer, http.StatusNotFound, "rule_not_found", err.Error())
		case errors.Is(err, accesscontrol.ErrRevisionStale):
			writeAccessError(writer, http.StatusConflict, "revision_stale", err.Error())
		default:
			writeAccessError(writer, http.StatusServiceUnavailable, "delete_failed", err.Error())
		}
		return
	}
	job, err := s.applyAccessDesired(request, device.device.ID, "access-rule-delete", nil)
	if err != nil {
		status, code := policyPlanApplyError(err)
		writeJSON(writer, status, accessPlanErrorPayload(err, map[string]any{"code": code, "error": err.Error(), "deleted": true, "desiredSaved": true}))
		return
	}
	writeJSON(writer, http.StatusAccepted, map[string]any{"deleted": true, "job": job, "jobId": job.ID})
}

func (s *Server) syncAccessControl(writer http.ResponseWriter, request *http.Request, device accessDeviceContext) {
	if !s.requirePolicyWriteAccess(writer, request, device.device) {
		return
	}
	var payload accessInternetEgressRequest
	if request.Body != nil {
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil && !errors.Is(err, io.EOF) {
			writeAccessError(writer, http.StatusBadRequest, "invalid_body", "invalid request body")
			return
		}
	}
	job, err := s.applyAccessDesired(request, device.device.ID, "access-control-sync", payload.InternetEgresses)
	if err != nil {
		status, code := policyPlanApplyError(err)
		writeJSON(writer, status, accessPlanErrorPayload(err, map[string]any{"code": code, "error": err.Error()}))
		return
	}
	writeJSON(writer, http.StatusAccepted, map[string]any{"job": job, "jobId": job.ID})
}

func (s *Server) serveAccessJob(writer http.ResponseWriter, request *http.Request, device accessDeviceContext, jobID string) {
	if s.policy == nil {
		writeAccessError(writer, http.StatusServiceUnavailable, "runtime_unavailable", "policy runtime is unavailable")
		return
	}
	job, err := s.policy.GetJob(request.Context(), device.device.ID, jobID)
	if errors.Is(err, policyv2.ErrJobNotFound) {
		writeAccessError(writer, http.StatusNotFound, "job_not_found", err.Error())
		return
	}
	if err != nil {
		writeAccessError(writer, http.StatusServiceUnavailable, "job_load_failed", err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"job": job})
}

func (s *Server) applyAccessDesired(request *http.Request, deviceID, kind string, internetEgresses map[string][]string) (policyv2.ApplyJob, error) {
	if s.policy == nil || s.policy.ApplierFor(deviceID) == nil {
		return policyv2.ApplyJob{}, errors.New("policy runtime is unavailable")
	}
	plan, err := s.policy.GeneratePlanWithOptions(request.Context(), deviceID, kind, policyv2.PlanOptions{InternetEgresses: internetEgresses})
	if err != nil {
		return policyv2.ApplyJob{}, err
	}
	if len(plan.Plan.Blockers) > 0 {
		return policyv2.ApplyJob{}, accessPlanBlockedError{blockers: plan.Plan.Blockers, candidates: plan.Plan.InternetEgressCandidates}
	}
	return s.policy.ApplyPlan(request.Context(), deviceID, plan.PlanID)
}

type accessPlanBlockedError struct {
	blockers   []policyv2.PlanIssue
	candidates map[string][]accesscontrol.InternetEgressCandidate
}

func (err accessPlanBlockedError) Error() string {
	if len(err.blockers) == 0 {
		return policyv2.ErrPlanBlocked.Error()
	}
	message := "RouterOS 同步已阻止：" + err.blockers[0].Reason
	if len(err.blockers) > 1 {
		message += fmt.Sprintf("（另有 %d 个阻断项）", len(err.blockers)-1)
	}
	return message
}

func (accessPlanBlockedError) Unwrap() error {
	return policyv2.ErrPlanBlocked
}

func accessPlanErrorPayload(err error, payload map[string]any) map[string]any {
	var blocked accessPlanBlockedError
	if errors.As(err, &blocked) && len(blocked.candidates) > 0 {
		candidates := make(map[string][]accesscontrol.InternetEgressCandidate, len(blocked.candidates))
		for _, blocker := range blocked.blockers {
			if blocker.Code != "access_internet_egress_unavailable" || blocker.Family == "" {
				continue
			}
			if values := blocked.candidates[blocker.Family]; len(values) > 0 {
				candidates[blocker.Family] = values
			}
		}
		if len(candidates) == 0 {
			candidates = blocked.candidates
		}
		payload["internetEgressCandidates"] = candidates
	}
	return payload
}

func accessRulePlanApplyError(err error) (int, string) {
	switch {
	case errors.Is(err, accesscontrol.ErrRevisionStale), errors.Is(err, policyv2.ErrRevisionStale), errors.Is(err, policyv2.ErrPlanStale):
		return http.StatusConflict, "revision_stale"
	case errors.Is(err, accesscontrol.ErrMemberDuplicate):
		return http.StatusConflict, "member_duplicate"
	case errors.Is(err, accesscontrol.ErrMemberAnchorChanged):
		return http.StatusConflict, "terminal_identity_changed"
	case errors.Is(err, accesscontrol.ErrMemberAnchorRequired):
		return http.StatusUnprocessableEntity, "terminal_unresolvable"
	case errors.Is(err, policyv2.ErrTargetListNotFound), errors.Is(err, policyv2.ErrSourceNotFound):
		return http.StatusUnprocessableEntity, "target_list_unavailable"
	default:
		return policyPlanApplyError(err)
	}
}

func (s *Server) accessTerminals(deviceID string) []accesscontrol.Terminal {
	if s.accessTerminalsFn != nil {
		return s.accessTerminalsFn(deviceID)
	}
	if s.manager == nil {
		return []accesscontrol.Terminal{}
	}
	monitor, err := s.manager.Monitor(deviceID)
	if err != nil {
		return []accesscontrol.Terminal{}
	}
	snapshot := monitor.Snapshot()
	result := make([]accesscontrol.Terminal, 0, len(snapshot.Terminals))
	for _, terminal := range snapshot.Terminals {
		result = append(result, accesscontrol.Terminal{
			ID: terminal.ID, DisplayName: terminal.DisplayName, MACAddress: terminal.MACAddress,
			IPv4: nonNilStrings(terminal.IPv4), IPv6: nonNilStrings(terminal.IPv6),
		})
	}
	return result
}

// nonNilStrings 保证 JSON 数组字段永远序列化为 [] 而不是 null，
// 前端对终端地址数组直接取 .length / 展开，null 会让整个页面渲染崩溃。
func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func accessTerminalByID(terminals []accesscontrol.Terminal, terminalID string) (accesscontrol.Terminal, bool) {
	for _, terminal := range terminals {
		if terminal.ID == terminalID {
			return terminal, true
		}
	}
	return accesscontrol.Terminal{}, false
}

func accessActor(request *http.Request) string {
	actor := strings.TrimSpace(sessionFromRequest(request).Username)
	if actor == "" {
		return "admin"
	}
	return actor
}

func writeAccessError(writer http.ResponseWriter, status int, code, message string) {
	writeJSON(writer, status, map[string]any{"code": code, "error": message})
}
