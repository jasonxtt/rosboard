package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"

	"github.com/google/uuid"
	"strings"
	"sync"
	"time"

	"rosboard/internal/config"
	"rosboard/internal/routeros"
	"rosboard/internal/service"
)

const provisioningSessionLifetime = 15 * time.Minute

const provisioningPasswordCharset = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789"

const provisioningPasswordLength = 32

type provisioningSession struct {
	deviceID   string
	deviceName string
	baseURL    string
	scheme     string
	host       string
	port       int
	username   string
	groupName  string
	password   string
	expiresAt  time.Time
}

type provisioningSessions struct {
	mu     sync.Mutex
	now    func() time.Time
	random io.Reader
	items  map[[32]byte]provisioningSession
}

func newProvisioningSessions() *provisioningSessions {
	return &provisioningSessions{now: time.Now, random: rand.Reader, items: make(map[[32]byte]provisioningSession)}
}

func (ps *provisioningSessions) create(deviceID, deviceName, scheme, host string, port int) (sessionID string, session provisioningSession, script string, err error) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.pruneLocked()

	suffix := make([]byte, 8)
	if _, err := io.ReadFull(ps.random, suffix); err != nil {
		return "", provisioningSession{}, "", fmt.Errorf("generate suffix: %w", err)
	}
	hexSuffix := fmt.Sprintf("%x", suffix)

	username := "rosboard_" + hexSuffix
	groupName := "rosboard_g_" + hexSuffix

	password := make([]byte, provisioningPasswordLength)
	charsetLen := big.NewInt(int64(len(provisioningPasswordCharset)))
	for i := range password {
		index, err := rand.Int(ps.random, charsetLen)
		if err != nil {
			return "", provisioningSession{}, "", fmt.Errorf("generate password char: %w", err)
		}
		password[i] = provisioningPasswordCharset[index.Int64()]
	}
	passwordStr := string(password)

	baseURL, err := normalizedRouterOSURL(scheme, host, port)
	if err != nil {
		return "", provisioningSession{}, "", err
	}

	expiresAt := ps.now().UTC().Add(provisioningSessionLifetime)
	session = provisioningSession{
		deviceID:   deviceID,
		deviceName: deviceName,
		baseURL:    baseURL,
		scheme:     scheme,
		host:       host,
		port:       port,
		username:   username,
		groupName:  groupName,
		password:   passwordStr,
		expiresAt:  expiresAt,
	}

	rawID := make([]byte, 32)
	if _, err := io.ReadFull(ps.random, rawID); err != nil {
		return "", provisioningSession{}, "", fmt.Errorf("generate session id: %w", err)
	}
	sessionID = base64.RawURLEncoding.EncodeToString(rawID)
	idHash := sha256.Sum256(rawID)
	ps.items[idHash] = session

	script = provisioningScript(username, groupName, passwordStr)
	return sessionID, session, script, nil
}

func (ps *provisioningSessions) get(sessionID string) (provisioningSession, bool) {
	idHash, err := provisioningSessionHash(sessionID)
	if err != nil {
		return provisioningSession{}, false
	}
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.pruneLocked()
	session, ok := ps.items[idHash]
	if !ok || !session.expiresAt.After(ps.now().UTC()) {
		return provisioningSession{}, false
	}
	return session, true
}

func (ps *provisioningSessions) consume(sessionID string) {
	idHash, err := provisioningSessionHash(sessionID)
	if err != nil {
		return
	}
	ps.mu.Lock()
	delete(ps.items, idHash)
	ps.mu.Unlock()
}

func (ps *provisioningSessions) clear() {
	ps.mu.Lock()
	clear(ps.items)
	ps.mu.Unlock()
}

func (ps *provisioningSessions) pruneLocked() {
	now := ps.now().UTC()
	for key, session := range ps.items {
		if !session.expiresAt.After(now) {
			delete(ps.items, key)
		}
	}
}

func provisioningSessionHash(sessionID string) ([32]byte, error) {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(sessionID))
	if err != nil || len(raw) != 32 {
		return [32]byte{}, errors.New("invalid session id")
	}
	return sha256.Sum256(raw), nil
}

func provisioningScript(username, groupName, password string) string {
	return fmt.Sprintf(`{
:local rbGroupId [/user group find where name="%s"]
:if ([:len $rbGroupId] = 0) do={
    /user group add name="%s" policy=read,write,test,api,rest-api
} else={
    /user group set $rbGroupId policy=read,write,test,api,rest-api
}
:local rbUserId [/user find where name="%s"]
:if ([:len $rbUserId] = 0) do={
    /user add name="%s" password="%s" group="%s" comment="Managed by rosboard"
} else={
    /user set $rbUserId password="%s" group="%s" disabled=no comment="Managed by rosboard"
}
:put ("rosboard account ready: " . "%s")
}
`, groupName, groupName, username, username, password, groupName, password, groupName, username)
}

