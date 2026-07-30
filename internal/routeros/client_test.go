package routeros

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSystemHealthAcceptsRouterOSObjectAndArrayResponses(t *testing.T) {
	for name, body := range map[string]string{
		"array":  `[{"state":"enabled","state-after-reboot":"enabled"}]`,
		"object": `{"state":"enabled","state-after-reboot":"enabled"}`,
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.URL.Path != "/rest/system/health" {
					t.Fatalf("path = %q", request.URL.Path)
				}
				writer.Header().Set("Content-Type", "application/json")
				_, _ = writer.Write([]byte(body))
			}))
			defer server.Close()

			health, err := NewClient(server.URL, "admin", "secret").SystemHealth(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if health.State != "enabled" || health.StateAfterReboot != "enabled" {
				t.Fatalf("unexpected health: %#v", health)
			}
		})
	}
}

func TestInterfaceTopologyEndpoints(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/rest/interface/vlan":
			_, _ = writer.Write([]byte(`[{"name":"vlan2-aruba","interface":"lan","vlan-id":"2"}]`))
		case "/rest/interface/bridge/port":
			_, _ = writer.Write([]byte(`[{"interface":"veth-test","bridge":"br-container-test"}]`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "admin", "secret")
	vlans, err := client.VLANInterfaces(context.Background())
	if err != nil || len(vlans) != 1 || vlans[0].Name != "vlan2-aruba" || vlans[0].Interface != "lan" {
		t.Fatalf("unexpected VLAN response: %#v err=%v", vlans, err)
	}
	ports, err := client.BridgePorts(context.Background())
	if err != nil || len(ports) != 1 || ports[0].Interface != "veth-test" || ports[0].Bridge != "br-container-test" {
		t.Fatalf("unexpected bridge-port response: %#v err=%v", ports, err)
	}
}

func TestIPPoolsAndExpandedDHCPLeaseFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/rest/ip/pool":
			_, _ = writer.Write([]byte(`[{".id":"*1","name":"dhcp_pool0","ranges":"10.0.0.10-10.0.0.254","comment":"lan"}]`))
		case "/rest/ip/dhcp-server/lease":
			_, _ = writer.Write([]byte(`[{".id":"*2","address":"10.0.0.20","server":"dhcp1","host-name":"nas","mac-address":"AA:BB:CC:DD:EE:FF","status":"bound","expires-after":"1d2h3m4s","last-seen":"5m10s","dynamic":"true","blocked":"false","disabled":"false","active-address":"10.0.0.20","active-mac-address":"AA:BB:CC:DD:EE:FF"}]`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "admin", "secret")
	pools, err := client.IPPools(context.Background())
	if err != nil || len(pools) != 1 || pools[0].Name != "dhcp_pool0" || pools[0].Ranges != "10.0.0.10-10.0.0.254" {
		t.Fatalf("unexpected pools: %#v err=%v", pools, err)
	}
	leases, err := client.DHCPLeases(context.Background())
	if err != nil || len(leases) != 1 {
		t.Fatalf("unexpected leases: %#v err=%v", leases, err)
	}
	lease := leases[0]
	if lease.ID != "*2" || lease.ExpiresAfter != "1d2h3m4s" || lease.LastSeen != "5m10s" || lease.Dynamic != "true" || lease.ActiveAddress != "10.0.0.20" || lease.ActiveMACAddress != "AA:BB:CC:DD:EE:FF" || lease.Blocked != "false" || lease.Disabled != "false" {
		t.Fatalf("lease fields not mapped: %#v", lease)
	}
}
