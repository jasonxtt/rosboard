package api

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"rosboard/internal/policy"
	"rosboard/internal/policyv2"
)

func isTargetListPath(path string) bool {
	return path == "/api/target-lists" || strings.HasPrefix(path, "/api/target-lists/")
}

func (s *Server) serveTargetListAPI(writer http.ResponseWriter, request *http.Request) {
	relative := strings.TrimPrefix(request.URL.Path, "/api/target-lists")
	relative = strings.TrimPrefix(relative, "/")
	if relative == "" {
		switch request.Method {
		case http.MethodGet:
			s.listTargetLists(writer, request)
		case http.MethodPost:
			s.saveTargetList(writer, request, "")
		default:
			writeTargetListMethodNotAllowed(writer, "GET, POST")
		}
		return
	}

	parts := strings.Split(relative, "/")
	if len(parts) == 2 && parts[1] == "preview" {
		if request.Method != http.MethodPost {
			writeTargetListMethodNotAllowed(writer, http.MethodPost)
			return
		}
		switch parts[0] {
		case "url":
			s.servePolicyURLPreview(writer, request)
		case "upload":
			s.servePolicyUploadPreview(writer, request)
		case "manual":
			s.servePolicyManualPreview(writer, request)
		default:
			writePolicyJson(writer, http.StatusNotFound, map[string]any{"error": "not found"})
		}
		return
	}

	id := parts[0]
	if len(parts) == 2 {
		switch parts[1] {
		case "rules":
			if request.Method == http.MethodGet {
				s.serveTargetListRules(writer, request, id)
				return
			}
			writeTargetListMethodNotAllowed(writer, http.MethodGet)
			return
		case "refresh":
			if request.Method == http.MethodPost {
				s.serveTargetListRefresh(writer, request, id)
				return
			}
			writeTargetListMethodNotAllowed(writer, http.MethodPost)
			return
		}
	}
	if len(parts) != 1 {
		writePolicyJson(writer, http.StatusNotFound, map[string]any{"error": "not found"})
		return
	}
	switch request.Method {
	case http.MethodGet:
		s.getTargetList(writer, request, id)
	case http.MethodPut:
		s.saveTargetList(writer, request, id)
	case http.MethodDelete:
		s.deleteTargetList(writer, request, id)
	default:
		writeTargetListMethodNotAllowed(writer, "GET, PUT, DELETE")
	}
}

