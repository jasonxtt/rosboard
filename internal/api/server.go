package api

import (
	"encoding/json"
	"errors"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
	"unicode/utf8"

	"rosboard/internal/config"
	"rosboard/internal/service"
	"rosboard/internal/store"
)

type Server struct {
	monitor      *service.Monitor
	assets       fs.FS
	allowedCIDRs []*net.IPNet
	fileServer   http.Handler
}

func NewServer(cfg config.Config, monitor *service.Monitor, assets fs.FS) *Server {
	return &Server{
		monitor:      monitor,
		assets:       assets,
		allowedCIDRs: parseAllowedCIDRs(cfg.AllowedCIDRs),
		fileServer:   http.FileServer(http.FS(assets)),
	}
}

func (s *Server) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if strings.HasPrefix(request.URL.Path, "/api/") {
		if !s.allowed(request) {
			writeError(writer, http.StatusForbidden, "forbidden")
			return
		}
		s.serveAPI(writer, request)
		return
	}

	s.serveApp(writer, request)
}

func (s *Server) serveAPI(writer http.ResponseWriter, request *http.Request) {
	switch request.URL.Path {
	case "/api/health":
		writeJSON(writer, http.StatusOK, map[string]any{"ok": true})
	case "/api/overview":
		writeJSON(writer, http.StatusOK, s.monitor.Snapshot().Overview)
	case "/api/interfaces":
		writeJSON(writer, http.StatusOK, map[string]any{"interfaces": s.monitor.Snapshot().Interfaces})
	case "/api/terminals":
		writeJSON(writer, http.StatusOK, map[string]any{"terminals": s.monitor.Snapshot().Terminals})
	case "/api/capabilities":
		writeJSON(writer, http.StatusOK, map[string]any{"capabilities": s.monitor.Snapshot().Capabilities})
	case "/api/dashboard":
		writeJSON(writer, http.StatusOK, s.monitor.Snapshot())
	case "/api/load":
		window := parseWindow(request.URL.Query().Get("window"))
		samples, err := s.monitor.LoadHistory(request.Context(), time.Now().UTC().Add(-window))
		if err != nil {
			writeError(writer, http.StatusInternalServerError, "failed to load history")
			return
		}
		writeJSON(writer, http.StatusOK, map[string]any{"samples": samples})
	case "/api/protocols":
		history, err := s.monitor.ProtocolHistory(request.Context(), time.Now().UTC().Add(-30*time.Minute))
		if err != nil {
			writeError(writer, http.StatusInternalServerError, "failed to load protocol history")
			return
		}
		writeJSON(writer, http.StatusOK, map[string]any{"protocols": s.monitor.Snapshot().Protocols, "history": history})
	case "/api/policies":
		writeJSON(writer, http.StatusOK, map[string]any{"policies": s.monitor.Snapshot().Policies})
	case "/api/routes":
		writeJSON(writer, http.StatusOK, map[string]any{"routes": s.monitor.Snapshot().Routes})
	default:
		if strings.HasPrefix(request.URL.Path, "/api/interfaces/") && request.Method == http.MethodGet {
			name, err := url.PathUnescape(strings.TrimPrefix(request.URL.Path, "/api/interfaces/"))
			if err != nil || name == "" {
				writeError(writer, http.StatusBadRequest, "invalid interface name")
				return
			}
			detail, ok, err := s.monitor.InterfaceDetail(request.Context(), name, time.Now().UTC().Add(-time.Hour))
			if err != nil {
				writeError(writer, http.StatusInternalServerError, "failed to load interface detail")
				return
			}
			if !ok {
				writeError(writer, http.StatusNotFound, "interface not found")
				return
			}
			writeJSON(writer, http.StatusOK, detail)
			return
		}
		if strings.HasPrefix(request.URL.Path, "/api/terminals/") {
			s.serveTerminalAPI(writer, request)
			return
		}
		writeError(writer, http.StatusNotFound, "not found")
	}
}

func parseWindow(value string) time.Duration {
	switch value {
	case "1d":
		return 24 * time.Hour
	case "1w":
		return 7 * 24 * time.Hour
	case "1m":
		return 31 * 24 * time.Hour
	default:
		return time.Hour
	}
}

