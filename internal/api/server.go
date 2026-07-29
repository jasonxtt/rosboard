package api

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"rosboard/internal/auth"
	"rosboard/internal/config"
	"rosboard/internal/service"
	"rosboard/internal/store"
)

type Server struct {
	cfgMu        sync.RWMutex
	deviceSaveMu sync.Mutex
	cfg          config.Config
	monitor      *service.Monitor
	manager      *service.MonitorManager
	store        *store.Store
	assets       fs.FS
	allowedCIDRs []*net.IPNet
	fileServer   http.Handler
	restart      func()
	auth         *auth.Service
	tickets      *verificationTickets
	provisioning *provisioningSessions
}

func NewServer(cfg config.Config, monitor *service.Monitor, assets fs.FS) *Server {
	return NewServerWithRestart(cfg, monitor, assets, nil)
}

func NewServerWithProvisioning(cfg config.Config, monitor *service.Monitor, assets fs.FS, restart func()) *Server {
	return &Server{
		cfg:          cfg,
		monitor:      monitor,
		assets:       assets,
		allowedCIDRs: parseAllowedCIDRs(cfg.AllowedCIDRs),
		fileServer:   http.FileServer(http.FS(assets)),
		restart:      restart,
		tickets:      newVerificationTickets(),
		provisioning: newProvisioningSessions(),
	}
}

func NewServerWithRestart(cfg config.Config, monitor *service.Monitor, assets fs.FS, restart func()) *Server {
	return &Server{
		cfg:          cfg,
		monitor:      monitor,
		assets:       assets,
		allowedCIDRs: parseAllowedCIDRs(cfg.AllowedCIDRs),
		fileServer:   http.FileServer(http.FS(assets)),
		restart:      restart,
		tickets:      newVerificationTickets(),
		provisioning: newProvisioningSessions(),
	}
}

func NewServerWithManager(cfg config.Config, manager *service.MonitorManager, storage *store.Store, assets fs.FS, restart func()) *Server {
	var authService *auth.Service
	if storage != nil {
		authService = auth.New(storage)
	}
	return &Server{
		cfg:          cfg,
		manager:      manager,
		store:        storage,
		assets:       assets,
		allowedCIDRs: parseAllowedCIDRs(cfg.AllowedCIDRs),
		fileServer:   http.FileServer(http.FS(assets)),
		restart:      restart,
		auth:         authService,
		tickets:      newVerificationTickets(),
		provisioning: newProvisioningSessions(),
	}
}

func NewServerWithAuth(cfg config.Config, manager *service.MonitorManager, storage *store.Store, assets fs.FS, restart func(), authService *auth.Service) *Server {
	server := NewServerWithManager(cfg, manager, storage, assets, restart)
	server.auth = authService
	return server
}

func (s *Server) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if strings.HasPrefix(request.URL.Path, "/api/") {
		setSecurityHeaders(writer)
		if !s.allowed(request) {
			writeError(writer, http.StatusForbidden, "forbidden")
			return
		}
		if s.auth != nil && !sameOriginWrite(request) {
			writeError(writer, http.StatusForbidden, "cross-origin request denied")
			return
		}
		if s.auth != nil && !publicAPI(request.URL.Path, request.Method) {
			session, ok := s.authenticateRequest(writer, request)
			if !ok {
				return
			}
			request = request.WithContext(withRequestSession(request.Context(), session))
			phaseAllowed, phaseErr := s.phaseAllows(request)
			if phaseErr != nil {
				writeAPIError(writer, http.StatusInternalServerError, "setup_state_failed", "failed to load setup state")
				return
			}
			if !phaseAllowed {
				writeAPIError(writer, http.StatusConflict, "onboarding_required", "complete setup before using this API")
				return
			}
		}
		s.serveAPI(writer, request)
		return
	}

	s.serveApp(writer, request)
}

