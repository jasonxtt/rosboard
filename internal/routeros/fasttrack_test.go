package routeros

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUnsetFirewallConnectionMarkTypedBoundary(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodPost || r.URL.Path != "/rest/ip/firewall/filter/unset" {
			t.Errorf("unexpected target %s %s", r.Method, r.URL.Path)
		}
		var fields map[string]string
		if err := json.NewDecoder(r.Body).Decode(&fields); err != nil {
			t.Error(err)
		}
		if len(fields) != 2 || fields[".id"] != "*A" || fields["value-name"] != "connection-mark" {
			t.Errorf("unexpected fields %+v", fields)
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()
	client := NewMutationClient(server.URL, "test", "fixture")
	if err := client.UnsetFirewallConnectionMark(context.Background(), MenuIPFirewallFilter, "*A"); err != nil {
		t.Fatal(err)
	}
	for _, input := range []struct {
		menu MutationMenu
		id   string
	}{{MenuIPFirewallMangle, "*A"}, {MenuIPFirewallFilter, "../filter"}, {MenuIPFirewallFilter, ""}} {
		if err := client.UnsetFirewallConnectionMark(context.Background(), input.menu, input.id); err == nil {
			t.Fatal("invalid target accepted")
		}
	}
	client.SetWriteGate(func(context.Context) error { return errors.New("blocked") })
	if err := client.UnsetFirewallConnectionMark(context.Background(), MenuIPFirewallFilter, "*A"); err == nil {
		t.Fatal("write gate bypass")
	}
	if calls != 1 {
		t.Fatalf("unexpected HTTP mutations %d", calls)
	}
}
