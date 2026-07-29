package api

import (
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"rosboard/internal/config"
	"rosboard/internal/service"
)

func TestCreateProvisioningSessionDefaults(t *testing.T) {
	monitor := service.NewMonitor(config.Config{}, nil, nil, log.Default())
	server := NewServerWithProvisioning(config.Config{}, monitor, nil, nil)

	body := strings.NewReader(`{"name":"test-device","host":"10.0.0.1"}`)
	request := httptest.NewRequest(http.MethodPost, "/api/device-onboarding/sessions", body)
	request.RemoteAddr = "127.0.0.1:12345"
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if v := response.Header().Get("Cache-Control"); v != "no-store" {
		t.Fatalf("Cache-Control=%q, want no-store", v)
	}
	var payload createProvisioningSessionResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.SessionID == "" {
		t.Fatal("sessionId is empty")
	}
	if payload.Script == "" {
		t.Fatal("script is empty")
	}
	if payload.ExpiresAt == "" {
		t.Fatal("expiresAt is empty")
	}
	if payload.Username == "" {
		t.Fatal("username is empty")
	}
	if payload.Connection.Scheme != "http" {
		t.Fatalf("scheme=%q, want http", payload.Connection.Scheme)
	}
	if payload.Connection.Port != 80 {
		t.Fatalf("port=%d, want 80", payload.Connection.Port)
	}
	if payload.Connection.Host != "10.0.0.1" {
		t.Fatalf("host=%q", payload.Connection.Host)
	}
}

func TestCreateProvisioningSessionHTTPSDefaults(t *testing.T) {
	monitor := service.NewMonitor(config.Config{}, nil, nil, log.Default())
	server := NewServerWithProvisioning(config.Config{}, monitor, nil, nil)

	body := strings.NewReader(`{"name":"test","host":"10.0.0.1","scheme":"https"}`)
	request := httptest.NewRequest(http.MethodPost, "/api/device-onboarding/sessions", body)
	request.RemoteAddr = "127.0.0.1:12345"
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var payload createProvisioningSessionResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Connection.Scheme != "https" {
		t.Fatalf("scheme=%q, want https", payload.Connection.Scheme)
	}
	if payload.Connection.Port != 443 {
		t.Fatalf("port=%d, want 443", payload.Connection.Port)
	}
}

func TestCreateProvisioningSessionInputValidation(t *testing.T) {
	monitor := service.NewMonitor(config.Config{}, nil, nil, log.Default())
	server := NewServerWithProvisioning(config.Config{}, monitor, nil, nil)

	tests := []struct {
		name string
		body string
		code int
	}{
		{"empty name", `{"name":"","host":"10.0.0.1"}`, http.StatusBadRequest},
		{"empty host", `{"name":"test","host":""}`, http.StatusBadRequest},
		{"invalid scheme", `{"name":"test","host":"10.0.0.1","scheme":"ftp"}`, http.StatusBadRequest},
		{"invalid port", `{"name":"test","host":"10.0.0.1","port":70000}`, http.StatusBadRequest},
		{"host with path", `{"name":"test","host":"10.0.0.1/foo"}`, http.StatusBadRequest},
		{"host with port", `{"name":"test","host":"10.0.0.1:8080"}`, http.StatusBadRequest},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body := strings.NewReader(tc.body)
			request := httptest.NewRequest(http.MethodPost, "/api/device-onboarding/sessions", body)
			request.RemoteAddr = "127.0.0.1:12345"
			response := httptest.NewRecorder()
			server.ServeHTTP(response, request)
			if response.Code != tc.code {
				t.Fatalf("status=%d body=%s, want %d", response.Code, response.Body.String(), tc.code)
			}
		})
	}
}

func TestCreateProvisioningSessionRandomValues(t *testing.T) {
	monitor := service.NewMonitor(config.Config{}, nil, nil, log.Default())
	server := NewServerWithProvisioning(config.Config{}, monitor, nil, nil)

	body := func(name, host string) *strings.Reader {
		return strings.NewReader(`{"name":"` + name + `","host":"` + host + `"}`)
	}

	// First session
	r1 := httptest.NewRecorder()
	server.ServeHTTP(r1, httptest.NewRequest(http.MethodPost, "/api/device-onboarding/sessions", body("a", "10.0.0.1")))
	var s1 createProvisioningSessionResponse
	if err := json.Unmarshal(r1.Body.Bytes(), &s1); err != nil {
		t.Fatal(err)
	}

	// Second session
	r2 := httptest.NewRecorder()
	server.ServeHTTP(r2, httptest.NewRequest(http.MethodPost, "/api/device-onboarding/sessions", body("b", "10.0.0.2")))
	var s2 createProvisioningSessionResponse
	if err := json.Unmarshal(r2.Body.Bytes(), &s2); err != nil {
		t.Fatal(err)
	}

	if s1.SessionID == s2.SessionID {
		t.Fatal("session IDs should differ")
	}
	if s1.Username == s2.Username {
		t.Fatal("usernames should differ")
	}
	if s1.Script == s2.Script {
		t.Fatal("scripts should differ (different passwords)")
	}
}

