package api

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"rosboard/internal/config"
	"rosboard/internal/policy"
	"rosboard/internal/policyv2"
	"rosboard/internal/routeros"
	"rosboard/internal/store"
)

func isPolicyRoutingPath(path string) bool {
	return path == "/api/policy-routing" || strings.HasPrefix(path, "/api/policy-routing/")
}

func (s *Server) servePolicyRoutingAPI(writer http.ResponseWriter, request *http.Request) {
	relative := strings.TrimPrefix(request.URL.Path, "/api/policy-routing")
	relative = strings.TrimPrefix(relative, "/")
	if relative == "" {
		writePolicyJSON(writer, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	parts := strings.Split(relative, "/")
	switch parts[0] {
	case "overview":
		if request.Method != http.MethodGet {
			writePolicyJSON(writer, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		s.servePolicyOverview(writer, request)
	case "discovery":
		if request.Method != http.MethodGet {
			writePolicyJSON(writer, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		s.servePolicyDiscovery(writer, request)
	case "traffic-ingress", "lan-scope":
		s.servePolicyTrafficIngress(writer, request)
	case "egresses":
		s.servePolicyEgress(writer, request, parts)
	case "sources":
		if len(parts) >= 2 && parts[1] == "url" && len(parts) >= 3 && parts[2] == "preview" {
			if request.Method != http.MethodPost {
				writePolicyJSON(writer, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
				return
			}
			s.servePolicyURLPreview(writer, request)
			return
		}
		if len(parts) >= 2 && parts[1] == "upload" && len(parts) >= 3 && parts[2] == "preview" {
			if request.Method != http.MethodPost {
				writePolicyJSON(writer, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
				return
			}
			s.servePolicyUploadPreview(writer, request)
			return
		}
		s.servePolicySource(writer, request, parts)
	case "plans":
		s.servePolicyPlans(writer, request, parts)
	case "jobs":
		s.servePolicyJobs(writer, request, parts)
	default:
		writePolicyJSON(writer, http.StatusNotFound, map[string]any{"error": "not found"})
	}
}

func writePolicyJSON(writer http.ResponseWriter, status int, data any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(data)
}

type policyDeviceContext struct {
	device     config.DeviceConfig
	repository *store.PolicyRepository
}

func (s *Server) resolvePolicyDevice(writer http.ResponseWriter, request *http.Request) (policyDeviceContext, bool) {
	deviceID := strings.TrimSpace(request.URL.Query().Get("device"))
	if deviceID == "" {
		writePolicyJSON(writer, http.StatusBadRequest, map[string]any{"code": "device_required", "error": "a device query parameter is required"})
		return policyDeviceContext{}, false
	}
	device, found := s.configSnapshot().Device(deviceID)
	if !found {
		writePolicyJson(writer, http.StatusNotFound, map[string]any{"code": "device_not_found", "error": "device not found"})
		return policyDeviceContext{}, false
	}
	var repo *store.PolicyRepository
	child, err := s.store.OpenDevice(deviceID)
	if err != nil {
		writePolicyJson(writer, http.StatusServiceUnavailable, map[string]any{"code": "policy_storage_unavailable", "error": "device storage unavailable"})
		return policyDeviceContext{}, false
	}
	repo = child.PolicyRepository()
	return policyDeviceContext{device: device, repository: repo}, true
}

func (s *Server) policySetupState(device config.DeviceConfig) string {
	if s.policy == nil || !s.policy.Enabled() {
		return "manager_unavailable"
	}
	if s.policy.ApplierFor(device.ID) == nil {
		return "runtime_unavailable"
	}
	return "ready"
}

// ---- Overview ----

func (s *Server) servePolicyOverview(writer http.ResponseWriter, request *http.Request) {
	device, ok := s.resolvePolicyDevice(writer, request)
	if !ok {
		return
	}
	ctx := request.Context()
	egresses, _ := device.repository.ListEgresses(ctx)
	if egresses == nil {
		egresses = []policyv2.Egress{}
	}
	sources, _ := device.repository.ListSources(ctx, "")
	if sources == nil {
		sources = []policyv2.Source{}
	}
	state, _ := device.repository.GetDeviceState(ctx)
	setupState := s.policySetupState(device.device)
	account := map[string]any{"username": device.device.RouterOS.Username, "permission": "unknown", "writeAccess": false}
	if setupState == "ready" {
		access, accessErr := s.policyAccountAccess(ctx, device.device)
		if accessErr != nil {
			account["error"] = accessErr.Error()
			setupState = "runtime_unavailable"
		} else {
			account["group"] = access.Group
			account["policies"] = access.Policies
			account["writeAccess"] = access.Writable
			if access.Writable {
				account["permission"] = "write"
			} else {
				account["permission"] = "read_only"
				setupState = "write_access_required"
			}
		}
	}

	// Attach source info to egresses
	egressSources := make(map[string][]map[string]any)
	for _, src := range sources {
		entry := map[string]any{"id": src.ID, "name": src.Name, "type": src.Type, "enabled": src.Enabled}
		if src.ActiveVersionID != "" {
			entry["ruleCount"] = src.Counts["valid"]
		} else {
			entry["ruleCount"] = 0
		}
		egressSources[src.EgressID] = append(egressSources[src.EgressID], entry)
	}
	egressResult := make([]map[string]any, 0, len(egresses))
	for _, eg := range egresses {
		srcs := egressSources[eg.ID]
		if srcs == nil {
			srcs = []map[string]any{}
		}
		entry := map[string]any{
			"id": eg.ID, "name": eg.Name, "priority": eg.Priority,
			"listMode": eg.ListMode, "listName": eg.ListName,
			"dnsUpstream": eg.DNSUpstream, "fakeAlias": eg.FakeAlias,
			"failureMode":  eg.FailureMode,
			"routerOutput": eg.RouterOutput, "enabled": eg.Enabled,
			"applied": eg.Applied, "pendingDeletion": eg.PendingDeletion,
			"revision": eg.Revision,
			"families": eg.Families, "sources": srcs,
		}
		egressResult = append(egressResult, entry)
	}

	writePolicyJSON(writer, http.StatusOK, map[string]any{
		"device":         map[string]any{"id": device.device.ID, "name": device.device.Name, "enabled": device.device.Enabled},
		"account":        account,
		"setup":          map[string]any{"state": setupState},
		"trafficIngress": decodeJSONObject(state.TrafficIngress),
		"egresses":       egressResult,
		"sources":        sources,
		"activeJobs":     activePolicyJobs(state.Job),
		"pendingJobs":    []any{},
		"health":         policyV2Health(state.Job),
		"drift":          map[string]any{"state": "", "items": []any{}},
		"applied":        state.Applied(),
	})
}

// ---- Discovery ----

func (s *Server) servePolicyDiscovery(writer http.ResponseWriter, request *http.Request) {
	device, ok := s.resolvePolicyDevice(writer, request)
	if !ok {
		return
	}
	if s.policySetupState(device.device) != "ready" {
		writePolicyJSON(writer, http.StatusOK, map[string]any{"available": false, "reason": "runtime not ready", "wans": []any{}, "trafficIngress": []any{}})
		return
	}
	if s.policy == nil {
		writePolicyJson(writer, http.StatusConflict, map[string]any{"code": "runtime_unavailable", "error": "policy discovery is unavailable"})
		return
	}
	applier := s.policy.ApplierFor(device.device.ID)
	if applier == nil || applier.Reader == nil {
		writePolicyJSON(writer, http.StatusOK, map[string]any{"available": false, "reason": "scanner not configured", "wans": []any{}, "trafficIngress": []any{}})
		return
	}
	scanner := policyv2.NewScanner(applier.Reader)
	result, err := scanner.Scan(request.Context(), device.device.ID)
	if err != nil {
		writePolicyJSON(writer, http.StatusOK, map[string]any{"available": false, "reason": err.Error(), "wans": []any{}, "trafficIngress": []any{}})
		return
	}
	writePolicyJSON(writer, http.StatusOK, result)
}

// ---- Traffic ingress ----

func (s *Server) servePolicyTrafficIngress(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPut {
		writePolicyJson(writer, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	device, ok := s.resolvePolicyDevice(writer, request)
	if !ok {
		return
	}
	var payload struct {
		TrafficIngress json.RawMessage `json:"trafficIngress"`
		Scope          json.RawMessage `json:"scope"`
	}
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		writePolicyJson(writer, http.StatusBadRequest, map[string]any{"code": "invalid_body", "error": err.Error()})
		return
	}
	if len(payload.TrafficIngress) == 0 {
		payload.TrafficIngress = payload.Scope
	}
	if len(payload.TrafficIngress) == 0 {
		writePolicyJson(writer, http.StatusBadRequest, map[string]any{"code": "invalid_traffic_ingress", "error": "trafficIngress is required"})
		return
	}
	scope, err := policyv2.ParseTrafficIngressScope(payload.TrafficIngress)
	if err != nil {
		writePolicyJson(writer, http.StatusBadRequest, map[string]any{"code": "invalid_traffic_ingress", "error": err.Error()})
		return
	}
	applier := s.policy.ApplierFor(device.device.ID)
	if applier == nil || applier.Reader == nil {
		writePolicyJson(writer, http.StatusConflict, map[string]any{"code": "runtime_unavailable", "error": "policy discovery is unavailable"})
		return
	}
	discovery, err := policyv2.NewScanner(applier.Reader).Scan(request.Context(), device.device.ID)
	if err != nil {
		writePolicyJson(writer, http.StatusServiceUnavailable, map[string]any{"code": "policy_discovery_unavailable", "error": err.Error()})
		return
	}
	scope, err = policyv2.NormalizeTrafficIngressScope(scope, discovery.TrafficIngress)
	if err != nil {
		writePolicyJson(writer, http.StatusBadRequest, map[string]any{"code": "invalid_traffic_ingress", "error": err.Error()})
		return
	}
	scopeJSON, err := policyv2.MarshalTrafficIngressScope(scope)
	if err != nil {
		writePolicyJson(writer, http.StatusBadRequest, map[string]any{"code": "invalid_traffic_ingress", "error": err.Error()})
		return
	}
	state, err := device.repository.SaveTrafficIngress(request.Context(), scopeJSON)
	if err != nil {
		writePolicyJson(writer, http.StatusServiceUnavailable, map[string]any{"code": "save_failed", "error": err.Error()})
		return
	}
	writePolicyJson(writer, http.StatusOK, map[string]any{"trafficIngress": decodeJSONObject(state.TrafficIngress)})
}

// ---- Egresses ----

func (s *Server) servePolicyEgress(writer http.ResponseWriter, request *http.Request, parts []string) {
	if len(parts) == 1 {
		if request.Method != http.MethodPost {
			writePolicyJson(writer, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		s.savePolicyEgress(writer, request, "")
		return
	}
	id := parts[1]
	if len(parts) == 3 && parts[2] == "state" {
		if request.Method != http.MethodPost {
			writePolicyJson(writer, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		s.setPolicyEgressState(writer, request, id)
		return
	}
	switch request.Method {
	case http.MethodGet:
		s.getPolicyEgress(writer, request, id)
	case http.MethodPut:
		s.savePolicyEgress(writer, request, id)
	case http.MethodDelete:
		s.deletePolicyEgress(writer, request, id)
	default:
		writePolicyJson(writer, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
	}
}

func (s *Server) setPolicyEgressState(writer http.ResponseWriter, request *http.Request, id string) {
	device, ok := s.resolvePolicyDevice(writer, request)
	if !ok {
		return
	}
	if !s.requirePolicyWriteAccess(writer, request, device.device) {
		return
	}
	var payload struct {
		Enabled  *bool `json:"enabled"`
		Revision int64 `json:"revision"`
	}
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		writePolicyJson(writer, http.StatusBadRequest, map[string]any{"code": "invalid_body", "error": err.Error()})
		return
	}
	if payload.Enabled == nil || payload.Revision < 0 {
		writePolicyJson(writer, http.StatusBadRequest, map[string]any{"code": "invalid_egress_state", "error": "enabled and a non-negative revision are required"})
		return
	}
	egress, err := device.repository.GetEgress(request.Context(), id)
	if err != nil {
		writePolicyJson(writer, http.StatusNotFound, map[string]any{"code": "egress_not_found", "error": "egress not found"})
		return
	}
	if egress.PendingDeletion {
		writePolicyJson(writer, http.StatusConflict, map[string]any{"code": "egress_pending_deletion", "error": "egress is pending deletion"})
		return
	}
	egress.Revision = payload.Revision
	egress.Enabled = *payload.Enabled
	if _, err := device.repository.SaveEgress(request.Context(), egress); err != nil {
		status, code := http.StatusServiceUnavailable, "save_failed"
		if errors.Is(err, policyv2.ErrRevisionStale) {
			status, code = http.StatusConflict, "revision_stale"
		}
		writePolicyJson(writer, status, map[string]any{"code": code, "error": err.Error()})
		return
	}
	job, err := s.policy.GenerateAndApply(request.Context(), device.device.ID, "egress-state")
	if err != nil {
		status, code := policyPlanApplyError(err)
		writePolicyJson(writer, status, map[string]any{"code": code, "error": err.Error()})
		return
	}
	writePolicyJson(writer, http.StatusAccepted, map[string]any{"job": job, "jobId": job.ID})
}

func (s *Server) getPolicyEgress(writer http.ResponseWriter, request *http.Request, id string) {
	device, ok := s.resolvePolicyDevice(writer, request)
	if !ok {
		return
	}
	eg, err := device.repository.GetEgress(request.Context(), id)
	if err != nil {
		writePolicyJson(writer, http.StatusNotFound, map[string]any{"code": "egress_not_found", "error": "egress not found"})
		return
	}
	writePolicyJson(writer, http.StatusOK, eg)
}

func (s *Server) savePolicyEgress(writer http.ResponseWriter, request *http.Request, pathID string) {
	device, ok := s.resolvePolicyDevice(writer, request)
	if !ok {
		return
	}
	var eg policyv2.Egress
	if err := json.NewDecoder(request.Body).Decode(&eg); err != nil {
		writePolicyJson(writer, http.StatusBadRequest, map[string]any{"code": "invalid_body", "error": err.Error()})
		return
	}
	if pathID != "" {
		eg.ID = pathID
	}
	if eg.ID == "" {
		eg.ID = uuid.NewString()
	}
	if strings.TrimSpace(eg.Name) == "" {
		writePolicyJson(writer, http.StatusBadRequest, map[string]any{"code": "invalid_egress", "error": "name is required"})
		return
	}
	if eg.ListMode == "" {
		eg.ListMode = policyv2.ListModeDedicated
	}
	if eg.FailureMode == "" {
		eg.FailureMode = "strict"
	}
	if err := normalizePolicyV2Egress(request.Context(), device.repository, &eg); err != nil {
		writePolicyJson(writer, http.StatusUnprocessableEntity, map[string]any{"code": "invalid_egress", "error": err.Error()})
		return
	}
	eg, err := device.repository.SaveEgress(request.Context(), eg)
	if err != nil {
		status, code := http.StatusServiceUnavailable, "save_failed"
		if errors.Is(err, policyv2.ErrRevisionStale) {
			status, code = http.StatusConflict, "revision_stale"
		}
		writePolicyJson(writer, status, map[string]any{"code": code, "error": err.Error()})
		return
	}
	writePolicyJson(writer, http.StatusOK, eg)
}

func normalizePolicyV2Egress(ctx context.Context, repository *store.PolicyRepository, egress *policyv2.Egress) error {
	if egress == nil {
		return errors.New("egress is required")
	}
	if current, err := repository.GetEgress(ctx, egress.ID); err == nil {
		if strings.TrimSpace(egress.FakeAlias) == "" {
			egress.FakeAlias = current.FakeAlias
		}
		if strings.TrimSpace(egress.DNSUpstream) == "" {
			egress.DNSUpstream = current.DNSUpstream
		}
	}
	if egress.ListMode != policyv2.ListModeShared && egress.ListMode != policyv2.ListModeDedicated {
		return errors.New("listMode must be shared or dedicated")
	}
	if egress.ListMode == policyv2.ListModeShared && strings.TrimSpace(egress.ListName) == "" {
		egress.ListName = "manual_" + shortPolicyAPIHash(egress.ID, 10) + "_lab"
	}
	if egress.FailureMode != "strict" && egress.FailureMode != "fallback" && egress.FailureMode != "existing" {
		return errors.New("failureMode must be strict, fallback, or existing")
	}
	seen := make(map[policyv2.AddressFamily]bool)
	managerID, err := repository.ManagerInstanceID(ctx)
	if err != nil {
		return err
	}
	hasIPv4, hasIPv6 := false, false
	for index := range egress.Families {
		family := &egress.Families[index]
		if family.Family != policyv2.FamilyIPv4 && family.Family != policyv2.FamilyIPv6 {
			return fmt.Errorf("unsupported address family %q", family.Family)
		}
		if seen[family.Family] {
			return fmt.Errorf("address family %q appears more than once", family.Family)
		}
		seen[family.Family] = true
		if !family.Enabled {
			continue
		}
		hasIPv4 = hasIPv4 || family.Family == policyv2.FamilyIPv4
		hasIPv6 = hasIPv6 || family.Family == policyv2.FamilyIPv6
		if family.WANSource == "next-hop" && strings.TrimSpace(family.Gateway) == "" {
			return fmt.Errorf("%s next-hop mode requires a gateway", family.Family)
		}
		if family.WANSource != "next-hop" && strings.TrimSpace(family.WANInterface) == "" && strings.TrimSpace(family.Gateway) == "" {
			return fmt.Errorf("%s requires a WAN interface or gateway", family.Family)
		}
		if family.RouteTable == "" {
			family.RouteTable = policyv2.DefaultRouteTable(managerID, repository.DeviceID(), egress.ID, family.Family)
		}
		if family.RouteMode != "" && family.RouteMode != "strict" && family.RouteMode != "fallback" {
			return fmt.Errorf("unsupported %s route mode %q", family.Family, family.RouteMode)
		}
		if family.NATMode != "" && family.NATMode != "none" && family.NATMode != "masquerade" {
			return fmt.Errorf("unsupported %s NAT mode %q", family.Family, family.NATMode)
		}
	}
	if !hasIPv4 && !hasIPv6 {
		return errors.New("at least one address family must be enabled")
	}
	if strings.TrimSpace(egress.DNSUpstream) == "" {
		if !hasIPv4 {
			return errors.New("an IPv6-only egress requires an explicit IPv6 DNS upstream")
		}
		egress.DNSUpstream = "1.1.1.1"
	}
	upstream, err := netip.ParseAddr(strings.TrimSpace(egress.DNSUpstream))
	if err != nil {
		return errors.New("dnsUpstream must be one IP address")
	}
	if upstream.Is4() && !hasIPv4 || !upstream.Is4() && !hasIPv6 {
		return errors.New("dnsUpstream address family must be enabled on this egress")
	}
	used := make([]string, 0)
	others, err := repository.ListEgresses(ctx)
	if err != nil {
		return err
	}
	for _, other := range others {
		if other.ID != egress.ID && other.FakeAlias != "" {
			used = append(used, other.FakeAlias)
		}
	}
	aliasFamily := policyv2.FamilyIPv4
	if !upstream.Is4() {
		aliasFamily = policyv2.FamilyIPv6
	}
	alias, err := policyv2.AllocateFakeDNSAlias(policyv2.FakeAliasRequest{EgressID: egress.ID, Family: aliasFamily, PersistedAlias: strings.TrimSpace(egress.FakeAlias), UsedAliases: used})
	if err != nil {
		return err
	}
	egress.FakeAlias = alias
	return nil
}

func shortPolicyAPIHash(value string, length int) string {
	digest := sha256.Sum256([]byte(value))
	encoded := hex.EncodeToString(digest[:])
	if length > len(encoded) {
		return encoded
	}
	return encoded[:length]
}

func (s *Server) deletePolicyEgress(writer http.ResponseWriter, request *http.Request, id string) {
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
	egress, err := device.repository.GetEgress(request.Context(), id)
	if err != nil {
		writePolicyJson(writer, http.StatusNotFound, map[string]any{"code": "egress_not_found", "error": "egress not found"})
		return
	}
	if err := device.repository.DeleteEgress(request.Context(), id, revision); err != nil {
		writePolicyJson(writer, http.StatusConflict, map[string]any{"code": "revision_stale", "error": err.Error()})
		return
	}
	if !egress.Applied {
		writePolicyJson(writer, http.StatusOK, map[string]any{"deleted": true})
		return
	}
	job, err := s.policy.GenerateAndApply(request.Context(), device.device.ID, "egress-delete")
	if err != nil {
		status, code := policyPlanApplyError(err)
		writePolicyJson(writer, status, map[string]any{"code": code, "error": err.Error(), "pendingDeletion": true})
		return
	}
	writePolicyJson(writer, http.StatusAccepted, map[string]any{"deleted": false, "pendingDeletion": true, "job": job, "jobId": job.ID})
}

// ---- Sources ----

func (s *Server) servePolicySource(writer http.ResponseWriter, request *http.Request, parts []string) {
	if len(parts) == 1 {
		if request.Method != http.MethodPost {
			writePolicyJson(writer, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		s.savePolicySource(writer, request, "")
		return
	}
	id := parts[1]
	if len(parts) >= 3 {
		switch parts[2] {
		case "rules":
			if request.Method == http.MethodGet {
				s.servePolicySourceRules(writer, request, id)
				return
			}
		case "refresh":
			if request.Method == http.MethodPost {
				s.servePolicySourceRefresh(writer, request, id)
				return
			}
		}
	}
	switch request.Method {
	case http.MethodGet:
		s.getPolicySource(writer, request, id)
	case http.MethodPut:
		s.savePolicySource(writer, request, id)
	case http.MethodDelete:
		s.deletePolicySource(writer, request, id)
	default:
		writePolicyJson(writer, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
	}
}

func (s *Server) getPolicySource(writer http.ResponseWriter, request *http.Request, id string) {
	device, ok := s.resolvePolicyDevice(writer, request)
	if !ok {
		return
	}
	src, err := device.repository.GetSource(request.Context(), id)
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, policyv2.ErrSourceNotFound) {
		writePolicyJson(writer, http.StatusNotFound, map[string]any{"code": "source_not_found", "error": "source not found"})
		return
	}
	if err != nil {
		writePolicyJson(writer, http.StatusServiceUnavailable, map[string]any{"code": "error", "error": err.Error()})
		return
	}
	writePolicyJson(writer, http.StatusOK, src)
}

func (s *Server) savePolicySource(writer http.ResponseWriter, request *http.Request, pathID string) {
	device, ok := s.resolvePolicyDevice(writer, request)
	if !ok {
		return
	}
	var payload struct {
		policyv2.Source
		PreviewID string `json:"previewId"`
	}
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		writePolicyJson(writer, http.StatusBadRequest, map[string]any{"code": "invalid_body", "error": err.Error()})
		return
	}
	src := payload.Source
	if pathID != "" {
		src.ID = pathID
	}
	if src.ID == "" {
		src.ID = uuid.NewString()
	}
	if strings.TrimSpace(src.Name) == "" {
		writePolicyJson(writer, http.StatusBadRequest, map[string]any{"code": "invalid_source", "error": "name is required"})
		return
	}
	if src.Type != "url" && src.Type != "upload" {
		writePolicyJson(writer, http.StatusUnprocessableEntity, map[string]any{"code": "invalid_source", "error": "type must be url or upload"})
		return
	}
	var current policyv2.Source
	if pathID != "" {
		var err error
		current, err = device.repository.GetSource(request.Context(), pathID)
		if err != nil {
			writePolicyJson(writer, http.StatusNotFound, map[string]any{"code": "source_not_found", "error": "source not found"})
			return
		}
	}
	if src.Type == "url" {
		interval, valid := policySourceSchedule(src.Schedule)
		if !valid {
			writePolicyJson(writer, http.StatusUnprocessableEntity, map[string]any{"code": "invalid_source", "error": "unsupported URL refresh schedule"})
			return
		}
		if src.Schedule == "" {
			src.Schedule = "24h"
		}
		if pathID == "" || current.Schedule != src.Schedule || current.NextRunAt.IsZero() {
			src.NextRunAt = time.Now().UTC().Add(interval)
		}
	} else {
		src.Schedule = "manual"
		src.NextRunAt = time.Unix(0, 0).UTC()
	}
	contentChanged := pathID == "" || payload.PreviewID != ""
	if pathID != "" {
		contentChanged = current.Type != src.Type || current.URL != src.URL
		contentChanged = contentChanged || payload.PreviewID != ""
	}
	var preview policyPreviewEntry
	if contentChanged {
		var found bool
		preview, found = s.policyPreview(payload.PreviewID, device.device.ID)
		if !found || preview.SourceType != src.Type {
			writePolicyJson(writer, http.StatusUnprocessableEntity, map[string]any{"code": "invalid_source_preview", "error": "a valid source preview is required"})
			return
		}
		if src.Type == "url" {
			src.URL = preview.URL
			src.ETag = preview.ETag
			src.LastModified = preview.LastModified
		}
	}
	src, err := device.repository.SaveSource(request.Context(), src)
	if err != nil {
		writePolicyJson(writer, http.StatusServiceUnavailable, map[string]any{"code": "save_failed", "error": err.Error()})
		return
	}
	if contentChanged {
		versionID := uuid.NewString()
		legacyVersion, legacyRules, err := preview.Content.PendingVersion(device.device.ID, src.ID, versionID)
		if err != nil {
			writePolicyJson(writer, http.StatusBadRequest, map[string]any{"code": "invalid_source_preview", "error": err.Error()})
			return
		}
		version, rules := policyV2SourceContent(legacyVersion, legacyRules)
		if err := device.repository.SavePendingSourceVersion(request.Context(), version, rules); err != nil {
			writePolicyJson(writer, http.StatusServiceUnavailable, map[string]any{"code": "save_failed", "error": err.Error()})
			return
		}
		s.discardPolicyPreview(payload.PreviewID)
		src, err = device.repository.GetSource(request.Context(), src.ID)
		if err != nil {
			writePolicyJson(writer, http.StatusServiceUnavailable, map[string]any{"code": "load_failed", "error": err.Error()})
			return
		}
	}
	writePolicyJson(writer, http.StatusOK, src)
}

func policySourceSchedule(value string) (time.Duration, bool) {
	switch strings.TrimSpace(value) {
	case "1h":
		return time.Hour, true
	case "6h":
		return 6 * time.Hour, true
	case "12h":
		return 12 * time.Hour, true
	case "", "24h":
		return 24 * time.Hour, true
	case "7d":
		return 7 * 24 * time.Hour, true
	case "30d":
		return 30 * 24 * time.Hour, true
	case "manual":
		return 0, true
	default:
		return 0, false
	}
}

func (s *Server) deletePolicySource(writer http.ResponseWriter, request *http.Request, id string) {
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
	source, err := device.repository.GetSource(request.Context(), id)
	if err != nil {
		writePolicyJson(writer, http.StatusNotFound, map[string]any{"code": "source_not_found", "error": "source not found"})
		return
	}
	if err := device.repository.DeleteSource(request.Context(), id, revision); err != nil {
		writePolicyJson(writer, http.StatusConflict, map[string]any{"code": "revision_stale", "error": err.Error()})
		return
	}
	if !source.Applied {
		writePolicyJson(writer, http.StatusOK, map[string]any{"deleted": true})
		return
	}
	job, err := s.policy.GenerateAndApply(request.Context(), device.device.ID, "source-delete")
	if err != nil {
		status, code := policyPlanApplyError(err)
		writePolicyJson(writer, status, map[string]any{"code": code, "error": err.Error(), "pendingDeletion": true})
		return
	}
	writePolicyJson(writer, http.StatusAccepted, map[string]any{"deleted": false, "pendingDeletion": true, "job": job, "jobId": job.ID})
}

func (s *Server) servePolicySourceRules(writer http.ResponseWriter, request *http.Request, id string) {
	device, ok := s.resolvePolicyDevice(writer, request)
	if !ok {
		return
	}
	src, err := device.repository.GetSource(request.Context(), id)
	if err != nil {
		writePolicyJson(writer, http.StatusNotFound, map[string]any{"code": "source_not_found", "error": "source not found"})
		return
	}
	if src.ActiveVersionID == "" {
		writePolicyJson(writer, http.StatusOK, map[string]any{"sourceId": id, "rules": []any{}, "nextCursor": ""})
		return
	}
	limit := 100
	if l := request.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 && v <= 1000 {
			limit = v
		}
	}
	query := policyv2.RuleQuery{Limit: limit, Query: strings.TrimSpace(request.URL.Query().Get("query")), RuleType: strings.TrimSpace(request.URL.Query().Get("type"))}
	if cursor := strings.TrimSpace(request.URL.Query().Get("cursor")); cursor != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(cursor)
		parts := strings.SplitN(string(decoded), "\x00", 2)
		if err != nil || len(parts) != 2 {
			writePolicyJson(writer, http.StatusBadRequest, map[string]any{"code": "invalid_cursor", "error": "rules cursor is invalid"})
			return
		}
		query.AfterType, query.AfterDomain = parts[0], parts[1]
	}
	rules, hasNext, err := device.repository.ListSourceRules(request.Context(), src.ActiveVersionID, query)
	if err != nil {
		writePolicyJson(writer, http.StatusServiceUnavailable, map[string]any{"code": "rules_unavailable", "error": err.Error()})
		return
	}
	result := make([]map[string]any, 0, len(rules))
	for _, rule := range rules {
		result = append(result, map[string]any{"type": rule.RuleType, "domain": rule.Domain})
	}
	nextCursor := ""
	if hasNext && len(rules) > 0 {
		last := rules[len(rules)-1]
		nextCursor = base64.RawURLEncoding.EncodeToString([]byte(last.RuleType + "\x00" + last.Domain))
	}
	writePolicyJson(writer, http.StatusOK, map[string]any{"sourceId": id, "versionId": src.ActiveVersionID, "rules": result, "nextCursor": nextCursor})
}

func (s *Server) servePolicySourceRefresh(writer http.ResponseWriter, request *http.Request, id string) {
	device, ok := s.resolvePolicyDevice(writer, request)
	if !ok {
		return
	}
	src, err := device.repository.GetSource(request.Context(), id)
	if err != nil || src.Type != "url" || src.URL == "" {
		writePolicyJson(writer, http.StatusConflict, map[string]any{"code": "source_unavailable", "error": "source cannot be refreshed"})
		return
	}
	fetcher := s.sourceFetcher
	if fetcher == nil {
		fetcher = policy.NewSourceFetcher(policy.FetcherOptions{})
	}
	result, err := fetcher.Preview(request.Context(), src.URL, policy.FetchOptions{ETag: src.ETag, LastModified: src.LastModified})
	if err != nil {
		writePolicyJson(writer, http.StatusBadGateway, map[string]any{"code": "fetch_failed", "error": err.Error()})
		return
	}
	if result.NotModified {
		writePolicyJson(writer, http.StatusOK, map[string]any{"notModified": true})
		return
	}
	versionID := uuid.NewString()
	legacyVersion, legacyRules, err := result.PendingVersion(device.device.ID, src.ID, versionID)
	if err != nil {
		writePolicyJson(writer, http.StatusBadRequest, map[string]any{"code": "parse_failed", "error": err.Error()})
		return
	}
	version, rules := policyV2SourceContent(legacyVersion, legacyRules)
	if err := device.repository.SavePendingSourceVersion(request.Context(), version, rules); err != nil {
		writePolicyJson(writer, http.StatusServiceUnavailable, map[string]any{"code": "save_failed", "error": err.Error()})
		return
	}
	writePolicyJson(writer, http.StatusOK, map[string]any{"sourceId": id, "versionId": versionID, "ruleCount": len(rules)})
}

// ---- Plans and apply ----

func (s *Server) servePolicyPlans(writer http.ResponseWriter, request *http.Request, parts []string) {
	if len(parts) == 1 {
		if request.Method != http.MethodPost {
			writePolicyJson(writer, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		device, ok := s.resolvePolicyDevice(writer, request)
		if !ok {
			return
		}
		if !s.requirePolicyWriteAccess(writer, request, device.device) {
			return
		}
		if s.policySetupState(device.device) != "ready" {
			writePolicyJson(writer, http.StatusServiceUnavailable, map[string]any{"code": "runtime_unavailable", "error": "policy runtime is not ready"})
			return
		}
		var payload struct {
			Kind string `json:"kind"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			writePolicyJson(writer, http.StatusBadRequest, map[string]any{"code": "invalid_body", "error": err.Error()})
			return
		}
		envelope, err := s.policy.GeneratePlan(request.Context(), device.device.ID, payload.Kind)
		if err != nil {
			writePolicyJson(writer, http.StatusUnprocessableEntity, map[string]any{"code": "plan_generation_failed", "error": err.Error(), "details": map[string]any{}})
			return
		}
		writePolicyJson(writer, http.StatusCreated, envelope)
		return
	}
	if len(parts) != 3 || parts[2] != "apply" || request.Method != http.MethodPost {
		writePolicyJson(writer, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	device, ok := s.resolvePolicyDevice(writer, request)
	if !ok {
		return
	}
	if !s.requirePolicyWriteAccess(writer, request, device.device) {
		return
	}
	var payload struct {
		Acknowledgements []string `json:"acknowledgements"`
	}
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		writePolicyJson(writer, http.StatusBadRequest, map[string]any{"code": "invalid_body", "error": err.Error()})
		return
	}
	job, err := s.policy.ApplyPlan(request.Context(), device.device.ID, parts[1])
	if err != nil {
		status, code := policyPlanApplyError(err)
		writePolicyJson(writer, status, map[string]any{"code": code, "error": err.Error(), "details": map[string]any{}})
		return
	}
	writePolicyJson(writer, http.StatusAccepted, map[string]any{"job": job, "jobId": job.ID})
}

func (s *Server) servePolicyJobs(writer http.ResponseWriter, request *http.Request, parts []string) {
	if len(parts) != 2 || request.Method != http.MethodGet {
		writePolicyJson(writer, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	device, ok := s.resolvePolicyDevice(writer, request)
	if !ok {
		return
	}
	job, err := s.policy.GetJob(request.Context(), device.device.ID, parts[1])
	if errors.Is(err, policyv2.ErrJobNotFound) {
		writePolicyJson(writer, http.StatusNotFound, map[string]any{"code": "job_not_found", "error": "job not found"})
		return
	}
	if err != nil {
		writePolicyJson(writer, http.StatusServiceUnavailable, map[string]any{"code": "job_unavailable", "error": err.Error()})
		return
	}
	writePolicyJson(writer, http.StatusOK, map[string]any{"job": job})
}

func policyPlanApplyError(err error) (int, string) {
	switch {
	case errors.Is(err, policyv2.ErrPlanNotFound):
		return http.StatusNotFound, "plan_not_found"
	case errors.Is(err, policyv2.ErrPlanExpired):
		return http.StatusConflict, "plan_expired"
	case errors.Is(err, policyv2.ErrPlanStale):
		return http.StatusConflict, "stale_plan"
	case errors.Is(err, policyv2.ErrPlanBlocked):
		return http.StatusUnprocessableEntity, "plan_blocked"
	case errors.Is(err, policyv2.ErrDeviceBusy):
		return http.StatusConflict, "job_conflict"
	default:
		return http.StatusInternalServerError, "apply_failed"
	}
}

func (s *Server) requirePolicyWriteAccess(writer http.ResponseWriter, request *http.Request, device config.DeviceConfig) bool {
	access, err := s.policyAccountAccess(request.Context(), device)
	if err != nil {
		writePolicyJson(writer, http.StatusServiceUnavailable, map[string]any{"code": "account_permission_unavailable", "error": "无法读取 RouterOS 账号权限，请在设备管理中检查账号"})
		return false
	}
	if !access.Writable {
		writePolicyJson(writer, http.StatusConflict, map[string]any{"code": "routeros_write_required", "error": "当前 RouterOS 账号只有只读权限，请在设备管理中更换为具备写入权限的账号"})
		return false
	}
	return true
}

func (s *Server) policyAccountAccess(ctx context.Context, device config.DeviceConfig) (routeros.AccountAccess, error) {
	if s.policy != nil {
		if applier := s.policy.ApplierFor(device.ID); applier != nil {
			if reader, ok := applier.Reader.(interface {
				AccountAccess(context.Context, string) (routeros.AccountAccess, error)
			}); ok {
				return reader.AccountAccess(ctx, device.RouterOS.Username)
			}
		}
	}
	return routeros.NewClient(device.RouterOS.BaseURL, device.RouterOS.Username, device.RouterOS.Password).AccountAccess(ctx, device.RouterOS.Username)
}

// ---- URL Preview ----

func (s *Server) servePolicyURLPreview(writer http.ResponseWriter, request *http.Request) {
	device, ok := s.resolvePolicyDevice(writer, request)
	if !ok {
		return
	}
	var payload struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		writePolicyJson(writer, http.StatusBadRequest, map[string]any{"code": "invalid_body", "error": err.Error()})
		return
	}
	normalized, err := policy.NormalizeSourceURL(payload.URL)
	if err != nil {
		writePolicyJson(writer, http.StatusBadRequest, map[string]any{"code": "invalid_url", "error": err.Error()})
		return
	}
	fetcher := s.sourceFetcher
	if fetcher == nil {
		fetcher = policy.NewSourceFetcher(policy.FetcherOptions{})
	}
	preview, err := fetcher.Preview(request.Context(), normalized, policy.FetchOptions{})
	if err != nil {
		writePolicyJson(writer, http.StatusBadGateway, map[string]any{"code": "fetch_failed", "error": err.Error()})
		return
	}
	rules := make([]map[string]any, 0)
	for i, r := range preview.Rules {
		if i >= 100 {
			break
		}
		rules = append(rules, map[string]any{"type": string(r.Type), "domain": r.Domain})
	}
	previewID := ""
	if !preview.NotModified {
		previewID = s.savePolicyPreview(policyPreviewEntry{
			DeviceID: device.device.ID, SourceType: "url", URL: preview.URL,
			ETag: preview.ETag, LastModified: preview.LastModified, Content: preview.PreparedSourceContent,
		})
	}
	writePolicyJson(writer, http.StatusOK, map[string]any{
		"previewId": previewID,
		"url":       preview.URL, "statusCode": preview.StatusCode,
		"etag": preview.ETag, "lastModified": preview.LastModified,
		"contentType": preview.ContentType, "sha256": preview.SHA256,
		"notModified": preview.NotModified, "size": preview.Size,
		"validRules": len(preview.Rules), "ignored": preview.Ignored,
		"errorSamples": preview.ErrorSamples,
		"rules":        rules,
	})
}

// ---- Upload Preview ----

func (s *Server) servePolicyUploadPreview(writer http.ResponseWriter, request *http.Request) {
	device, ok := s.resolvePolicyDevice(writer, request)
	if !ok {
		return
	}
	uploadService := policy.NewUploadService(filepath.Join(strings.TrimSpace(s.configSnapshot().DataDir), "tmp"))
	preview, err := uploadService.PreviewMultipart(request.Context(), request.Header.Get("Content-Type"), request.Body)
	if err != nil {
		writePolicyJson(writer, http.StatusBadRequest, map[string]any{"code": "upload_failed", "error": err.Error()})
		return
	}
	rules := make([]map[string]any, 0)
	for i, r := range preview.Rules {
		if i >= 100 {
			break
		}
		rules = append(rules, map[string]any{"type": string(r.Type), "domain": r.Domain})
	}
	previewID := s.savePolicyPreview(policyPreviewEntry{
		DeviceID: device.device.ID, SourceType: "upload", Content: preview.PreparedSourceContent,
	})
	writePolicyJson(writer, http.StatusOK, map[string]any{
		"previewId": previewID, "filename": preview.Filename, "sha256": preview.SHA256, "size": preview.Size,
		"validRules": len(preview.Rules), "ignored": preview.Ignored,
		"errorSamples": preview.ErrorSamples,
		"rules":        rules,
	})
}

// ---- Helpers ----

func queryRevision(request *http.Request) int64 {
	value := strings.TrimSpace(request.URL.Query().Get("revision"))
	if value == "" {
		return 0
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < 0 {
		return -1
	}
	return parsed
}

func decodeJSONObject(payload []byte) map[string]any {
	result := make(map[string]any)
	if len(payload) == 0 {
		return result
	}
	_ = json.Unmarshal(payload, &result)
	return result
}

func activePolicyJobs(job policyv2.ApplyJob) []policyv2.ApplyJob {
	switch job.State {
	case "queued", "staging", "verifying":
		return []policyv2.ApplyJob{job}
	default:
		return []policyv2.ApplyJob{}
	}
}

func policyV2Health(job policyv2.ApplyJob) map[string]any {
	state := "ok"
	if job.State == "failed" {
		state = "degraded"
	}
	return map[string]any{
		"state": state, "driftState": "", "mutationPaused": false,
		"manualInterventionRequired": false, "pauseReason": job.Error, "pauseJobId": job.ID,
	}
}

func policyV2SourceContent(version policy.SourceVersion, rules []policy.SourceRule) (policyv2.SourceVersion, []policyv2.SourceRule) {
	counts := map[string]int{}
	diff := map[string]any{}
	_ = json.Unmarshal(version.CountsJSON, &counts)
	_ = json.Unmarshal(version.DiffSummaryJSON, &diff)
	converted := policyv2.SourceVersion{
		ID: version.ID, SourceID: version.SourceID, SHA256: version.SHA256,
		CompressedYAML: append([]byte(nil), version.CompressedYAML...), State: "pending",
		Error: version.Error, HTTPStatus: version.HTTPStatus, Counts: counts, Diff: diff,
		CreatedAt: version.CreatedAt,
	}
	convertedRules := make([]policyv2.SourceRule, len(rules))
	for index, rule := range rules {
		convertedRules[index] = policyv2.SourceRule{VersionID: rule.VersionID, RuleType: rule.RuleType, Domain: rule.Domain}
	}
	return converted, convertedRules
}

// writePolicyJson is a convenience wrapper.
func writePolicyJson(writer http.ResponseWriter, status int, data any) {
	writePolicyJSON(writer, status, data)
}