func provisioningCleanupScript(username, groupName string) string {
	return fmt.Sprintf(`{
:local rbUserId [/user find where name="%s"]
:if ([:len $rbUserId] > 0) do={
    /user remove $rbUserId
}
:local rbGroupId [/user group find where name="%s"]
:if ([:len $rbGroupId] > 0) do={
    :local rbGroupUsers [/user find where group="%s"]
    :if ([:len $rbGroupUsers] = 0) do={
        /user group remove $rbGroupId
    } else={
        :put ("rosboard group retained because it is still in use: " . "%s")
    }
}
:put ("rosboard account cleanup complete: " . "%s")
}
`, username, groupName, groupName, groupName, username)
}

type routerOSCleanupResponse struct {
	DeviceID string `json:"deviceId"`
	Name     string `json:"name"`
	Username string `json:"username"`
	Group    string `json:"groupName"`
	Script   string `json:"script"`
}

func managedRouterOSAccount(device config.DeviceConfig) (config.ManagedRouterOSAccount, bool) {
	if device.ManagedAccount != nil {
		username := strings.TrimSpace(device.ManagedAccount.Username)
		groupName := strings.TrimSpace(device.ManagedAccount.GroupName)
		if username != "" && groupName != "" {
			return config.ManagedRouterOSAccount{Username: username, GroupName: groupName}, true
		}
	}

	const prefix = "rosboard_"
	username := strings.TrimSpace(device.RouterOS.Username)
	if !strings.HasPrefix(username, prefix) {
		return config.ManagedRouterOSAccount{}, false
	}
	suffix := strings.TrimPrefix(username, prefix)
	if len(suffix) != 16 {
		return config.ManagedRouterOSAccount{}, false
	}
	for _, character := range suffix {
		if !((character >= '0' && character <= '9') || (character >= 'a' && character <= 'f')) {
			return config.ManagedRouterOSAccount{}, false
		}
	}
	return config.ManagedRouterOSAccount{
		Username:  username,
		GroupName: "rosboard_g_" + suffix,
	}, true
}

func routerOSCleanupForDevice(device config.DeviceConfig) (routerOSCleanupResponse, bool) {
	account, ok := managedRouterOSAccount(device)
	if !ok {
		return routerOSCleanupResponse{}, false
	}
	return routerOSCleanupResponse{
		DeviceID: device.ID,
		Name:     device.Name,
		Username: account.Username,
		Group:    account.GroupName,
		Script:   provisioningCleanupScript(account.Username, account.GroupName),
	}, true
}

type createProvisioningSessionRequest struct {
	DeviceID string `json:"deviceId"`
	Name     string `json:"name"`
	Host     string `json:"host"`
	Scheme   string `json:"scheme"`
	Port     int    `json:"port"`
}

type createProvisioningSessionResponse struct {
	SessionID  string `json:"sessionId"`
	Script     string `json:"script"`
	ExpiresAt  string `json:"expiresAt"`
	Username   string `json:"username"`
	Connection struct {
		Scheme string `json:"scheme"`
		Host   string `json:"host"`
		Port   int    `json:"port"`
	} `json:"connection"`
}