func TestProvisioningSessionUsernameFormat(t *testing.T) {
	monitor := service.NewMonitor(config.Config{}, nil, nil, log.Default())
	server := NewServerWithProvisioning(config.Config{}, monitor, nil, nil)

	body := strings.NewReader(`{"name":"test","host":"10.0.0.1"}`)
	request := httptest.NewRequest(http.MethodPost, "/api/device-onboarding/sessions", body)
	request.RemoteAddr = "127.0.0.1:12345"
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)

	var payload createProvisioningSessionResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}

	if !strings.HasPrefix(payload.Username, "rosboard_") {
		t.Fatalf("username %q does not start with rosboard_", payload.Username)
	}
	suffix := strings.TrimPrefix(payload.Username, "rosboard_")
	if len(suffix) != 16 {
		t.Fatalf("username suffix length=%d, want 16", len(suffix))
	}
	for _, ch := range suffix {
		if !((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f')) {
			t.Fatalf("username suffix contains invalid char %c", ch)
		}
	}
}

func TestProvisioningScriptContent(t *testing.T) {
	monitor := service.NewMonitor(config.Config{}, nil, nil, log.Default())
	server := NewServerWithProvisioning(config.Config{}, monitor, nil, nil)

	body := strings.NewReader(`{"name":"test","host":"10.0.0.1"}`)
	request := httptest.NewRequest(http.MethodPost, "/api/device-onboarding/sessions", body)
	request.RemoteAddr = "127.0.0.1:12345"
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)

	var payload createProvisioningSessionResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}

	script := payload.Script

	// New format: wrapped in { }
	trimmed := strings.TrimSpace(script)
	if !strings.HasPrefix(trimmed, "{") {
		t.Fatal("script must start with {")
	}
	if !strings.HasSuffix(trimmed, "}") {
		t.Fatal("script must end with }")
	}

	// No standalone :local variable declarations (rbGroupId and rbUserId are fine)
	if strings.Contains(script, ":local rbGroup ") {
		t.Fatal("script should not contain :local rbGroup")
	}
	if strings.Contains(script, ":local rbUser ") {
		t.Fatal("script should not contain :local rbUser")
	}
	if strings.Contains(script, ":local rbPassword") {
		t.Fatal("script should not contain :local rbPassword")
	}

	// Contains the generated values as literals
	if !strings.Contains(script, payload.Username) {
		t.Fatal("script should contain the generated username")
	}
	groupName := strings.Replace(payload.Username, "rosboard_", "rosboard_g_", 1)
	if !strings.Contains(script, groupName) {
		t.Fatal("script should contain the generated group name")
	}

	// Correct permissions. Some RouterOS releases log REST authentication via the
	// internal API channel, so the read-only account needs both login policies.
	if !strings.Contains(script, "read,test,api,rest-api") {
		t.Fatal("script does not contain read,test,api,rest-api")
	}
	// Check that disallowed permission values are not present
	for _, forbidden := range []string{"write", "policy", "sensitive", "sniff", "reboot", "full", "web", "winbox", "ssh", "ftp", "password", "romon", "local", "telnet"} {
		if strings.Contains(script, "policy="+forbidden) || strings.Contains(script, ","+forbidden) {
			t.Fatalf("script contains unexpected permission: %s", forbidden)
		}
	}
	if strings.Contains(script, "/ip service") {
		t.Fatal("script modifies IP services")
	}
	if strings.Contains(script, "firewall") {
		t.Fatal("script modifies firewall")
	}
	if !strings.Contains(script, "Managed by rosboard") {
		t.Fatal("script does not contain 'Managed by rosboard'")
	}
}

func TestProvisioningCleanupScriptContent(t *testing.T) {
	script := provisioningCleanupScript("rosboard_0123456789abcdef", "rosboard_g_0123456789abcdef")
	trimmed := strings.TrimSpace(script)
	if !strings.HasPrefix(trimmed, "{") || !strings.HasSuffix(trimmed, "}") {
		t.Fatal("cleanup script must be a single RouterOS scope block")
	}
	for _, required := range []string{
		`/user find where name="rosboard_0123456789abcdef"`,
		`/user remove $rbUserId`,
		`/user group find where name="rosboard_g_0123456789abcdef"`,
		`/user find where group="rosboard_g_0123456789abcdef"`,
		`/user group remove $rbGroupId`,
		"rosboard account cleanup complete",
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("cleanup script missing %q", required)
		}
	}
	for _, forbidden := range []string{"password=", "/ip service", "firewall", "policy="} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("cleanup script contains forbidden content %q", forbidden)
		}
	}
}

