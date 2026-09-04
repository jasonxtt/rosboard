package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"rosboard/internal/accesscontrol"
	"rosboard/internal/policyv2"
	"rosboard/internal/subject"
)

func (s *Server) servePolicyRoutingRules(writer http.ResponseWriter, request *http.Request, parts []string) {
	switch len(parts) {
	case 1:
		switch request.Method {
		case http.MethodGet:
			s.listPolicyRoutingRules(writer, request)
		case http.MethodPost:
			s.savePolicyRoutingRule(writer, request, "")
		default:
			writePolicyJson(writer, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		}
	case 2:
		switch request.Method {
		case http.MethodGet:
			s.getPolicyRoutingRule(writer, request, parts[1])
		case http.MethodPut:
			s.savePolicyRoutingRule(writer, request, parts[1])
		case http.MethodDelete:
			s.deletePolicyRoutingRule(writer, request, parts[1])
		default:
			writePolicyJson(writer, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		}
	default:
		writePolicyJson(writer, http.StatusNotFound, map[string]any{"error": "not found"})
	}
}

func (s *Server) listPolicyRoutingRules(writer http.ResponseWriter, request *http.Request) {
	device, ok := s.resolvePolicyDevice(writer, request)
	if !ok {
		return
	}
	rules, err := device.repository.ListRoutingRules(request.Context())
	if err != nil {
		writePolicyJson(writer, http.StatusServiceUnavailable, map[string]any{"code": "routing_rules_unavailable", "error": err.Error()})
		return
	}
	writePolicyJson(writer, http.StatusOK, map[string]any{"rules": rules})
}

type routingRuleSavePayload struct {
	policyv2.RoutingRule
	DeferApply bool `json:"deferApply"`
}

type routingRuleSubjectError struct {
	code    string
	message string
}

func (e *routingRuleSubjectError) Error() string { return e.message }

func (s *Server) canonicalizeRoutingRuleSubject(ctx context.Context, device policyDeviceContext, rule policyv2.RoutingRule) (policyv2.RoutingRule, error) {
	if rule.Subject.Mode == policyv2.SubjectModeAll {
		return rule, nil
	}

	terminals := s.accessTerminals(device.device.ID)
	var existing *policyv2.RoutingRule
	if strings.TrimSpace(rule.ID) != "" {
		current, err := device.repository.GetRoutingRule(ctx, rule.ID)
		switch {
		case err == nil:
			existing = &current
		case !errors.Is(err, policyv2.ErrRoutingRuleNotFound):
			return policyv2.RoutingRule{}, err
		}
	}

	members := append([]policyv2.SubjectMember(nil), rule.Subject.Members...)
	for index := range members {
		member := &members[index]
		member.TerminalID = strings.TrimSpace(member.TerminalID)
		terminal, found := accessTerminalByID(terminals, member.TerminalID)
		if !found {
			return policyv2.RoutingRule{}, &routingRuleSubjectError{code: "terminal_unavailable", message: "设备当前不在监控快照中，无法确认其可用 IP 地址；请等待设备上线后重试"}
		}
		ipv4, ipv6 := policyv2.RoutingUsableTerminalAddresses(terminal)
		if len(ipv4)+len(ipv6) == 0 {
			return policyv2.RoutingRule{}, &routingRuleSubjectError{code: "terminal_no_address", message: "该设备当前没有可用 IP 地址，无法作为策略来源；请使用手动地址/CIDR或等待设备上线"}
		}
		member.Binding = strings.TrimSpace(member.Binding)
		switch member.Binding {
		case subject.BindingAuto:
			anchor := ""
			if accesscontrol.IsReliableMAC(terminal.MACAddress) {
				anchor, _ = accesscontrol.NormalizeMAC(terminal.MACAddress)
			}
			if anchor != "" {
				member.AnchorMAC = anchor
				member.PinnedIPv4, member.PinnedIPv6 = []string{}, []string{}
				if previous, ok := routingRuleSubjectMember(existing, member.TerminalID); ok && previous.Binding == subject.BindingAuto && previous.AnchorMAC == anchor {
					member.LastIPv4 = append([]string{}, previous.LastIPv4...)
					member.LastIPv6 = append([]string{}, previous.LastIPv6...)
				}
				continue
			}
			member.Binding = subject.BindingFixed
			member.AnchorMAC = ""
			member.PinnedIPv4 = append([]string{}, ipv4...)
			member.PinnedIPv6 = append([]string{}, ipv6...)
			member.LastIPv4, member.LastIPv6 = []string{}, []string{}
		case subject.BindingFixed:
			pinnedIPv4, err := subject.NormalizeAddresses(member.PinnedIPv4, true)
			if err != nil {
				return policyv2.RoutingRule{}, &routingRuleSubjectError{code: "invalid_pinned_address", message: err.Error()}
			}
			pinnedIPv6, err := subject.NormalizeAddresses(member.PinnedIPv6, false)
			if err != nil {
				return policyv2.RoutingRule{}, &routingRuleSubjectError{code: "invalid_pinned_address", message: err.Error()}
			}
			member.PinnedIPv4, member.PinnedIPv6 = pinnedIPv4, pinnedIPv6
			member.AnchorMAC = ""
			observed := terminal
			observed.IPv4, observed.IPv6 = ipv4, ipv6
			if err := validatePinnedAddresses(accesscontrol.RuleMember{PinnedIPv4: pinnedIPv4, PinnedIPv6: pinnedIPv6}, observed); err != nil {
				return policyv2.RoutingRule{}, &routingRuleSubjectError{code: "invalid_pinned_address", message: err.Error()}
			}
		}
	}
	rule.Subject.Members = members
	return rule, nil
}

func routingRuleSubjectMember(rule *policyv2.RoutingRule, terminalID string) (policyv2.SubjectMember, bool) {
	if rule == nil {
		return policyv2.SubjectMember{}, false
	}
	for _, member := range rule.Subject.Members {
		if member.TerminalID == terminalID {
			return member, true
		}
	}
	return policyv2.SubjectMember{}, false
}

func (s *Server) getPolicyRoutingRule(writer http.ResponseWriter, request *http.Request, id string) {
	device, ok := s.resolvePolicyDevice(writer, request)
	if !ok {
		return
	}
	rule, err := device.repository.GetRoutingRule(request.Context(), id)
	if errors.Is(err, policyv2.ErrRoutingRuleNotFound) {
		writePolicyJson(writer, http.StatusNotFound, map[string]any{"code": "routing_rule_not_found", "error": "routing rule not found"})
		return
	}
	if err != nil {
		writePolicyJson(writer, http.StatusServiceUnavailable, map[string]any{"code": "routing_rule_unavailable", "error": err.Error()})
		return
	}
	writePolicyJson(writer, http.StatusOK, rule)
}

func (s *Server) savePolicyRoutingRule(writer http.ResponseWriter, request *http.Request, pathID string) {
	device, ok := s.resolvePolicyDevice(writer, request)
	if !ok {
		return
	}
	if !s.requirePolicyWriteAccess(writer, request, device.device) {
		return
	}
	var payload routingRuleSavePayload
	if err := decodeJSONBody(writer, request, &payload); err != nil {
		return
	}
	if pathID != "" {
		payload.RoutingRule.ID = pathID
	}
	if payload.RoutingRule.ID == "" {
		payload.RoutingRule.ID = uuid.NewString()
	}
	canonical, err := s.canonicalizeRoutingRuleSubject(request.Context(), device, payload.RoutingRule)
	if err != nil {
		writeRoutingRuleSaveError(writer, err)
		return
	}
	payload.RoutingRule = canonical
	rule, err := device.repository.SaveRoutingRule(request.Context(), payload.RoutingRule)
	if err != nil {
		writeRoutingRuleSaveError(writer, err)
		return
	}
	if !payload.DeferApply && s.policy != nil && s.policy.ApplierFor(device.device.ID) != nil {
		job, applyErr := s.policy.GenerateAndApply(request.Context(), device.device.ID, "routing-rule-save")
		if applyErr != nil {
			status, code := policyPlanApplyError(applyErr)
			writePolicyJson(writer, status, map[string]any{"code": code, "error": applyErr.Error(), "rule": rule})
			return
		}
		writePolicyJson(writer, http.StatusAccepted, map[string]any{"rule": rule, "job": job, "jobId": job.ID})
		return
	}
	writePolicyJson(writer, http.StatusOK, rule)
}

func (s *Server) deletePolicyRoutingRule(writer http.ResponseWriter, request *http.Request, id string) {
	device, ok := s.resolvePolicyDevice(writer, request)
	if !ok {
		return
	}
	if !s.requirePolicyWriteAccess(writer, request, device.device) {
		return
	}
	revision := queryRevision(request)
	if revision < 0 {
		writePolicyJson(writer, http.StatusBadRequest, map[string]any{"code": "invalid_revision", "error": "revision must be non-negative"})
		return
	}
	if err := device.repository.DeleteRoutingRule(request.Context(), id, revision); err != nil {
		if errors.Is(err, policyv2.ErrRoutingRuleNotFound) {
			writePolicyJson(writer, http.StatusNotFound, map[string]any{"code": "routing_rule_not_found", "error": "routing rule not found"})
			return
		}
		if errors.Is(err, policyv2.ErrRevisionStale) {
			writePolicyJson(writer, http.StatusConflict, map[string]any{"code": "revision_stale", "error": err.Error()})
			return
		}
		writePolicyJson(writer, http.StatusServiceUnavailable, map[string]any{"code": "delete_failed", "error": err.Error()})
		return
	}
	if s.policy != nil && s.policy.ApplierFor(device.device.ID) != nil {
		job, err := s.policy.GenerateAndApply(request.Context(), device.device.ID, "routing-rule-delete")
		if err != nil {
			status, code := policyPlanApplyError(err)
			writePolicyJson(writer, status, map[string]any{"code": code, "error": err.Error()})
			return
		}
		writePolicyJson(writer, http.StatusAccepted, map[string]any{"deleted": true, "job": job, "jobId": job.ID})
		return
	}
	writePolicyJson(writer, http.StatusOK, map[string]any{"deleted": true})
}

func writeRoutingRuleSaveError(writer http.ResponseWriter, err error) {
	status, code := http.StatusUnprocessableEntity, "invalid_routing_rule"
	var subjectErr *routingRuleSubjectError
	switch {
	case errors.As(err, &subjectErr):
		code = subjectErr.code
	case errors.Is(err, policyv2.ErrRoutingRuleNotFound):
		status, code = http.StatusNotFound, "routing_rule_not_found"
	case errors.Is(err, policyv2.ErrRevisionStale):
		status, code = http.StatusConflict, "revision_stale"
	case errors.Is(err, policyv2.ErrEgressNotFound):
		status, code = http.StatusUnprocessableEntity, "egress_not_found"
	case errors.Is(err, policyv2.ErrTargetListNotFound):
		status, code = http.StatusUnprocessableEntity, "target_list_not_found"
	case errors.Is(err, policyv2.ErrRoutingExcludedRequiresIngress):
		status, code = http.StatusUnprocessableEntity, "routing_excluded_requires_ingress"
	case strings.Contains(err.Error(), "database"):
		status, code = http.StatusServiceUnavailable, "save_failed"
	}
	writePolicyJson(writer, status, map[string]any{"code": code, "error": err.Error()})
}