func (s *Server) serveTerminalAPI(writer http.ResponseWriter, request *http.Request) {
	trimmed := strings.TrimPrefix(request.URL.Path, "/api/terminals/")
	parts := strings.Split(strings.Trim(trimmed, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(writer, http.StatusNotFound, "not found")
		return
	}

	terminalID, err := url.PathUnescape(parts[0])
	if err != nil {
		writeError(writer, http.StatusBadRequest, "invalid terminal id")
		return
	}

	if len(parts) == 1 && request.Method == http.MethodGet {
		detail, ok := s.monitor.TerminalDetail(terminalID)
		if !ok {
			writeError(writer, http.StatusNotFound, "terminal not found")
			return
		}
		writeJSON(writer, http.StatusOK, detail)
		return
	}

	if len(parts) == 2 && parts[1] == "remark" && request.Method == http.MethodPost {
		var payload struct {
			Remark string `json:"remark"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil && !errors.Is(err, http.ErrBodyNotAllowed) {
			writeError(writer, http.StatusBadRequest, "invalid json body")
			return
		}
		if err := s.monitor.UpdateTerminalRemark(request.Context(), terminalID, strings.TrimSpace(payload.Remark)); err != nil {
			if errors.Is(err, store.ErrTerminalNotFound) {
				writeError(writer, http.StatusNotFound, "terminal not found")
				return
			}
			writeError(writer, http.StatusInternalServerError, "failed to update remark")
			return
		}
		detail, ok := s.monitor.TerminalDetail(terminalID)
		if !ok {
			writeError(writer, http.StatusNotFound, "terminal not found")
			return
		}
		writeJSON(writer, http.StatusOK, detail)
		return
	}

	if len(parts) == 2 && parts[1] == "metadata" && request.Method == http.MethodPost {
		var payload struct {
			CustomName string `json:"customName"`
			Remark     string `json:"remark"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			writeError(writer, http.StatusBadRequest, "invalid json body")
			return
		}
		payload.CustomName = strings.TrimSpace(payload.CustomName)
		payload.Remark = strings.TrimSpace(payload.Remark)
		if utf8.RuneCountInString(payload.CustomName) > 100 {
			writeError(writer, http.StatusBadRequest, "device name is too long")
			return
		}
		if utf8.RuneCountInString(payload.Remark) > 500 {
			writeError(writer, http.StatusBadRequest, "remark is too long")
			return
		}
		detail, err := s.monitor.UpdateTerminalMetadata(request.Context(), terminalID, payload.CustomName, payload.Remark)
		if errors.Is(err, store.ErrTerminalNotFound) {
			writeError(writer, http.StatusNotFound, "terminal not found")
			return
		}
		if err != nil {
			writeError(writer, http.StatusInternalServerError, "failed to update terminal metadata")
			return
		}
		writeJSON(writer, http.StatusOK, detail)
		return
	}

	writeError(writer, http.StatusNotFound, "not found")
}

func (s *Server) serveApp(writer http.ResponseWriter, request *http.Request) {
	cleanPath := path.Clean(strings.TrimPrefix(request.URL.Path, "/"))
	if cleanPath == "." || cleanPath == "/" {
		cleanPath = "index.html"
	}

	if _, err := fs.Stat(s.assets, cleanPath); err == nil {
		s.fileServer.ServeHTTP(writer, request)
		return
	}

	index, err := fs.ReadFile(s.assets, "index.html")
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "frontend assets unavailable")
		return
	}
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(index)
}

func (s *Server) allowed(request *http.Request) bool {
	if len(s.allowedCIDRs) == 0 {
		return true
	}
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		host = request.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	for _, network := range s.allowedCIDRs {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func parseAllowedCIDRs(values []string) []*net.IPNet {
	result := make([]*net.IPNet, 0, len(values))
	for _, value := range values {
		_, network, err := net.ParseCIDR(strings.TrimSpace(value))
		if err == nil {
			result = append(result, network)
		}
	}
	return result
}

func writeJSON(writer http.ResponseWriter, status int, payload any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(payload)
}

func writeError(writer http.ResponseWriter, status int, message string) {
	writeJSON(writer, status, map[string]string{"error": message})
}