func (s *Server) serveCreateProvisioningSession(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(writer, http.MethodPost)
		return
	}
	var payload createProvisioningSessionRequest
	if err := decodeJSONBody(writer, request, &payload); err != nil {
		return
	}
	name := strings.TrimSpace(payload.Name)
	deviceID := strings.TrimSpace(payload.DeviceID)
	if deviceID != "" {
		device, found := s.configSnapshot().Device(deviceID)
		if !found {
			writeAPIError(writer, http.StatusNotFound, "device_not_found", "device not found")
			return
		}
		name = device.Name
		payload.Scheme, payload.Host, payload.Port = routerOSConnectionParts(device.RouterOS.BaseURL)
	}
	if name == "" {
		writeAPIError(writer, http.StatusBadRequest, "invalid_device", "device name is required")
		return
	}
	host := strings.TrimSpace(payload.Host)
	if host == "" {
		writeAPIError(writer, http.StatusBadRequest, "invalid_device", "host is required")
		return
	}
	scheme := strings.ToLower(strings.TrimSpace(payload.Scheme))
	if scheme == "" {
		scheme = "http"
	}
	if scheme != "http" && scheme != "https" {
		writeAPIError(writer, http.StatusBadRequest, "invalid_device", "scheme must be http or https")
		return
	}
	port := payload.Port
	if port == 0 {
		if scheme == "https" {
			port = 443
		} else {
			port = 80
		}
	}
	if port < 1 || port > 65535 {
		writeAPIError(writer, http.StatusBadRequest, "invalid_device", "port must be between 1 and 65535")
		return
	}
	// Validate the host portion without triggering a connection test
	normalizedHost := strings.TrimSpace(host)
	if strings.Contains(normalizedHost, "/") {
		writeAPIError(writer, http.StatusBadRequest, "invalid_device", "host must not contain a path")
		return
	}
	// Allow IPv6 in brackets
	if strings.HasPrefix(normalizedHost, "[") && strings.HasSuffix(normalizedHost, "]") {
		inner := normalizedHost[1 : len(normalizedHost)-1]
		if net.ParseIP(inner) == nil {
			writeAPIError(writer, http.StatusBadRequest, "invalid_device", "invalid IPv6 address")
			return
		}
	} else if net.ParseIP(normalizedHost) == nil {
		// Not an IP — allow hostname; just check it's non-empty and has no embedded port
		if strings.Contains(normalizedHost, ":") {
			writeAPIError(writer, http.StatusBadRequest, "invalid_device", "host must not contain port; use the port field")
			return
		}
	}

	sessionID, session, script, err := s.provisioning.create(deviceID, name, scheme, host, port)
	if err != nil {
		writeAPIError(writer, http.StatusInternalServerError, "provisioning_failed", "failed to create provisioning session")
		return
	}

	writer.Header().Set("Cache-Control", "no-store")
	writeJSON(writer, http.StatusCreated, createProvisioningSessionResponse{
		SessionID: sessionID,
		Script:    script,
		ExpiresAt: session.expiresAt.Format(time.RFC3339),
		Username:  session.username,
		Connection: struct {
			Scheme string `json:"scheme"`
			Host   string `json:"host"`
			Port   int    `json:"port"`
		}{
			Scheme: session.scheme,
			Host:   session.host,
			Port:   session.port,
		},
	})
}

type completeProvisioningRequest struct {
	CompleteOnboarding bool `json:"completeOnboarding"`
	DeferRestart       bool `json:"deferRestart"`
}

type completeProvisioningResponse struct {
	ID         string                         `json:"id"`
	Restarting bool                           `json:"restarting"`
	Identity   *routeros.VerificationIdentity `json:"identity,omitempty"`
	Warnings   []routeros.VerificationWarning `json:"warnings,omitempty"`
}

