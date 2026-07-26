package api

import (
	"encoding/json"
	"errors"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"rosboard/internal/config"
	"rosboard/internal/service"
	"rosboard/internal/store"
)

type Server struct {
	cfgMu        sync.RWMutex
	cfg          config.Config
	monitor      *service.Monitor
	assets       fs.FS
	allowedCIDRs []*net.IPNet
	fileServer   http.Handler
	restart      func()
}

func NewServer(cfg config.Config, monitor *service.Monitor, assets fs.FS) *Server {
	return NewServerWithRestart(cfg, monitor, assets, nil)
}

func NewServerWithRestart(cfg config.Config, monitor *service.Monitor, assets fs.FS, restart func()) *Server {
	return &Server{
		cfg:          cfg,
		monitor:      monitor,
		assets:       assets,
		allowedCIDRs: parseAllowedCIDRs(cfg.AllowedCIDRs),
		fileServer:   http.FileServer(http.FS(assets)),
		restart:      restart,
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
	if request.URL.Path == "/api/settings/connection" {
		s.serveConnectionSettings(writer, request)
		return
	}
	if request.URL.Path == "/api/settings/collection" {
		s.serveCollectionSettings(writer, request)
		return
	}
	if request.URL.Path == "/api/settings/restart" {
		s.serveRestart(writer, request)
		return
	}
	if s.monitor == nil && request.URL.Path != "/api/health" && request.URL.Path != "/api/settings" {
		writeJSON(writer, http.StatusServiceUnavailable, map[string]any{"setupRequired": true, "error": "routeros is not configured"})
		return
	}
	switch request.URL.Path {
	case "/api/health":
		writeJSON(writer, http.StatusOK, map[string]any{"ok": true})
	case "/api/overview":
		writeJSON(writer, http.StatusOK, s.monitor.Snapshot().Overview)
	case "/api/realtime":
		writeJSON(writer, http.StatusOK, s.monitor.Snapshot().Overview)
	case "/api/viewer-heartbeat":
		if request.Method != http.MethodPost {
			writer.Header().Set("Allow", http.MethodPost)
			writeError(writer, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		writeJSON(writer, http.StatusOK, map[string]any{"activeUntil": s.monitor.ViewerHeartbeat()})
	case "/api/interfaces":
		writeJSON(writer, http.StatusOK, map[string]any{"interfaces": s.monitor.Snapshot().Interfaces})
	case "/api/terminals":
		writeJSON(writer, http.StatusOK, map[string]any{"terminals": s.monitor.Snapshot().Terminals})
	case "/api/capabilities":
		writeJSON(writer, http.StatusOK, map[string]any{"capabilities": s.monitor.Snapshot().Capabilities})
	case "/api/dashboard":
		writeJSON(writer, http.StatusOK, s.monitor.Snapshot())
	case "/api/settings":
		writeJSON(writer, http.StatusOK, s.settingsResponse())
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

type settingsResponse struct {
	Connection  settingsConnection  `json:"connection"`
	Collection  settingsCollection  `json:"collection"`
	Diagnostics settingsDiagnostics `json:"diagnostics"`
}

type settingsConnection struct {
	APIBasePath         string   `json:"apiBasePath"`
	Configured          bool     `json:"configured"`
	ListenAddress       string   `json:"listenAddress"`
	AllowedCIDRs        []string `json:"allowedCidrs"`
	RouterOSBaseURL     string   `json:"routerosBaseUrl"`
	RouterOSScheme      string   `json:"routerosScheme"`
	RouterOSHost        string   `json:"routerosHost"`
	RouterOSPort        int      `json:"routerosPort"`
	RouterOSUsername    string   `json:"routerosUsername"`
	RouterOSPassword    string   `json:"routerosPassword"`
	RouterOSPasswordSet bool     `json:"routerosPasswordSet"`
}

type settingsCollection struct {
	PollIntervalSeconds         int      `json:"pollIntervalSeconds"`
	RealtimePollIntervalSeconds int      `json:"realtimePollIntervalSeconds"`
	TerminalPollIntervalSeconds int      `json:"terminalPollIntervalSeconds"`
	SampleRetentionHours        int      `json:"sampleRetentionHours"`
	TrafficInterfaces           []string `json:"trafficInterfaces"`
	TerminalCIDRs               []string `json:"terminalCidrs"`
}

type settingsDiagnostics struct {
	RouterName string    `json:"routerName"`
	Version    string    `json:"version"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

func (s *Server) settingsResponse() settingsResponse {
	cfg := s.configSnapshot()

	var overview serviceOverview
	if s.monitor != nil {
		snapshot := s.monitor.Snapshot()
		overview = serviceOverview{
			RouterName: snapshot.Overview.RouterName,
			Version:    snapshot.Overview.Version,
			UpdatedAt:  snapshot.Overview.UpdatedAt,
		}
	}
	scheme, host, port := routerOSConnectionParts(cfg.RouterOS.BaseURL)
	return settingsResponse{
		Connection: settingsConnection{
			APIBasePath:         "/api",
			Configured:          cfg.RouterOSConfigured(),
			ListenAddress:       cfg.ListenAddress,
			AllowedCIDRs:        cloneStrings(cfg.AllowedCIDRs),
			RouterOSBaseURL:     cfg.RouterOS.BaseURL,
			RouterOSScheme:      scheme,
			RouterOSHost:        host,
			RouterOSPort:        port,
			RouterOSUsername:    cfg.RouterOS.Username,
			RouterOSPassword:    cfg.RouterOS.Password,
			RouterOSPasswordSet: strings.TrimSpace(cfg.RouterOS.Password) != "",
		},
		Collection: settingsCollection{
			PollIntervalSeconds:         cfg.PollIntervalSeconds,
			RealtimePollIntervalSeconds: cfg.RealtimePollIntervalSeconds,
			TerminalPollIntervalSeconds: cfg.TerminalPollIntervalSeconds,
			SampleRetentionHours:        cfg.SampleRetentionHours,
			TrafficInterfaces:           cloneStrings(cfg.RouterOS.TrafficInterfaces),
			TerminalCIDRs:               cloneStrings(cfg.RouterOS.TerminalCIDRs),
		},
		Diagnostics: settingsDiagnostics{
			RouterName: overview.RouterName,
			Version:    overview.Version,
			UpdatedAt:  overview.UpdatedAt,
		},
	}
}

type serviceOverview struct {
	RouterName string
	Version    string
	UpdatedAt  time.Time
}

type connectionSettingsRequest struct {
	Scheme   string `json:"scheme"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) serveConnectionSettings(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writer.Header().Set("Allow", http.MethodPost)
		writeError(writer, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if strings.TrimSpace(s.configSnapshot().Path) == "" {
		writeError(writer, http.StatusBadRequest, "config path is required to save settings")
		return
	}

	var payload connectionSettingsRequest
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid json body")
		return
	}
	baseURL, err := routerOSBaseURL(payload)
	if err != nil {
		writeError(writer, http.StatusBadRequest, err.Error())
		return
	}

	if err := s.saveSettings(func(next *config.Config) {
		next.RouterOS.BaseURL = baseURL
		next.RouterOS.Username = strings.TrimSpace(payload.Username)
		next.RouterOS.Password = strings.TrimSpace(payload.Password)
	}); err != nil {
		writeError(writer, http.StatusInternalServerError, "failed to save settings")
		return
	}

	s.scheduleRestart()
	writeJSON(writer, http.StatusOK, map[string]any{"ok": true, "restarting": s.restart != nil})
}

type collectionSettingsRequest struct {
	PollIntervalSeconds         int      `json:"pollIntervalSeconds"`
	RealtimePollIntervalSeconds int      `json:"realtimePollIntervalSeconds"`
	TerminalPollIntervalSeconds int      `json:"terminalPollIntervalSeconds"`
	SampleRetentionHours        int      `json:"sampleRetentionHours"`
	TrafficInterfaces           []string `json:"trafficInterfaces"`
	TerminalCIDRs               []string `json:"terminalCidrs"`
}

func (s *Server) serveCollectionSettings(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writer.Header().Set("Allow", http.MethodPost)
		writeError(writer, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if strings.TrimSpace(s.configSnapshot().Path) == "" {
		writeError(writer, http.StatusBadRequest, "config path is required to save settings")
		return
	}

	var payload collectionSettingsRequest
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid json body")
		return
	}
	if payload.PollIntervalSeconds <= 0 || payload.RealtimePollIntervalSeconds <= 0 || payload.TerminalPollIntervalSeconds <= 0 || payload.SampleRetentionHours <= 0 {
		writeError(writer, http.StatusBadRequest, "collection intervals and retention must be positive")
		return
	}

	if err := s.saveSettings(func(next *config.Config) {
		next.PollIntervalSeconds = payload.PollIntervalSeconds
		next.RealtimePollIntervalSeconds = payload.RealtimePollIntervalSeconds
		next.TerminalPollIntervalSeconds = payload.TerminalPollIntervalSeconds
		next.SampleRetentionHours = payload.SampleRetentionHours
		next.RouterOS.TrafficInterfaces = cleanStrings(payload.TrafficInterfaces)
		next.RouterOS.TerminalCIDRs = cleanStrings(payload.TerminalCIDRs)
	}); err != nil {
		writeError(writer, http.StatusInternalServerError, "failed to save settings")
		return
	}

	s.scheduleRestart()
	writeJSON(writer, http.StatusOK, map[string]any{"ok": true, "restarting": s.restart != nil})
}

func (s *Server) serveRestart(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writer.Header().Set("Allow", http.MethodPost)
		writeError(writer, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.restart == nil {
		writeError(writer, http.StatusServiceUnavailable, "restart is not available")
		return
	}
	s.scheduleRestart()
	writeJSON(writer, http.StatusOK, map[string]any{"ok": true, "restarting": true})
}

func (s *Server) configSnapshot() config.Config {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	return s.cfg
}

func (s *Server) saveSettings(update func(*config.Config)) error {
	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()
	next := s.cfg
	update(&next)
	if err := config.Save(next.Path, next); err != nil {
		return err
	}
	s.cfg = next
	return nil
}

func (s *Server) scheduleRestart() {
	if s.restart == nil {
		return
	}
	go func() {
		time.Sleep(300 * time.Millisecond)
		s.restart()
	}()
}

func cleanStrings(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func routerOSBaseURL(payload connectionSettingsRequest) (string, error) {
	scheme := strings.ToLower(strings.TrimSpace(payload.Scheme))
	if scheme == "" {
		scheme = "http"
	}
	if scheme != "http" && scheme != "https" {
		return "", errors.New("scheme must be http or https")
	}
	host := strings.TrimSpace(payload.Host)
	if host == "" {
		return "", errors.New("host is required")
	}
	if strings.TrimSpace(payload.Username) == "" {
		return "", errors.New("username is required")
	}
	if strings.TrimSpace(payload.Password) == "" {
		return "", errors.New("password is required")
	}
	port := payload.Port
	if port == 0 {
		port = defaultRouterOSRESTPort(scheme)
	}
	if port < 1 || port > 65535 {
		return "", errors.New("port must be between 1 and 65535")
	}
	return (&url.URL{Scheme: scheme, Host: net.JoinHostPort(host, strconv.Itoa(port))}).String(), nil
}

func routerOSConnectionParts(baseURL string) (string, string, int) {
	value := strings.TrimSpace(baseURL)
	if value == "" {
		return "http", "10.0.0.1", 80
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" {
		parsed, _ = url.Parse("http://" + value)
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "https" {
		scheme = "http"
	}
	host := parsed.Hostname()
	if host == "" {
		host = strings.Trim(parsed.Path, "/")
	}
	port := defaultRouterOSRESTPort(scheme)
	if parsed.Port() != "" {
		if parsedPort, err := strconv.Atoi(parsed.Port()); err == nil {
			port = parsedPort
		}
	}
	return scheme, host, port
}

func defaultRouterOSRESTPort(scheme string) int {
	if scheme == "https" {
		return 443
	}
	return 80
}

func cloneStrings(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	return append([]string(nil), values...)
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
