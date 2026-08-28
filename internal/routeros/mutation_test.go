package routeros

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func noMutationSleep(context.Context, time.Duration) error {
	return nil
}

type mutationRoundTripper func(*http.Request) (*http.Response, error)

func (roundTrip mutationRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

func assertNoSensitiveText(t *testing.T, value string, secrets ...string) {
	t.Helper()
	for _, secret := range secrets {
		if secret != "" && strings.Contains(value, secret) {
			t.Fatal("sensitive credential leaked into observable error text")
		}
	}
}

func TestMutationCRUDWireContractsAndNormalization(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok || username != "policy" || password != "policy-secret" {
			t.Fatal("unexpected RouterOS Basic Auth credentials")
		}
		mu.Lock()
		seen = append(seen, r.Method+" "+r.URL.RequestURI())
		mu.Unlock()
		if r.URL.Path == "/rest/ip/dns/static" && r.Method == http.MethodGet {
			if r.URL.Query().Get(".proplist") != ".id,disabled,distance" || r.URL.Query().Get("comment") != "managed" {
				t.Fatalf("unexpected list query: %s", r.URL.RawQuery)
			}
			_, _ = io.WriteString(w, `[{".id":"*1","disabled":false,"distance":5}]`)
			return
		}
		switch {
		case r.URL.Path == "/rest/ip/dns/static/*1" && r.Method == http.MethodGet:
			_, _ = io.WriteString(w, `{".id":"*1","disabled":"true","distance":"10"}`)
		case r.URL.Path == "/rest/ip/dns/static" && r.Method == http.MethodPut:
			var fields map[string]any
			if err := json.NewDecoder(r.Body).Decode(&fields); err != nil {
				t.Fatal("invalid test request body")
			}
			if fields["comment"] != "managed" || fields["disabled"] != true || fields["distance"] != float64(5) {
				t.Fatalf("unexpected create body: %#v", fields)
			}
			_, _ = io.WriteString(w, `{".id":"*2","disabled":"false","distance":"5"}`)
		case r.URL.Path == "/rest/ip/dns/static/*2" && r.Method == http.MethodPatch:
			_, _ = io.WriteString(w, `{".id":"*2","disabled":"true","distance":15}`)
		case r.URL.Path == "/rest/ip/dns/static/*2" && r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewMutationClient(server.URL, "policy", "policy-secret")
	ctx := context.Background()
	objects, err := client.List(ctx, MenuIPDNSStatic, MutationQuery{Filters: map[string]string{"comment": "managed"}, Proplist: []string{".id", "disabled", "distance"}})
	if err != nil || len(objects) != 1 || objects[0][".id"] != "*1" || objects[0]["disabled"] != "false" || objects[0]["distance"] != "5" {
		if err != nil {
			assertNoSensitiveText(t, err.Error(), "policy-secret")
		}
		t.Fatalf("unexpected list result: %#v", objects)
	}
	if disabled, err := objects[0].Bool("disabled"); err != nil || disabled {
		t.Fatalf("normalized boolean = %v err=%v", disabled, err)
	}
	if distance, err := objects[0].Int("distance"); err != nil || distance != 5 {
		t.Fatalf("normalized integer = %v err=%v", distance, err)
	}

	got, err := client.Get(ctx, MenuIPDNSStatic, "*1", MutationQuery{})
	if err != nil || got[".id"] != "*1" || got["disabled"] != "true" || got["distance"] != "10" {
		if err != nil {
			assertNoSensitiveText(t, err.Error(), "policy-secret")
		}
		t.Fatalf("unexpected get result: %#v", got)
	}
	created, err := client.Create(ctx, MenuIPDNSStatic, RouterOSFields{"comment": "managed", "disabled": true, "distance": 5})
	if err != nil || created.ID() != "*2" {
		if err != nil {
			assertNoSensitiveText(t, err.Error(), "policy-secret")
		}
		t.Fatalf("unexpected create result: %#v", created)
	}
	patched, err := client.Patch(ctx, MenuIPDNSStatic, "*2", RouterOSFields{"disabled": false})
	if err != nil || patched.ID() != "*2" || patched["distance"] != "15" {
		if err != nil {
			assertNoSensitiveText(t, err.Error(), "policy-secret")
		}
		t.Fatalf("unexpected patch result: %#v", patched)
	}
	if err := client.Delete(ctx, MenuIPDNSStatic, "*2"); err != nil {
		assertNoSensitiveText(t, err.Error(), "policy-secret")
		t.Fatal("Delete() failed")
	}
	if len(seen) != 5 {
		t.Fatalf("request count = %d, want 5: %v", len(seen), seen)
	}
}

func TestMutationCommandsAreAllowlistedAndPathsAreScoped(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok || username != "policy" || password != "secret" {
			t.Fatal("unexpected RouterOS Basic Auth credentials")
		}
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		switch r.URL.Path {
		case "/rest/ip/firewall/nat/move":
			var fields map[string]string
			if err := json.NewDecoder(r.Body).Decode(&fields); err != nil {
				t.Fatal("invalid move request body")
			}
			if fields[".id"] != "*9" || fields["destination"] != "*C" {
				t.Fatalf("unexpected move body: %#v", fields)
			}
			_, _ = io.WriteString(w, `[]`)
		case "/rest/ip/dns/cache/flush":
			w.WriteHeader(http.StatusNoContent)
		case "/rest/ip/dns/static/print":
			_, _ = io.WriteString(w, `[{".id":"*1","comment":"managed"}]`)
		case "/rest/ip/dns/set":
			var fields map[string]any
			if err := json.NewDecoder(r.Body).Decode(&fields); err != nil {
				t.Fatal("invalid settings request body")
			}
			if len(fields) != 1 || fields["allow-remote-requests"] != "yes" && fields["cache-size"] != "32768KiB" {
				t.Fatalf("unexpected DNS settings body: %#v", fields)
			}
			w.WriteHeader(http.StatusNoContent)
		case "/rest/export":
			w.Header().Set("Content-Type", "text/plain")
			_, _ = io.WriteString(w, "/ip dns\n")
		default:
			t.Fatalf("unexpected mutation path: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	client := NewMutationClient(server.URL, "policy", "secret")
	if _, err := client.Move(context.Background(), MenuIPFirewallNAT, MoveRequest{ID: "*9", BeforeID: "*C"}); err != nil {
		assertNoSensitiveText(t, err.Error(), "secret")
		t.Fatal("Move() failed")
	}
	if err := client.FlushDNSCache(context.Background()); err != nil {
		assertNoSensitiveText(t, err.Error(), "secret")
		t.Fatal("FlushDNSCache() failed")
	}
	printed, err := client.Print(context.Background(), MenuIPDNSStatic, RouterOSFields{".proplist": ".id,comment"})
	if err != nil || len(printed.Objects) != 1 || printed.Objects[0]["comment"] != "managed" {
		if err != nil {
			assertNoSensitiveText(t, err.Error(), "secret")
		}
		t.Fatalf("unexpected Print() result: %#v", printed)
	}
	export, err := client.Export(context.Background(), RouterOSFields{"compact": ""})
	if err != nil || string(export) != "/ip dns\n" {
		if err != nil {
			assertNoSensitiveText(t, err.Error(), "secret")
		}
		t.Fatalf("unexpected Export() result: %q", export)
	}
	if err := client.SetDNSSettings(context.Background(), RouterOSFields{"allow-remote-requests": "yes"}); err != nil {
		assertNoSensitiveText(t, err.Error(), "secret")
		t.Fatal("SetDNSSettings() failed")
	}
	if err := client.SetDNSSettings(context.Background(), RouterOSFields{"cache-size": "32768KiB"}); err != nil {
		assertNoSensitiveText(t, err.Error(), "secret")
		t.Fatal("SetDNSSettings(cache-size) failed")
	}

	if _, err := client.Command(context.Background(), MutationMenu("ip/dns/static/../../execute"), CommandPrint, nil); err == nil {
		t.Fatal("unallowlisted menu was accepted")
	}
	if _, err := client.Command(context.Background(), MenuIPDNSStatic, MutationCommand("resolve"), nil); err == nil {
		t.Fatal("unsupported resolve command was accepted")
	}
	if _, err := client.Command(context.Background(), MenuIPDNSStatic, MutationCommand("execute"), nil); err == nil {
		t.Fatal("unallowlisted command was accepted")
	}
	if _, err := client.Command(context.Background(), MutationMenu("ipv6/dns/static"), CommandPrint, nil); err == nil {
		t.Fatal("invented IPv6 DNS static menu was accepted")
	}
	for _, menu := range []MutationMenu{"system/script", "user", "password", "execute"} {
		if _, err := client.Command(context.Background(), menu, CommandPrint, nil); err == nil {
			t.Errorf("dangerous menu %q was accepted", menu)
		}
	}
	if _, err := client.Get(context.Background(), MenuIPDNSStatic, "*1/../../execute", MutationQuery{}); err == nil {
		t.Fatal("path-injection ID was accepted")
	}
}

func TestMutationRetriesTransientErrorsButNotClientErrors(t *testing.T) {
	var mu sync.Mutex
	transientCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		transientCalls++
		call := transientCalls
		mu.Unlock()
		if call <= 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, `{"message":"temporary"}`)
			return
		}
		_, _ = io.WriteString(w, `[{".id":"*1"}]`)
	}))
	defer server.Close()
	client, err := NewMutationClientWithOptions(server.URL, "policy", "secret", MutationClientOptions{MaxRetries: 2, RetryBaseDelay: 0, Sleep: noMutationSleep})
	if err != nil {
		t.Fatal("mutation client setup failed")
	}
	if _, err := client.List(context.Background(), MenuIPDNSStatic, MutationQuery{}); err != nil {
		assertNoSensitiveText(t, err.Error(), "secret")
		t.Fatal("transient read retry failed")
	}
	if transientCalls != 3 {
		t.Fatalf("transient call count = %d, want 3", transientCalls)
	}

	clientErrorCalls := 0
	badServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clientErrorCalls++
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"message":"bad request"}`)
	}))
	defer badServer.Close()
	badClient, err := NewMutationClientWithOptions(badServer.URL, "policy", "secret", MutationClientOptions{MaxRetries: 4, RetryBaseDelay: 0, Sleep: noMutationSleep})
	if err != nil {
		t.Fatal("bad client setup failed")
	}
	if _, err := badClient.List(context.Background(), MenuIPDNSStatic, MutationQuery{}); err == nil {
		t.Fatal("400 response was accepted")
	}
	if clientErrorCalls != 1 {
		t.Fatalf("4xx call count = %d, want 1", clientErrorCalls)
	}

	rateCalls := 0
	rateServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rateCalls++
		if rateCalls == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = io.WriteString(w, `[{".id":"*1"}]`)
	}))
	defer rateServer.Close()
	rateClient, err := NewMutationClientWithOptions(rateServer.URL, "policy", "secret", MutationClientOptions{MaxRetries: 1, RetryBaseDelay: 0, Sleep: noMutationSleep})
	if err != nil {
		t.Fatal("rate-limit client setup failed")
	}
	if _, err := rateClient.List(context.Background(), MenuIPDNSStatic, MutationQuery{}); err != nil || rateCalls != 2 {
		if err != nil {
			assertNoSensitiveText(t, err.Error(), "secret")
		}
		t.Fatalf("429 retry result: calls=%d", rateCalls)
	}
}

func TestMutationReadRetryUsesExponentialBackoff(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls <= 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(w, `[]`)
	}))
	defer server.Close()

	const baseDelay = 3 * time.Millisecond
	var delays []time.Duration
	client, err := NewMutationClientWithOptions(server.URL, "policy", "secret", MutationClientOptions{
		MaxRetries:     2,
		RetryBaseDelay: baseDelay,
		Sleep: func(ctx context.Context, delay time.Duration) error {
			delays = append(delays, delay)
			return nil
		},
	})
	if err != nil {
		t.Fatal("backoff client setup failed")
	}
	if _, err := client.List(context.Background(), MenuIPDNSStatic, MutationQuery{}); err != nil {
		assertNoSensitiveText(t, err.Error(), "secret")
		t.Fatal("List() retry failed")
	}
	if calls != 3 || len(delays) != 2 || delays[0] != baseDelay || delays[1] != 2*baseDelay {
		t.Fatalf("calls=%d delays=%v, want exponential delays", calls, delays)
	}
}

func TestMutationDoesNotRetryAmbiguousOperations(t *testing.T) {
	var calls int
	client, err := NewMutationClientWithOptions("http://router.test", "policy", "secret", MutationClientOptions{
		HTTPClient: &http.Client{Transport: mutationRoundTripper(func(request *http.Request) (*http.Response, error) {
			calls++
			return nil, errors.New("connection lost after RouterOS applied the request")
		})},
		MaxRetries:     4,
		RetryBaseDelay: time.Millisecond,
		Sleep:          func(context.Context, time.Duration) error { t.Fatal("ambiguous mutation was retried"); return nil },
	})
	if err != nil {
		t.Fatal("ambiguous mutation client setup failed")
	}
	if _, err := client.Create(context.Background(), MenuIPDNSStatic, RouterOSFields{"comment": "managed"}); err == nil {
		t.Fatal("Create() accepted an unknown outcome")
	} else {
		var unknown *MutationOutcomeUnknownError
		if !errors.As(err, &unknown) || unknown.Path != "/rest/ip/dns/static" || strings.Contains(err.Error(), "secret") {
			assertNoSensitiveText(t, err.Error(), "secret")
			t.Fatal("Create() returned an unsafe or unexpected unknown-outcome error")
		}
	}
	if _, err := client.Move(context.Background(), MenuIPFirewallNAT, MoveRequest{ID: "*1", BeforeID: "*A"}); err == nil {
		t.Fatal("Move() accepted an unknown outcome")
	} else {
		var unknown *MutationOutcomeUnknownError
		if !errors.As(err, &unknown) || unknown.Method != http.MethodPost {
			assertNoSensitiveText(t, err.Error(), "secret")
			t.Fatal("Move() returned an unexpected unknown-outcome error")
		}
	}
	if calls != 2 {
		t.Fatalf("ambiguous mutation calls = %d, want 2", calls)
	}

	var serverCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverCalls++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	serverClient, err := NewMutationClientWithOptions(server.URL, "policy", "secret", MutationClientOptions{
		MaxRetries: 4,
		Sleep:      func(context.Context, time.Duration) error { t.Fatal("5xx mutation was retried"); return nil },
	})
	if err != nil {
		t.Fatal("5xx mutation client setup failed")
	}
	if _, err := serverClient.Create(context.Background(), MenuIPDNSStatic, RouterOSFields{"comment": "managed"}); err == nil {
		t.Fatal("5xx Create() accepted an unknown outcome")
	} else {
		var unknown *MutationOutcomeUnknownError
		if !errors.As(err, &unknown) || unknown.StatusCode != http.StatusServiceUnavailable {
			assertNoSensitiveText(t, err.Error(), "secret")
			t.Fatal("5xx Create() returned an unexpected error")
		}
	}
	if serverCalls != 1 {
		t.Fatalf("5xx mutation calls = %d, want 1", serverCalls)
	}
}

func TestMutationTimeoutAndBoundedErrorsDoNotLeakCredentials(t *testing.T) {
	secret := "router-password-secret"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rest/ip/dns/static" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"message":"`+strings.Repeat("x", 20<<10)+`","detail":"`+secret+`"}`)
			return
		}
		time.Sleep(100 * time.Millisecond)
	}))
	defer server.Close()
	client, err := NewMutationClientWithOptions(server.URL, "policy", secret, MutationClientOptions{RequestTimeout: 10 * time.Millisecond, MaxRetries: 0})
	if err != nil {
		t.Fatal("bounded-error client setup failed")
	}
	_, err = client.List(context.Background(), MenuIPDNSStatic, MutationQuery{})
	if err == nil {
		t.Fatal("bounded error response was accepted")
	}
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) || len(httpErr.Detail) > maxMutationDetailBytes || strings.Contains(err.Error(), secret) || strings.Contains(httpErr.Detail, secret) {
		if err != nil {
			assertNoSensitiveText(t, err.Error(), secret)
		}
		if httpErr != nil {
			assertNoSensitiveText(t, httpErr.Detail, secret)
		}
		t.Fatal("HTTP error was unsafe or unbounded")
	}

	slowServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
	}))
	defer slowServer.Close()
	deadlineClient, err := NewMutationClientWithOptions(slowServer.URL, "policy", secret, MutationClientOptions{RequestTimeout: 10 * time.Millisecond, MaxRetries: 0})
	if err != nil {
		t.Fatal("timeout client setup failed")
	}
	if _, err := deadlineClient.List(context.Background(), MenuIPDNSStatic, MutationQuery{}); err == nil || strings.Contains(err.Error(), secret) {
		if err != nil {
			assertNoSensitiveText(t, err.Error(), secret)
		}
		t.Fatal("timeout error was unsafe or missing")
	}
}

