package policy

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestSourceFetcherClassifiesRetryableHTTPStatuses(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout} {
		t.Run(fmt.Sprintf("status-%d", status), func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
			defer server.Close()
			server.Config.Handler = http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { writer.WriteHeader(status) })
			resolver := &testResolver{answers: map[string][]netip.Addr{"source.test": {netip.MustParseAddr("93.184.216.34")}}}
			fetcher := testFetcher(server, resolver, localDialer(server, new(string)))
			_, err := fetcher.Fetch(context.Background(), testHTTPSURL(server, "source.test", "/rules.yaml"), FetchOptions{})
			if err == nil || !IsRetryableSourceError(err) || !errors.Is(err, ErrSourceHTTP) {
				t.Fatalf("status %d classification: err=%v retryable=%t", status, err, IsRetryableSourceError(err))
			}
			var statusErr *SourceHTTPStatusError
			if !errors.As(err, &statusErr) || statusErr.StatusCode != status {
				t.Fatalf("status error=%#v, want status %d", statusErr, status)
			}
		})
	}
}

func TestSourceFetcherDoesNotClassifyMissingHTTPResourcesAsRetryable(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusGone} {
		t.Run(fmt.Sprintf("status-%d", status), func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { writer.WriteHeader(status) }))
			defer server.Close()
			resolver := &testResolver{answers: map[string][]netip.Addr{"source.test": {netip.MustParseAddr("93.184.216.34")}}}
			fetcher := testFetcher(server, resolver, localDialer(server, new(string)))
			_, err := fetcher.Fetch(context.Background(), testHTTPSURL(server, "source.test", "/rules.yaml"), FetchOptions{})
			if err == nil || IsRetryableSourceError(err) {
				t.Fatalf("status %d was incorrectly classified as retryable: %v", status, err)
			}
		})
	}
}

func TestSourceFetcherClassifiesDNSAndNetworkFailuresAsRetryable(t *testing.T) {
	dnsFetcher := NewSourceFetcher(FetcherOptions{
		Resolver: &testResolver{answers: map[string][]netip.Addr{}},
	})
	_, err := dnsFetcher.Fetch(context.Background(), "https://source.test/rules.yaml", FetchOptions{})
	if err == nil || !IsRetryableSourceError(err) || !errors.Is(err, ErrSourceTransport) {
		t.Fatalf("DNS failure classification: err=%v retryable=%t", err, IsRetryableSourceError(err))
	}

	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	resolver := &testResolver{answers: map[string][]netip.Addr{"source.test": {netip.MustParseAddr("93.184.216.34")}}}
	fetcher := NewSourceFetcher(FetcherOptions{
		Resolver: resolver,
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			return nil, errors.New("connect failed")
		},
		TLSConfig: &tls.Config{InsecureSkipVerify: true}, // test server only
	})
	_, err = fetcher.Fetch(context.Background(), testHTTPSURL(server, "source.test", "/rules.yaml"), FetchOptions{})
	if err == nil || !IsRetryableSourceError(err) || !errors.Is(err, ErrSourceTransport) {
		t.Fatalf("network failure classification: err=%v retryable=%t", err, IsRetryableSourceError(err))
	}
}

func TestSourceFetcherDoesNotRetryCanceledRequests(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	resolver := &testResolver{answers: map[string][]netip.Addr{"source.test": {netip.MustParseAddr("93.184.216.34")}}}
	fetcher := testFetcher(server, resolver, localDialer(server, new(string)))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := fetcher.Fetch(ctx, testHTTPSURL(server, "source.test", "/rules.yaml"), FetchOptions{})
	if err == nil || IsRetryableSourceError(err) || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled request classification: err=%v retryable=%t", err, IsRetryableSourceError(err))
	}
}
