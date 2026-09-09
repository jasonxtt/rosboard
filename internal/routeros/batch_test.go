package routeros

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func batchResponse(t *testing.T, script, prefix, closing, suffix string) string {
	t.Helper()
	start := strings.Index(script, prefix)
	if start < 0 {
		t.Fatalf("batch script is missing marker prefix %q: %q", prefix, script)
	}
	markerStart := start + len(prefix)
	end := strings.Index(script[markerStart:], closing)
	if end < 0 {
		t.Fatalf("batch script is missing marker closing %q: %q", closing, script)
	}
	marker := script[start : markerStart+end+len(closing)]
	body, err := json.Marshal(map[string]string{"ret": marker + suffix})
	if err != nil {
		t.Fatalf("marshal batch response: %v", err)
	}
	return string(body)
}

func batchSuccessResponse(t *testing.T, script string) string {
	return batchResponse(t, script, batchScriptOKPrefix, "__", "")
}

func batchErrorResponse(t *testing.T, script, detail string) string {
	return batchResponse(t, script, batchScriptErrorPrefix, "__:", " "+detail)
}

func TestMutationBatchUsesBoundedRouterOSScripts(t *testing.T) {
	var scripts []string
	var payloads []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/rest/execute":
			username, password, ok := r.BasicAuth()
			if !ok || username != "policy" || password != "secret" {
				t.Fatal("unexpected RouterOS credentials")
			}
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode batch request: %v", err)
			}
			scripts = append(scripts, payload["script"].(string))
			payloads = append(payloads, payload)
			_, _ = io.WriteString(w, batchSuccessResponse(t, payload["script"].(string)))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/ip/dns/static":
			_, _ = io.WriteString(w, `[{".id":"*1","disabled":"false"},{".id":"*2","disabled":"false"}]`)
		default:
			t.Fatalf("unexpected batch request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewMutationClient(server.URL, "policy", "secret")
	entries := make([]RouterOSFields, 300)
	for index := range entries {
		entries[index] = RouterOSFields{
			"name":            "domain-" + strconv.Itoa(index) + ".example",
			"type":            "FWD",
			"forward-to":      "rosboard_forwarder",
			"address-list":    "policy-list",
			"match-subdomain": false,
			"disabled":        true,
		}
	}
	if err := client.CreateBatch(context.Background(), MenuIPDNSStatic, entries); err != nil {
		t.Fatal(err)
	}
	if err := client.SetDisabledBatch(context.Background(), MenuIPDNSStatic, []string{"*1", "*2"}, false); err != nil {
		t.Fatal(err)
	}
	if len(scripts) != 3 {
		t.Fatalf("batch request count = %d, want 3", len(scripts))
	}
	// Every /rest/execute call — each CreateBatch chunk and every
	// SetDisabledBatch chunk — must request synchronous execution by carrying
	// the `as-string` key. The map decode distinguishes an absent key from
	// the empty-string value the RouterOS API expects.
	for index, payload := range payloads {
		script, hasScript := payload["script"].(string)
		if !hasScript || script == "" {
			t.Fatalf("execute request %d is missing its script: %#v", index+1, payload)
		}
		asString, hasAsString := payload["as-string"]
		if !hasAsString {
			t.Fatalf("execute request %d lacks the as-string sync key: %#v", index+1, payload)
		}
		if value, ok := asString.(string); !ok || value != "" {
			t.Fatalf("execute request %d as-string = %#v, want empty string", index+1, asString)
		}
	}
	if !strings.Contains(scripts[0], "/ip/dns/static/add") || !strings.Contains(scripts[0], "disabled=yes") {
		t.Fatalf("create script is not a disabled DNS batch: %q", scripts[0])
	}
	if !strings.Contains(scripts[1], "/ip/dns/static/add") || !strings.Contains(scripts[1], "domain-299.example") {
		t.Fatalf("second create chunk is incomplete: %q", scripts[1])
	}
	if !strings.Contains(scripts[2], "/ip/dns/static/enable *1\n/ip/dns/static/enable *2") ||
		!strings.Contains(scripts[2], ":onerror e in={") || !strings.Contains(scripts[2], ":put \""+batchScriptOKPrefix) {
		t.Fatalf("unexpected enable script: %q", scripts[2])
	}
}