func TestMutationNotFoundDetailIsBounded(t *testing.T) {
	secret := "router-password-secret"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"message":"missing","detail":"`+strings.Repeat("d", 20<<10)+secret+`"}`)
	}))
	defer server.Close()
	client, err := NewMutationClientWithOptions(server.URL, "policy", secret, MutationClientOptions{MaxRetries: 0})
	if err != nil {
		t.Fatal("not-found client setup failed")
	}
	_, err = client.List(context.Background(), MenuIPDNSStatic, MutationQuery{})
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) || len(httpErr.Detail) > maxMutationDetailBytes || strings.Contains(httpErr.Detail, secret) {
		if err != nil {
			assertNoSensitiveText(t, err.Error(), secret)
		}
		if httpErr != nil {
			assertNoSensitiveText(t, httpErr.Detail, secret)
		}
		t.Fatal("404 detail was unsafe or unbounded")
	}
}

func TestMutationRequiresRouterOSInternalIDs(t *testing.T) {
	client := NewMutationClient("http://router.test", "policy", "secret")
	for _, id := range []string{"*1", "*A", "*1A"} {
		if err := validateMutationID(id); err != nil {
			t.Errorf("validateMutationID(%q) error = %v", id, err)
		}
	}
	for _, id := range []string{"", "ether1", "LAN", "my-route", "*1/../../x", "%2F", "*"} {
		if err := validateMutationID(id); err == nil {
			t.Errorf("validateMutationID(%q) accepted", id)
		}
	}
	for _, id := range []string{"ether1", "LAN", "my-route", "*1/../../x", "%2F"} {
		if _, err := client.Get(context.Background(), MenuIPDNSStatic, id, MutationQuery{}); err == nil {
			t.Errorf("Get(%q) accepted non-.id", id)
		}
		if _, err := client.Patch(context.Background(), MenuIPDNSStatic, id, RouterOSFields{"comment": "x"}); err == nil {
			t.Errorf("Patch(%q) accepted non-.id", id)
		}
		if err := client.Delete(context.Background(), MenuIPDNSStatic, id); err == nil {
			t.Errorf("Delete(%q) accepted non-.id", id)
		}
	}
	if _, err := client.Move(context.Background(), MenuIPFirewallNAT, MoveRequest{ID: "ether1", BeforeID: "*A"}); err == nil {
		t.Fatal("Move() accepted non-.id source")
	}
	if _, err := client.Move(context.Background(), MenuIPFirewallNAT, MoveRequest{ID: "*1", BeforeID: "LAN"}); err == nil {
		t.Fatal("Move() accepted non-.id anchor")
	}
}

func TestMutationWriteProbeProvesCreateAndDeleteCapability(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	var createdID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Method+" "+r.URL.Path)
		mu.Unlock()
		switch {
		case r.URL.Path == "/rest/ip/firewall/filter" && r.Method == http.MethodPut:
			var fields map[string]any
			if err := json.NewDecoder(r.Body).Decode(&fields); err != nil {
				t.Fatal("invalid test probe body")
			}
			if fields["disabled"] != "yes" || fields["chain"] != "output" || fields["action"] != "accept" || !strings.Contains(fmt.Sprint(fields["comment"]), "rosboard policy write probe") {
				t.Fatalf("unexpected probe body: %#v", fields)
			}
			createdID = "*1f"
			_, _ = io.WriteString(w, `{".id":"*1f","disabled":"true"}`)
		case r.URL.Path == "/rest/ip/firewall/filter/*1f" && r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	if err := NewMutationClient(server.URL, "policy", "policy-secret").WriteProbe(context.Background()); err != nil {
		t.Fatalf("write probe failed: %v", err)
	}
	if createdID == "" || len(seen) != 2 || seen[0] != "PUT /rest/ip/firewall/filter" || seen[1] != "DELETE /rest/ip/firewall/filter/*1f" {
		t.Fatalf("unexpected probe calls: %#v", seen)
	}
}

func TestMutationWriteProbeFailsClosedForReadOnlyAndCleanup(t *testing.T) {
	tests := []struct {
		name    string
		handler func(w http.ResponseWriter, r *http.Request)
	}{
		{name: "read-only create denied", handler: func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/rest/ip/firewall/filter" && r.Method == http.MethodPut {
				http.Error(w, "not enough permissions", http.StatusForbidden)
				return
			}
			http.NotFound(w, r)
		}},
		{name: "create succeeds but cleanup denied", handler: func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/rest/ip/firewall/filter" && r.Method == http.MethodPut {
				_, _ = io.WriteString(w, `{".id":"*1f","disabled":"true"}`)
				return
			}
			if r.URL.Path == "/rest/ip/firewall/filter/*1f" && r.Method == http.MethodDelete {
				http.Error(w, "not enough permissions", http.StatusForbidden)
				return
			}
			http.NotFound(w, r)
		}},
		{name: "create returns no id", handler: func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/rest/ip/firewall/filter" && r.Method == http.MethodPut {
				_, _ = io.WriteString(w, `{"disabled":"true"}`)
				return
			}
			http.NotFound(w, r)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(test.handler))
			defer server.Close()
			err := NewMutationClient(server.URL, "policy", "policy-secret").WriteProbe(context.Background())
			if err == nil {
				t.Fatalf("%s: write probe reported success for a non-write-capable account", test.name)
			}
			if strings.Contains(err.Error(), "policy-secret") {
				t.Fatalf("%s: write probe leaked credentials: %v", test.name, err)
			}
		})
	}
}

func TestMutationCommandFieldAllowLists(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/export" || r.Method != http.MethodPost {
			t.Fatalf("unexpected export request: %s %s", r.Method, r.URL.Path)
		}
		var fields map[string]any
		if err := json.NewDecoder(r.Body).Decode(&fields); err != nil {
			t.Fatal("invalid export request body")
		}
		if len(fields) != 1 || fields["compact"] != "" {
			t.Fatalf("unexpected export fields: %#v", fields)
		}
		_, _ = io.WriteString(w, "/ip dns\n")
	}))
	defer server.Close()
	client := NewMutationClient(server.URL, "policy", "secret")
	response, err := client.Command(context.Background(), "", CommandExport, RouterOSFields{"compact": ""})
	if err != nil || string(response.Raw) != "/ip dns\n" {
		if err != nil {
			assertNoSensitiveText(t, err.Error(), "secret")
		}
		t.Fatalf("approved generic export failed: raw response mismatch")
	}
	for _, fields := range []RouterOSFields{
		{"file": "router-export"},
		{"show-sensitive": ""},
		{"unsafe": "value"},
	} {
		if _, err := client.Command(context.Background(), "", CommandExport, fields); err == nil {
			t.Errorf("Export fields %#v were accepted", fields)
		}
		if _, err := client.Export(context.Background(), fields); err == nil {
			t.Errorf("Export wrapper fields %#v were accepted", fields)
		}
	}
	if _, err := client.Command(context.Background(), MenuIPDNS, CommandDNSCacheFlush, RouterOSFields{}); err == nil {
		t.Fatal("DNS flush accepted fields")
	}
	if _, err := client.Command(context.Background(), MenuIPFirewallNAT, CommandMove, RouterOSFields{".id": "*1", "destination": "*A"}); err == nil {
		t.Fatal("generic Command() bypassed typed Move()")
	}
	if _, err := client.Command(context.Background(), MenuIPDNS, CommandDNSSettingsSet, RouterOSFields{"script": ":put secret"}); err == nil {
		t.Fatal("DNS settings accepted an unallowlisted field")
	}
	if _, err := client.Command(context.Background(), MenuIPDNSStatic, CommandDNSSettingsSet, RouterOSFields{"allow-remote-requests": "yes"}); err == nil {
		t.Fatal("DNS settings accepted the wrong menu")
	}
}

func TestMutationErrorRedactsBasicAuthorization(t *testing.T) {
	username := "policy"
	password := "secret"
	basicToken := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"message":"failure","detail":"unknown parameter type; Authorization: Basic `+basicToken+`"}`)
	}))
	defer server.Close()
	client, err := NewMutationClientWithOptions(server.URL, username, password, MutationClientOptions{MaxRetries: 0})
	if err != nil {
		t.Fatal("redaction client setup failed")
	}
	_, err = client.List(context.Background(), MenuIPDNSStatic, MutationQuery{})
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("mutation error = %v, want HTTPError", err)
	}
	if !strings.Contains(err.Error(), "unknown parameter type") || !strings.Contains(err.Error(), "[redacted]") {
		t.Fatalf("mutation error did not expose sanitized detail: %v", err)
	}
	assertNoSensitiveText(t, err.Error(), password, username+":"+password, basicToken)
	assertNoSensitiveText(t, httpErr.Detail, password, username+":"+password, basicToken)
	if !strings.Contains(httpErr.Detail, "[redacted]") {
		t.Fatalf("mutation detail did not preserve redaction marker: %q", httpErr.Detail)
	}
}