func targetListSchedule(value string) (time.Duration, bool) {
	switch strings.TrimSpace(value) {
	case "1h":
		return time.Hour, true
	case "6h":
		return 6 * time.Hour, true
	case "12h":
		return 12 * time.Hour, true
	case "":
		return 7 * 24 * time.Hour, true
	case "24h":
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

func writeTargetListMethodNotAllowed(writer http.ResponseWriter, allow string) {
	writer.Header().Set("Allow", allow)
	writePolicyJson(writer, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
}

func (s *Server) listTargetLists(writer http.ResponseWriter, request *http.Request) {
	device, ok := s.resolvePolicyDevice(writer, request)
	if !ok {
		return
	}
	targets, err := device.repository.ListTargetLists(request.Context())
	if err != nil {
		writePolicyJson(writer, http.StatusServiceUnavailable, map[string]any{"code": "target_lists_unavailable", "error": err.Error()})
		return
	}
	if request.URL.Query().Get("includePreset") != "true" {
		visible := targets[:0]
		for _, target := range targets {
			if target.SourceType != policyv2.TargetSourceTypePreset {
				visible = append(visible, target)
			}
		}
		targets = visible
	}
	writePolicyJson(writer, http.StatusOK, map[string]any{"targetLists": targets})
}

func (s *Server) getTargetList(writer http.ResponseWriter, request *http.Request, id string) {
	device, ok := s.resolvePolicyDevice(writer, request)
	if !ok {
		return
	}
	target, err := device.repository.GetTargetList(request.Context(), id)
	if errors.Is(err, policyv2.ErrTargetListNotFound) {
		writePolicyJson(writer, http.StatusNotFound, map[string]any{"code": "target_list_not_found", "error": "target list not found"})
		return
	}
	if err != nil {
		writePolicyJson(writer, http.StatusServiceUnavailable, map[string]any{"code": "target_list_unavailable", "error": err.Error()})
		return
	}
	writePolicyJson(writer, http.StatusOK, target)
}

type targetListSavePayload struct {
	policyv2.TargetList
	PreviewID  string `json:"previewId"`
	DeferApply bool   `json:"deferApply"`
}

func (s *Server) saveTargetList(writer http.ResponseWriter, request *http.Request, pathID string) {
	device, ok := s.resolvePolicyDevice(writer, request)
	if !ok {
		return
	}
	if !s.requirePolicyWriteAccess(writer, request, device.device) {
		return
	}
	var payload targetListSavePayload
	if err := decodeJSONBody(writer, request, &payload); err != nil {
		return
	}
	src := payload.TargetList.ToSource()
	src, err := s.saveTargetListModel(request, device, pathID, src, payload.PreviewID)
	if err != nil {
		writeTargetListSaveError(writer, err)
		return
	}
	target, err := device.repository.GetTargetList(request.Context(), src.ID)
	if err != nil {
		writePolicyJson(writer, http.StatusServiceUnavailable, map[string]any{"code": "load_failed", "error": err.Error()})
		return
	}
	if !payload.DeferApply && s.policy != nil && s.policy.ApplierFor(device.device.ID) != nil && policyv2.SourceAutoApplyEligible(request.Context(), device.repository, src, device.accessRepository) {
		job, err := s.policy.GenerateAndApplyTarget(request.Context(), device.device.ID, "target-list-save", src.ID)
		if err != nil {
			status, code := policyPlanApplyError(err)
			writePolicyJson(writer, status, map[string]any{"code": code, "error": err.Error(), "targetList": target})
			return
		}
		if job.ID == "" {
			writePolicyJson(writer, http.StatusOK, target)
			return
		}
		writePolicyJson(writer, http.StatusAccepted, map[string]any{"targetList": target, "job": job, "jobId": job.ID})
		return
	}
	writePolicyJson(writer, http.StatusOK, target)
}

func (s *Server) deleteTargetList(writer http.ResponseWriter, request *http.Request, id string) {
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
	if errors.Is(err, policyv2.ErrSourceNotFound) {
		writePolicyJson(writer, http.StatusNotFound, map[string]any{"code": "target_list_not_found", "error": "target list not found"})
		return
	}
	if err != nil {
		writePolicyJson(writer, http.StatusServiceUnavailable, map[string]any{"code": "target_list_unavailable", "error": err.Error()})
		return
	}
	if err := device.repository.DeleteTargetList(request.Context(), id, revision); err != nil {
		if errors.Is(err, policyv2.ErrPresetTargetListProtected) {
			writePolicyJson(writer, http.StatusConflict, map[string]any{"code": "preset_target_list_protected", "error": err.Error()})
			return
		}
		if errors.Is(err, policyv2.ErrTargetListInUse) {
			writePolicyJson(writer, http.StatusConflict, map[string]any{"code": "target_list_in_use", "error": err.Error()})
			return
		}
		writePolicyJson(writer, http.StatusConflict, map[string]any{"code": "revision_stale", "error": err.Error()})
		return
	}
	if !source.Applied {
		writePolicyJson(writer, http.StatusOK, map[string]any{"deleted": true})
		return
	}
	job, err := s.policy.GenerateAndApplyTarget(request.Context(), device.device.ID, "target-list-delete", id)
	if err != nil {
		status, code := policyPlanApplyError(err)
		writePolicyJson(writer, status, map[string]any{"code": code, "error": err.Error(), "pendingDeletion": true})
		return
	}
	if job.ID == "" {
		writePolicyJson(writer, http.StatusOK, map[string]any{"deleted": true})
		return
	}
	writePolicyJson(writer, http.StatusAccepted, map[string]any{"deleted": false, "pendingDeletion": true, "job": job, "jobId": job.ID})
}

type targetListSaveValidationError struct {
	status  int
	code    string
	message string
}

func (e *targetListSaveValidationError) Error() string { return e.message }

func newTargetListSaveValidationError(status int, code, message string) error {
	return &targetListSaveValidationError{status: status, code: code, message: message}
}

func writeTargetListSaveError(writer http.ResponseWriter, err error) {
	var validation *targetListSaveValidationError
	if errors.As(err, &validation) {
		writePolicyJson(writer, validation.status, map[string]any{"code": validation.code, "error": validation.message})
		return
	}
	if errors.Is(err, policyv2.ErrInvalidTargetListKind) || errors.Is(err, policyv2.ErrInvalidTargetListSourceType) {
		writePolicyJson(writer, http.StatusUnprocessableEntity, map[string]any{"code": "invalid_target_list", "error": err.Error()})
		return
	}
	if errors.Is(err, policyv2.ErrRevisionStale) {
		writePolicyJson(writer, http.StatusConflict, map[string]any{"code": "revision_stale", "error": err.Error()})
		return
	}
	if errors.Is(err, policyv2.ErrTargetListKindImmutable) || errors.Is(err, policyv2.ErrTargetListTypeImmutable) {
		writePolicyJson(writer, http.StatusUnprocessableEntity, map[string]any{"code": "invalid_target_list", "error": err.Error()})
		return
	}
	writePolicyJson(writer, http.StatusServiceUnavailable, map[string]any{"code": "save_failed", "error": err.Error()})
}

func (s *Server) saveTargetListModel(request *http.Request, device policyDeviceContext, pathID string, src policyv2.Source, previewID string) (policyv2.Source, error) {
	// Target Library entries are always available for selection. Whether they
	// are projected is determined solely by routing/access-control references.
	src.Enabled = true
	if pathID != "" {
		src.ID = pathID
	}
	if src.ID == "" {
		src.ID = uuid.NewString()
	}
	if strings.TrimSpace(src.Name) == "" {
		return policyv2.Source{}, newTargetListSaveValidationError(http.StatusBadRequest, "invalid_target_list", "name is required")
	}
	var current policyv2.Source
	if pathID != "" {
		var err error
		current, err = device.repository.GetSource(request.Context(), pathID)
		if err != nil {
			return policyv2.Source{}, newTargetListSaveValidationError(http.StatusNotFound, "target_list_not_found", "target list not found")
		}
		if src.Type == "" {
			src.Type = current.Type
		}
		if src.Kind == "" {
			src.Kind = current.Kind
		}
	}
	if err := policyv2.ValidateTargetListSourceType(src.Type); err != nil {
		return policyv2.Source{}, newTargetListSaveValidationError(http.StatusUnprocessableEntity, "invalid_target_list", err.Error())
	}
	if err := policyv2.ValidateTargetListKind(src.Kind); err != nil {
		return policyv2.Source{}, newTargetListSaveValidationError(http.StatusUnprocessableEntity, "invalid_target_list", err.Error())
	}
	if pathID != "" {
		if current.Type != src.Type {
			return policyv2.Source{}, newTargetListSaveValidationError(http.StatusUnprocessableEntity, "invalid_target_list", "target list source type cannot be changed")
		}
		if current.Kind != "" && current.Kind != src.Kind {
			return policyv2.Source{}, newTargetListSaveValidationError(http.StatusUnprocessableEntity, "invalid_target_list", "target list kind cannot be changed")
		}
	}
	if src.Type == policyv2.TargetSourceTypeURL || src.Type == policyv2.TargetSourceTypePreset {
		interval, valid := targetListSchedule(src.Schedule)
		if !valid {
			return policyv2.Source{}, newTargetListSaveValidationError(http.StatusUnprocessableEntity, "invalid_target_list", "unsupported target list refresh schedule")
		}
		if src.Schedule == "" {
			src.Schedule = "7d"
		}
		if pathID == "" || current.Schedule != src.Schedule || current.NextRunAt.IsZero() {
			src.NextRunAt = time.Now().UTC().Add(interval)
		}
	} else {
		src.URL = ""
		src.ETag = ""
		src.LastModified = ""
		src.Schedule = "manual"
		src.NextRunAt = time.Unix(0, 0).UTC()
	}
	contentChanged := pathID == "" || previewID != ""
	if pathID != "" {
		contentChanged = (src.Type == policyv2.TargetSourceTypeURL || src.Type == policyv2.TargetSourceTypePreset) && current.URL != src.URL
		contentChanged = contentChanged || previewID != ""
	}
	var preview policyPreviewEntry
	if contentChanged {
		var found bool
		preview, found = s.policyPreview(previewID, device.device.ID)
		if !found || preview.SourceType != src.Type || policyv2.NormalizeSourceKind(preview.Kind) != src.Kind {
			return policyv2.Source{}, newTargetListSaveValidationError(http.StatusUnprocessableEntity, "invalid_target_list_preview", "a valid target list preview is required")
		}
		if len(preview.Content.Rules) == 0 {
			return policyv2.Source{}, newTargetListSaveValidationError(http.StatusUnprocessableEntity, "invalid_target_list_preview", "target list preview contains no valid rules")
		}
		if src.Type == policyv2.TargetSourceTypeURL || src.Type == policyv2.TargetSourceTypePreset {
			src.URL = preview.URL
			src.ETag = preview.ETag
			src.LastModified = preview.LastModified
		}
	}
	var err error
	_, err = device.repository.SaveTargetList(request.Context(), policyv2.TargetListFromSource(src))
	if err != nil {
		return policyv2.Source{}, err
	}
	src, err = device.repository.GetSource(request.Context(), src.ID)
	if err != nil {
		return policyv2.Source{}, fmt.Errorf("reload target list: %w", err)
	}
	if contentChanged {
		versionID := uuid.NewString()
		legacyVersion, legacyRules, err := preview.Content.PendingVersion(device.device.ID, src.ID, versionID)
		if err != nil {
			return policyv2.Source{}, newTargetListSaveValidationError(http.StatusBadRequest, "invalid_target_list_preview", err.Error())
		}
		version, rules := policyV2TargetListContent(legacyVersion, legacyRules)
		targetRules := make([]policyv2.TargetListRule, len(rules))
		for i, rule := range rules {
			targetRules[i] = policyv2.TargetListRuleFromSource(rule)
		}
		err = device.repository.SavePendingTargetListVersion(request.Context(), policyv2.TargetListVersionFromSource(version), targetRules)
		if err != nil {
			return policyv2.Source{}, err
		}
		s.discardPolicyPreview(previewID)
		src, err = device.repository.GetSource(request.Context(), src.ID)
		if err != nil {
			return policyv2.Source{}, fmt.Errorf("reload saved target list: %w", err)
		}
	}
	return src, nil
}

type targetListRulesPage struct {
	source     policyv2.Source
	versionID  string
	rules      []policyv2.SourceRule
	nextCursor string
}

func (s *Server) loadTargetListRules(request *http.Request, device policyDeviceContext, id string) (targetListRulesPage, error) {
	src, err := device.repository.GetSource(request.Context(), id)
	if err != nil {
		return targetListRulesPage{}, err
	}
	limit := 100
	if value := request.URL.Query().Get("limit"); value != "" {
		if parsed, parseErr := strconv.Atoi(value); parseErr == nil && parsed > 0 && parsed <= 1000 {
			limit = parsed
		}
	}
	query := policyv2.RuleQuery{Limit: limit, Query: strings.TrimSpace(request.URL.Query().Get("query")), RuleType: strings.TrimSpace(request.URL.Query().Get("type"))}
	if cursor := strings.TrimSpace(request.URL.Query().Get("cursor")); cursor != "" {
		decoded, decodeErr := base64.RawURLEncoding.DecodeString(cursor)
		parts := strings.SplitN(string(decoded), "\x00", 2)
		if decodeErr != nil || len(parts) != 2 {
			return targetListRulesPage{}, &targetListOperationError{status: http.StatusBadRequest, code: "invalid_cursor", message: "rules cursor is invalid"}
		}
		query.AfterType, query.AfterDomain = parts[0], parts[1]
	}
	versionID := src.ActiveVersionID
	if request.URL.Query().Get("version") == "pending" && src.PendingVersionID != "" {
		versionID = src.PendingVersionID
	} else if versionID == "" {
		versionID = src.PendingVersionID
	}
	rules, hasNext, err := device.repository.ListSourceRules(request.Context(), versionID, query)
	if err != nil {
		return targetListRulesPage{}, err
	}
	nextCursor := ""
	if hasNext && len(rules) > 0 {
		last := rules[len(rules)-1]
		nextCursor = base64.RawURLEncoding.EncodeToString([]byte(last.RuleType + "\x00" + last.Domain))
	}
	return targetListRulesPage{source: src, versionID: versionID, rules: rules, nextCursor: nextCursor}, nil
}

func targetListRuleResponse(kind string, rules []policyv2.SourceRule) []map[string]any {
	result := make([]map[string]any, 0, len(rules))
	for _, rule := range rules {
		if kind == policyv2.KindIP {
			result = append(result, map[string]any{"type": rule.RuleType, "address": rule.Domain})
		} else {
			result = append(result, map[string]any{"type": rule.RuleType, "domain": rule.Domain})
		}
	}
	return result
}

func (s *Server) serveTargetListRules(writer http.ResponseWriter, request *http.Request, id string) {
	device, ok := s.resolvePolicyDevice(writer, request)
	if !ok {
		return
	}
	page, err := s.loadTargetListRules(request, device, id)
	if err != nil {
		var operationErr *targetListOperationError
		if errors.As(err, &operationErr) {
			writePolicyJson(writer, operationErr.status, map[string]any{"code": operationErr.code, "error": operationErr.message})
			return
		}
		if errors.Is(err, policyv2.ErrSourceNotFound) {
			writePolicyJson(writer, http.StatusNotFound, map[string]any{"code": "target_list_not_found", "error": "target list not found"})
			return
		}
		writePolicyJson(writer, http.StatusServiceUnavailable, map[string]any{"code": "rules_unavailable", "error": err.Error()})
		return
	}
	writePolicyJson(writer, http.StatusOK, map[string]any{
		"targetListId": id, "versionId": page.versionID, "rules": targetListRuleResponse(page.source.Kind, page.rules), "nextCursor": page.nextCursor,
	})
}

type targetListOperationError struct {
	status  int
	code    string
	message string
	cause   error
}

func (e *targetListOperationError) Error() string { return e.message }
func (e *targetListOperationError) Unwrap() error { return e.cause }

func (s *Server) refreshTargetListModel(request *http.Request, device policyDeviceContext, id string) (targetListRefreshResult, error) {
	src, err := device.repository.GetSource(request.Context(), id)
	if err != nil || (src.Type != policyv2.TargetSourceTypeURL && src.Type != policyv2.TargetSourceTypePreset) || src.URL == "" {
		return targetListRefreshResult{}, &targetListOperationError{status: http.StatusConflict, code: "target_list_unavailable", message: "target list cannot be refreshed", cause: err}
	}
	fetcher := s.sourceFetcher
	if fetcher == nil {
		fetcher = policy.NewSourceFetcher(policy.FetcherOptions{})
	}
	result, err := fetcher.Preview(request.Context(), src.URL, policy.FetchOptions{ETag: src.ETag, LastModified: src.LastModified, Kind: src.Kind})
	if err != nil {
		return targetListRefreshResult{}, &targetListOperationError{status: http.StatusBadGateway, code: "fetch_failed", message: err.Error(), cause: err}
	}
	interval, valid := targetListSchedule(src.Schedule)
	if !valid {
		return targetListRefreshResult{}, &targetListOperationError{status: http.StatusConflict, code: "target_list_unavailable", message: "target list refresh schedule is invalid"}
	}
	refresh := policyv2.TargetListRefresh{NotModified: result.NotModified, ETag: result.ETag, LastModified: result.LastModified}
	versionID := ""
	if !result.NotModified {
		versionID = uuid.NewString()
		legacyVersion, legacyRules, err := result.PendingVersion(device.device.ID, src.ID, versionID)
		if err != nil {
			return targetListRefreshResult{}, &targetListOperationError{status: http.StatusBadRequest, code: "parse_failed", message: err.Error(), cause: err}
		}
		version, rules := policyV2TargetListContent(legacyVersion, legacyRules)
		convertedVersion := policyv2.TargetListVersionFromSource(version)
		refresh.Version = &convertedVersion
		refresh.Rules = make([]policyv2.TargetListRule, len(rules))
		for i, rule := range rules {
			refresh.Rules[i] = policyv2.TargetListRuleFromSource(rule)
		}
	}
	err = device.repository.SaveTargetListRefresh(request.Context(), policyv2.TargetListFromSource(src), refresh, time.Now().UTC().Add(interval))
	if err != nil {
		return targetListRefreshResult{}, &targetListOperationError{status: http.StatusServiceUnavailable, code: "save_failed", message: err.Error(), cause: err}
	}
	src, err = device.repository.GetSource(request.Context(), id)
	if err != nil {
		return targetListRefreshResult{}, &targetListOperationError{status: http.StatusServiceUnavailable, code: "load_failed", message: err.Error(), cause: err}
	}
	return targetListRefreshResult{source: src, versionID: versionID, ruleCount: len(refresh.Rules), notModified: result.NotModified}, nil
}

type targetListRefreshResult struct {
	source      policyv2.Source
	versionID   string
	ruleCount   int
	notModified bool
}

func writeTargetListOperationError(writer http.ResponseWriter, err error) bool {
	var operationErr *targetListOperationError
	if !errors.As(err, &operationErr) {
		return false
	}
	writePolicyJson(writer, operationErr.status, map[string]any{"code": operationErr.code, "error": operationErr.message})
	return true
}

func (s *Server) serveTargetListRefresh(writer http.ResponseWriter, request *http.Request, id string) {
	device, ok := s.resolvePolicyDevice(writer, request)
	if !ok {
		return
	}
	if !s.requirePolicyWriteAccess(writer, request, device.device) {
		return
	}
	result, err := s.refreshTargetListModel(request, device, id)
	if err != nil {
		if !writeTargetListOperationError(writer, err) {
			writePolicyJson(writer, http.StatusServiceUnavailable, map[string]any{"code": "refresh_failed", "error": err.Error()})
		}
		return
	}
	if result.notModified {
		writePolicyJson(writer, http.StatusOK, map[string]any{"notModified": true})
		return
	}
	target, err := device.repository.GetTargetList(request.Context(), id)
	if err != nil {
		writePolicyJson(writer, http.StatusServiceUnavailable, map[string]any{"code": "load_failed", "error": err.Error()})
		return
	}
	if s.policy != nil && s.policy.ApplierFor(device.device.ID) != nil && policyv2.SourceAutoApplyEligible(request.Context(), device.repository, result.source, device.accessRepository) {
		job, err := s.policy.GenerateAndApplyTarget(request.Context(), device.device.ID, "target-list-refresh", id)
		if err != nil {
			status, code := policyPlanApplyError(err)
			writePolicyJson(writer, status, map[string]any{"code": code, "error": err.Error(), "targetList": target})
			return
		}
		if job.ID == "" {
			writePolicyJson(writer, http.StatusOK, map[string]any{"targetList": target, "targetListId": id, "versionId": result.versionID, "ruleCount": result.ruleCount})
			return
		}
		writePolicyJson(writer, http.StatusAccepted, map[string]any{"targetList": target, "job": job, "jobId": job.ID})
		return
	}
	writePolicyJson(writer, http.StatusOK, map[string]any{"targetList": target, "targetListId": id, "versionId": result.versionID, "ruleCount": result.ruleCount})
}
