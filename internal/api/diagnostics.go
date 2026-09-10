package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"rosboard/internal/config"
	"rosboard/internal/diagnostics"
	"rosboard/internal/policyv2"
	"rosboard/internal/store"
)

func isDiagnosticsPath(path string) bool {
	return path == "/api/diagnostics" || path == "/api/diagnostics/deep" || path == "/api/diagnostics/export"
}

func (s *Server) serveDiagnostics(writer http.ResponseWriter, request *http.Request) {
	if request.URL.Path == "/api/diagnostics" && request.Method != http.MethodGet {
		writer.Header().Set("Allow", http.MethodGet)
		writeAPIError(writer, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	if request.URL.Path == "/api/diagnostics/deep" && request.Method != http.MethodPost {
		writer.Header().Set("Allow", http.MethodPost)
		writeAPIError(writer, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	if request.URL.Path == "/api/diagnostics/export" && request.Method != http.MethodPost {
		writer.Header().Set("Allow", http.MethodPost)
		writeAPIError(writer, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	deviceID := strings.TrimSpace(request.URL.Query().Get("device"))
	if deviceID == "" {
		writeAPIError(writer, http.StatusBadRequest, "device_required", "a device query parameter is required")
		return
	}
	cfg := s.configSnapshot()
	device, found := cfg.Device(deviceID)
	if !found || device.Archived {
		writeAPIError(writer, http.StatusNotFound, "device_not_found", "device not found")
		return
	}
	if request.URL.Path == "/api/diagnostics/deep" {
		if !s.beginDeepDiagnostics(deviceID) {
			writeAPIError(writer, http.StatusConflict, "deep_diagnostics_in_progress", "a deep diagnostic is already running for this device")
			return
		}
		defer s.endDeepDiagnostics(deviceID)

		report := s.runDeepDiagnostic(request.Context(), cfg, device)
		s.rememberDeepReport(deviceID, report)
		writeJSON(writer, http.StatusOK, report)
		return
	}
	if request.URL.Path == "/api/diagnostics/export" {
		report, ok := s.cachedDeepReport(deviceID)
		if !ok {
			if !s.beginDeepDiagnostics(deviceID) {
				writeAPIError(writer, http.StatusConflict, "deep_diagnostics_in_progress", "a deep diagnostic is already running for this device")
				return
			}
			defer s.endDeepDiagnostics(deviceID)
			report = s.runDeepDiagnostic(request.Context(), cfg, device)
			s.rememberDeepReport(deviceID, report)
		}
		recentLogs := ""
		if s.diagnosticLogs != nil {
			recentLogs = s.diagnosticLogs.Recent()
		}
		payload, filename, err := diagnostics.BuildDiagnosticExport(report, recentLogs)
		if err != nil {
			writeAPIError(writer, http.StatusInternalServerError, "diagnostics_export_failed", "failed to build diagnostic package")
			return
		}
		writer.Header().Set("Content-Type", "application/zip")
		writer.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
		writer.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write(payload)
		return
	}

	deviceStore, storeErr := s.existingDiagnosticStore(deviceID)
	report := diagnostics.Runner{
		Config:     cfg,
		Device:     device,
		Manager:    s.manager,
		Store:      deviceStore,
		StoreError: storeErr,
		Updater:    s.updater,
	}.Quick(request.Context())
	writeJSON(writer, http.StatusOK, report)
}

func (s *Server) runDeepDiagnostic(ctx context.Context, cfg config.Config, device config.DeviceConfig) diagnostics.DeepReport {
	deviceStore, storeErr := s.existingDiagnosticStore(device.ID)
	var reader policyv2.PolicyReader
	if s.policy != nil {
		if applier := s.policy.ApplierFor(device.ID); applier != nil {
			reader = applier.Reader
		}
	}
	return diagnostics.Runner{
		Config:       cfg,
		Device:       device,
		Manager:      s.manager,
		PolicyReader: reader,
		Store:        deviceStore,
		StoreError:   storeErr,
		Updater:      s.updater,
	}.Deep(ctx)
}

func (s *Server) existingDiagnosticStore(deviceID string) (*store.Store, error) {
	if s.store == nil {
		return nil, errors.New("device store is unavailable")
	}
	return s.store.ExistingDevice(deviceID)
}

func (s *Server) beginDeepDiagnostics(deviceID string) bool {
	s.diagnosticsMu.Lock()
	defer s.diagnosticsMu.Unlock()
	if s.deepDiagnostics == nil {
		s.deepDiagnostics = make(map[string]bool)
	}
	if s.deepDiagnostics[deviceID] {
		return false
	}
	s.deepDiagnostics[deviceID] = true
	return true
}

func (s *Server) endDeepDiagnostics(deviceID string) {
	s.diagnosticsMu.Lock()
	defer s.diagnosticsMu.Unlock()
	delete(s.deepDiagnostics, deviceID)
}

func (s *Server) rememberDeepReport(deviceID string, report diagnostics.DeepReport) {
	s.diagnosticsMu.Lock()
	defer s.diagnosticsMu.Unlock()
	if s.deepReports == nil {
		s.deepReports = make(map[string]diagnostics.DeepReport)
	}
	s.deepReports[deviceID] = report
}

func (s *Server) cachedDeepReport(deviceID string) (diagnostics.DeepReport, bool) {
	s.diagnosticsMu.Lock()
	defer s.diagnosticsMu.Unlock()
	report, ok := s.deepReports[deviceID]
	return report, ok
}