func TestMutationBatchAllowsDNSForwarderActivation(t *testing.T) {
	var script string
	var payload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/rest/execute":
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode batch request: %v", err)
			}
			script, _ = payload["script"].(string)
			_, _ = io.WriteString(w, batchSuccessResponse(t, script))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/ip/dns/forwarders":
			_, _ = io.WriteString(w, `[{".id":"*f1","disabled":"false"},{".id":"*f2","disabled":"false"}]`)
		default:
			t.Fatalf("unexpected batch request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewMutationClient(server.URL, "policy", "secret")
	if err := client.SetDisabledBatch(context.Background(), MenuIPDNSForwarders, []string{"*f1", "*f2"}, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(script, "/ip/dns/forwarders/enable *f1\n/ip/dns/forwarders/enable *f2") {
		t.Fatalf("unexpected DNS forwarder enable script: %q", script)
	}
	// Activation enable/disable batches must also finish synchronously before
	// the apply verify pass reads RouterOS back.
	if asString, hasAsString := payload["as-string"]; !hasAsString {
		t.Fatalf("forwarder activation request lacks the as-string sync key: %#v", payload)
	} else if value, ok := asString.(string); !ok || value != "" {
		t.Fatalf("forwarder activation as-string = %#v, want empty string", asString)
	}
}

func TestMutationBatchEscapesAndValidatesScriptValues(t *testing.T) {
	line, err := batchCreateLine(MenuIPDNSStatic, RouterOSFields{
		"name":     `safe.example`,
		"comment":  `quote " slash \ dollar $`,
		"disabled": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(line, `comment="quote \" slash \\ dollar \$"`) || !strings.Contains(line, "disabled=no") {
		t.Fatalf("RouterOS string escaping is wrong: %q", line)
	}

	invalid := []struct {
		name   string
		fields RouterOSFields
	}{
		{name: "unknown field", fields: RouterOSFields{"script": ":put secret"}},
		{name: "control character", fields: RouterOSFields{"name": "bad\nname"}},
		{name: "unsupported type", fields: RouterOSFields{"name": 123}},
	}
	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			if _, err := batchCreateLine(MenuIPDNSStatic, test.fields); err == nil {
				t.Fatal("invalid batch field was accepted")
			}
		})
	}
	if _, err := batchCreateLine(MenuIPFirewallFilter, RouterOSFields{"comment": "x"}); err == nil {
		t.Fatal("unsupported batch menu was accepted")
	}
	if err := (&MutationClient{}).SetDisabledBatch(context.Background(), MenuIPDNSStatic, []string{"not-an-id"}, false); err == nil {
		t.Fatal("invalid RouterOS ID was accepted")
	}
}

func TestMutationBatchRejectsScriptErrorResponse(t *testing.T) {
	var executeCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/rest/execute" {
			t.Fatalf("unexpected batch request: %s %s", r.Method, r.URL.Path)
		}
		executeCalls++
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode batch request: %v", err)
		}
		_, _ = io.WriteString(w, batchErrorResponse(t, payload["script"].(string), "123 (:error; line 1)"))
	}))
	defer server.Close()

	client := NewMutationClient(server.URL, "policy", "secret")
	err := client.CreateBatch(context.Background(), MenuIPDNSStatic, []RouterOSFields{{"name": "example.com"}})
	if err == nil || !strings.Contains(err.Error(), "RouterOS batch script failed: 123") {
		t.Fatalf("CreateBatch error = %v, want the RouterOS script error", err)
	}
	if executeCalls != 1 {
		t.Fatalf("CreateBatch execute calls = %d, want 1", executeCalls)
	}
}

