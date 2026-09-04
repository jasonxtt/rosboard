package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"rosboard/internal/applicationpreset"
	"rosboard/internal/policy"
	"rosboard/internal/policyv2"
)

type applicationPresetPreview struct {
	DeviceID     string
	PresetID     string
	Domain       policy.PreparedSourceContent
	IP           policy.PreparedSourceContent
	URL          string
	ETag         string
	LastModified string
	ExpiresAt    time.Time
}

func isApplicationPresetPath(path string) bool {
	return path == "/api/application-presets" || strings.HasPrefix(path, "/api/application-presets/")
}

func (s *Server) applicationPresets() *applicationpreset.Registry {
	if s.presetRegistry != nil {
		return s.presetRegistry
	}
	return applicationpreset.Default()
}

func (s *Server) serveApplicationPresetAPI(writer http.ResponseWriter, request *http.Request) {
	relative := strings.Trim(strings.TrimPrefix(request.URL.Path, "/api/application-presets"), "/")
	if relative == "" {
		if request.Method != http.MethodGet {
			writePolicyJson(writer, http.StatusMethodNotAllowed, map[string]any{"code": "method_not_allowed", "error": "method not allowed"})
			return
		}
		writePolicyJson(writer, http.StatusOK, map[string]any{"presets": s.applicationPresets().List()})
		return
	}
	parts := strings.Split(relative, "/")
	if len(parts) != 2 || (parts[1] != "preview" && parts[1] != "target-lists") || request.Method != http.MethodPost {
		writePolicyJson(writer, http.StatusMethodNotAllowed, map[string]any{"code": "method_not_allowed", "error": "method not allowed"})
		return
	}
	preset, ok := s.applicationPresets().Get(parts[0])
	if !ok {
		writePolicyJson(writer, http.StatusNotFound, map[string]any{"code": "preset_not_found", "error": "application preset not found"})
		return
	}
	device, ok := s.resolvePolicyDevice(writer, request)
	if !ok {
		return
	}
	if parts[1] == "preview" {
		s.previewApplicationPreset(writer, request, device, preset)
		return
	}
	if !s.requirePolicyWriteAccess(writer, request, device.device) {
		return
	}
	s.materializeApplicationPreset(writer, request, device, preset)
}

func (s *Server) previewApplicationPreset(writer http.ResponseWriter, request *http.Request, device policyDeviceContext, preset applicationpreset.ApplicationPreset) {
	fetcher := s.sourceFetcher
	if fetcher == nil {
		fetcher = policy.NewSourceFetcher(policy.FetcherOptions{})
	}
	fetched, err := fetchApplicationPresetRule(request.Context(), fetcher, preset)
	if err != nil {
		writePolicyJson(writer, http.StatusBadGateway, map[string]any{"code": "fetch_failed", "error": err.Error()})
		return
	}
	preview := applicationPresetPreview{DeviceID: device.device.ID, PresetID: preset.ID, URL: preset.RuleURL, ETag: fetched.ETag, LastModified: fetched.LastModified, ExpiresAt: time.Now().UTC().Add(policyPreviewLifetime)}
	preview.Domain, _ = policy.PrepareSourceContent(fetched.Body, policy.KindDomain)
	preview.IP, _ = policy.PrepareSourceContent(fetched.Body, policy.KindIP)
	if len(preview.Domain.Rules) == 0 && len(preview.IP.Rules) == 0 {
		writePolicyJson(writer, http.StatusUnprocessableEntity, map[string]any{"code": "preset_has_no_supported_rules", "error": "preset contains no supported domain or IP rules"})
		return
	}
	previewID := s.saveApplicationPresetPreview(preview)
	existing := s.existingPresetTargetIDs(request, device, preset.ID)
	writePolicyJson(writer, http.StatusOK, map[string]any{
		"previewId": previewID, "id": preset.ID, "name": preset.Name, "category": preset.Category, "aliases": preset.Aliases, "rulePath": preset.RulePath, "ruleURL": preset.RuleURL,
		"domain": preparedPresetSummary(preview.Domain, policy.KindDomain), "ip": preparedPresetSummary(preview.IP, policy.KindIP),
		"existingTargetListIds": existing,
	})
}

