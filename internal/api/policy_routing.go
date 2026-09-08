package api

import (
	"context"
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

	"rosboard/internal/applicationpreset"
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
	case "rules":
		s.servePolicyRoutingRules(writer, request, parts)
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
	device           config.DeviceConfig
	repository       *store.PolicyRepository
	accessRepository *store.AccessRepository
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
	return policyDeviceContext{device: device, repository: repo, accessRepository: child.AccessRepository()}, true
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
	routingRules, _ := device.repository.ListRoutingRules(ctx)
	if routingRules == nil {
		routingRules = []policyv2.RoutingRule{}
	}
	egresses, _ := device.repository.ListEgresses(ctx)
	if egresses == nil {
		egresses = []policyv2.Egress{}
	}
	targetLists, _ := device.repository.ListTargetLists(ctx)
	if targetLists == nil {
		targetLists = []policyv2.TargetList{}
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

	egressResult := make([]map[string]any, 0, len(egresses))
	for _, eg := range egresses {
		entry := map[string]any{
			"id": eg.ID, "name": eg.Name, "priority": eg.Priority,
			"listMode": eg.ListMode, "listName": eg.ListName,
			"dnsUpstream": eg.DNSUpstream, "fakeAlias": eg.FakeAlias,
			"failureMode":  eg.FailureMode,
			"routerOutput": eg.RouterOutput, "enabled": eg.Enabled,
			"applied": eg.Applied, "pendingDeletion": eg.PendingDeletion,
			"revision": eg.Revision,
			"families": eg.Families,
		}
		egressResult = append(egressResult, entry)
	}

	writePolicyJSON(writer, http.StatusOK, map[string]any{
		"device":         map[string]any{"id": device.device.ID, "name": device.device.Name, "enabled": device.device.Enabled},
		"account":        account,
		"setup":          map[string]any{"state": setupState},
		"trafficIngress": decodeJSONObject(state.TrafficIngress),
		"egresses":       egressResult,
		"targetLists":    targetLists,
		"rules":          routingRules,
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
	if !s.requirePolicyWriteAccess(writer, request, device.device) {
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
	if !s.requirePolicyWriteAccess(writer, request, device.device) {
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
	if eg.ListMode == "" {
		eg.ListMode = policyv2.ListModeShared
	}
	if eg.FailureMode == "" {
		eg.FailureMode = "strict"
	}
	var reader policyv2.PolicyReader
	if s.policy != nil {
		if applier := s.policy.ApplierFor(device.device.ID); applier != nil {
			reader = applier.Reader
		}
	}
	if err := normalizePolicyV2Egress(request.Context(), device.repository, reader, &eg); err != nil {
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

func normalizePolicyV2Egress(ctx context.Context, repository *store.PolicyRepository, reader policyv2.PolicyReader, egress *policyv2.Egress) error {
	if egress == nil {
		return errors.New("egress is required")
	}
	existingEgress := false
	if current, err := repository.GetEgress(ctx, egress.ID); err == nil {
		existingEgress = true
		egress.Name = current.Name
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
		if family.WANSource == "next-hop" {
			if !policyGatewayMatchesFamily(family.Gateway, family.Family) {
				return fmt.Errorf("%s next-hop gateway must be a valid %s IP", family.Family, family.Family)
			}
		} else if strings.TrimSpace(family.Gateway) == "" && strings.TrimSpace(family.WANInterface) != "" {
			if reader == nil {
				return fmt.Errorf("%s cannot determine the next-hop gateway; please fill it manually", family.Family)
			}
			resolution, resolveErr := policyv2.ResolveGateway(ctx, reader, *family)
			if resolveErr != nil {
				return fmt.Errorf("%s gateway discovery failed: %w", family.Family, resolveErr)
			}
			if !resolution.PointToPoint {
				switch len(resolution.Candidates) {
				case 1:
					family.Gateway = resolution.Gateway
				case 0:
					return fmt.Errorf("%s is not point-to-point and no next-hop gateway was found; please fill the gateway IP", family.Family)
				default:
					return fmt.Errorf("%s has multiple possible next-hop gateways; please fill one gateway IP", family.Family)
				}
			}
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
	if !existingEgress || strings.TrimSpace(egress.Name) == "" {
		egress.Name = policyv2.InternalEgressName(*egress, "")
	}
	if egress.ListMode == policyv2.ListModeShared {
		egress.ListName = policyv2.SharedListName(egress.Name)
	}
	used := make([]string, 0)
	others, err := repository.ListEgresses(ctx)
	if err != nil {
		return err
	}
	for _, other := range others {
		if other.ID == egress.ID {
			continue
		}
		otherMode := other.ListMode
		if otherMode == "" {
			otherMode = policyv2.ListModeShared
		}
		if egress.ListMode == policyv2.ListModeShared && otherMode == policyv2.ListModeShared {
			otherListName := strings.TrimSpace(other.ListName)
			if otherListName == "" {
				otherListName = policyv2.SharedListName(other.Name)
			}
			if strings.EqualFold(otherListName, egress.ListName) {
				return fmt.Errorf("shared address-list name %q is already in use", egress.ListName)
			}
		}
		if other.FakeAlias != "" {
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

func policyGatewayMatchesFamily(value string, family policyv2.AddressFamily) bool {
	value = strings.TrimSpace(value)
	if index := strings.IndexByte(value, '%'); index > 0 {
		value = value[:index]
	}
	address, err := netip.ParseAddr(value)
	if err != nil || address.IsUnspecified() {
		return false
	}
	return (family == policyv2.FamilyIPv4 && address.Is4()) || (family == policyv2.FamilyIPv6 && !address.Is4())
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

// ---- Plans and apply ----

type policyPlanPresetSelection struct {
	PresetID       string   `json:"presetId"`
	PreviewID      string   `json:"previewId"`
	RequestedKinds []string `json:"requestedKinds"`
}

type policyPlanProposalPayload struct {
	Egress           *policyv2.Egress              `json:"egress"`
	TrafficIngress   *policyv2.TrafficIngressScope `json:"trafficIngress"`
	RoutingRule      *policyv2.RoutingRule         `json:"routingRule"`
	PresetSelections []policyPlanPresetSelection   `json:"presetSelections"`
}

func (s *Server) preparePolicyPlanProposal(ctx context.Context, device policyDeviceContext, payload *policyPlanProposalPayload) (*policyv2.PolicyProposal, error) {
	if payload == nil {
		return nil, nil
	}
	proposal := &policyv2.PolicyProposal{Egress: payload.Egress, TrafficIngress: payload.TrafficIngress, RoutingRule: payload.RoutingRule}
	currentEgressID := ""
	if payload.RoutingRule != nil {
		currentEgressID = strings.TrimSpace(payload.RoutingRule.EgressID)
		if existingRule, err := device.repository.GetRoutingRule(ctx, payload.RoutingRule.ID); err == nil {
			if currentEgressID == "" {
				currentEgressID = existingRule.EgressID
			}
		} else if !errors.Is(err, policyv2.ErrRoutingRuleNotFound) {
			return nil, err
		}
	}
	boundEgressID := currentEgressID
	if proposal.Egress != nil {
		egress := *proposal.Egress
		fakeAliasWasExplicit := strings.TrimSpace(egress.FakeAlias) != ""
		if strings.TrimSpace(egress.ID) == "" && currentEgressID != "" {
			egress.ID = currentEgressID
		}
		if strings.TrimSpace(egress.ID) == "" {
			egress.Enabled = true
			// Normalize needs a provisional identity so it can derive an alias;
			// ResolvePolicyEgress assigns the final reusable identity below.
			egress.ID = uuid.NewString()
		}
		if egress.ListMode == "" {
			egress.ListMode = policyv2.ListModeShared
		}
		if egress.FailureMode == "" {
			egress.FailureMode = "strict"
		}
		var reader policyv2.PolicyReader
		if s.policy != nil {
			if applier := s.policy.ApplierFor(device.device.ID); applier != nil {
				reader = applier.Reader
			}
		}
		if err := normalizePolicyV2Egress(ctx, device.repository, reader, &egress); err != nil {
			return nil, err
		}
		if !fakeAliasWasExplicit {
			currentAlias := ""
			if currentEgressID != "" {
				if current, currentErr := device.repository.GetEgress(ctx, currentEgressID); currentErr == nil {
					currentAlias = current.FakeAlias
				}
			}
			if currentEgressID == "" || strings.TrimSpace(currentAlias) == "" {
				// The allocator's alias is derived from the eventual identity. Keep
				// an omitted alias semantic so equivalent policy exits can be reused.
				egress.FakeAlias = ""
			}
		}
		existingEgresses, err := device.repository.ListEgresses(ctx)
		if err != nil {
			return nil, err
		}
		refCounts := make(map[string]int)
		rules, listErr := device.repository.ListRoutingRules(ctx)
		if listErr != nil {
			return nil, listErr
		}
		for _, rule := range rules {
			if id := strings.TrimSpace(rule.EgressID); id != "" {
				refCounts[id]++
			}
		}
		sources, err := device.repository.ListSources(ctx, "")
		if err != nil {
			return nil, err
		}
		for _, source := range sources {
			if id := strings.TrimSpace(source.EgressID); id != "" {
				refCounts[id]++
			}
		}
		resolved, shouldSave := policyv2.ResolvePolicyEgress(egress, currentEgressID, existingEgresses, refCounts, uuid.NewString())
		boundEgressID = resolved.ID
		if shouldSave {
			proposal.Egress = &resolved
		} else {
			proposal.Egress = nil
		}
	}
	if proposal.TrafficIngress != nil {
		applier := s.policy.ApplierFor(device.device.ID)
		if applier == nil || applier.Reader == nil {
			return nil, errors.New("policy discovery is unavailable")
		}
		discovery, err := policyv2.NewScanner(applier.Reader).Scan(ctx, device.device.ID)
		if err != nil {
			return nil, err
		}
		scope, err := policyv2.NormalizeTrafficIngressScope(*proposal.TrafficIngress, discovery.TrafficIngress)
		if err != nil {
			return nil, err
		}
		proposal.TrafficIngress = &scope
	}
	if proposal.RoutingRule != nil {
		rule := *proposal.RoutingRule
		if rule.ID == "" {
			rule.ID = uuid.NewString()
		}
		if payload.Egress != nil {
			// ResolvePolicyEgress may have selected an equivalent existing exit or
			// allocated a Copy-on-Write exit. The rule must follow that result even
			// when the draft carried the previous Egress ID.
			rule.EgressID = boundEgressID
		} else if rule.EgressID == "" {
			rule.EgressID = boundEgressID
		}
		rule, err := s.canonicalizeRoutingRuleSubject(ctx, device, rule)
		if err != nil {
			return nil, err
		}
		rule, err = policyv2.NormalizeRoutingRule(rule)
		if err != nil {
			return nil, err
		}
		proposal.RoutingRule = &rule
	}
	for _, selection := range payload.PresetSelections {
		if err := s.appendProposedPresetTargets(ctx, device, selection, proposal); err != nil {
			return nil, err
		}
	}
	if proposal.Empty() {
		return nil, errors.New("policy proposal is empty")
	}
	return proposal, nil
}

func (s *Server) appendProposedPresetTargets(ctx context.Context, device policyDeviceContext, selection policyPlanPresetSelection, proposal *policyv2.PolicyProposal) error {
	presetID := strings.TrimSpace(selection.PresetID)
	previewID := strings.TrimSpace(selection.PreviewID)
	preset, ok := s.applicationPresets().Get(presetID)
	if !ok {
		return fmt.Errorf("application preset not found: %s", presetID)
	}
	preview, ok := s.applicationPresetPreview(previewID, device.device.ID, preset.ID)
	if !ok {
		return fmt.Errorf("a valid preset preview is required for %s", preset.Name)
	}
	requestedKinds, err := applicationpreset.ResolveRequestedKinds(selection.RequestedKinds, len(preview.Domain.Rules) > 0, len(preview.IP.Rules) > 0)
	if err != nil {
		return err
	}
	for _, kind := range requestedKinds {
		content := preview.Domain
		label := "域名"
		if kind == policy.KindIP {
			content = preview.IP
			label = "IP"
		}
		targetID := "preset:" + preset.ID + ":" + kind
		target, findErr := device.repository.GetTargetList(ctx, targetID)
		if errors.Is(findErr, policyv2.ErrTargetListNotFound) {
			target = policyv2.TargetList{ID: targetID, Revision: 0, Kind: kind, SourceType: policyv2.TargetSourceTypePreset, PresetID: preset.ID, Enabled: true, Schedule: "7d"}
		} else if findErr != nil {
			return findErr
		} else if target.SourceType != policyv2.TargetSourceTypePreset || target.PresetID != preset.ID || target.Kind != kind {
			return fmt.Errorf("target list %s is not owned by preset %s", targetID, preset.ID)
		} else if !target.PendingDeletion {
			activeVersionID := strings.TrimSpace(target.ActiveVersionID)
			pendingVersionID := strings.TrimSpace(target.PendingVersionID)
			hasUsableVersion := false
			for _, version := range target.Versions {
				versionID := strings.TrimSpace(version.ID)
				if versionID != "" && (versionID == activeVersionID || versionID == pendingVersionID) {
					hasUsableVersion = true
					break
				}
			}
			if hasUsableVersion {
				// A normal backing target is already referenceable. Selection reuses
				// it; refresh/materialization is an explicit operation. If a legacy
				// row has neither active nor pending content, fall through and repair
				// it for the new rule instead of creating a reference to an empty list.
				continue
			}
		}
		// An explicit preset selection may revive a legacy row that was left in
		// pending-deletion state. Rebuild its selected content in the proposal so
		// the overlay does not turn the new rule into a missing-target blocker.
		target.PendingDeletion = false
		version, rules, err := content.PendingVersion(device.device.ID, targetID, uuid.NewString())
		if err != nil {
			return err
		}
		target.Name = preset.Name + " · " + label
		target.URL = preview.URL
		target.Schedule = "7d"
		target.Enabled = true
		target.PendingVersionID = version.ID
		if target.NextRunAt.IsZero() {
			target.NextRunAt = time.Now().UTC().Add(7 * 24 * time.Hour)
		}
		targetRules := make([]policyv2.TargetListRule, len(rules))
		for index, rule := range rules {
			targetRules[index] = policyv2.TargetListRule{VersionID: version.ID, RuleType: rule.RuleType, Domain: rule.Domain}
		}
		targetVersion := policyv2.TargetListVersion{ID: version.ID, TargetListID: targetID, SHA256: version.SHA256, CompressedYAML: version.CompressedYAML, State: "pending", Counts: map[string]int{"valid": len(targetRules)}, CreatedAt: version.CreatedAt}
		proposal.TargetLists = append(proposal.TargetLists, policyv2.ProposedTargetList{Target: target, Version: targetVersion, Rules: targetRules})
	}
	return nil
}

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
			Kind             string                     `json:"kind"`
			InternetEgresses map[string][]string        `json:"internetEgresses"`
			Proposal         *policyPlanProposalPayload `json:"proposal"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			writePolicyJson(writer, http.StatusBadRequest, map[string]any{"code": "invalid_body", "error": err.Error()})
			return
		}
		proposal, err := s.preparePolicyPlanProposal(request.Context(), device, payload.Proposal)
		if err != nil {
			writePolicyJSON(writer, http.StatusUnprocessableEntity, map[string]any{"code": "invalid_policy_proposal", "error": err.Error()})
			return
		}
		envelope, err := s.policy.GeneratePlanWithOptions(request.Context(), device.device.ID, payload.Kind, policyv2.PlanOptions{InternetEgresses: payload.InternetEgresses, Proposal: proposal})
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
		PlanHash         string   `json:"planHash"`
	}
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		writePolicyJson(writer, http.StatusBadRequest, map[string]any{"code": "invalid_body", "error": err.Error()})
		return
	}
	job, err := s.policy.ApplyPlanWithHash(request.Context(), device.device.ID, parts[1], strings.TrimSpace(payload.PlanHash))
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
		URL  string `json:"url"`
		Kind string `json:"kind"`
	}
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		writePolicyJson(writer, http.StatusBadRequest, map[string]any{"code": "invalid_body", "error": err.Error()})
		return
	}
	kind, err := resolvePolicyPreviewKind(payload.Kind)
	if err != nil {
		writePolicyJson(writer, http.StatusUnprocessableEntity, map[string]any{"code": "invalid_target_list", "error": err.Error()})
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
	preview, err := fetcher.Preview(request.Context(), normalized, policy.FetchOptions{Kind: kind})
	if err != nil {
		writePolicyJson(writer, http.StatusBadGateway, map[string]any{"code": "fetch_failed", "error": err.Error()})
		return
	}
	rules := make([]map[string]any, 0)
	for i, r := range preview.Rules {
		if i >= 100 {
			break
		}
		if kind == policy.KindIP {
			rules = append(rules, map[string]any{"type": string(r.Type), "address": r.Domain})
		} else {
			rules = append(rules, map[string]any{"type": string(r.Type), "domain": r.Domain})
		}
	}
	previewID := ""
	if !preview.NotModified {
		previewID = s.savePolicyPreview(policyPreviewEntry{
			DeviceID: device.device.ID, SourceType: "url", Kind: kind, URL: preview.URL,
			ETag: preview.ETag, LastModified: preview.LastModified, Content: preview.PreparedSourceContent,
		})
	}
	writePolicyJson(writer, http.StatusOK, map[string]any{
		"previewId": previewID,
		"kind":      kind,
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
	kind, err := resolvePolicyPreviewKind(request.URL.Query().Get("kind"))
	if err != nil {
		writePolicyJson(writer, http.StatusUnprocessableEntity, map[string]any{"code": "invalid_target_list", "error": err.Error()})
		return
	}
	uploadService := policy.NewUploadService(filepath.Join(strings.TrimSpace(s.configSnapshot().DataDir), "tmp"))
	preview, err := uploadService.PreviewMultipart(request.Context(), request.Header.Get("Content-Type"), request.Body, kind)
	if err != nil {
		writePolicyJson(writer, http.StatusBadRequest, map[string]any{"code": "upload_failed", "error": err.Error()})
		return
	}
	rules := make([]map[string]any, 0)
	for i, r := range preview.Rules {
		if i >= 100 {
			break
		}
		if kind == policy.KindIP {
			rules = append(rules, map[string]any{"type": string(r.Type), "address": r.Domain})
		} else {
			rules = append(rules, map[string]any{"type": string(r.Type), "domain": r.Domain})
		}
	}
	previewID := s.savePolicyPreview(policyPreviewEntry{
		DeviceID: device.device.ID, SourceType: "upload", Kind: kind, Content: preview.PreparedSourceContent,
	})
	writePolicyJson(writer, http.StatusOK, map[string]any{
		"previewId": previewID, "kind": kind, "filename": preview.Filename, "sha256": preview.SHA256, "size": preview.Size,
		"validRules": len(preview.Rules), "ignored": preview.Ignored,
		"errorSamples": preview.ErrorSamples,
		"rules":        rules,
	})
}

// ---- Manual Preview ----

func (s *Server) servePolicyManualPreview(writer http.ResponseWriter, request *http.Request) {
	device, ok := s.resolvePolicyDevice(writer, request)
	if !ok {
		return
	}
	request.Body = http.MaxBytesReader(writer, request.Body, policy.MaxSourceBytes+(64<<10))
	var payload struct {
		Text string `json:"text"`
		Kind string `json:"kind"`
	}
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		writePolicyJson(writer, http.StatusBadRequest, map[string]any{"code": "invalid_body", "error": err.Error()})
		return
	}
	kind, err := resolvePolicyPreviewKind(payload.Kind)
	if err != nil {
		writePolicyJson(writer, http.StatusUnprocessableEntity, map[string]any{"code": "invalid_target_list", "error": err.Error()})
		return
	}
	var content policy.PreparedSourceContent
	if kind == policy.KindIP {
		content, err = policy.PrepareIPLines(payload.Text)
	} else {
		content, err = policy.PrepareDomainLines(payload.Text)
	}
	if err != nil {
		writePolicyJson(writer, http.StatusBadRequest, map[string]any{"code": "invalid_input", "error": err.Error()})
		return
	}
	rules := make([]map[string]any, 0)
	for i, r := range content.Rules {
		if i >= 100 {
			break
		}
		if kind == policy.KindIP {
			rules = append(rules, map[string]any{"type": string(r.Type), "address": r.Domain})
		} else {
			rules = append(rules, map[string]any{"type": string(r.Type), "domain": r.Domain})
		}
	}
	previewID := s.savePolicyPreview(policyPreviewEntry{
		DeviceID: device.device.ID, SourceType: "manual", Kind: kind, Content: content,
	})
	writePolicyJson(writer, http.StatusOK, map[string]any{
		"previewId": previewID, "kind": kind, "filename": "手动输入", "sha256": content.SHA256, "size": content.Size,
		"validRules": len(content.Rules), "ignored": content.Ignored,
		"errorSamples": content.ErrorSamples,
		"rules":        rules,
	})
}

func resolvePolicyPreviewKind(kind string) (string, error) {
	if err := policyv2.ValidateTargetListKind(kind); err != nil {
		return "", err
	}
	return kind, nil
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

func policyV2TargetListContent(version policy.SourceVersion, rules []policy.SourceRule) (policyv2.SourceVersion, []policyv2.SourceRule) {
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