func TestMutationBatchDoesNotReplayUnknownCreateOutcome(t *testing.T) {
	var executeCalls int
	client, err := NewMutationClientWithOptions("http://router.test", "policy", "secret", MutationClientOptions{
		HTTPClient: &http.Client{Transport: mutationRoundTripper(func(request *http.Request) (*http.Response, error) {
			if request.Method != http.MethodPost || request.URL.Path != "/rest/execute" {
				t.Fatalf("unexpected batch request: %s %s", request.Method, request.URL.Path)
			}
			executeCalls++
			return nil, errors.New("connection lost after batch reached RouterOS")
		})},
		MaxRetries: 2,
	})
	if err != nil {
		t.Fatal(err)
	}

	err = client.CreateBatch(context.Background(), MenuIPDNSStatic, []RouterOSFields{{"name": "example.com"}})
	if err == nil || !strings.Contains(err.Error(), "outcome unknown") {
		t.Fatalf("CreateBatch error = %v, want unknown-outcome error", err)
	}
	if executeCalls != 1 {
		t.Fatalf("CreateBatch execute calls = %d, want 1", executeCalls)
	}
}

func TestMutationBatchRepairsPartialActivationWithRESTPatch(t *testing.T) {
	state := map[string]bool{"*1": true, "*2": true}
	var patchIDs []string
	var executeCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/rest/execute":
			executeCalls++
			// Simulate a batch that changed one item before RouterOS reported
			// an execution error for a later item.
			state["*1"] = false
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode batch request: %v", err)
			}
			_, _ = io.WriteString(w, batchErrorResponse(t, payload["script"].(string), "no such command"))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/ip/dns/static":
			value := func(id string) string {
				if state[id] {
					return "true"
				}
				return "false"
			}
			_ = json.NewEncoder(w).Encode([]map[string]string{
				{".id": "*1", "disabled": value("*1")},
				{".id": "*2", "disabled": value("*2")},
			})
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/rest/ip/dns/static/"):
			id := strings.TrimPrefix(r.URL.Path, "/rest/ip/dns/static/")
			patchIDs = append(patchIDs, id)
			state[id] = false
			_, _ = io.WriteString(w, `{".id":"`+id+`","disabled":"false"}`)
		default:
			t.Fatalf("unexpected batch request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewMutationClient(server.URL, "policy", "secret")
	if err := client.SetDisabledBatch(context.Background(), MenuIPDNSStatic, []string{"*1", "*2"}, false); err != nil {
		t.Fatal(err)
	}
	if executeCalls != 1 {
		t.Fatalf("execute calls = %d, want 1", executeCalls)
	}
	if len(patchIDs) != 1 || patchIDs[0] != "*2" {
		t.Fatalf("fallback patch IDs = %v, want [*2]", patchIDs)
	}
}

func TestMutationBatchRepairsReadbackMismatchAfterSuccessfulScript(t *testing.T) {
	state := map[string]bool{"*1": true}
	var patchCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/rest/execute":
			// Return the protocol success marker but leave the item disabled;
			// read-back must catch this silent non-convergence.
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode batch request: %v", err)
			}
			response := batchSuccessResponse(t, payload["script"].(string))
			var value map[string]string
			if err := json.Unmarshal([]byte(response), &value); err != nil {
				t.Fatalf("decode generated batch response: %v", err)
			}
			value["ret"] = "normal command output\r\n" + value["ret"]
			_ = json.NewEncoder(w).Encode(value)
		case r.Method == http.MethodGet && r.URL.Path == "/rest/ip/dns/static":
			value := "true"
			if !state["*1"] {
				value = "false"
			}
			_, _ = io.WriteString(w, `[{".id":"*1","disabled":"`+value+`"}]`)
		case r.Method == http.MethodPatch && r.URL.Path == "/rest/ip/dns/static/*1":
			patchCalls++
			state["*1"] = false
			_, _ = io.WriteString(w, `{".id":"*1","disabled":"false"}`)
		default:
			t.Fatalf("unexpected batch request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewMutationClient(server.URL, "policy", "secret")
	if err := client.SetDisabledBatch(context.Background(), MenuIPDNSStatic, []string{"*1"}, false); err != nil {
		t.Fatal(err)
	}
	if patchCalls != 1 {
		t.Fatalf("fallback patch calls = %d, want 1", patchCalls)
	}
}

