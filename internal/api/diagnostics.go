package api

import (
	"errors"
	"net/http"
	"strings"

	"rosboard/internal/diagnostics"
	"rosboard/internal/store"
)

func isDiagnosticsPath(path string) bool {
	return path == "/api/diagnostics"
}

func (s *Server) serveDiagnostics(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writer.Header().Set("Allow", http.MethodGet)
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

	var deviceStore *store.Store
	var storeErr error
	if s.store == nil {
		storeErr = errors.New("device store is unavailable")
	} else {
		deviceStore, storeErr = s.store.ExistingDevice(deviceID)
	}
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
