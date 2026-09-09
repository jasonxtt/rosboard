package api

import (
	"net/http"
	"strings"

	"rosboard/internal/update"
)

func (s *Server) SetUpdater(updater *update.Manager) { s.updater = updater }
func (s *Server) serveUpdate(writer http.ResponseWriter, request *http.Request) {
	if s.updater == nil {
		writeAPIError(writer, http.StatusServiceUnavailable, "update_unavailable", "版本更新不可用")
		return
	}
	switch request.URL.Path {
	case "/api/settings/update":
		if request.Method != http.MethodGet {
			methodNotAllowed(writer, http.MethodGet)
			return
		}
		writeJSON(writer, http.StatusOK, s.updater.Status())
	case "/api/settings/update/check":
		if request.Method != http.MethodPost {
			methodNotAllowed(writer, http.MethodPost)
			return
		}
		status, err := s.updater.Check(request.Context())
		// Check failures retain cached metadata and are explicitly represented in status.
		if err != nil && status.CheckError == "" {
			writeAPIError(writer, http.StatusConflict, "update_check_busy", err.Error())
			return
		}
		writeJSON(writer, http.StatusOK, status)
	case "/api/settings/update/install":
		if request.Method != http.MethodPost {
			methodNotAllowed(writer, http.MethodPost)
			return
		}
		var payload struct {
			Version string `json:"version"`
		}
		if err := decodeJSONBody(writer, request, &payload); err != nil {
			return
		}
		status, err := s.updater.Install(payload.Version)
		if err != nil {
			writeAPIError(writer, http.StatusConflict, "update_rejected", err.Error())
			return
		}
		writeJSON(writer, http.StatusAccepted, status)
	default:
		writeError(writer, http.StatusNotFound, "not found")
	}
}
func updatePath(path string) bool {
	return path == "/api/settings/update" || strings.HasPrefix(path, "/api/settings/update/")
}