func (s *Server) serveCompleteProvisioning(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(writer, http.MethodPost)
		return
	}

	// Extract sessionId from path: /api/device-onboarding/sessions/{sessionId}/complete
	path := strings.TrimPrefix(request.URL.Path, "/api/device-onboarding/sessions/")
	path = strings.TrimSuffix(path, "/complete")
	sessionID := strings.TrimSpace(path)
	if sessionID == "" {
		writeAPIError(writer, http.StatusBadRequest, "invalid_session", "session ID is required")
		return
	}

	// Validate session ID format before looking up
	if _, err := provisioningSessionHash(sessionID); err != nil {
		writeAPIError(writer, http.StatusBadRequest, "invalid_session", "invalid session ID")
		return
	}

	session, ok := s.provisioning.get(sessionID)
	if !ok {
		writeAPIError(writer, http.StatusGone, "provisioning_expired", "接入脚本已过期，请重新生成。")
		return
	}

	var payload completeProvisioningRequest
	if err := decodeJSONBody(writer, request, &payload); err != nil {
		return
	}

	// Step 1: Connect and verify
	ctx, cancel := context.WithTimeout(request.Context(), 25*time.Second)
	defer cancel()
	client := routeros.NewClient(session.baseURL, session.username, session.password)
	result, err := client.Verify(ctx)
	if err != nil {
		var verificationErr *routeros.VerificationError
		if errors.As(err, &verificationErr) {
			writeAPIError(writer, http.StatusBadGateway, "routeros_"+verificationErr.Kind, verificationErr.Message)
		} else {
			writeAPIError(writer, http.StatusBadGateway, "routeros_connection", "无法连接 RouterOS。请检查 IP、协议、端口以及 RouterOS 的 www/www-ssl 服务。")
		}
		return // Don't consume session — user can retry
	}

	// Step 2: Create default auto scope configuration
	trafficConfig := config.TrafficScopeConfig{Mode: "auto"}
	terminalConfig := config.TerminalScopeConfig{Mode: "auto"}

	// Step 3: Preview scopes
	previewConfig := config.RouterOSConfig{TrafficScope: trafficConfig, TerminalScope: terminalConfig}
	_, trafficScope := service.PreviewScopes(previewConfig, result.Topology)

	// Step 4: Ensure at least one ISP traffic interface was identified
	if len(trafficScope.Interfaces) == 0 {
		writeAPIError(writer, http.StatusBadGateway, "routeros_no_traffic", "账号已连接成功，但无法自动识别上网线路。请改用手动添加并在高级设置中指定采集接口。")
		return // Don't consume session
	}

	// Step 5: Generate verification ticket
	fingerprint := connectionFingerprint(session.baseURL, session.username, session.password)
	token, _, err := s.tickets.issueWithTopology(fingerprint, result.Interfaces, result.Topology)
	if err != nil {
		writeAPIError(writer, http.StatusInternalServerError, "verification_failed", "failed to create verification ticket")
		return
	}

	// Step 6: Build device settings request
	devicePayload := deviceSettingsRequest{
		Name:               session.deviceName,
		Enabled:            true,
		Scheme:             session.scheme,
		Host:               session.host,
		Port:               session.port,
		Username:           session.username,
		Password:           session.password,
		TrafficScope:       trafficConfig,
		TerminalScope:      terminalConfig,
		VerificationToken:  token,
		CompleteOnboarding: payload.CompleteOnboarding,
	}

	// Step 7: Prepare device (with deviceSaveMu lock)
	s.deviceSaveMu.Lock()
	deviceID, err := func() (string, error) {
		deviceID := session.deviceID
		var existing *config.DeviceConfig
		if deviceID == "" {
			deviceID = uuid.NewString()
		} else {
			current, found := s.configSnapshot().Device(deviceID)
			if !found {
				return "", errors.New("device not found")
			}
			existing = &current
			devicePayload.Name = current.Name
			devicePayload.Enabled = current.Enabled
			devicePayload.TrafficInterfaces = current.RouterOS.TrafficInterfaces
			devicePayload.TrafficScope = current.RouterOS.TrafficScope
			devicePayload.TerminalCIDRs = current.RouterOS.TerminalCIDRs
			devicePayload.TerminalScope = current.RouterOS.TerminalScope
		}
		device, consumeTicket, err := s.prepareDevice(ctx, deviceID, devicePayload, existing)
		if err != nil {
			return "", err
		}
		device.ManagedAccount = &config.ManagedRouterOSAccount{
			Username:  session.username,
			GroupName: session.groupName,
		}
		if err := s.saveSettings(func(next *config.Config) {
			if existing == nil {
				next.Devices = append(next.Devices, device)
				return
			}
			for index := range next.Devices {
				if next.Devices[index].ID == deviceID {
					next.Devices[index] = device
					return
				}
			}
		}); err != nil {
			return "", fmt.Errorf("save device: %w", err)
		}
		if consumeTicket {
			s.tickets.consume(token)
		}
		return device.ID, nil
	}()
	s.deviceSaveMu.Unlock()

	if err != nil {
		// Don't consume provisioning session; clean up verification ticket if issued
		s.tickets.consume(token)
		writeDeviceValidationError(writer, err)
		return
	}

	// Step 8: Success — consume provisioning session and complete onboarding
	s.provisioning.consume(sessionID)

	if s.auth != nil && payload.CompleteOnboarding {
		if err := s.auth.CompleteOnboarding(request.Context()); err != nil {
			writeAPIError(writer, http.StatusInternalServerError, "setup_completion_failed", "设备已保存但设置完成标记失败")
			return
		}
	}

	deferRestart := payload.DeferRestart && !payload.CompleteOnboarding
	if !deferRestart {
		s.scheduleRestart()
	}

	writer.Header().Set("Cache-Control", "no-store")
	writeJSON(writer, http.StatusCreated, completeProvisioningResponse{
		ID:         deviceID,
		Restarting: !deferRestart && s.restart != nil,
		Identity: &routeros.VerificationIdentity{
			RouterName: result.Identity.RouterName,
			Version:    result.Identity.Version,
			Platform:   result.Identity.Platform,
			BoardName:  result.Identity.BoardName,
		},
		Warnings: result.Warnings,
	})
}