func TestMutationConstructorRejectsUnsafeBaseURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{name: "unsupported scheme", url: "ftp://router.test"},
		{name: "userinfo", url: "http://user:secret@router.test"},
		{name: "path", url: "http://router.test/rest/execute"},
		{name: "fragment", url: "http://router.test/#fragment"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewMutationClientWithOptions(test.url, "policy", "secret", MutationClientOptions{}); err == nil {
				t.Fatal("unsafe mutation base URL was accepted")
			}
		})
	}
}

func TestRouterOSNormalizationHelpers(t *testing.T) {
	object := RouterOSObject{"enabled": "yes", "count": "0x10", "empty": ""}
	if enabled, err := object.Bool("enabled"); err != nil || !enabled {
		t.Fatalf("Bool() = %v err=%v", enabled, err)
	}
	if count, err := object.Int("count"); err != nil || count != 16 {
		t.Fatalf("Int() = %v err=%v", count, err)
	}
	for _, value := range []string{"true", "TRUE", "yes", "1", "on"} {
		if parsed, err := ParseRouterOSBool(value); err != nil || !parsed {
			t.Errorf("ParseRouterOSBool(%q) = %v err=%v", value, parsed, err)
		}
	}
	for _, value := range []string{"false", "FALSE", "no", "0", "off"} {
		if parsed, err := ParseRouterOSBool(value); err != nil || parsed {
			t.Errorf("ParseRouterOSBool(%q) = %v err=%v", value, parsed, err)
		}
	}
	if _, err := ParseRouterOSBool("maybe"); err == nil {
		t.Fatal("invalid RouterOS boolean was accepted")
	}
	if number, err := ParseRouterOSFloat("1.25"); err != nil || number != 1.25 {
		t.Fatalf("ParseRouterOSFloat() = %v err=%v", number, err)
	}
}

