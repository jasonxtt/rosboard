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
		if r.Method != http.MethodPost || r.URL.Path != "/rest/execute" {
			t.Fatalf("unexpected batch request: %s %s", r.Method, r.URL.Path)
		}
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
		_, _ = io.WriteString(w, `{}`)
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
	if scripts[2] != "/ip/dns/static/enable *1\n/ip/dns/static/enable *2" {
		t.Fatalf("unexpected enable script: %q", scripts[2])
	}
}

func TestMutationBatchAllowsDNSForwarderActivation(t *testing.T) {
	var script string
	var payload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/rest/execute" {
			t.Fatalf("unexpected batch request: %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode batch request: %v", err)
		}
		script, _ = payload["script"].(string)
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()

	client := NewMutationClient(server.URL, "policy", "secret")
	if err := client.SetDisabledBatch(context.Background(), MenuIPDNSForwarders, []string{"*f1", "*f2"}, false); err != nil {
		t.Fatal(err)
	}
	if script != "/ip/dns/forwarders/enable *f1\n/ip/dns/forwarders/enable *f2" {
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
