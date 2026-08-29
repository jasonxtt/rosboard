package policy

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

type testResolver struct {
	mu      sync.Mutex
	answers map[string][]netip.Addr
	calls   []string
}

func (r *testResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, host)
	ips, ok := r.answers[host]
	if !ok {
		return nil, fmt.Errorf("no test DNS answer for %s", host)
	}
	return append([]netip.Addr(nil), ips...), nil
}

func (r *testResolver) callCount(host string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	count := 0
	for _, call := range r.calls {
		if call == host {
			count++
		}
	}
	return count
}

func testHTTPSURL(server *httptest.Server, host, resource string) string {
	port := server.Listener.Addr().(*net.TCPAddr).Port
	return fmt.Sprintf("https://%s:%d%s", host, port, resource)
}

func testFetcher(server *httptest.Server, resolver *testResolver, dial func(context.Context, string, string) (net.Conn, error)) *SourceFetcher {
	return NewSourceFetcher(FetcherOptions{
		Resolver:    resolver,
		DialContext: dial,
		TLSConfig:   &tls.Config{InsecureSkipVerify: true}, // test server only
	})
}

func localDialer(server *httptest.Server, seen *string) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		*seen = address
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}
}

func TestSourceFetcherFetchesHTTPSAndPreservesValidators(t *testing.T) {
	var gotHost, gotSNI string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHost = r.Host
		if r.TLS != nil {
			gotSNI = r.TLS.ServerName
		}
		if r.Header.Get("If-None-Match") == `"old"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("ETag", `"new"`)
		w.Header().Set("Last-Modified", "Wed, 21 Oct 2015 07:28:00 GMT")
		_, _ = w.Write([]byte("payload:\n  - DOMAIN,example.com\n"))
	}))
	defer server.Close()

	resolver := &testResolver{answers: map[string][]netip.Addr{
		"source.test": {netip.MustParseAddr("93.184.216.34")},
	}}
	var dialed string
	fetcher := testFetcher(server, resolver, localDialer(server, &dialed))
	requestURL := testHTTPSURL(server, "source.test", "/source.yaml")

	result, err := fetcher.Fetch(context.Background(), requestURL, FetchOptions{})
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if result.StatusCode != http.StatusOK || result.NotModified {
		t.Fatalf("unexpected result: %#v", result)
	}
	if result.ETag != `"new"` || result.LastModified == "" || result.SHA256 == "" {
		t.Fatalf("missing response metadata: %#v", result)
	}
	parsed, _ := url.Parse(requestURL)
	if gotHost != parsed.Host || gotSNI != parsed.Hostname() {
		t.Fatalf("Host/SNI = %q/%q, want %q/%q", gotHost, gotSNI, parsed.Host, parsed.Hostname())
	}
	if dialed == "" || !strings.HasPrefix(dialed, "93.184.216.34:") {
		t.Fatalf("dial was not pinned to validated IP: %q", dialed)
	}

	result, err = fetcher.Fetch(context.Background(), requestURL, FetchOptions{ETag: `"old"`, LastModified: "Wed, 21 Oct 2015 07:28:00 GMT"})
	if err != nil {
		t.Fatalf("Fetch() 304 error = %v", err)
	}
	if !result.NotModified || result.StatusCode != http.StatusNotModified || result.Body != nil {
		t.Fatalf("unexpected 304 result: %#v", result)
	}
}

func TestSourceFetcherPreviewBuildsPendingVersionAndSharesUploadPreparation(t *testing.T) {
	body := []byte("payload:\n  - DOMAIN,Example.com.\n  - DOMAIN-SUFFIX,bücher.de\n")
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/acme/rules/main/config.yaml" {
			t.Errorf("raw GitHub path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/yaml")
		w.Header().Set("ETag", `"rules-1"`)
		_, _ = w.Write(body)
	}))
	defer server.Close()
	resolver := &testResolver{answers: map[string][]netip.Addr{
		"raw.githubusercontent.com": {netip.MustParseAddr("93.184.216.34")},
	}}
	fetcher := testFetcher(server, resolver, localDialer(server, new(string)))
	preview, err := fetcher.Preview(context.Background(), "https://github.com/acme/rules/blob/main/config.yaml", FetchOptions{})
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
	if preview.URL != "https://raw.githubusercontent.com/acme/rules/main/config.yaml" || preview.ETag != `"rules-1"` || preview.NotModified {
		t.Fatalf("unexpected URL preview metadata: %#v", preview)
	}
	if len(preview.Rules) != 2 || preview.Rules[0].Type != RuleTypeExact || preview.Rules[1].Type != RuleTypeSuffix {
		t.Fatalf("URL preview did not preserve both rule semantics: %#v", preview.Rules)
	}
	version, rules, err := preview.PendingVersion("router-a", "source-a", "version-url")
	if err != nil {
		t.Fatalf("URL PendingVersion() error = %v", err)
	}
	if version.State != "pending" || version.SHA256 != preview.SHA256 || len(rules) != 2 {
		t.Fatalf("unexpected URL pending version: %#v %#v", version, rules)
	}

	upload, err := NewUploadService(t.TempDir()).Preview(context.Background(), "rules.yaml", bytes.NewReader(body), KindDomain)
	if err != nil {
		t.Fatalf("Upload Preview() error = %v", err)
	}
	if upload.SHA256 != preview.SHA256 || !bytes.Equal(upload.CompressedYAML, preview.CompressedYAML) || len(upload.Rules) != len(preview.Rules) {
		t.Fatal("URL and upload did not use the same prepared source content")
	}
}

func TestSourceFetcherPreviewRejectsNonClashUTF8(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("plain text is not a Clash source"))
	}))
	defer server.Close()
	resolver := &testResolver{answers: map[string][]netip.Addr{"source.test": {netip.MustParseAddr("93.184.216.34")}}}
	fetcher := testFetcher(server, resolver, localDialer(server, new(string)))
	if _, err := fetcher.Preview(context.Background(), testHTTPSURL(server, "source.test", "/not-clash"), FetchOptions{}); err == nil {
		t.Fatal("non-Clash UTF-8 source was accepted as a preview")
	}
}

func TestSourceFetcher304RequiresValidatorsAndPreservesMissingResponseValidators(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == "" && r.Header.Get("If-Modified-Since") == "" {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.WriteHeader(http.StatusNotModified)
	}))
	defer server.Close()
	resolver := &testResolver{answers: map[string][]netip.Addr{"source.test": {netip.MustParseAddr("93.184.216.34")}}}
	fetcher := testFetcher(server, resolver, localDialer(server, new(string)))
	requestURL := testHTTPSURL(server, "source.test", "/not-modified")
	if _, err := fetcher.Fetch(context.Background(), requestURL, FetchOptions{}); err == nil {
		t.Fatal("304 without validators was accepted")
	}
	result, err := fetcher.Fetch(context.Background(), requestURL, FetchOptions{ETag: `"old"`, LastModified: "Wed, 21 Oct 2015 07:28:00 GMT"})
	if err != nil {
		t.Fatalf("validated 304 error = %v", err)
	}
	if !result.NotModified || result.Body != nil || result.ETag != `"old"` || result.LastModified != "Wed, 21 Oct 2015 07:28:00 GMT" {
		t.Fatalf("304 did not preserve validator state: %#v", result)
	}
	preview, err := fetcher.Preview(context.Background(), requestURL, FetchOptions{ETag: `"old"`})
	if err != nil {
		t.Fatalf("304 preview error = %v", err)
	}
	if !preview.NotModified || preview.PreparedSourceContent.SHA256 != "" {
		t.Fatalf("304 preview unexpectedly contains content: %#v", preview)
	}
	if _, _, err := preview.PendingVersion("router-a", "source-a", "version-304"); err == nil {
		t.Fatal("304 preview produced a pending version")
	}
}

func TestSourceFetcherContentTypesAndMissingContentPolicy(t *testing.T) {
	body := []byte("payload:\n  - DOMAIN,example.com\n")
	for _, contentType := range []string{"text/plain", "application/yaml", "application/x-yaml", "text/yaml"} {
		t.Run(contentType, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", contentType)
				_, _ = w.Write(body)
			}))
			defer server.Close()
			resolver := &testResolver{answers: map[string][]netip.Addr{"source.test": {netip.MustParseAddr("93.184.216.34")}}}
			fetcher := testFetcher(server, resolver, localDialer(server, new(string)))
			if _, err := fetcher.Preview(context.Background(), testHTTPSURL(server, "source.test", "/source.yaml"), FetchOptions{}); err != nil {
				t.Fatalf("approved content type rejected: %v", err)
			}
		})
	}
	for _, contentType := range []string{"application/octet-stream", "text/plain; charset=iso-8859-1"} {
		t.Run("reject "+contentType, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", contentType)
				_, _ = w.Write(body)
			}))
			defer server.Close()
			resolver := &testResolver{answers: map[string][]netip.Addr{"source.test": {netip.MustParseAddr("93.184.216.34")}}}
			fetcher := testFetcher(server, resolver, localDialer(server, new(string)))
			if _, err := fetcher.Fetch(context.Background(), testHTTPSURL(server, "source.test", "/source.bin"), FetchOptions{}); err == nil {
				t.Fatal("unapproved content type accepted")
			}
		})
	}
	if err := validateSourceContentType("", []byte{0x00, 0x01, 0x02}); err == nil {
		t.Fatal("missing content type with binary body was accepted")
	}
	if err := validateSourceContentType("", body); err != nil {
		t.Fatalf("missing content type with sniffable YAML rejected: %v", err)
	}
}

func TestSourceFetcherErrorsDoNotLeakURLSecrets(t *testing.T) {
	secret := "super-secret"
	if _, err := NormalizeSourceURL("https://user:" + secret + "@example.com/%zz"); err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("malformed URL error leaked secret or was not returned: %v", err)
	}

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "https://user:redirect-secret@example.com/%zz")
		w.WriteHeader(http.StatusFound)
	}))
	defer server.Close()
	resolver := &testResolver{answers: map[string][]netip.Addr{"source.test": {netip.MustParseAddr("93.184.216.34")}}}
	fetcher := testFetcher(server, resolver, func(context.Context, string, string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(context.Background(), "tcp", server.Listener.Addr().String())
	})
	if _, err := fetcher.Fetch(context.Background(), testHTTPSURL(server, "source.test", "/redirect-secret"), FetchOptions{}); err == nil || strings.Contains(err.Error(), "redirect-secret") {
		t.Fatalf("malformed redirect error leaked secret or was not returned: %v", err)
	}

	failing := testFetcher(server, resolver, func(context.Context, string, string) (net.Conn, error) {
		return nil, fmt.Errorf("dial failed for %s", secret)
	})
	if _, err := failing.Fetch(context.Background(), testHTTPSURL(server, "source.test", "/dial-error"), FetchOptions{}); err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("client error leaked secret or was not returned: %v", err)
	}
}

func TestNormalizeSourceURLGitHubBlobAndRejectsUnsafeURLs(t *testing.T) {
	got, err := NormalizeSourceURL("https://github.com/acme/rules/blob/main/config.yaml")
	if err != nil {
		t.Fatalf("NormalizeSourceURL() error = %v", err)
	}
	if want := "https://raw.githubusercontent.com/acme/rules/main/config.yaml"; got != want {
		t.Fatalf("normalized URL = %q, want %q", got, want)
	}

	for _, raw := range []string{
		"http://example.com/source.yaml",
		"https://user:pass@example.com/source.yaml",
		"https://example.com/source.yaml#fragment",
		"https://github.com/acme/rules/tree/main/config.yaml",
		"https://github.com/acme/rules/blob/main/../secret.yaml",
	} {
		if _, err := NormalizeSourceURL(raw); err == nil {
			t.Errorf("NormalizeSourceURL(%q) error = nil, want error", raw)
		}
	}
}

func TestSourceFetcherRejectsPrivateMixedDNSAndRedirectTargets(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "https://private.test/secret")
		w.WriteHeader(http.StatusFound)
	}))
	defer server.Close()
	resolver := &testResolver{answers: map[string][]netip.Addr{
		"source.test":  {netip.MustParseAddr("93.184.216.34")},
		"private.test": {netip.MustParseAddr("93.184.216.34"), netip.MustParseAddr("10.0.0.1")},
	}}
	fetcher := testFetcher(server, resolver, localDialer(server, new(string)))
	if _, err := fetcher.Fetch(context.Background(), testHTTPSURL(server, "source.test", "/redirect"), FetchOptions{}); err == nil {
		t.Fatal("unsafe mixed-DNS redirect was accepted")
	}
	if resolver.callCount("private.test") != 1 {
		t.Fatalf("redirect target was not resolved exactly once: %d", resolver.callCount("private.test"))
	}
}

func TestSourceFetcherRejectsLocalAndReservedAddresses(t *testing.T) {
	for _, raw := range []string{
		"10.0.0.1",
		"127.0.0.1",
		"169.254.1.1",
		"172.16.0.1",
		"192.168.1.1",
		"100.64.0.1",
		"198.18.0.1",
		"192.0.2.1",
		"198.51.100.1",
		"203.0.113.1",
		"224.0.0.1",
		"240.0.0.1",
		"255.255.255.255",
		"fc00::1",
		"fe80::1",
		"ff02::1",
		"100::1",
		"2001:0::1",
		"2001:2::1",
		"2001:db8::1",
		"3fff::1",
	} {
		ip, err := netip.ParseAddr(raw)
		if err != nil {
			t.Fatalf("ParseAddr(%q): %v", raw, err)
		}
		if !isForbiddenSourceIP(ip) {
			t.Errorf("isForbiddenSourceIP(%q) = false, want true", raw)
		}
	}
	if isForbiddenSourceIP(netip.MustParseAddr("93.184.216.34")) {
		t.Fatal("public address was rejected")
	}
	if isForbiddenSourceIP(netip.MustParseAddr("2001:4860:4860::8888")) {
		t.Fatal("public IPv6 address was rejected")
	}
}

func TestSourceFetcherRejectsBodyLimitMIMEAndInvalidUTF8(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        func() []byte
	}{
		{name: "too large", contentType: "text/plain", body: func() []byte { return make([]byte, MaxSourceBytes+1) }},
		{name: "binary mime", contentType: "application/octet-stream", body: func() []byte { return []byte("payload: []\n") }},
		{name: "invalid utf8", contentType: "text/plain", body: func() []byte { return []byte{0xff} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tt.contentType)
				_, _ = w.Write(tt.body())
			}))
			defer server.Close()
			resolver := &testResolver{answers: map[string][]netip.Addr{
				"source.test": {netip.MustParseAddr("93.184.216.34")},
			}}
			fetcher := testFetcher(server, resolver, localDialer(server, new(string)))
			if _, err := fetcher.Fetch(context.Background(), testHTTPSURL(server, "source.test", "/source.yaml"), FetchOptions{}); err == nil {
				t.Fatal("unsafe response was accepted")
			}
		})
	}
}

func TestSourceFetcherRejectsMoreThanFiveRedirectsAndHonorsTimeout(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var next int
		_, _ = fmt.Sscanf(r.URL.Query().Get("n"), "%d", &next)
		w.Header().Set("Location", fmt.Sprintf("/redirect?n=%d", next+1))
		w.WriteHeader(http.StatusFound)
	}))
	defer server.Close()
	resolver := &testResolver{answers: map[string][]netip.Addr{"source.test": {netip.MustParseAddr("93.184.216.34")}}}
	fetcher := testFetcher(server, resolver, localDialer(server, new(string)))
	if _, err := fetcher.Fetch(context.Background(), testHTTPSURL(server, "source.test", "/redirect?n=0"), FetchOptions{}); err == nil {
		t.Fatal("more than five redirects were accepted")
	}

	slow := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(100 * time.Millisecond):
			_, _ = w.Write([]byte("payload:\n  - DOMAIN,slow.example\n"))
		case <-r.Context().Done():
		}
	}))
	defer slow.Close()
	slowResolver := &testResolver{answers: map[string][]netip.Addr{"source.test": {netip.MustParseAddr("93.184.216.34")}}}
	slowFetcher := testFetcher(slow, slowResolver, localDialer(slow, new(string)))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := slowFetcher.Fetch(ctx, testHTTPSURL(slow, "source.test", "/slow"), FetchOptions{}); err == nil {
		t.Fatal("timed out fetch was accepted")
	}
}
