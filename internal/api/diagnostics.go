package api

import (
	"errors"
	"net/http"
	"strings"

	"rosboard/internal/diagnostics"
	"rosboard/internal/policyv2"
	"rosboard/internal/store"
)

func isDiagnosticsPath(path string) bool {
	return path == "/api/diagnostics" || path == "/api/diagnostics/deep"
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

		deviceStore, storeErr := s.existingDiagnosticStore(deviceID)
		var reader policyv2.PolicyReader
		if s.policy != nil {
			if applier := s.policy.ApplierFor(deviceID); applier != nil {
				reader = applier.Reader
			}
		}
		report := diagnostics.Runner{
			Config:       cfg,
			Device:       device,
			Manager:      s.manager,
			PolicyReader: reader,
			Store:        deviceStore,
			StoreError:   storeErr,
			Updater:      s.updater,
		}.Deep(request.Context())
		writeJSON(writer, http.StatusOK, report)
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
