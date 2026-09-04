package routeros

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPolicyListIsReadOnlyAllowlistedAndUsesProplist(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/rest/ip/dns/static" {
			t.Fatalf("unexpected read request: %s %s", request.Method, request.URL.Path)
		}
		if request.URL.Query().Get(".proplist") != ".id,type,regexp,forward-to,address-list,match-subdomain" {
			t.Fatalf("unexpected proplist: %q", request.URL.Query().Get(".proplist"))
		}
		_, _ = writer.Write([]byte(`[{".id":"*1","type":"FWD","forward-to":"1.1.1.1","address-list":"policy","match-subdomain":"yes"}]`))
	}))
	defer server.Close()

	objects, err := NewClient(server.URL, "reader", "secret").PolicyList(context.Background(), ReadMenuIPDNSStatic, []string{".id", "type", "regexp", "forward-to", "address-list", "match-subdomain"})
	if err != nil || len(objects) != 1 || objects[0]["type"] != "FWD" {
		t.Fatalf("unexpected policy read: %#v err=%v", objects, err)
	}
	if _, err := NewClient(server.URL, "reader", "secret").PolicyList(context.Background(), ReadMenu("ipv6/dns/static"), nil); err == nil {
		t.Fatal("forbidden IPv6 DNS Static menu was accepted")
	}
}

func TestPolicyListBridgePortMenu(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/interface/bridge/port" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`[{"interface":"ether1","bridge":"bridge-lan","disabled":"false"}]`))
	}))
	defer server.Close()
	objects, err := NewClient(server.URL, "reader", "secret").PolicyList(context.Background(), ReadMenuBridgePort, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(objects) != 1 || objects[0]["interface"] != "ether1" || objects[0]["bridge"] != "bridge-lan" {
		t.Fatalf("bridge port read wrong: %#v", objects)
	}
}
