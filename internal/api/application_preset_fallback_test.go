package api

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"

	"rosboard/internal/applicationpreset"
	"rosboard/internal/policy"
	"rosboard/internal/policyv2"
)

const testBlackmatrixRulePath = "rule/Clash/Fixture/Fixture.yaml"

type applicationPresetFetchHarness struct {
	server         *httptest.Server
	fetcher        *policy.SourceFetcher
	primaryMode    string
	primaryStatus  int
	fallbackStatus int
	primaryBody    []byte
	fallbackBody   []byte
	mu             sync.Mutex
	primaryCalls   int
	fallbackCalls  int
}

func newApplicationPresetFetchHarness(t *testing.T, primaryMode string, primaryStatus, fallbackStatus int, primaryBody, fallbackBody []byte) *applicationPresetFetchHarness {
	t.Helper()
	harness := &applicationPresetFetchHarness{
		primaryMode: primaryMode, primaryStatus: primaryStatus, fallbackStatus: fallbackStatus,
		primaryBody: primaryBody, fallbackBody: fallbackBody,
	}
	harness.server = httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		isPrimary := request.Host == "raw.githubusercontent.com"
		harness.mu.Lock()
		if isPrimary {
			harness.primaryCalls++
		} else if request.Host == "cdn.jsdelivr.net" {
			harness.fallbackCalls++
		}
		harness.mu.Unlock()
		if !isPrimary && request.Host != "cdn.jsdelivr.net" {
			http.Error(writer, "unexpected host", http.StatusBadGateway)
			return
		}
		status := http.StatusOK
		body := harness.primaryBody
		if !isPrimary {
			status = harness.fallbackStatus
			body = harness.fallbackBody
		}
		if isPrimary {
			status = harness.primaryStatus
		}
		if status == 0 {
			status = http.StatusOK
		}
		if status != http.StatusOK {
			writer.WriteHeader(status)
			return
		}
		writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = writer.Write(body)
	}))
	t.Cleanup(harness.server.Close)
	resolver := applicationPresetResolver{harness: harness}
	dialer := &net.Dialer{}
	harness.fetcher = policy.NewSourceFetcher(policy.FetcherOptions{
		Resolver: resolver,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			if strings.HasPrefix(address, "93.184.216.34:") {
				switch harness.primaryMode {
				case "timeout":
					return nil, context.DeadlineExceeded
				case "network":
					return nil, errors.New("connect failed")
				}
			}
			return dialer.DialContext(ctx, network, harness.server.Listener.Addr().String())
		},
		TLSConfig: &tls.Config{InsecureSkipVerify: true}, // test server only
	})
	return harness
}

type applicationPresetResolver struct {
	harness *applicationPresetFetchHarness
}

func (r applicationPresetResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	if host == "raw.githubusercontent.com" && r.harness.primaryMode == "dns" {
		return nil, errors.New("DNS unavailable")
	}
	switch host {
	case "raw.githubusercontent.com":
		return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
	case "cdn.jsdelivr.net":
		return []netip.Addr{netip.MustParseAddr("93.184.216.35")}, nil
	default:
		return nil, errors.New("unexpected host")
	}
}

func blackmatrixTestPreset() applicationpreset.ApplicationPreset {
	return applicationpreset.ApplicationPreset{
		ID: "fixture", Name: "Fixture", Category: "测试", RulePath: testBlackmatrixRulePath,
		RuleURL: "https://raw.githubusercontent.com/blackmatrix7/ios_rule_script/master/" + testBlackmatrixRulePath,
	}
}

