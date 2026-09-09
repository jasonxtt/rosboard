package routeros

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

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
			_, _ = io.WriteString(w, `{"ret":"__rosboard_batch_ok__"}`)
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
		!strings.Contains(scripts[2], ":onerror e in={") || !strings.Contains(scripts[2], ":put \"__rosboard_batch_ok__\"") {
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
			_, _ = io.WriteString(w, `{"ret":"__rosboard_batch_ok__"}`)
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/rest/execute" {
			t.Fatalf("unexpected batch request: %s %s", r.Method, r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"ret":"__rosboard_batch_error__:123 (:error; line 1)"}`)
	}))
	defer server.Close()

	client := NewMutationClient(server.URL, "policy", "secret")
	err := client.CreateBatch(context.Background(), MenuIPDNSStatic, []RouterOSFields{{"name": "example.com"}})
	if err == nil || !strings.Contains(err.Error(), "RouterOS batch script failed: 123") {
		t.Fatalf("CreateBatch error = %v, want the RouterOS script error", err)
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
			_, _ = io.WriteString(w, `{"ret":"__rosboard_batch_error__:no such command"}`)
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
			_, _ = io.WriteString(w, `{"ret":"normal command output\r\n__rosboard_batch_ok__"}`)
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
			_, _ = io.WriteString(w, `{"ret":"__rosboard_batch_ok__"}`)
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
