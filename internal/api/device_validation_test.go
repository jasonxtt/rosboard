package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"rosboard/internal/config"
	"rosboard/internal/routeros"
)

func TestDeviceCreateRequiresMatchingVerificationAndCanonicalizesCIDRs(t *testing.T) {
	router := newDeviceTestRouter(t)
	defer router.Close()
	scheme, host, port := testConnectionParts(t, router.URL)
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := config.Config{Path: path, PollIntervalSeconds: 10, RealtimePollIntervalSeconds: 1, TerminalPollIntervalSeconds: 3, SampleRetentionHours: 48}
	server := NewServer(cfg, nil, nil)

	withoutTicket := `{"name":"Edge","enabled":true,"scheme":"` + scheme + `","host":"` + host + `","port":` + strconv.Itoa(port) + `,"username":"admin","password":" secret ","trafficInterfaces":["ether1"],"terminalCidrs":["10.0.0.7/24"]}`
	response := serveJSON(server, http.MethodPost, "/api/devices", withoutTicket)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "verification") {
		t.Fatalf("missing-ticket status=%d body=%s", response.Code, response.Body.String())
	}

	baseURL, err := normalizedRouterOSURL(scheme, host, port)
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := server.tickets.issue(connectionFingerprint(baseURL, "admin", " secret "), []routeros.VerificationInterface{{Name: "ether1"}})
	if err != nil {
		t.Fatal(err)
	}
	withTicket := strings.TrimSuffix(withoutTicket, "}") + `,"verificationToken":"` + token + `"}`
	response = serveJSON(server, http.MethodPost, "/api/devices", withTicket)
	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	saved, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Devices) != 1 || saved.Devices[0].RouterOS.Password != " secret " {
		t.Fatalf("unexpected saved device: %#v", saved.Devices)
	}
	if got := saved.Devices[0].RouterOS.TerminalCIDRs; len(got) != 1 || got[0] != "10.0.0.0/24" {
		t.Fatalf("CIDRs were not canonicalized: %v", got)
	}
}

func TestDeviceCreateOnlyCompletesOnboardingWhenRequested(t *testing.T) {
	restarted := make(chan struct{}, 2)
	server, _ := newAuthServerWithRestart(t, nil, func() { restarted <- struct{}{} })
	created := authRequest(t, server, http.MethodPost, "/api/setup/admin", `{"username":"admin","password":"1234","passwordConfirmation":"1234"}`, nil)
	cookie := responseCookie(t, created)

	createDevice := func(router *httptest.Server, completeOnboarding bool) *httptest.ResponseRecorder {
		t.Helper()
		scheme, host, port := testConnectionParts(t, router.URL)
		baseURL, err := normalizedRouterOSURL(scheme, host, port)
		if err != nil {
			t.Fatal(err)
		}
		token, _, err := server.tickets.issue(connectionFingerprint(baseURL, "admin", "secret"), []routeros.VerificationInterface{{Name: "ether1"}})
		if err != nil {
			t.Fatal(err)
		}
		body := `{"name":"Edge","enabled":true,"scheme":"` + scheme + `","host":"` + host + `","port":` + strconv.Itoa(port) + `,"username":"admin","password":"secret","trafficInterfaces":["ether1"],"terminalCidrs":["10.0.0.0/24"],"verificationToken":"` + token + `","completeOnboarding":` + strconv.FormatBool(completeOnboarding) + `,"deferRestart":` + strconv.FormatBool(!completeOnboarding) + `}`
		return authRequest(t, server, http.MethodPost, "/api/devices", body, cookie)
	}

	firstRouter := newDeviceTestRouter(t)
	defer firstRouter.Close()
	firstResponse := createDevice(firstRouter, false)
	if firstResponse.Code != http.StatusCreated {
		t.Fatalf("save-only status=%d body=%s", firstResponse.Code, firstResponse.Body.String())
	}
	var saved struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(firstResponse.Body.Bytes(), &saved); err != nil || saved.ID == "" {
		t.Fatalf("decode saved device: %v body=%s", err, firstResponse.Body.String())
	}
	if bootstrap := authRequest(t, server, http.MethodGet, "/api/bootstrap", "", cookie); responsePhase(t, bootstrap) != "needs_routeros" {
		t.Fatalf("save-only completed onboarding: %s", bootstrap.Body.String())
	}
	select {
	case <-restarted:
		t.Fatal("save-only restarted the panel")
	case <-time.After(150 * time.Millisecond):
	}

	scheme, host, port := testConnectionParts(t, firstRouter.URL)
	body := `{"name":"Edge","enabled":true,"scheme":"` + scheme + `","host":"` + host + `","port":` + strconv.Itoa(port) + `,"username":"admin","password":"","trafficInterfaces":["ether1"],"terminalCidrs":["10.0.0.0/24"],"completeOnboarding":true}`
	if response := authRequest(t, server, http.MethodPut, "/api/devices/"+saved.ID, body, cookie); response.Code != http.StatusOK {
		t.Fatalf("save-and-enter status=%d body=%s", response.Code, response.Body.String())
	}
	if bootstrap := authRequest(t, server, http.MethodGet, "/api/bootstrap", "", cookie); responsePhase(t, bootstrap) != "ready" {
		t.Fatalf("save-and-enter did not complete onboarding: %s", bootstrap.Body.String())
	}
	select {
	case <-restarted:
	case <-time.After(time.Second):
		t.Fatal("save-and-enter did not restart the panel")
	}
}