var _ MoveAdapter = (*MutationClient)(nil)
var _ ExportReader = (*MutationClient)(nil)

func TestMutationErrorPathDoesNotIncludeAuthorization(t *testing.T) {
	client, err := NewMutationClientWithOptions("http://router.test", "policy", "secret", MutationClientOptions{
		HTTPClient: &http.Client{Transport: mutationRoundTripper(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("connection failed")
		})},
		MaxRetries: 0,
	})
	if err != nil {
		t.Fatal("mutation error client setup failed")
	}
	_, err = client.List(context.Background(), MenuIPDNSStatic, MutationQuery{})
	if err == nil {
		t.Fatal("expected mutation network error")
	}
	if strings.Contains(err.Error(), "Authorization") || strings.Contains(err.Error(), "secret") {
		assertNoSensitiveText(t, err.Error(), "Authorization", "secret")
		t.Fatal("mutation error exposed credentials")
	}
}

// mutationProbeRawServer is a minimal HTTP/1.1 server that exposes connection
// identity so a regression can prove the mutation client never reuses a
// keep-alive connection between the WriteProbe CREATE (PUT) and DELETE.
type mutationProbeRawServer struct {
	listener  net.Listener
	mu        sync.Mutex
	connSeq   int
	method    string
	delConn   int
	delTotal  int
	delFail   bool
	delTarget string
}

