package routeros

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUnsetFirewallMatcherHasNarrowTypedBoundary(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "POST" || r.URL.Path != "/rest/ipv6/firewall/mangle/unset" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if payload[".id"] != "*A" || payload["value-name"] != "in-interface" {
			t.Errorf("payload: %#v", payload)
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()
	client := NewMutationClient(server.URL, "fixture", "fixture")
	if err := client.UnsetFirewallMatcher(context.Background(), MenuIPv6FirewallMangle, "*A", "in-interface"); err != nil {
		t.Fatal(err)
	}
	for _, x := range []struct {
		menu      MutationMenu
		id, field string
	}{{MenuIPRoute, "*A", "in-interface"}, {MenuIPFirewallMangle, "*A", "action"}, {MenuIPFirewallMangle, "bad", "in-interface"}} {
		if err := client.UnsetFirewallMatcher(context.Background(), x.menu, x.id, x.field); err == nil {
			t.Fatalf("unsafe unset accepted: %#v", x)
		}
	}
	if calls != 1 {
		t.Fatalf("requests=%d", calls)
	}
}