func TestUnsavedDeviceCanCompleteOnboardingDirectly(t *testing.T) {
	restarted := make(chan struct{}, 1)
	server, _ := newAuthServerWithRestart(t, nil, func() { restarted <- struct{}{} })
	created := authRequest(t, server, http.MethodPost, "/api/setup/admin", `{"username":"admin","password":"1234","passwordConfirmation":"1234"}`, nil)
	cookie := responseCookie(t, created)
	router := newDeviceTestRouter(t)
	defer router.Close()
	scheme, host, port := testConnectionParts(t, router.URL)
	baseURL, err := normalizedRouterOSURL(scheme, host, port)
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := server.tickets.issue(connectionFingerprint(baseURL, "admin", "secret"), []routeros.VerificationInterface{{Name: "ether1"}})
	if err != nil {
		t.Fatal(err)
	}
	body := `{"name":"Edge","enabled":true,"scheme":"` + scheme + `","host":"` + host + `","port":` + strconv.Itoa(port) + `,"username":"admin","password":"secret","trafficInterfaces":["ether1"],"terminalCidrs":["10.0.0.0/24"],"verificationToken":"` + token + `","completeOnboarding":true}`
	response := authRequest(t, server, http.MethodPost, "/api/devices", body, cookie)
	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if len(server.configSnapshot().Devices) != 1 {
		t.Fatalf("device was not saved: %#v", server.configSnapshot().Devices)
	}
	if bootstrap := authRequest(t, server, http.MethodGet, "/api/bootstrap", "", cookie); responsePhase(t, bootstrap) != "ready" {
		t.Fatalf("direct completion did not enter ready phase: %s", bootstrap.Body.String())
	}
	select {
	case <-restarted:
	case <-time.After(time.Second):
		t.Fatal("direct completion did not restart the panel")
	}
}

func TestDeviceCreateRejectsEndpointUsedByArchivedDevice(t *testing.T) {
	router := newDeviceTestRouter(t)
	defer router.Close()
	scheme, host, port := testConnectionParts(t, router.URL)
	baseURL, _ := normalizedRouterOSURL(scheme, host, port)
	cfg := config.Config{
		Path: filepath.Join(t.TempDir(), "config.yaml"), PollIntervalSeconds: 10, RealtimePollIntervalSeconds: 1, TerminalPollIntervalSeconds: 3, SampleRetentionHours: 48,
		Devices: []config.DeviceConfig{{ID: "old", Name: "Old", Archived: true, RouterOS: config.RouterOSConfig{BaseURL: baseURL, Username: "admin", Password: "secret"}}},
	}
	server := NewServer(cfg, nil, nil)
	token, _, err := server.tickets.issue(connectionFingerprint(baseURL, "other", "secret"), []routeros.VerificationInterface{{Name: "ether1"}})
	if err != nil {
		t.Fatal(err)
	}
	body := `{"name":"New","enabled":true,"scheme":"` + scheme + `","host":"` + host + `","port":` + strconv.Itoa(port) + `,"username":"other","password":"secret","trafficInterfaces":["ether1"],"terminalCidrs":["10.0.0.0/24"],"verificationToken":"` + token + `"}`
	response := serveJSON(server, http.MethodPost, "/api/devices", body)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "already uses") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestDeviceEditKeepsBlankPasswordWithoutRetest(t *testing.T) {
	router := newDeviceTestRouter(t)
	defer router.Close()
	scheme, host, port := testConnectionParts(t, router.URL)
	baseURL, _ := normalizedRouterOSURL(scheme, host, port)
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := config.Config{
		Path: path, PollIntervalSeconds: 10, RealtimePollIntervalSeconds: 1, TerminalPollIntervalSeconds: 3, SampleRetentionHours: 48,
		Devices: []config.DeviceConfig{{ID: "edge", Name: "Edge", Enabled: true, RouterOS: config.RouterOSConfig{BaseURL: baseURL, Username: "admin", Password: "secret", TrafficInterfaces: []string{"ether1"}, TerminalCIDRs: []string{"10.0.0.0/24"}}}},
	}
	server := NewServer(cfg, nil, nil)
	body := `{"name":"Renamed","enabled":true,"scheme":"` + scheme + `","host":"` + host + `","port":` + strconv.Itoa(port) + `,"username":"admin","password":"","trafficInterfaces":["ether1"],"terminalCidrs":["10.0.0.0/24"]}`
	response := serveJSON(server, http.MethodPut, "/api/devices/edge", body)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	saved, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Devices[0].Name != "Renamed" || saved.Devices[0].RouterOS.Password != "secret" {
		t.Fatalf("unexpected saved device: %#v", saved.Devices[0])
	}
}

func newDeviceTestRouter(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/rest/interface":
			_, _ = writer.Write([]byte(`[{"name":"ether1","running":"true"}]`))
		case request.Method == http.MethodPost && request.URL.Path == "/rest/interface/monitor-traffic":
			_, _ = writer.Write([]byte(`[{"name":"ether1","rx-bits-per-second":"1","tx-bits-per-second":"2"}]`))
		default:
			http.NotFound(writer, request)
		}
	}))
}

func testConnectionParts(t *testing.T, rawURL string) (string, string, int) {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatal(err)
	}
	return parsed.Scheme, parsed.Hostname(), port
}

func serveJSON(server http.Handler, method, path, body string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	server.ServeHTTP(response, request)
	return response
}