func startMutationProbeServer(t *testing.T, failDelete bool) *mutationProbeRawServer {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &mutationProbeRawServer{listener: listener, method: "put", delFail: failDelete}
	go server.serve()
	t.Cleanup(func() { _ = listener.Close() })
	return server
}

func (s *mutationProbeRawServer) serve() {
	index := 0
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		index++
		s.mu.Lock()
		s.connSeq = index
		connID := index
		s.mu.Unlock()
		go s.handle(conn, connID)
	}
}

func (s *mutationProbeRawServer) handle(conn net.Conn, connID int) {
	defer conn.Close()
	reader := bufio.NewReader(conn)
	for {
		request, err := readRawRequest(reader)
		if err != nil {
			return
		}
		switch request.method {
		case "PUT":
			body := `{".id":"*F","chain":"output","action":"accept","disabled":"yes"}`
			s.writeResponse(conn, 200, "application/json", body, request.close)
		case "DELETE":
			s.mu.Lock()
			s.delTotal++
			s.delTarget = request.path
			if connID == 2 && !s.delFail {
				s.delConn = connID
			}
			reused := connID != 2
			s.mu.Unlock()
			if reused || s.delFail {
				s.writeResponse(conn, 500, "application/json", `{"error":"unexpected reused connection"}`, true)
				return
			}
			if !s.writeResponse(conn, 200, "application/json", "", request.close) {
				return
			}
		default:
			s.writeResponse(conn, 404, "text/plain", "not found", true)
			return
		}
		if request.close {
			return
		}
	}
}