func TestMutationBatchReportsReadbackAndFallbackFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/rest/execute":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode batch request: %v", err)
			}
			_, _ = io.WriteString(w, batchSuccessResponse(t, payload["script"].(string)))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/ip/dns/static":
			w.WriteHeader(http.StatusServiceUnavailable)
		case r.Method == http.MethodPatch && r.URL.Path == "/rest/ip/dns/static/*1":
			w.WriteHeader(http.StatusServiceUnavailable)
		default:
			t.Fatalf("unexpected batch request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := NewMutationClientWithOptions(server.URL, "policy", "secret", MutationClientOptions{MaxRetries: 0})
	if err != nil {
		t.Fatal(err)
	}
	err = client.SetDisabledBatch(context.Background(), MenuIPDNSStatic, []string{"*1"}, false)
	if err == nil || !strings.Contains(err.Error(), "individual REST fallback") {
		t.Fatalf("SetDisabledBatch error = %v, want read-back/fallback context", err)
	}
}

func TestBatchScriptResponseProtocolIsTokenBoundAndFailClosed(t *testing.T) {
	first := newBatchScriptProtocol()
	second := newBatchScriptProtocol()
	if first.ok == second.ok || first.errorPrefix == second.errorPrefix {
		t.Fatalf("batch protocol markers are not unique: first=%#v second=%#v", first, second)
	}
	client := NewMutationClient("http://router.test", "policy", "secret")
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "success", body: `{"ret":"` + first.ok + `"}`},
		{name: "normal output and success", body: `{"ret":"harmless output\r\n` + first.ok + `"}`},
		{name: "script error", body: `{"ret":"` + first.errorPrefix + `failure"}`, want: "RouterOS batch script failed: failure"},
		{name: "missing sentinel", body: `{"ret":"harmless output"}`, want: "missing success marker"},
		{name: "malformed JSON", body: `{"ret":`, want: "decode RouterOS batch response"},
		{name: "malformed ret type", body: `{"ret":123}`, want: "ret is not a string"},
		{name: "both markers", body: `{"ret":"` + first.ok + `\n` + first.errorPrefix + `failure"}`, want: "both success and error markers"},
		{name: "wrong token", body: `{"ret":"` + second.ok + `"}`, want: "missing success marker"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := client.validateBatchScriptResponse([]byte(test.body), first)
			if test.want == "" {
				if err != nil {
					t.Fatalf("validateBatchScriptResponse() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("validateBatchScriptResponse() error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestMutationBatchAllowsScriptErrorWhenReadbackConverged(t *testing.T) {
	var patchCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/rest/execute":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode batch request: %v", err)
			}
			_, _ = io.WriteString(w, batchErrorResponse(t, payload["script"].(string), "late command error"))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/ip/dns/static":
			_, _ = io.WriteString(w, `[{".id":"*1","disabled":"false"}]`)
		case r.Method == http.MethodPatch:
			patchCalls++
			_, _ = io.WriteString(w, `{ ".id": "*1", "disabled": "false" }`)
		default:
			t.Fatalf("unexpected batch request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewMutationClient(server.URL, "policy", "secret")
	if err := client.SetDisabledBatch(context.Background(), MenuIPDNSStatic, []string{"*1"}, false); err != nil {
		t.Fatal(err)
	}
	if patchCalls != 0 {
		t.Fatalf("fallback patch calls = %d, want 0", patchCalls)
	}
}

func TestMutationBatchFailsWhenFallbackDoesNotConverge(t *testing.T) {
	var patchCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/rest/execute":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode batch request: %v", err)
			}
			_, _ = io.WriteString(w, batchSuccessResponse(t, payload["script"].(string)))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/ip/dns/static":
			_, _ = io.WriteString(w, `[{".id":"*1","disabled":"true"}]`)
		case r.Method == http.MethodPatch && r.URL.Path == "/rest/ip/dns/static/*1":
			patchCalls++
			_, _ = io.WriteString(w, `{ ".id": "*1", "disabled": "false" }`)
		default:
			t.Fatalf("unexpected batch request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewMutationClient(server.URL, "policy", "secret")
	err := client.SetDisabledBatch(context.Background(), MenuIPDNSStatic, []string{"*1"}, false)
	if err == nil || !strings.Contains(err.Error(), "did not converge") {
		t.Fatalf("SetDisabledBatch error = %v, want non-convergence error", err)
	}
	if patchCalls != 1 {
		t.Fatalf("fallback patch calls = %d, want 1", patchCalls)
	}
}

func TestMutationBatchReadbackRejectsMissingAndInvalidObjects(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
		want string
	}{
		{name: "missing", body: `[{".id":"*2","disabled":"false"}]`, want: "missing IDs *1"},
		{name: "invalid disabled", body: `[{".id":"*1","disabled":"unknown"}]`, want: "invalid RouterOS boolean"},
		{name: "duplicate", body: `[{".id":"*1","disabled":"false"},{".id":"*1","disabled":"false"}]`, want: "duplicate ID *1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodPost && r.URL.Path == "/rest/execute":
					var payload map[string]any
					if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
						t.Fatalf("decode batch request: %v", err)
					}
					_, _ = io.WriteString(w, batchSuccessResponse(t, payload["script"].(string)))
				case r.Method == http.MethodGet && r.URL.Path == "/rest/ip/dns/static":
					_, _ = io.WriteString(w, test.body)
				case r.Method == http.MethodPatch:
					t.Fatalf("unexpected fallback PATCH for invalid read-back: %s", r.URL.Path)
				default:
					t.Fatalf("unexpected batch request: %s %s", r.Method, r.URL.Path)
				}
			}))
			defer server.Close()

			client := NewMutationClient(server.URL, "policy", "secret")
			err := client.SetDisabledBatch(context.Background(), MenuIPDNSStatic, []string{"*1"}, false)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("SetDisabledBatch error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestMutationBatchReconcilesUnknownScriptOutcome(t *testing.T) {
	state := map[string]bool{"*1": true, "*2": true}
	var patchIDs []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/ip/dns/static":
			value := func(id string) string {
				if state[id] {
					return "true"
				}
				return "false"
			}
			_ = json.NewEncoder(w).Encode([]map[string]string{
				{".id": "*1", "disabled": value("*1")},
				{".id": "*2", "disabled": value("*2")},
			})
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/rest/ip/dns/static/"):
			id := strings.TrimPrefix(r.URL.Path, "/rest/ip/dns/static/")
			patchIDs = append(patchIDs, id)
			state[id] = false
			_, _ = io.WriteString(w, `{ ".id": "`+id+`", "disabled": "false" }`)
		default:
			t.Fatalf("unexpected batch request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := NewMutationClientWithOptions(server.URL, "policy", "secret", MutationClientOptions{
		HTTPClient: &http.Client{Transport: mutationRoundTripper(func(request *http.Request) (*http.Response, error) {
			if request.URL.Path == "/rest/execute" {
				// Simulate the request reaching RouterOS and applying the first
				// command before the connection is lost.
				state["*1"] = false
				return nil, errors.New("connection lost after batch applied")
			}
			return http.DefaultTransport.RoundTrip(request)
		})},
		MaxRetries: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.SetDisabledBatch(context.Background(), MenuIPDNSStatic, []string{"*1", "*2"}, false); err != nil {
		t.Fatal(err)
	}
	if len(patchIDs) != 1 || patchIDs[0] != "*2" {
		t.Fatalf("unknown outcome fallback patch IDs = %v, want [*2]", patchIDs)
	}
}