func preparedPresetSummary(content policy.PreparedSourceContent, kind string) map[string]any {
	rules := make([]map[string]string, 0, minPresetPreviewRules)
	for index, rule := range content.Rules {
		if index >= minPresetPreviewRules {
			break
		}
		key := "domain"
		if kind == policy.KindIP {
			key = "address"
		}
		rules = append(rules, map[string]string{"type": string(rule.Type), key: rule.Domain})
	}
	return map[string]any{"validRules": len(content.Rules), "ignored": content.Ignored, "errorSamples": content.ErrorSamples, "rules": rules}
}

const minPresetPreviewRules = 100

const applicationPresetPrimaryTimeout = 6 * time.Second

func fetchApplicationPresetRule(ctx context.Context, fetcher *policy.SourceFetcher, preset applicationpreset.ApplicationPreset) (policy.FetchResult, error) {
	fallbackURL, canFallback := applicationpreset.CDNRuleURL(preset)
	if !canFallback {
		return fetcher.Fetch(ctx, preset.RuleURL, policy.FetchOptions{})
	}

	totalCtx, cancel := context.WithTimeout(ctx, policy.SourceFetchTimeout)
	defer cancel()
	primaryCtx, cancelPrimary := context.WithTimeout(totalCtx, applicationPresetPrimaryTimeout)
	primary, primaryErr := fetcher.Fetch(primaryCtx, preset.RuleURL, policy.FetchOptions{})
	cancelPrimary()
	if primaryErr == nil {
		return primary, nil
	}
	if !policy.IsRetryableSourceError(primaryErr) {
		return policy.FetchResult{}, primaryErr
	}

	fallback, fallbackErr := fetcher.Fetch(totalCtx, fallbackURL, policy.FetchOptions{})
	if fallbackErr != nil {
		return policy.FetchResult{}, fmt.Errorf("application preset source unavailable: %w", fallbackErr)
	}
	return fallback, nil
}