func presetPreviewRequest(t *testing.T, server *Server, preset applicationpreset.ApplicationPreset) *httptest.ResponseRecorder {
	t.Helper()
	device, ok := server.resolvePolicyDevice(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/?device=edge", nil))
	if !ok {
		t.Fatal("failed to resolve test policy device")
	}
	request := httptest.NewRequest(http.MethodPost, "/api/application-presets/fixture/preview?device=edge", nil)
	response := httptest.NewRecorder()
	server.previewApplicationPreset(response, request, device, preset)
	return response
}

func newPresetFallbackAPIServer(t *testing.T, harness *applicationPresetFetchHarness, preset applicationpreset.ApplicationPreset) *Server {
	t.Helper()
	server, storage := newPolicyV2APIServer(t)
	t.Cleanup(func() { _ = storage.Close() })
	server.sourceFetcher = harness.fetcher
	server.SetApplicationPresetRegistry(applicationpreset.New([]applicationpreset.ApplicationPreset{preset}))
	return server
}

func (h *applicationPresetFetchHarness) counts() (int, int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.primaryCalls, h.fallbackCalls
}

func TestApplicationPresetUsesCDNFallbackOnlyForRetryablePrimaryFailures(t *testing.T) {
	validBody := []byte("DOMAIN-SUFFIX,cdn.example\n")
	tests := []struct {
		name            string
		primaryMode     string
		primaryStatus   int
		wantStatus      int
		wantFallback    int
		wantPrimaryBody []byte
	}{
		{name: "github success", wantStatus: http.StatusOK, wantFallback: 0, wantPrimaryBody: validBody},
		{name: "timeout", primaryMode: "timeout", wantStatus: http.StatusOK, wantFallback: 1, wantPrimaryBody: validBody},
		{name: "dns failure", primaryMode: "dns", wantStatus: http.StatusOK, wantFallback: 1, wantPrimaryBody: validBody},
		{name: "network failure", primaryMode: "network", wantStatus: http.StatusOK, wantFallback: 1, wantPrimaryBody: validBody},
		{name: "github 503", primaryStatus: http.StatusServiceUnavailable, wantStatus: http.StatusOK, wantFallback: 1, wantPrimaryBody: validBody},
		{name: "github 429", primaryStatus: http.StatusTooManyRequests, wantStatus: http.StatusOK, wantFallback: 1, wantPrimaryBody: validBody},
		{name: "github 404", primaryStatus: http.StatusNotFound, wantStatus: http.StatusBadGateway, wantFallback: 0, wantPrimaryBody: validBody},
		{name: "invalid content", wantStatus: http.StatusUnprocessableEntity, wantFallback: 0, wantPrimaryBody: []byte("not a supported source")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			harness := newApplicationPresetFetchHarness(t, test.primaryMode, test.primaryStatus, http.StatusOK, test.wantPrimaryBody, validBody)
			server := newPresetFallbackAPIServer(t, harness, blackmatrixTestPreset())
			response := presetPreviewRequest(t, server, blackmatrixTestPreset())
			if response.Code != test.wantStatus {
				t.Fatalf("preview status=%d body=%s, want %d", response.Code, response.Body.String(), test.wantStatus)
			}
			_, fallbackCalls := harness.counts()
			if fallbackCalls != test.wantFallback {
				t.Fatalf("CDN requests=%d, want %d; body=%s", fallbackCalls, test.wantFallback, response.Body.String())
			}
		})
	}
}