func (s *Server) serveAPI(writer http.ResponseWriter, request *http.Request) {
	if s.serveAuthAPI(writer, request) {
		return
	}
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
	if request.URL.Path == "/api/settings/full-reset" {
		s.serveFullReset(writer, request)
		return
	}
	if request.URL.Path == "/api/devices" && request.Method == http.MethodGet {
		writeJSON(writer, http.StatusOK, map[string]any{"devices": s.deviceStatuses(false), "archivedDevices": s.deviceStatuses(true)})
		return
	}
	if request.URL.Path == "/api/device-onboarding/sessions" {
		s.serveCreateProvisioningSession(writer, request)
		return
	}
	if strings.HasPrefix(request.URL.Path, "/api/device-onboarding/sessions/") {
		s.serveCompleteProvisioning(writer, request)
		return
	}
	if request.URL.Path == "/api/devices/test-connection" {
		s.serveDeviceConnectionTest(writer, request)
		return
	}
	if request.URL.Path == "/api/devices" || strings.HasPrefix(request.URL.Path, "/api/devices/") {
		s.serveDeviceAPI(writer, request)
		return
	}
	if request.URL.Path == "/api/health" {
		writeJSON(writer, http.StatusOK, map[string]any{"ok": true})
		return
	}
	monitor, monitorErr := s.monitorFor(request)
	if monitorErr != nil && request.URL.Path != "/api/settings" {
		if errors.Is(monitorErr, service.ErrDeviceNotFound) {
			writeError(writer, http.StatusNotFound, monitorErr.Error())
			return
		}
		if errors.Is(monitorErr, service.ErrDeviceUnavailable) {
			writeError(writer, http.StatusServiceUnavailable, monitorErr.Error())
			return
		}
		writeJSON(writer, http.StatusServiceUnavailable, map[string]any{"setupRequired": true, "error": "routeros is not configured"})
		return
	}
	switch request.URL.Path {
	case "/api/overview":
		writeJSON(writer, http.StatusOK, monitor.Snapshot().Overview)
	case "/api/realtime":
		writeJSON(writer, http.StatusOK, monitor.Snapshot().Overview)
	case "/api/viewer-heartbeat":
		if request.Method != http.MethodPost {
			writer.Header().Set("Allow", http.MethodPost)
			writeError(writer, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		activeUntil := monitor.ViewerHeartbeat()
		if s.manager != nil {
			activeUntil = s.manager.ViewerHeartbeat()
		}
		writeJSON(writer, http.StatusOK, map[string]any{"activeUntil": activeUntil})
	case "/api/terminal-viewer-heartbeat":
		if request.Method != http.MethodPost {
			writer.Header().Set("Allow", http.MethodPost)
			writeError(writer, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		writeJSON(writer, http.StatusOK, map[string]any{"activeUntil": monitor.TerminalViewerHeartbeat()})
	case "/api/interfaces":
		writeJSON(writer, http.StatusOK, map[string]any{"interfaces": monitor.Snapshot().Interfaces})
	case "/api/terminals":
		writeJSON(writer, http.StatusOK, map[string]any{"terminals": monitor.Snapshot().Terminals})
	case "/api/capabilities":
		writeJSON(writer, http.StatusOK, map[string]any{"capabilities": monitor.Snapshot().Capabilities})
	case "/api/dashboard":
		writeJSON(writer, http.StatusOK, monitor.Snapshot())
	case "/api/settings":
		writeJSON(writer, http.StatusOK, s.settingsResponse())
	case "/api/load":
		windowName := request.URL.Query().Get("window")
		window := parseWindow(windowName)
		samples, err := monitor.LoadHistory(request.Context(), time.Now().UTC().Add(-window), loadWindowBucket(windowName))
		if err != nil {
			writeError(writer, http.StatusInternalServerError, "failed to load history")
			return
		}
		writeJSON(writer, http.StatusOK, map[string]any{"samples": samples})
	case "/api/traffic-history":
		windowName, window, bucket, ok := parseTrafficWindow(request.URL.Query().Get("window"))
		if !ok {
			writeError(writer, http.StatusBadRequest, "window must be one of 5m, 1h, 6h, or 24h")
			return
		}
		samples, err := monitor.TrafficHistory(request.Context(), time.Now().UTC().Add(-window), bucket)
		if err != nil {
			writeError(writer, http.StatusInternalServerError, "failed to load traffic history")
			return
		}
		writeJSON(writer, http.StatusOK, map[string]any{"window": windowName, "samples": samples, "trafficInterfaces": monitor.Snapshot().Overview.TrafficInterfaces})
	case "/api/protocols":
		history, err := monitor.ProtocolHistory(request.Context(), time.Now().UTC().Add(-30*time.Minute))
		if err != nil {
			writeError(writer, http.StatusInternalServerError, "failed to load protocol history")
			return
		}
		writeJSON(writer, http.StatusOK, map[string]any{"protocols": monitor.Snapshot().Protocols, "history": history})
	case "/api/policies":
		writeJSON(writer, http.StatusOK, map[string]any{"policies": monitor.Snapshot().Policies})
	case "/api/routes":
		writeJSON(writer, http.StatusOK, map[string]any{"routes": monitor.Snapshot().Routes})
	default:
		if strings.HasPrefix(request.URL.Path, "/api/interfaces/") && request.Method == http.MethodGet {
			name, err := url.PathUnescape(strings.TrimPrefix(request.URL.Path, "/api/interfaces/"))
			if err != nil || name == "" {
				writeError(writer, http.StatusBadRequest, "invalid interface name")
				return
			}
			detail, ok, err := monitor.InterfaceDetail(request.Context(), name, time.Now().UTC().Add(-time.Hour))
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
			s.serveTerminalAPI(writer, request, monitor)
			return
		}
		writeError(writer, http.StatusNotFound, "not found")
	}
}

type settingsResponse struct {
	Connection  settingsConnection  `json:"connection"`
	Collection  settingsCollection  `json:"collection"`
	Diagnostics settingsDiagnostics `json:"diagnostics"`
	Devices     []settingsDevice    `json:"devices"`
}

type settingsDevice struct {
	ID                string                     `json:"id"`
	Name              string                     `json:"name"`
	Enabled           bool                       `json:"enabled"`
	Archived          bool                       `json:"archived"`
	Scheme            string                     `json:"scheme"`
	Host              string                     `json:"host"`
	Port              int                        `json:"port"`
	Username          string                     `json:"username"`
	PasswordSet       bool                       `json:"passwordSet"`
	TrafficInterfaces []string                   `json:"trafficInterfaces"`
	TrafficScope      config.TrafficScopeConfig  `json:"trafficScope"`
	TerminalCIDRs     []string                   `json:"terminalCidrs"`
	TerminalScope     config.TerminalScopeConfig `json:"terminalScope"`
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
	RouterOSPasswordSet bool     `json:"routerosPasswordSet"`
}

type settingsCollection struct {
	PollIntervalSeconds         int `json:"pollIntervalSeconds"`
	RealtimePollIntervalSeconds int `json:"realtimePollIntervalSeconds"`
	TerminalPollIntervalSeconds int `json:"terminalPollIntervalSeconds"`
	SampleRetentionHours        int `json:"sampleRetentionHours"`
}

type settingsDiagnostics struct {
	RouterName string    `json:"routerName"`
	Version    string    `json:"version"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

func (s *Server) settingsResponse() settingsResponse {
	cfg := s.configSnapshot()

	var overview serviceOverview
	monitor := s.monitor
	if s.manager != nil {
		monitor, _ = s.manager.Monitor("")
	}
	if monitor != nil {
		snapshot := monitor.Snapshot()
		overview = serviceOverview{
			RouterName: snapshot.Overview.RouterName,
			Version:    snapshot.Overview.Version,
			UpdatedAt:  snapshot.Overview.UpdatedAt,
		}
	}
	scheme, host, port := routerOSConnectionParts(cfg.RouterOS.BaseURL)
	devices := make([]settingsDevice, 0, len(cfg.Devices))
	for _, device := range cfg.Devices {
		deviceScheme, deviceHost, devicePort := routerOSConnectionParts(device.RouterOS.BaseURL)
		devices = append(devices, settingsDevice{
			ID: device.ID, Name: device.Name, Enabled: device.Enabled, Archived: device.Archived,
			Scheme: deviceScheme, Host: deviceHost, Port: devicePort, Username: device.RouterOS.Username,
			PasswordSet:       strings.TrimSpace(device.RouterOS.Password) != "",
			TrafficInterfaces: cloneStrings(device.RouterOS.TrafficInterfaces), TrafficScope: cloneTrafficScope(device.RouterOS.TrafficScope), TerminalCIDRs: cloneStrings(device.RouterOS.TerminalCIDRs), TerminalScope: cloneTerminalScope(device.RouterOS.TerminalScope),
		})
	}
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
			RouterOSPasswordSet: strings.TrimSpace(cfg.RouterOS.Password) != "",
		},
		Collection: settingsCollection{
			PollIntervalSeconds:         cfg.PollIntervalSeconds,
			RealtimePollIntervalSeconds: cfg.RealtimePollIntervalSeconds,
			TerminalPollIntervalSeconds: cfg.TerminalPollIntervalSeconds,
			SampleRetentionHours:        cfg.SampleRetentionHours,
		},
		Diagnostics: settingsDiagnostics{
			RouterName: overview.RouterName,
			Version:    overview.Version,
			UpdatedAt:  overview.UpdatedAt,
		},
		Devices: devices,
	}
}

type serviceOverview struct {
	RouterName string
	Version    string
	UpdatedAt  time.Time
}

func (s *Server) monitorFor(request *http.Request) (*service.Monitor, error) {
	if s.manager != nil {
		return s.manager.Monitor(strings.TrimSpace(request.URL.Query().Get("device")))
	}
	if s.monitor != nil {
		return s.monitor, nil
	}
	return nil, service.ErrDeviceUnavailable
}

func (s *Server) deviceStatuses(includeArchived bool) []service.DeviceStatus {
	cfg := s.configSnapshot()
	if s.manager != nil {
		return s.manager.Statuses(includeArchived, cfg.Devices)
	}
	statuses := make([]service.DeviceStatus, 0, len(cfg.Devices))
	for _, device := range cfg.Devices {
		if device.Archived && !includeArchived {
			continue
		}
		status := service.DeviceStatus{ID: device.ID, Name: device.Name, Enabled: device.Enabled, Archived: device.Archived}
		if s.monitor != nil && device.ID == config.DefaultDeviceID {
			overview := s.monitor.Snapshot().Overview
			status.Healthy = true
			status.RouterName = overview.RouterName
			status.Version = overview.Version
			status.UpdatedAt = overview.UpdatedAt
		}
		statuses = append(statuses, status)
	}
	return statuses
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
		methodNotAllowed(writer, http.MethodPost)
		return
	}
	writeAPIError(writer, http.StatusGone, "device_api_required", "use the verified device API to save RouterOS connections")
}

type collectionSettingsRequest struct {
	PollIntervalSeconds         int `json:"pollIntervalSeconds"`
	RealtimePollIntervalSeconds int `json:"realtimePollIntervalSeconds"`
	TerminalPollIntervalSeconds int `json:"terminalPollIntervalSeconds"`
	SampleRetentionHours        int `json:"sampleRetentionHours"`
}

type deviceSettingsRequest struct {
	Name               string                     `json:"name"`
	Enabled            bool                       `json:"enabled"`
	Scheme             string                     `json:"scheme"`
	Host               string                     `json:"host"`
	Port               int                        `json:"port"`
	Username           string                     `json:"username"`
	Password           string                     `json:"password"`
	TrafficInterfaces  []string                   `json:"trafficInterfaces"`
	TrafficScope       config.TrafficScopeConfig  `json:"trafficScope"`
	TerminalCIDRs      []string                   `json:"terminalCidrs"`
	TerminalScope      config.TerminalScopeConfig `json:"terminalScope"`
	VerificationToken  string                     `json:"verificationToken"`
	CompleteOnboarding bool                       `json:"completeOnboarding"`
	DeferRestart       bool                       `json:"deferRestart"`
	Confirmation       string                     `json:"confirmation"`
}

func (s *Server) shouldDeferDeviceRestart(ctx context.Context, payload deviceSettingsRequest) (bool, error) {
	if !payload.DeferRestart || payload.CompleteOnboarding || s.auth == nil {
		return false, nil
	}
	complete, err := s.auth.OnboardingComplete(ctx)
	if err != nil {
		return false, err
	}
	return !complete, nil
}

func (s *Server) serveDeviceAPI(writer http.ResponseWriter, request *http.Request) {
	if strings.TrimSpace(s.configSnapshot().Path) == "" {
		writeError(writer, http.StatusBadRequest, "config path is required to save settings")
		return
	}
	if request.URL.Path == "/api/devices" {
		if request.Method != http.MethodPost {
			writer.Header().Set("Allow", http.MethodGet+", "+http.MethodPost)
			writeError(writer, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var payload deviceSettingsRequest
		if err := decodeJSONBody(writer, request, &payload); err != nil {
			return
		}
		deferRestart, err := s.shouldDeferDeviceRestart(request.Context(), payload)
		if err != nil {
			writeAPIError(writer, http.StatusInternalServerError, "setup_state_failed", "failed to load setup state")
			return
		}
		s.deviceSaveMu.Lock()
		defer s.deviceSaveMu.Unlock()
		device, consumeTicket, err := s.prepareDevice(request.Context(), uuid.NewString(), payload, nil)
		if err != nil {
			writeDeviceValidationError(writer, err)
			return
		}
		if err := s.saveSettings(func(next *config.Config) { next.Devices = append(next.Devices, device) }); err != nil {
			writeError(writer, http.StatusInternalServerError, "failed to save device")
			return
		}
		if consumeTicket {
			s.tickets.consume(payload.VerificationToken)
		}
		if s.auth != nil && payload.CompleteOnboarding {
			if err := s.auth.CompleteOnboarding(request.Context()); err != nil {
				writeAPIError(writer, http.StatusInternalServerError, "setup_completion_failed", "device was saved but setup could not be completed")
				return
			}
		}
		if !deferRestart {
			s.scheduleRestart()
		}
		writeJSON(writer, http.StatusCreated, map[string]any{"id": device.ID, "restarting": !deferRestart && s.restart != nil})
		return
	}

	relative := strings.TrimPrefix(request.URL.Path, "/api/devices/")
	parts := strings.Split(relative, "/")
	deviceID, err := url.PathUnescape(parts[0])
	if err != nil || strings.TrimSpace(deviceID) == "" {
		writeError(writer, http.StatusBadRequest, "invalid device id")
		return
	}
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	switch {
	case request.Method == http.MethodPut && action == "":
		var payload deviceSettingsRequest
		if err := decodeJSONBody(writer, request, &payload); err != nil {
			return
		}
		deferRestart, err := s.shouldDeferDeviceRestart(request.Context(), payload)
		if err != nil {
			writeAPIError(writer, http.StatusInternalServerError, "setup_state_failed", "failed to load setup state")
			return
		}
		s.deviceSaveMu.Lock()
		defer s.deviceSaveMu.Unlock()
		cfg := s.configSnapshot()
		existing, found := cfg.Device(deviceID)
		if !found {
			writeError(writer, http.StatusNotFound, "device not found")
			return
		}
		device, consumeTicket, err := s.prepareDevice(request.Context(), deviceID, payload, &existing)
		if err != nil {
			writeDeviceValidationError(writer, err)
			return
		}
		found = false
		err = s.saveSettings(func(next *config.Config) {
			for index := range next.Devices {
				if next.Devices[index].ID == deviceID {
					device.Archived = next.Devices[index].Archived
					next.Devices[index] = device
					found = true
					break
				}
			}
		})
		if err != nil {
			writeError(writer, http.StatusInternalServerError, "failed to save device")
			return
		}
		if !found {
			writeError(writer, http.StatusNotFound, "device not found")
			return
		}
		if consumeTicket {
			s.tickets.consume(payload.VerificationToken)
		}
		if s.auth != nil && payload.CompleteOnboarding {
			if err := s.auth.CompleteOnboarding(request.Context()); err != nil {
				writeAPIError(writer, http.StatusInternalServerError, "setup_completion_failed", "device was saved but setup could not be completed")
				return
			}
		}
		if deferRestart {
			writeJSON(writer, http.StatusOK, map[string]any{"ok": true, "id": deviceID, "restarting": false})
			return
		}
	case request.Method == http.MethodDelete && action == "":
		found := false
		err := s.saveSettings(func(next *config.Config) {
			for index := range next.Devices {
				if next.Devices[index].ID == deviceID {
					next.Devices[index].Archived = true
					next.Devices[index].Enabled = false
					found = true
					break
				}
			}
		})
		if err != nil {
			writeError(writer, http.StatusInternalServerError, "failed to archive device")
			return
		}
		if !found {
			writeError(writer, http.StatusNotFound, "device not found")
			return
		}
	case request.Method == http.MethodPost && action == "restore":
		found := false
		err := s.saveSettings(func(next *config.Config) {
			for index := range next.Devices {
				if next.Devices[index].ID == deviceID {
					next.Devices[index].Archived = false
					found = true
					break
				}
			}
		})
		if err != nil {
			writeError(writer, http.StatusInternalServerError, "failed to restore device")
			return
		}
		if !found {
			writeError(writer, http.StatusNotFound, "device not found")
			return
		}
	case request.Method == http.MethodDelete && action == "data":
		var payload deviceSettingsRequest
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			writeError(writer, http.StatusBadRequest, "invalid json body")
			return
		}
		cfg := s.configSnapshot()
		device, found := cfg.Device(deviceID)
		if !found {
			writeError(writer, http.StatusNotFound, "device not found")
			return
		}
		if !device.Archived || payload.Confirmation != device.Name {
			writeError(writer, http.StatusBadRequest, "archived device name confirmation is required")
			return
		}
		if s.store == nil {
			writeError(writer, http.StatusServiceUnavailable, "device data store is unavailable")
			return
		}
		if err := s.store.PurgeDevice(request.Context(), deviceID); err != nil {
			writeError(writer, http.StatusInternalServerError, "failed to purge device data")
			return
		}
		if err := s.saveSettings(func(next *config.Config) {
			filtered := next.Devices[:0]
			for _, candidate := range next.Devices {
				if candidate.ID != deviceID {
					filtered = append(filtered, candidate)
				}
			}
			next.Devices = filtered
			if len(next.Devices) == 0 {
				next.RouterOS = config.RouterOSConfig{}
			}
		}); err != nil {
			writeError(writer, http.StatusInternalServerError, "device data was purged but settings cleanup failed")
			return
		}
	default:
		writeError(writer, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	s.scheduleRestart()
	writeJSON(writer, http.StatusOK, map[string]any{"ok": true, "restarting": s.restart != nil})
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

func (s *Server) serveFullReset(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(writer, http.MethodPost)
		return
	}
	if s.store == nil || s.auth == nil {
		writeAPIError(writer, http.StatusServiceUnavailable, "full_reset_unavailable", "full reset is unavailable")
		return
	}
	if strings.TrimSpace(s.configSnapshot().Path) == "" {
		writeAPIError(writer, http.StatusBadRequest, "config_path_required", "config path is required for full reset")
		return
	}
	var payload struct {
		Confirmed bool `json:"confirmed"`
	}
	if err := decodeJSONBody(writer, request, &payload); err != nil {
		return
	}
	if !payload.Confirmed {
		writeAPIError(writer, http.StatusBadRequest, "confirmation_required", "full reset must be confirmed")
		return
	}

	s.deviceSaveMu.Lock()
	defer s.deviceSaveMu.Unlock()
	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()
	previous := s.cfg
	if err := os.Remove(previous.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
		writeAPIError(writer, http.StatusInternalServerError, "full_reset_failed", "failed to remove configuration")
		return
	}
	if err := s.store.ResetAll(request.Context()); err != nil {
		if restoreErr := config.Save(previous.Path, previous); restoreErr != nil {
			writeAPIError(writer, http.StatusInternalServerError, "full_reset_failed", "full reset failed and device settings could not be restored")
			return
		}
		writeAPIError(writer, http.StatusInternalServerError, "full_reset_failed", "full reset failed; configuration was restored")
		return
	}
	resetConfig, err := config.Load(previous.Path)
	if err != nil {
		writeAPIError(writer, http.StatusInternalServerError, "full_reset_failed", "data was reset but default configuration could not be loaded")
		return
	}
	s.cfg = resetConfig
	if s.restart != nil {
		_ = s.store.Close()
	}
	s.tickets.clear()
	s.provisioning.clear()
	clearSessionCookie(writer, request)
	s.scheduleRestart()
	writeJSON(writer, http.StatusOK, map[string]any{"ok": true, "phase": "needs_admin", "restarting": s.restart != nil})
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
	next.AllowedCIDRs = cloneStrings(s.cfg.AllowedCIDRs)
	next.RouterOS.TrafficInterfaces = cloneStrings(s.cfg.RouterOS.TrafficInterfaces)
	next.RouterOS.TrafficScope = cloneTrafficScope(s.cfg.RouterOS.TrafficScope)
	next.RouterOS.TerminalCIDRs = cloneStrings(s.cfg.RouterOS.TerminalCIDRs)
	next.RouterOS.TerminalScope = cloneTerminalScope(s.cfg.RouterOS.TerminalScope)
	next.Devices = append([]config.DeviceConfig(nil), s.cfg.Devices...)
	for index := range next.Devices {
		next.Devices[index].RouterOS.TrafficInterfaces = cloneStrings(s.cfg.Devices[index].RouterOS.TrafficInterfaces)
		next.Devices[index].RouterOS.TrafficScope = cloneTrafficScope(s.cfg.Devices[index].RouterOS.TrafficScope)
		next.Devices[index].RouterOS.TerminalCIDRs = cloneStrings(s.cfg.Devices[index].RouterOS.TerminalCIDRs)
		next.Devices[index].RouterOS.TerminalScope = cloneTerminalScope(s.cfg.Devices[index].RouterOS.TerminalScope)
	}
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

func parseTrafficWindow(value string) (string, time.Duration, time.Duration, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "5m", "5min":
		return "5m", 5 * time.Minute, time.Second, true
	case "1h":
		return "1h", time.Hour, 10 * time.Second, true
	case "6h":
		return "6h", 6 * time.Hour, time.Minute, true
	case "24h":
		return "24h", 24 * time.Hour, 4 * time.Minute, true
	default:
		return "", 0, 0, false
	}
}

func routerOSBaseURL(payload connectionSettingsRequest) (string, error) {
	if strings.TrimSpace(payload.Username) == "" {
		return "", errors.New("username is required")
	}
	if payload.Password == "" {
		return "", errors.New("password is required")
	}
	return normalizedRouterOSURL(payload.Scheme, payload.Host, payload.Port)
}

func normalizedRouterOSURL(rawScheme, rawHost string, rawPort int) (string, error) {
	scheme := strings.ToLower(strings.TrimSpace(rawScheme))
	if scheme == "" {
		scheme = "http"
	}
	if scheme != "http" && scheme != "https" {
		return "", errors.New("scheme must be http or https")
	}
	host := strings.TrimSpace(rawHost)
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = strings.TrimSpace(host[1 : len(host)-1])
	}
	if host == "" {
		return "", errors.New("host is required")
	}
	port := rawPort
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

func cloneTrafficScope(scope config.TrafficScopeConfig) config.TrafficScopeConfig {
	scope.IncludeInterfaces = cloneStrings(scope.IncludeInterfaces)
	scope.ExcludeInterfaces = cloneStrings(scope.ExcludeInterfaces)
	return scope
}

func cloneTerminalScope(scope config.TerminalScopeConfig) config.TerminalScopeConfig {
	scope.IncludeInterfaces = cloneStrings(scope.IncludeInterfaces)
	scope.ExcludeInterfaces = cloneStrings(scope.ExcludeInterfaces)
	scope.IncludeCIDRs = cloneStrings(scope.IncludeCIDRs)
	scope.ExcludeCIDRs = cloneStrings(scope.ExcludeCIDRs)
	return scope
}

func cloneStrings(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	return append([]string(nil), values...)
}

func parseWindow(value string) time.Duration {
	switch value {
	case "5m", "5min":
		return 5 * time.Minute
	case "6h":
		return 6 * time.Hour
	case "24h":
		return 24 * time.Hour
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

func loadWindowBucket(value string) time.Duration {
	switch value {
	case "24h", "1d":
		return 4 * time.Minute
	case "1w":
		return 30 * time.Minute
	case "1m":
		return 3 * time.Hour
	default:
		return time.Minute
	}
}

func (s *Server) serveTerminalAPI(writer http.ResponseWriter, request *http.Request, monitor *service.Monitor) {
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
		detail, ok := monitor.TerminalDetail(terminalID)
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
		if err := monitor.UpdateTerminalRemark(request.Context(), terminalID, strings.TrimSpace(payload.Remark)); err != nil {
			if errors.Is(err, store.ErrTerminalNotFound) {
				writeError(writer, http.StatusNotFound, "terminal not found")
				return
			}
			writeError(writer, http.StatusInternalServerError, "failed to update remark")
			return
		}
		detail, ok := monitor.TerminalDetail(terminalID)
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
		detail, err := monitor.UpdateTerminalMetadata(request.Context(), terminalID, payload.CustomName, payload.Remark)
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