func (s *Server) materializeApplicationPreset(writer http.ResponseWriter, request *http.Request, device policyDeviceContext, preset applicationpreset.ApplicationPreset) {
	previewID := strings.TrimSpace(request.URL.Query().Get("previewId"))
	preview, ok := s.applicationPresetPreview(previewID, device.device.ID, preset.ID)
	if !ok {
		writePolicyJson(writer, http.StatusUnprocessableEntity, map[string]any{"code": "invalid_preset_preview", "error": "a valid preset preview is required"})
		return
	}
	var payload struct {
		RequestedKinds []string `json:"requestedKinds"`
	}
	if request.Body != nil {
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil && !errors.Is(err, io.EOF) {
			writePolicyJson(writer, http.StatusBadRequest, map[string]any{"code": "invalid_body", "error": err.Error()})
			return
		}
	}
	requestedKinds, err := applicationpreset.ResolveRequestedKinds(payload.RequestedKinds, len(preview.Domain.Rules) > 0, len(preview.IP.Rules) > 0)
	if err != nil {
		writePolicyJson(writer, http.StatusUnprocessableEntity, map[string]any{"code": "requested_kind_unavailable", "error": err.Error()})
		return
	}
	created := make([]policyv2.TargetList, 0, len(requestedKinds))
	for _, kind := range requestedKinds {
		content := preview.Domain
		label := "域名"
		if kind == policy.KindIP {
			content = preview.IP
			label = "IP"
		}
		targetID := "preset:" + preset.ID + ":" + kind
		target, findErr := device.repository.FindTargetListByPresetKind(request.Context(), preset.ID, kind)
		if errors.Is(findErr, policyv2.ErrTargetListNotFound) {
			target, findErr = device.repository.SaveTargetList(request.Context(), policyv2.TargetList{
				ID: targetID, Name: preset.Name + " · " + label, Kind: kind,
				SourceType: policyv2.TargetSourceTypePreset, PresetID: preset.ID, URL: preset.RuleURL,
				Schedule: "7d", Enabled: true, NextRunAt: time.Now().UTC().Add(7 * 24 * time.Hour),
			})
		}
		if findErr != nil {
			writePolicyJson(writer, http.StatusConflict, map[string]any{"code": "target_list_unavailable", "error": findErr.Error()})
			return
		}
		versions, err := device.repository.ListTargetListVersions(request.Context(), target.ID)
		if err != nil {
			writePolicyJson(writer, http.StatusServiceUnavailable, map[string]any{"code": "target_list_unavailable", "error": err.Error()})
			return
		}
		duplicate := false
		for _, version := range versions {
			if version.SHA256 == content.SHA256 {
				duplicate = true
				break
			}
		}
		if !duplicate {
			version, rules, err := content.PendingVersion(device.device.ID, target.ID, uuid.NewString())
			if err != nil {
				writePolicyJson(writer, http.StatusUnprocessableEntity, map[string]any{"code": "preset_preview_invalid", "error": err.Error()})
				return
			}
			targetVersion := policyv2.TargetListVersion{ID: version.ID, TargetListID: target.ID, SHA256: version.SHA256, CompressedYAML: version.CompressedYAML, State: version.State, Error: version.Error, HTTPStatus: version.HTTPStatus, Counts: map[string]int{"valid": len(content.Rules)}, CreatedAt: version.CreatedAt}
			targetRules := make([]policyv2.TargetListRule, len(rules))
			for index, rule := range rules {
				targetRules[index] = policyv2.TargetListRule{VersionID: version.ID, RuleType: rule.RuleType, Domain: rule.Domain}
			}
			if err := device.repository.SavePendingTargetListVersion(request.Context(), targetVersion, targetRules); err != nil {
				writePolicyJson(writer, http.StatusServiceUnavailable, map[string]any{"code": "target_list_unavailable", "error": err.Error()})
				return
			}
		}
		target, err = device.repository.GetTargetList(request.Context(), target.ID)
		if err != nil {
			writePolicyJson(writer, http.StatusServiceUnavailable, map[string]any{"code": "target_list_unavailable", "error": err.Error()})
			return
		}
		created = append(created, target)
	}
	s.discardApplicationPresetPreview(previewID)
	sort.Slice(created, func(i, j int) bool { return created[i].ID < created[j].ID })
	writePolicyJson(writer, http.StatusOK, map[string]any{"preset": preset, "targetLists": created})
}

func (s *Server) existingPresetTargetIDs(request *http.Request, device policyDeviceContext, presetID string) []string {
	ids := make([]string, 0, 2)
	for _, kind := range []string{policy.KindDomain, policy.KindIP} {
		if target, err := device.repository.FindTargetListByPresetKind(request.Context(), presetID, kind); err == nil {
			ids = append(ids, target.ID)
		}
	}
	return ids
}

func (s *Server) saveApplicationPresetPreview(entry applicationPresetPreview) string {
	s.previewMu.Lock()
	defer s.previewMu.Unlock()
	if s.presetPreviews == nil {
		s.presetPreviews = make(map[string]applicationPresetPreview)
	}
	now := time.Now().UTC()
	for id, candidate := range s.presetPreviews {
		if !candidate.ExpiresAt.After(now) {
			delete(s.presetPreviews, id)
		}
	}
	id := uuid.NewString()
	s.presetPreviews[id] = entry
	return id
}

func (s *Server) applicationPresetPreview(id, deviceID, presetID string) (applicationPresetPreview, bool) {
	s.previewMu.Lock()
	defer s.previewMu.Unlock()
	entry, ok := s.presetPreviews[id]
	if !ok || entry.DeviceID != deviceID || entry.PresetID != presetID || !entry.ExpiresAt.After(time.Now().UTC()) {
		if ok {
			delete(s.presetPreviews, id)
		}
		return applicationPresetPreview{}, false
	}
	return entry, true
}

func (s *Server) discardApplicationPresetPreview(id string) {
	s.previewMu.Lock()
	defer s.previewMu.Unlock()
	delete(s.presetPreviews, id)
}
