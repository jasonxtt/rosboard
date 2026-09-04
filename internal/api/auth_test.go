package api

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"rosboard/internal/auth"
	"rosboard/internal/config"
	"rosboard/internal/store"
)

func newAuthServer(t *testing.T, allowedCIDRs []string) (*Server, *store.Store) {
	return newAuthServerWithRestart(t, allowedCIDRs, nil)
}

func newAuthServerWithRestart(t *testing.T, allowedCIDRs []string, restart func()) (*Server, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	storage, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	authService := auth.NewWithOptions(storage, auth.Options{
		Random: bytes.NewReader(bytes.Repeat([]byte{0x5a}, 8192)),
		PasswordParams: auth.PasswordParams{
			Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 8, KeyLength: 16,
		},
	})
	cfg := config.Config{
		Path: filepath.Join(dir, "config.yaml"), DataDir: dir, ListenAddress: ":8080",
		PollIntervalSeconds: 10, RealtimePollIntervalSeconds: 1, TerminalPollIntervalSeconds: 3, SampleRetentionHours: 48,
		AllowedCIDRs: allowedCIDRs,
	}
	return NewServerWithAuth(cfg, nil, storage, nil, restart, authService), storage
}

func authRequest(t *testing.T, server *Server, method, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.RemoteAddr = "127.0.0.1:12345"
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions {
		request.Header.Set("Origin", "http://example.com")
	}
	if cookie != nil {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	return response
}

func responseCookie(t *testing.T, response *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	result := response.Result()
	defer result.Body.Close()
	for _, cookie := range result.Cookies() {
		if cookie.Name == auth.SessionCookieName {
			return cookie
		}
	}
	t.Fatal("session cookie missing")
	return nil
}

func responsePhase(t *testing.T, response *httptest.ResponseRecorder) string {
	t.Helper()
	var payload struct {
		Phase string `json:"phase"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	return payload.Phase
}

func TestAuthBootstrapOnboardingAndLogoutFlow(t *testing.T) {
	server, _ := newAuthServer(t, []string{"127.0.0.0/8"})

	bootstrap := authRequest(t, server, http.MethodGet, "/api/bootstrap", "", nil)
	if bootstrap.Code != http.StatusOK || responsePhase(t, bootstrap) != "needs_admin" {
		t.Fatalf("bootstrap status=%d body=%s", bootstrap.Code, bootstrap.Body.String())
	}
	unauthorized := authRequest(t, server, http.MethodGet, "/api/settings", "", nil)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("settings status=%d body=%s", unauthorized.Code, unauthorized.Body.String())
	}

	created := authRequest(t, server, http.MethodPost, "/api/setup/admin", `{"username":" admin ","password":"1234","passwordConfirmation":"1234"}`, nil)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	cookie := responseCookie(t, created)
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.MaxAge != int(auth.SessionLifetime/time.Second) {
		t.Fatalf("unsafe session cookie: %#v", cookie)
	}

	resumed := authRequest(t, server, http.MethodGet, "/api/bootstrap", "", cookie)
	if resumed.Code != http.StatusOK || responsePhase(t, resumed) != "needs_routeros" {
		t.Fatalf("resumed status=%d body=%s", resumed.Code, resumed.Body.String())
	}
	blocked := authRequest(t, server, http.MethodGet, "/api/dashboard", "", cookie)
	if blocked.Code != http.StatusConflict {
		t.Fatalf("dashboard before setup status=%d body=%s", blocked.Code, blocked.Body.String())
	}
	completed := authRequest(t, server, http.MethodPost, "/api/setup/complete", `{"skipRouterOS":true}`, cookie)
	if completed.Code != http.StatusOK || responsePhase(t, completed) != "ready" {
		t.Fatalf("complete status=%d body=%s", completed.Code, completed.Body.String())
	}
	ready := authRequest(t, server, http.MethodGet, "/api/bootstrap", "", cookie)
	if responsePhase(t, ready) != "ready" {
		t.Fatalf("ready body=%s", ready.Body.String())
	}
	logout := authRequest(t, server, http.MethodPost, "/api/auth/logout", "", cookie)
	if logout.Code != http.StatusOK || responseCookie(t, logout).MaxAge != -1 {
		t.Fatalf("logout status=%d body=%s", logout.Code, logout.Body.String())
	}
	if got := authRequest(t, server, http.MethodGet, "/api/settings", "", cookie); got.Code != http.StatusUnauthorized {
		t.Fatalf("revoked cookie status=%d body=%s", got.Code, got.Body.String())
	}
}

func TestSetupCompleteWithSavedDeviceRestartsPanel(t *testing.T) {
	restarted := make(chan struct{}, 1)
	server, _ := newAuthServerWithRestart(t, nil, func() { restarted <- struct{}{} })
	server.cfg.Devices = []config.DeviceConfig{{ID: "edge", Name: "Edge", Enabled: true}}
	created := authRequest(t, server, http.MethodPost, "/api/setup/admin", `{"username":"admin","password":"1234","passwordConfirmation":"1234"}`, nil)
	cookie := responseCookie(t, created)

	completed := authRequest(t, server, http.MethodPost, "/api/setup/complete", `{"skipRouterOS":false}`, cookie)
	if completed.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", completed.Code, completed.Body.String())
	}
	select {
	case <-restarted:
	case <-time.After(time.Second):
		t.Fatal("enter panel did not restart the service")
	}
}

func TestCredentialsChangeDoesNotRequireCurrentPasswordAndRevokesSession(t *testing.T) {
	server, _ := newAuthServer(t, nil)
	created := authRequest(t, server, http.MethodPost, "/api/setup/admin", `{"username":"admin","password":"1234","passwordConfirmation":"1234"}`, nil)
	cookie := responseCookie(t, created)

	invalid := authRequest(t, server, http.MethodPut, "/api/account", `{"username":"owner","password":"next","passwordConfirmation":"mismatch"}`, cookie)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid confirmation status=%d body=%s", invalid.Code, invalid.Body.String())
	}
	if got := authRequest(t, server, http.MethodGet, "/api/settings", "", cookie); got.Code != http.StatusOK {
		t.Fatalf("invalid update revoked session status=%d body=%s", got.Code, got.Body.String())
	}
	changed := authRequest(t, server, http.MethodPut, "/api/account", `{"username":"owner","password":"next","passwordConfirmation":"next"}`, cookie)
	if changed.Code != http.StatusOK {
		t.Fatalf("change status=%d body=%s", changed.Code, changed.Body.String())
	}
	if got := authRequest(t, server, http.MethodGet, "/api/settings", "", cookie); got.Code != http.StatusUnauthorized {
		t.Fatalf("old session status=%d body=%s", got.Code, got.Body.String())
	}
	oldLogin := authRequest(t, server, http.MethodPost, "/api/auth/login", `{"username":"admin","password":"1234"}`, nil)
	if oldLogin.Code != http.StatusUnauthorized {
		t.Fatalf("old password login status=%d body=%s", oldLogin.Code, oldLogin.Body.String())
	}
	newLogin := authRequest(t, server, http.MethodPost, "/api/auth/login", `{"username":"owner","password":"next"}`, nil)
	if newLogin.Code != http.StatusOK {
		t.Fatalf("new password login status=%d body=%s", newLogin.Code, newLogin.Body.String())
	}
}

func TestAllowedCIDRPrecedesBootstrapAndSetup(t *testing.T) {
	server, _ := newAuthServer(t, []string{"10.0.0.0/8"})
	request := httptest.NewRequest(http.MethodGet, "/api/bootstrap", nil)
	request.RemoteAddr = "192.168.1.20:1234"
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestCrossSiteWriteIsRejected(t *testing.T) {
	server, _ := newAuthServer(t, nil)
	request := httptest.NewRequest(http.MethodPost, "/api/setup/admin", strings.NewReader(`{"username":"admin","password":"1234","passwordConfirmation":"1234"}`))
	request.RemoteAddr = "127.0.0.1:1234"
	request.Header.Set("Origin", "https://attacker.example")
	request.Header.Set("Sec-Fetch-Site", "cross-site")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestAuthenticatedServerRejectsWriteWithoutOrigin(t *testing.T) {
	server, _ := newAuthServer(t, nil)
	request := httptest.NewRequest(http.MethodPost, "/api/setup/admin", strings.NewReader(`{"username":"admin","password":"1234","passwordConfirmation":"1234"}`))
	request.RemoteAddr = "127.0.0.1:1234"
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestSameOriginWriteUsesExactEffectiveOrigin(t *testing.T) {
	httpRequest := httptest.NewRequest(http.MethodPost, "http://panel.example/api/settings", nil)
	for _, origin := range []string{"http://panel.example", "http://panel.example:80"} {
		httpRequest.Header.Set("Origin", origin)
		if !sameOriginWrite(httpRequest) {
			t.Fatalf("valid HTTP origin %q was rejected", origin)
		}
	}
	for _, origin := range []string{"https://panel.example", "http://panel.example/path", "http://user@panel.example", "http://panel.example:81"} {
		httpRequest.Header.Set("Origin", origin)
		if sameOriginWrite(httpRequest) {
			t.Fatalf("invalid HTTP origin %q was accepted", origin)
		}
	}

	httpsRequest := httptest.NewRequest(http.MethodPost, "https://panel.example/api/settings", nil)
	httpsRequest.TLS = &tls.ConnectionState{}
	for _, origin := range []string{"http://panel.example", "https://panel.example/path", "https://user@panel.example"} {
		httpsRequest.Header.Set("Origin", origin)
		if sameOriginWrite(httpsRequest) {
			t.Fatalf("invalid HTTPS origin %q was accepted", origin)
		}
	}
	httpsRequest.Header.Set("Origin", "https://panel.example:443")
	if !sameOriginWrite(httpsRequest) {
		t.Fatal("valid HTTPS origin was rejected")
	}

	absoluteForm := httptest.NewRequest(http.MethodPost, "https://panel.example/api/settings", nil)
	absoluteForm.TLS = nil
	absoluteForm.Header.Set("Origin", "https://panel.example")
	if sameOriginWrite(absoluteForm) {
		t.Fatal("absolute-form request target changed the trusted HTTP scheme")
	}
	absoluteForm.Header.Set("Origin", "http://panel.example")
	if !sameOriginWrite(absoluteForm) {
		t.Fatal("absolute-form HTTP request did not use the direct connection scheme")
	}

	proxyMismatch := httptest.NewRequest(http.MethodPost, "http://panel.example/api/settings", nil)
	proxyMismatch.TLS = &tls.ConnectionState{}
	proxyMismatch.Header.Set("Origin", "http://panel.example")
	if sameOriginWrite(proxyMismatch) {
		t.Fatal("proxy scheme mismatch was accepted")
	}
}

func TestFullResetRequiresConfirmationAndReturnsToAdminSetup(t *testing.T) {
	server, storage := newAuthServer(t, nil)
	server.cfg.Devices = []config.DeviceConfig{{
		ID: "edge", Name: "Edge", Enabled: true,
		RouterOS: config.RouterOSConfig{BaseURL: "http://10.0.0.1", Username: "admin", Password: "secret"},
	}}
	server.cfg.PollIntervalSeconds = 17
	server.cfg.AllowedCIDRs = []string{"10.0.0.0/8"}
	if err := config.Save(server.cfg.Path, server.cfg); err != nil {
		t.Fatal(err)
	}
	if err := storage.ForDevice("edge").UpsertTerminal(t.Context(), "mac:test", "AA:BB:CC:DD:EE:FF", "test", time.Now()); err != nil {
		t.Fatal(err)
	}
	created := authRequest(t, server, http.MethodPost, "/api/setup/admin", `{"username":"admin","password":"1234","passwordConfirmation":"1234"}`, nil)
	cookie := responseCookie(t, created)
	if completed := authRequest(t, server, http.MethodPost, "/api/setup/complete", `{"skipRouterOS":false}`, cookie); completed.Code != http.StatusOK {
		t.Fatalf("complete status=%d body=%s", completed.Code, completed.Body.String())
	}

	wrong := authRequest(t, server, http.MethodPost, "/api/settings/full-reset", `{"confirmed":false}`, cookie)
	if wrong.Code != http.StatusBadRequest {
		t.Fatalf("wrong confirmation status=%d body=%s", wrong.Code, wrong.Body.String())
	}
	if _, err := storage.Admin(t.Context()); err != nil {
		t.Fatalf("wrong confirmation changed administrator: %v", err)
	}
	if _, err := os.Stat(server.cfg.Path); err != nil {
		t.Fatalf("wrong confirmation removed configuration: %v", err)
	}
	if totals, err := storage.ForDevice("edge").TerminalTotals(t.Context(), []string{"mac:test"}); err != nil || len(totals) != 1 {
		t.Fatalf("wrong confirmation removed monitoring data: totals=%#v err=%v", totals, err)
	}

	reset := authRequest(t, server, http.MethodPost, "/api/settings/full-reset", `{"confirmed":true}`, cookie)
	if reset.Code != http.StatusOK {
		t.Fatalf("reset status=%d body=%s", reset.Code, reset.Body.String())
	}
	if _, err := os.Stat(server.cfg.Path); !os.IsNotExist(err) {
		t.Fatalf("configuration file survived reset: %v", err)
	}
	if cleared := responseCookie(t, reset); cleared.MaxAge != -1 {
		t.Fatalf("session cookie was not cleared: %#v", cleared)
	}
	if bootstrap := authRequest(t, server, http.MethodGet, "/api/bootstrap", "", nil); responsePhase(t, bootstrap) != "needs_admin" {
		t.Fatalf("bootstrap did not return to admin setup: %s", bootstrap.Body.String())
	}
	loaded, err := config.Load(server.cfg.Path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Devices) != 0 || loaded.RouterOSConfigured() {
		t.Fatalf("device settings survived reset: %#v", loaded)
	}
	if loaded.PollIntervalSeconds != 10 || len(loaded.AllowedCIDRs) != 0 {
		t.Fatalf("runtime settings survived reset: %#v", loaded)
	}
	if totals, err := storage.ForDevice("edge").TerminalTotals(t.Context(), []string{"mac:test"}); err != nil || len(totals) != 0 {
		t.Fatalf("monitoring data survived reset: totals=%#v err=%v", totals, err)
	}
}

func TestFullResetSchedulesRestartAfterClosingStore(t *testing.T) {
	restarted := make(chan struct{}, 1)
	server, _ := newAuthServerWithRestart(t, nil, func() { restarted <- struct{}{} })
	if err := config.Save(server.cfg.Path, server.cfg); err != nil {
		t.Fatal(err)
	}
	created := authRequest(t, server, http.MethodPost, "/api/setup/admin", `{"username":"admin","password":"1234","passwordConfirmation":"1234"}`, nil)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	cookie := responseCookie(t, created)
	completed := authRequest(t, server, http.MethodPost, "/api/setup/complete", `{"skipRouterOS":true}`, cookie)
	if completed.Code != http.StatusOK {
		t.Fatalf("complete status=%d body=%s", completed.Code, completed.Body.String())
	}
	reset := authRequest(t, server, http.MethodPost, "/api/settings/full-reset", `{"confirmed":true}`, cookie)
	if reset.Code != http.StatusOK {
		t.Fatalf("reset status=%d body=%s", reset.Code, reset.Body.String())
	}
	select {
	case <-restarted:
	case <-time.After(time.Second):
		t.Fatal("full reset did not schedule a restart")
	}
}
