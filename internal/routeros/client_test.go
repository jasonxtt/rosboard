package routeros

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPErrorIncludesBoundedSanitizedRouterOSDetail(t *testing.T) {
	username := "policy"
	password := "secret"
	basicToken := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusBadRequest)
		_, _ = writer.Write([]byte(`{"message":"Bad Request","detail":"unknown parameter type; Authorization: Basic ` + basicToken + `"}`))
	}))
	defer server.Close()

	_, err := NewClient(server.URL, username, password).IPRoutes(context.Background())
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("IPRoutes error = %v, want HTTPError", err)
	}
	if !strings.Contains(err.Error(), "Bad Request: unknown parameter type") {
		t.Fatalf("HTTP error omitted RouterOS detail: %v", err)
	}
	if !strings.Contains(err.Error(), "[redacted]") || !strings.Contains(httpErr.Detail, "[redacted]") {
		t.Fatalf("HTTP error did not preserve redaction marker: err=%q detail=%q", err.Error(), httpErr.Detail)
	}
	for _, value := range []string{password, username + ":" + password, basicToken, username} {
		if strings.Contains(err.Error(), value) || strings.Contains(httpErr.Detail, value) {
			t.Fatalf("HTTP error leaked credential %q: err=%q detail=%q", value, err.Error(), httpErr.Detail)
		}
	}
	if len(httpErr.Detail) > maxMutationDetailBytes {
		t.Fatalf("HTTP error detail length = %d, want <= %d", len(httpErr.Detail), maxMutationDetailBytes)
	}
}

func TestAccountAccessResolvesCurrentUserGroupPolicies(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/rest/user":
			_, _ = writer.Write([]byte(`[{"name":"rosboard","group":"rosboard_g"}]`))
		case "/rest/user/group":
			_, _ = writer.Write([]byte(`[{"name":"rosboard_g","policy":"read,write,test,api,rest-api,!policy"}]`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	access, err := NewClient(server.URL, "rosboard", "secret").AccountAccess(context.Background(), "rosboard")
	if err != nil {
		t.Fatal(err)
	}
	if !access.Writable || access.Group != "rosboard_g" || strings.Join(access.Policies, ",") != "read,write,test,api,rest-api" {
		t.Fatalf("unexpected account access: %#v", access)
	}
}

func TestAccountAccessRecognizesExplicitlyMissingWrite(t *testing.T) {
	policies, writable := normalizeAccountPolicies("read,test,api,rest-api,!write")
	if writable || strings.Contains(strings.Join(policies, ","), "write") {
		t.Fatalf("read-only policy was treated as writable: %#v", policies)
	}
}

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

func TestSystemResourceAcceptsRouterOSObjectAndArrayResponses(t *testing.T) {
	for name, body := range map[string]string{
		"object": `{"board-name":"RB5009","version":"7.16.2"}`,
		"array":  `[{"board-name":"RB5009","version":"7.10"}]`,
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.URL.Path != "/rest/system/resource" {
					t.Fatalf("path = %q", request.URL.Path)
				}
				writer.Header().Set("Content-Type", "application/json")
				_, _ = writer.Write([]byte(body))
			}))
			defer server.Close()

			resource, err := NewClient(server.URL, "admin", "secret").SystemResource(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if resource.BoardName != "RB5009" {
				t.Fatalf("unexpected board name: %#v", resource)
			}
			if name == "array" && resource.Version != "7.10" {
				t.Fatalf("unexpected array resource: %#v", resource)
			}
		})
	}
}

func TestSystemResourceDetailEndpoints(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/rest/system/resource/cpu":
			_, _ = writer.Write([]byte(`[{"cpu":"0","load":"12","irq":"2","disk":"1"}]`))
		case "/rest/system/resource/irq":
			_, _ = writer.Write([]byte(`[{"cpu":"auto","active-cpu":"0","count":"99","irq":"5","users":"ethernet"}]`))
		case "/rest/system/resource/hardware":
			_, _ = writer.Write([]byte(`[{"location":"0:0","type":"pci","vendor":"MikroTik","name":"ethernet","serial-number":"SN1","irq":"5"}]`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "admin", "secret")
	cpus, err := client.SystemResourceCPU(context.Background())
	if err != nil || len(cpus) != 1 || cpus[0].CPU != "0" || cpus[0].Load != "12" || cpus[0].IRQ != "2" || cpus[0].Disk != "1" {
		t.Fatalf("unexpected CPU details: %#v err=%v", cpus, err)
	}
	irqs, err := client.SystemResourceIRQs(context.Background())
	if err != nil || len(irqs) != 1 || irqs[0].ActiveCPU != "0" || irqs[0].Users != "ethernet" {
		t.Fatalf("unexpected IRQ details: %#v err=%v", irqs, err)
	}
	hardware, err := client.SystemResourceHardware(context.Background())
	if err != nil || len(hardware) != 1 || hardware[0].Name != "ethernet" || hardware[0].SerialNumber != "SN1" {
		t.Fatalf("unexpected hardware details: %#v err=%v", hardware, err)
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