func (s *mutationProbeRawServer) writeResponse(conn net.Conn, status int, contentType, body string, closeConn bool) bool {
	payload := []byte(body)
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("HTTP/1.1 %d %s\r\n", status, http.StatusText(status)))
	builder.WriteString("Content-Type: " + contentType + "\r\n")
	builder.WriteString(fmt.Sprintf("Content-Length: %d\r\n", len(payload)))
	if closeConn {
		builder.WriteString("Connection: close\r\n")
	}
	builder.WriteString("\r\n")
	builder.Write(payload)
	if _, err := conn.Write([]byte(builder.String())); err != nil {
		return false
	}
	if !closeConn {
		// keep the connection open so a naively reusing client CAN reuse it
	}
	return true
}

type rawServerRequest struct {
	method string
	path   string
	close  bool
}

func readRawRequest(reader *bufio.Reader) (rawServerRequest, error) {
	start, err := reader.ReadString('\n')
	if err != nil {
		return rawServerRequest{}, err
	}
	parts := strings.Split(strings.TrimSpace(start), " ")
	if len(parts) < 2 {
		return rawServerRequest{}, errors.New("bad request line")
	}
	request := rawServerRequest{method: parts[0], path: parts[1]}
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return rawServerRequest{}, err
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			break
		}
		if strings.EqualFold(strings.SplitN(trimmed, ":", 2)[0], "Connection") && strings.Contains(strings.ToLower(trimmed), "close") {
			request.close = true
		}
	}
	return request, nil
}