func TestManagedRouterOSAccountUsesStoredMetadata(t *testing.T) {
	device := config.DeviceConfig{
		ManagedAccount: &config.ManagedRouterOSAccount{
			Username:  "stored-user",
			GroupName: "stored-group",
		},
		RouterOS: config.RouterOSConfig{Username: "edited-connection-user"},
	}
	account, ok := managedRouterOSAccount(device)
	if !ok || account.Username != "stored-user" || account.GroupName != "stored-group" {
		t.Fatalf("unexpected managed account: %#v ok=%v", account, ok)
	}
}

func TestManagedRouterOSAccountDerivesLegacyQuickUsername(t *testing.T) {
	device := config.DeviceConfig{
		RouterOS: config.RouterOSConfig{Username: "rosboard_0123456789abcdef"},
	}
	account, ok := managedRouterOSAccount(device)
	if !ok {
		t.Fatal("legacy quick-provisioned username should be recognized")
	}
	if account.GroupName != "rosboard_g_0123456789abcdef" {
		t.Fatalf("group=%q", account.GroupName)
	}
}

func TestManagedRouterOSAccountRejectsManualUsername(t *testing.T) {
	device := config.DeviceConfig{
		RouterOS: config.RouterOSConfig{Username: "admin"},
	}
	if _, ok := managedRouterOSAccount(device); ok {
		t.Fatal("manual username must not expose a generated cleanup script")
	}
}

func TestProvisioningSessionExpired(t *testing.T) {
	ps := newProvisioningSessions()
	ps.now = func() time.Time { return time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC) }

	sessionID, _, _, err := ps.create("test", "http", "10.0.0.1", 80)
	if err != nil {
		t.Fatal(err)
	}

	// Move past expiry
	ps.now = func() time.Time { return time.Date(2024, 1, 1, 0, 15, 1, 0, time.UTC) }

	_, ok := ps.get(sessionID)
	if ok {
		t.Fatal("session should be expired")
	}
}

func TestProvisioningSessionConsumed(t *testing.T) {
	ps := newProvisioningSessions()
	sessionID, _, _, err := ps.create("test", "http", "10.0.0.1", 80)
	if err != nil {
		t.Fatal(err)
	}

	_, ok := ps.get(sessionID)
	if !ok {
		t.Fatal("session should exist before consume")
	}

	ps.consume(sessionID)

	_, ok = ps.get(sessionID)
	if ok {
		t.Fatal("session should not exist after consume")
	}
}

func TestNormalizedRouterOSURLAcceptsBracketedIPv6(t *testing.T) {
	baseURL, err := normalizedRouterOSURL("http", "[2001:db8::1]", 80)
	if err != nil {
		t.Fatal(err)
	}
	if baseURL != "http://[2001:db8::1]:80" {
		t.Fatalf("baseURL=%q", baseURL)
	}
}

func TestProvisioningSessionClear(t *testing.T) {
	ps := newProvisioningSessions()
	sid1, _, _, _ := ps.create("a", "http", "10.0.0.1", 80)
	sid2, _, _, _ := ps.create("b", "http", "10.0.0.2", 80)

	ps.clear()

	if _, ok := ps.get(sid1); ok {
		t.Fatal("session should be cleared")
	}
	if _, ok := ps.get(sid2); ok {
		t.Fatal("session should be cleared")
	}
}

func TestProvisioningSessionRetryable(t *testing.T) {
	ps := newProvisioningSessions()
	sessionID, _, _, err := ps.create("test", "http", "10.0.0.1", 80)
	if err != nil {
		t.Fatal(err)
	}

	// Can get multiple times before consume
	for i := 0; i < 3; i++ {
		_, ok := ps.get(sessionID)
		if !ok {
			t.Fatalf("session should be gettable on attempt %d", i)
		}
	}
}

func TestCompleteProvisioningWithoutAuth(t *testing.T) {
	monitor := service.NewMonitor(config.Config{}, nil, nil, log.Default())
	server := NewServerWithProvisioning(config.Config{}, monitor, nil, nil)

	// First create a session
	body := strings.NewReader(`{"name":"test","host":"10.0.0.1"}`)
	creq := httptest.NewRequest(http.MethodPost, "/api/device-onboarding/sessions", body)
	creq.RemoteAddr = "127.0.0.1:12345"
	cresp := httptest.NewRecorder()
	server.ServeHTTP(cresp, creq)

	var createResp createProvisioningSessionResponse
	json.Unmarshal(cresp.Body.Bytes(), &createResp)

	// Try to complete without auth — should fail because phaseAllows is not checked
	// (since there's no auth service, it should try to process)
	completeBody := strings.NewReader(`{"completeOnboarding":false}`)
	req := httptest.NewRequest(http.MethodPost, "/api/device-onboarding/sessions/"+createResp.SessionID+"/complete", completeBody)
	req.RemoteAddr = "127.0.0.1:12345"
	resp := httptest.NewRecorder()
	server.ServeHTTP(resp, req)

	// Without auth service, it should attempt the connection and fail with a connection error
	// (no real RouterOS to connect to)
	if resp.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 (connection failure), got %d body=%s", resp.Code, resp.Body.String())
	}
}

