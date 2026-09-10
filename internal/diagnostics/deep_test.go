package diagnostics

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"rosboard/internal/config"
	"rosboard/internal/policyv2"
	"rosboard/internal/routeros"
)

type deepTestReader struct {
	objects   map[routeros.ReadMenu][]routeros.RouterOSObject
	errors    map[routeros.ReadMenu]error
	counts    map[routeros.ReadMenu]int
	blockMenu routeros.ReadMenu
}

func (r *deepTestReader) PolicyList(ctx context.Context, menu routeros.ReadMenu, _ []string) ([]routeros.RouterOSObject, error) {
	if r.counts == nil {
		r.counts = make(map[routeros.ReadMenu]int)
	}
	r.counts[menu]++
	if menu == r.blockMenu {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if err := r.errors[menu]; err != nil {
		return nil, err
	}
	return r.objects[menu], nil
}

func deepTestDevice() config.DeviceConfig {
	return config.DeviceConfig{
		ID:      "edge",
		Name:    "Edge",
		Enabled: true,
		RouterOS: config.RouterOSConfig{
			BaseURL: "http://router.test", Username: "reader", Password: "fixture",
		},
	}
}

func deepTestTopologyReader() *deepTestReader {
	return &deepTestReader{
		objects: map[routeros.ReadMenu][]routeros.RouterOSObject{
			routeros.ReadMenuSystemResource: {{"board-name": "router", "platform": "test", "version": "7.22.3"}},
			routeros.ReadMenuInterface: {
				{"name": "bridge-lan", "type": "bridge", "running": "true"},
				{"name": "ether2", "type": "ether", "running": "true"},
				{"name": "wireguard1", "type": "wg", "running": "true"},
			},
			routeros.ReadMenuIPRoute: {
				{"dst-address": "0.0.0.0/0", "gateway": "192.0.2.1", "immediate-gw": "192.0.2.1%ether-wan", "routing-table": "main", "active": "true"},
			},
			routeros.ReadMenuIPv6Route: {
				{"dst-address": "::/0", "gateway": "2001:db8::1", "immediate-gw": "2001:db8::1%ether-wan", "routing-table": "main", "active": "true"},
			},
			routeros.ReadMenuInterfaceList: {
				{"name": "LAN"},
			},
			routeros.ReadMenuInterfaceListMember: {
				{"list": "LAN", "interface": "bridge-lan"},
			},
			routeros.ReadMenuBridgePort: {
				{"bridge": "bridge-lan", "interface": "ether2"},
			},
			routeros.ReadMenuIPAddress: {
				{"interface": "bridge-lan", "address": "192.0.2.10/24"},
				{"interface": "wireguard1", "address": "10.66.0.1/24"},
			},
			routeros.ReadMenuIPv6Address:    {},
			routeros.ReadMenuIPDHCPClient:   {},
			routeros.ReadMenuIPv6DHCPClient: {},
			routeros.ReadMenuPPPoEClient:    {},
		},
		errors: make(map[routeros.ReadMenu]error),
	}
}

func TestDeepSnapshotReadsEachEndpointOnce(t *testing.T) {
	reader := deepTestTopologyReader()
	now := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	report := (Runner{
		Device:       deepTestDevice(),
		PolicyReader: reader,
		Now:          func() time.Time { return now },
	}).Deep(context.Background())

	if report.Mode != ModeDeep || report.Snapshot.Fingerprint == "" {
		t.Fatalf("unexpected deep report: %#v", report)
	}
	if len(report.Snapshot.Endpoints) != 12 {
		t.Fatalf("endpoint count = %d, want 12: %#v", len(report.Snapshot.Endpoints), report.Snapshot.Endpoints)
	}
	for _, endpoint := range report.Snapshot.Endpoints {
		if endpoint.ReadCount != 1 || endpoint.CacheHits != 0 {
			t.Fatalf("endpoint was not read exactly once: %#v", endpoint)
		}
		if endpoint.Purpose == "" || len(endpoint.SharedBy) == 0 {
			t.Fatalf("endpoint evidence lacks reuse metadata: %#v", endpoint)
		}
		if reader.counts[routeros.ReadMenu(endpoint.Endpoint)] != 1 {
			t.Fatalf("source read count for %s = %d, want 1", endpoint.Endpoint, reader.counts[routeros.ReadMenu(endpoint.Endpoint)])
		}
	}
	if len(report.IngressTrace) == 0 {
		t.Fatal("deep report has no ingress decision trace")
	}
	for _, finding := range report.Findings {
		if finding.ID == "topology.deep" {
			if finding.Status != StatusOK {
				t.Fatalf("topology finding = %#v", finding)
			}
			return
		}
	}
	t.Fatal("topology finding not found")
}

func TestDeepKeepsOptionalRouterOSFailuresInSnapshot(t *testing.T) {
	reader := deepTestTopologyReader()
	reader.errors[routeros.ReadMenuInterfaceList] = errors.New("interface list denied")
	reader.errors[routeros.ReadMenuBridgePort] = errors.New("bridge port denied")
	reader.errors[routeros.ReadMenuIPv6Route] = errors.New("ipv6 route denied")
	report := (Runner{Device: deepTestDevice(), PolicyReader: reader}).Deep(context.Background())

	var topology Finding
	for _, finding := range report.Findings {
		if finding.ID == "topology.deep" {
			topology = finding
			break
		}
	}
	if topology.Status != StatusWarning {
		t.Fatalf("topology status = %q, want warning: %#v", topology.Status, topology)
	}
	for _, endpoint := range report.Snapshot.Endpoints {
		switch endpoint.Endpoint {
		case string(routeros.ReadMenuInterfaceList), string(routeros.ReadMenuBridgePort), string(routeros.ReadMenuIPv6Route):
			if endpoint.Error == "" || endpoint.ReadCount != 1 {
				t.Fatalf("failed endpoint evidence = %#v", endpoint)
			}
		}
	}
}

func TestDeepTimeoutReturnsPartialEvidenceWithoutPanicking(t *testing.T) {
	reader := deepTestTopologyReader()
	reader.blockMenu = routeros.ReadMenuInterface
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	report := (Runner{Device: deepTestDevice(), PolicyReader: reader}).Deep(ctx)

	for _, finding := range report.Findings {
		if finding.ID == "topology.deep" {
			if finding.Status != StatusError {
				t.Fatalf("topology status = %q, want error: %#v", finding.Status, finding)
			}
			if len(report.Snapshot.Endpoints) == 0 {
				t.Fatal("timeout report dropped partial endpoint evidence")
			}
			return
		}
	}
	t.Fatal("topology finding not found")
}

func TestDeepReaderDoesNotExposeCredentialsInEndpointEvidence(t *testing.T) {
	reader := deepTestTopologyReader()
	reader.objects[routeros.ReadMenuInterface] = append(reader.objects[routeros.ReadMenuInterface], routeros.RouterOSObject{"name": "reader", "comment": "no password here"})
	report := (Runner{Device: deepTestDevice(), PolicyReader: reader}).Deep(context.Background())
	for _, endpoint := range report.Snapshot.Endpoints {
		if endpoint.Error != "" && strings.Contains(endpoint.Error, "fixture") {
			t.Fatalf("endpoint error leaked credential: %q", endpoint.Error)
		}
	}
	if report.Snapshot.Fingerprint == "" {
		t.Fatalf("deep fingerprint is empty: %#v", report.Snapshot)
	}
}

func TestOnlyWireGuardCandidatesRequiresEveryCandidateToBeWireGuard(t *testing.T) {
	cases := []struct {
		name       string
		candidates []policyv2.TrafficIngressCandidate
		want       bool
	}{
		{name: "empty", want: false},
		{name: "wireguard only", candidates: []policyv2.TrafficIngressCandidate{{Kind: "wireguard"}}, want: true},
		{name: "interface list and wireguard", candidates: []policyv2.TrafficIngressCandidate{{Kind: "interface-list"}, {Kind: "wireguard"}}, want: false},
		{name: "interface list only", candidates: []policyv2.TrafficIngressCandidate{{Kind: "interface-list"}}, want: false},
		{name: "bridge and wireguard", candidates: []policyv2.TrafficIngressCandidate{{Kind: "bridge"}, {Kind: "wireguard"}}, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := onlyWireGuardCandidates(tc.candidates); got != tc.want {
				t.Fatalf("onlyWireGuardCandidates() = %v, want %v", got, tc.want)
			}
		})
	}
}