// TestMutationWriteProbeUsesFreshConnectionAfterCreate proves the deterministic
// regression behind the Phase 12 discovery: RouterOS reported the inert probe
// CREATE (PUT) succeeding while the follow-up DELETE failed at the network
// layer, a signature of a stale/reused keep-alive connection. The mutation
// client must therefore use independent connections per request so the DELETE
// always lands on a fresh connection.
func TestMutationWriteProbeUsesFreshConnectionAfterCreate(t *testing.T) {
	server := startMutationProbeServer(t, false)
	client, err := NewMutationClientWithOptions("http://"+server.listener.Addr().String(), "probe", "secret", MutationClientOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := client.WriteProbe(ctx); err != nil {
		t.Fatalf("WriteProbe failed over the raw keep-alive-sensitive server: %v", err)
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if server.delTotal != 1 {
		t.Fatalf("DELETE count = %d, want exactly 1", server.delTotal)
	}
	if server.delConn != 2 {
		t.Fatalf("DELETE connected on conn %d; the mutation client must use a fresh connection (2) after the PUT create (1)", server.delConn)
	}
	if server.delTarget == "" || strings.Contains(server.delTarget, "%2A") {
		t.Fatalf("DELETE request-target = %q; the RouterOS .id must stay literal (no %%2A)", server.delTarget)
	}
	if !strings.HasPrefix(server.delTarget, "/rest/ip/firewall/filter/*") {
		t.Fatalf("DELETE request-target = %q; unexpected object path", server.delTarget)
	}
}

// TestMutationWriteProbeInjectedKeepAliveClientStillFresh proves the explicit
// connection-reuse boundary in execute(): even when a caller injects an
// ordinary keep-alive http.Client (no DisableKeepAlives), the DELETE after the
// CREATE still lands on a fresh connection because idle connections are
// explicitly closed after each mutation response.
func TestMutationWriteProbeInjectedKeepAliveClientStillFresh(t *testing.T) {
	server := startMutationProbeServer(t, false)
	keepAlive := &http.Client{Timeout: 10 * time.Second} // default transport, keep-alives allowed
	client, err := NewMutationClientWithOptions("http://"+server.listener.Addr().String(), "probe", "secret", MutationClientOptions{HTTPClient: keepAlive})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := client.WriteProbe(ctx); err != nil {
		t.Fatalf("WriteProbe with injected keep-alive client failed: %v", err)
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if server.delTotal != 1 || server.delConn != 2 {
		t.Fatalf("injected keep-alive client reused a connection for DELETE: delTotal=%d delConn=%d", server.delTotal, server.delConn)
	}
}

// TestMutationWriteProbeDeleteFailureFailsClosedWithoutRetry locks the
// fail-closed contract: an ambiguous DELETE failure surfaces as an error and
// is never automatically retried, while the CREATE probe body is exact.
func TestMutationWriteProbeDeleteFailureFailsClosedWithoutRetry(t *testing.T) {
	server := startMutationProbeServer(t, true)
	client, err := NewMutationClientWithOptions("http://"+server.listener.Addr().String(), "probe", "secret", MutationClientOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := client.WriteProbe(ctx); err == nil {
		t.Fatal("WriteProbe with failing DELETE unexpectedly succeeded")
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if server.delTotal != 1 {
		t.Fatalf("ambiguous DELETE was retried: %d attempts (must fail closed with exactly one)", server.delTotal)
	}
}

// mutationLiteralTargetServer is a raw HTTP server that only accepts the
// WriteProbe if the DELETE request-target carries the RouterOS .id VERBATIM
// ("*12"), mirroring RouterOS REST: a percent-encoded "%2A12" matches no
// object, so the server fails the cleanup and WriteProbe must fail closed.
type mutationLiteralTargetServer struct {
	listener   net.Listener
	mu         sync.Mutex
	delTarget  string
	delReached bool
}

func startMutationLiteralTargetServer(t *testing.T) *mutationLiteralTargetServer {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &mutationLiteralTargetServer{listener: listener}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				reader := bufio.NewReader(c)
				for {
					request, err := readRawRequest(reader)
					if err != nil {
						return
					}
					switch request.method {
					case "PUT":
						body := `{".id":"*12","chain":"output","action":"accept","disabled":"yes"}`
						server.write(conn, 200, "application/json", body, true)
						return
					case "DELETE":
						server.mu.Lock()
						server.delReached = true
						server.delTarget = request.path
						literal := request.path == "/rest/ip/firewall/filter/*12"
						server.mu.Unlock()
						if literal {
							server.write(conn, 200, "application/json", "", true)
						} else {
							// RouterOS would not find object "%2A12": fail the
							// cleanup so WriteProbe fails closed.
							server.write(conn, 404, "application/json", `{"error":"object not found"}`, true)
						}
						return
					default:
						server.write(conn, 405, "text/plain", "method not allowed", true)
						return
					}
				}
			}(conn)
		}
	}()
	return server
}

func (s *mutationLiteralTargetServer) write(conn net.Conn, status int, contentType, body string, closeConn bool) {
	payload := []byte(body)
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("HTTP/1.1 %d %s\r\n", status, http.StatusText(status)))
	builder.WriteString("Content-Type: " + contentType + "\r\n")
	builder.WriteString(fmt.Sprintf("Content-Length: %d\r\n", len(payload)))
	if closeConn {
		builder.WriteString("Connection: close\r\n")
	}
	builder.WriteString("\r\n")
	_, _ = conn.Write([]byte(builder.String()))
	_, _ = conn.Write(payload)
}

// TestMutationWriteProbeRequiresLiteralObjectIDTarget is the Phase 12
// regression for the DELETE-not-arriving bug: RouterOS matches object .id only
// when the request-target carries "*12" verbatim (python/curl shape); Go's
// default serialization percent-encodes '*' to %2A12 which RouterOS treats as
// a different object (create succeeds, cleanup fails, orphan probe left).
// The raw server enforces the literal target, so this test fails on the
// unpatched client and passes once the mutation client preserves RawPath.
func TestMutationWriteProbeRequiresLiteralObjectIDTarget(t *testing.T) {
	server := startMutationLiteralTargetServer(t)
	defer server.listener.Close()
	client, err := NewMutationClientWithOptions("http://"+server.listener.Addr().String(), "probe", "secret", MutationClientOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := client.WriteProbe(ctx); err != nil {
		t.Fatalf("WriteProbe failed over the literal-target server (DELETE not accepted): %v", err)
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if !server.delReached {
		t.Fatal("DELETE never reached the server")
	}
	if server.delTarget == "" || strings.Contains(server.delTarget, "%2A") {
		t.Fatalf("DELETE request-target = %q; the RouterOS .id must stay literal (no %%2A)", server.delTarget)
	}
	if !strings.HasPrefix(server.delTarget, "/rest/ip/firewall/filter/*") {
		t.Fatalf("DELETE request-target = %q; unexpected object path", server.delTarget)
	}
}