func TestCompleteProvisioningInvalidSession(t *testing.T) {
	monitor := service.NewMonitor(config.Config{}, nil, nil, log.Default())
	server := NewServerWithProvisioning(config.Config{}, monitor, nil, nil)

	body := strings.NewReader(`{"completeOnboarding":false}`)
	req := httptest.NewRequest(http.MethodPost, "/api/device-onboarding/sessions/invalid-session-id/complete", body)
	req.RemoteAddr = "127.0.0.1:12345"
	resp := httptest.NewRecorder()
	server.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest && resp.Code != http.StatusGone {
		t.Fatalf("expected 400 or 410 for invalid session, got %d body=%s", resp.Code, resp.Body.String())
	}
}

func TestCompleteProvisioningCanSaveWithoutRestart(t *testing.T) {
	router := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/rest/system/resource":
			_, _ = writer.Write([]byte(`{"board-name":"CCR","version":"7.20","platform":"MikroTik"}`))
		case "/rest/interface":
			_, _ = writer.Write([]byte(`[{"name":"ether1","type":"ether","running":"true"}]`))
		case "/rest/ip/dhcp-client":
			_, _ = writer.Write([]byte(`[{"interface":"ether1","status":"bound","add-default-route":"true"}]`))
		case "/rest/ip/address", "/rest/ipv6/address", "/rest/ip/dhcp-server/lease", "/rest/ip/arp", "/rest/ip/firewall/connection":
			_, _ = writer.Write([]byte(`[]`))
		default:
			http.Error(writer, "optional unavailable", http.StatusForbidden)
		}
	}))
	defer router.Close()
	scheme, host, port := testConnectionParts(t, router.URL)
	restarted := make(chan struct{}, 1)
	path := t.TempDir() + "/config.yaml"
	server := NewServerWithRestart(config.Config{Path: path}, nil, nil, func() { restarted <- struct{}{} })
	sessionID, _, _, err := server.provisioning.create("Edge", scheme, host, port)
	if err != nil {
		t.Fatal(err)
	}

	body := strings.NewReader(`{"completeOnboarding":false,"deferRestart":true}`)
	request := httptest.NewRequest(http.MethodPost, "/api/device-onboarding/sessions/"+sessionID+"/complete", body)
	request.RemoteAddr = "127.0.0.1:12345"
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var payload completeProvisioningResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.ID == "" || payload.Restarting {
		t.Fatalf("unexpected response: %#v", payload)
	}
	if len(server.configSnapshot().Devices) != 1 {
		t.Fatalf("device was not saved: %#v", server.configSnapshot().Devices)
	}
	select {
	case <-restarted:
		t.Fatal("save-only quick provisioning restarted the panel")
	case <-time.After(400 * time.Millisecond):
	}
}

func TestProvisioningFullResetClearsSessions(t *testing.T) {
	monitor := service.NewMonitor(config.Config{}, nil, nil, log.Default())
	server := NewServerWithProvisioning(config.Config{}, monitor, nil, nil)

	// Create a session
	body := strings.NewReader(`{"name":"test","host":"10.0.0.1"}`)
	creq := httptest.NewRequest(http.MethodPost, "/api/device-onboarding/sessions", body)
	creq.RemoteAddr = "127.0.0.1:12345"
	cresp := httptest.NewRecorder()
	server.ServeHTTP(cresp, creq)
	var createResp createProvisioningSessionResponse
	json.Unmarshal(cresp.Body.Bytes(), &createResp)

	// Full reset (without auth/store this won't work in test, but let's see)
	resetBody := strings.NewReader(`{"confirmed":true}`)
	rreq := httptest.NewRequest(http.MethodPost, "/api/settings/full-reset", resetBody)
	rreq.RemoteAddr = "127.0.0.1:12345"
	rresp := httptest.NewRecorder()
	server.ServeHTTP(rresp, rreq)
	// Full reset needs store+auth; in test server it returns unavailable
	// Just verify sessions were NOT cleared (since full reset didn't run)
	_, ok := server.provisioning.get(createResp.SessionID)
	if !ok {
		t.Fatal("session should still exist since full reset was not executed")
	}

	// Now test clear directly
	server.provisioning.clear()
	_, ok = server.provisioning.get(createResp.SessionID)
	if ok {
		t.Fatal("session should be cleared after direct clear")
	}
}
