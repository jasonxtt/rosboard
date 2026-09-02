package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"rosboard/internal/policyv2"
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
	switch {
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