func TestApplicationPresetFallbackKeepsCanonicalGitHubProvenance(t *testing.T) {
	validBody := []byte("DOMAIN-SUFFIX,cdn.example\n")
	harness := newApplicationPresetFetchHarness(t, "network", http.StatusOK, http.StatusOK, validBody, validBody)
	preset := blackmatrixTestPreset()
	server := newPresetFallbackAPIServer(t, harness, preset)
	previewResponse := presetPreviewRequest(t, server, preset)
	if previewResponse.Code != http.StatusOK {
		t.Fatalf("fallback preview status=%d body=%s", previewResponse.Code, previewResponse.Body.String())
	}
	var preview struct {
		PreviewID string `json:"previewId"`
		RuleURL   string `json:"ruleURL"`
	}
	if err := json.Unmarshal(previewResponse.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	if preview.PreviewID == "" || preview.RuleURL != preset.RuleURL {
		t.Fatalf("fallback preview lost canonical RuleURL: %#v", preview)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/application-presets/fixture/target-lists?device=edge&previewId="+preview.PreviewID, bytes.NewBufferString(`{"requestedKinds":["domain"]}`))
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("fallback materialize status=%d body=%s", response.Code, response.Body.String())
	}
	var materialized struct {
		TargetLists []policyv2.TargetList `json:"targetLists"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &materialized); err != nil {
		t.Fatal(err)
	}
	if len(materialized.TargetLists) != 1 || materialized.TargetLists[0].URL != preset.RuleURL {
		t.Fatalf("fallback materialize changed canonical target provenance: %#v", materialized.TargetLists)
	}
	_, fallbackCalls := harness.counts()
	if fallbackCalls != 1 {
		t.Fatalf("fallback request count=%d, want one", fallbackCalls)
	}
}

func TestApplicationPresetFallbackRequiresCanonicalBuiltInURL(t *testing.T) {
	validBody := []byte("DOMAIN-SUFFIX,cdn.example\n")
	harness := newApplicationPresetFetchHarness(t, "network", http.StatusOK, http.StatusOK, validBody, validBody)
	preset := applicationpreset.ApplicationPreset{ID: "fixture", Name: "Fixture", RuleURL: "https://preset.example.test/rules.yaml"}
	server := newPresetFallbackAPIServer(t, harness, preset)
	response := presetPreviewRequest(t, server, preset)
	if response.Code != http.StatusBadGateway {
		t.Fatalf("non-canonical preset status=%d body=%s", response.Code, response.Body.String())
	}
	_, fallbackCalls := harness.counts()
	if fallbackCalls != 0 {
		t.Fatalf("non-canonical preset used CDN fallback: %d requests", fallbackCalls)
	}
}

func TestApplicationPresetCDNFailureIsSafeAndClear(t *testing.T) {
	validBody := []byte("DOMAIN-SUFFIX,cdn.example\n")
	harness := newApplicationPresetFetchHarness(t, "", http.StatusServiceUnavailable, http.StatusServiceUnavailable, validBody, validBody)
	server := newPresetFallbackAPIServer(t, harness, blackmatrixTestPreset())
	response := presetPreviewRequest(t, server, blackmatrixTestPreset())
	if response.Code != http.StatusBadGateway {
		t.Fatalf("CDN failure status=%d body=%s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "raw.githubusercontent.com") || strings.Contains(response.Body.String(), "cdn.jsdelivr.net") {
		t.Fatalf("CDN failure leaked source URLs: %s", response.Body.String())
	}
	_, fallbackCalls := harness.counts()
	if fallbackCalls != 1 {
		t.Fatalf("CDN failure request count=%d, want one", fallbackCalls)
	}
}

func TestUserURLPreviewDoesNotUseApplicationPresetCDNFallback(t *testing.T) {
	validBody := []byte("DOMAIN-SUFFIX,cdn.example\n")
	harness := newApplicationPresetFetchHarness(t, "network", http.StatusOK, http.StatusOK, validBody, validBody)
	server, storage := newPolicyV2APIServer(t)
	t.Cleanup(func() { _ = storage.Close() })
	server.sourceFetcher = harness.fetcher
	request := httptest.NewRequest(http.MethodPost, "/api/policy-routing/sources/url/preview?device=edge", bytes.NewBufferString(`{"url":"https://raw.githubusercontent.com/example/rules/master/rules.yaml","kind":"domain"}`))
	response := httptest.NewRecorder()
	server.servePolicyURLPreview(response, request)
	if response.Code != http.StatusBadGateway {
		t.Fatalf("user URL preview status=%d body=%s", response.Code, response.Body.String())
	}
	_, fallbackCalls := harness.counts()
	if fallbackCalls != 0 {
		t.Fatalf("ordinary user URL triggered CDN fallback: %d requests", fallbackCalls)
	}
}
