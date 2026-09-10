package policyv2

import (
	"context"
	"errors"
	"testing"

	"rosboard/internal/routeros"
)

func TestSourceSelectorsExposeFactsWhenInferenceReadFails(t *testing.T) {
	reader := discoveryErrorReader{
		objects: map[routeros.ReadMenu][]routeros.RouterOSObject{
			routeros.ReadMenuInterface: {
				{"name": "bridge1", "type": "bridge", "running": "true"},
				{"name": "pppoe-out1", "type": "pppoe-out", "running": "true"},
			},
			routeros.ReadMenuInterfaceList: {
				{"name": "all"},
				{"name": "LAN", "comment": "client LAN"},
			},
			routeros.ReadMenuInterfaceListMember: {{"list": "LAN", "interface": "bridge1"}},
		},
		errors: map[routeros.ReadMenu]error{
			routeros.ReadMenuIPRoute: errors.New("route snapshot unavailable"),
		},
	}

	discovery, err := NewScanner(reader).SourceSelectors(context.Background(), "edge")
	if err != nil {
		t.Fatal(err)
	}
	if !discovery.Available || discovery.FactStatus != sourceSelectorFactAvailable {
		t.Fatalf("facts were not retained: %#v", discovery)
	}
	if discovery.RecommendationStatus != sourceSelectorFactPartial {
		t.Fatalf("recommendation status = %q, want partial", discovery.RecommendationStatus)
	}
	if len(discovery.Interfaces) != 2 || discovery.Interfaces[0].Name != "bridge1" {
		t.Fatalf("unexpected interface facts: %#v", discovery.Interfaces)
	}
	if !containsSourceSelectorInterfaceList(discovery.InterfaceLists, "all") || !containsSourceSelectorInterfaceList(discovery.InterfaceLists, "LAN") {
		t.Fatalf("interface-list facts were filtered: %#v", discovery.InterfaceLists)
	}
	for _, list := range discovery.InterfaceLists {
		if list.Name == "all" && list.SafetyCode != RoutingSourceInterfaceListAllDeferredCode {
			t.Fatalf("built-in all safety metadata missing: %#v", list)
		}
	}
	if len(discovery.Warnings) == 0 {
		t.Fatal("inference failure did not produce a warning")
	}
}

func TestSourceSelectorsKeepNonRecommendedInterfacesSelectable(t *testing.T) {
	reader := discoveryReader{
		routeros.ReadMenuInterface: {
			{"name": "bridge1", "type": "bridge", "running": "true"},
			{"name": "ether1", "type": "ether", "running": "true"},
		},
		routeros.ReadMenuInterfaceList:       {{"name": "LAN"}},
		routeros.ReadMenuInterfaceListMember: {{"list": "LAN", "interface": "bridge1"}},
		routeros.ReadMenuBridgePort:          {{"bridge": "bridge1", "interface": "ether1"}},
	}

	discovery, err := NewScanner(reader).SourceSelectors(context.Background(), "edge")
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range discovery.Interfaces {
		if item.Name == "ether1" && item.Recommended {
			t.Fatalf("bridge slave unexpectedly became recommended: %#v", item)
		}
		if item.Name == "ether1" && item.Reason == "" {
			t.Fatalf("non-recommended fact lost its explanation: %#v", item)
		}
	}
}

type countingDiscoveryReader struct {
	objects map[routeros.ReadMenu][]routeros.RouterOSObject
	errors  map[routeros.ReadMenu]error
	calls   map[routeros.ReadMenu]int
}

func (r *countingDiscoveryReader) PolicyList(_ context.Context, menu routeros.ReadMenu, _ []string) ([]routeros.RouterOSObject, error) {
	if r.calls == nil {
		r.calls = make(map[routeros.ReadMenu]int)
	}
	r.calls[menu]++
	if err := r.errors[menu]; err != nil {
		return nil, err
	}
	return r.objects[menu], nil
}

func TestScanAndSourceSelectorsShareOneEvidenceSnapshot(t *testing.T) {
	reader := &countingDiscoveryReader{
		objects: map[routeros.ReadMenu][]routeros.RouterOSObject{
			routeros.ReadMenuSystemResource:      {{"board-name": "router", "version": "7.22.3"}},
			routeros.ReadMenuInterface:           {{"name": "bridge1", "type": "bridge", "running": "true"}},
			routeros.ReadMenuInterfaceList:       {{"name": "LAN"}},
			routeros.ReadMenuInterfaceListMember: {{"list": "LAN", "interface": "bridge1"}},
		},
		errors: make(map[routeros.ReadMenu]error),
	}

	snapshot, err := NewScanner(reader).ScanAndSourceSelectors(context.Background(), "edge")
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Discovery.Available || !snapshot.SourceSelectors.Available {
		t.Fatalf("shared snapshot lost a projection: %#v", snapshot)
	}
	if snapshot.Discovery.Snapshot.Fingerprint == "" || snapshot.Discovery.Snapshot.Fingerprint != snapshot.SourceSelectors.Snapshot.Fingerprint {
		t.Fatalf("projections do not share a fingerprint: discovery=%q selectors=%q", snapshot.Discovery.Snapshot.Fingerprint, snapshot.SourceSelectors.Snapshot.Fingerprint)
	}
	for menu, calls := range reader.calls {
		if calls != 1 {
			t.Fatalf("menu %s was read %d times in one snapshot", menu, calls)
		}
	}
}

func TestScanAndSourceSelectorsKeepFactsWhenInferenceReadFails(t *testing.T) {
	reader := &countingDiscoveryReader{
		objects: map[routeros.ReadMenu][]routeros.RouterOSObject{
			routeros.ReadMenuSystemResource:      {{"board-name": "router"}},
			routeros.ReadMenuInterface:           {{"name": "bridge1", "type": "bridge", "running": "true"}},
			routeros.ReadMenuInterfaceList:       {{"name": "LAN"}},
			routeros.ReadMenuInterfaceListMember: {{"list": "LAN", "interface": "bridge1"}},
		},
		errors: map[routeros.ReadMenu]error{routeros.ReadMenuIPRoute: errors.New("route snapshot unavailable")},
	}

	snapshot, err := NewScanner(reader).ScanAndSourceSelectors(context.Background(), "edge")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Discovery.Available {
		t.Fatalf("legacy projection should report its required route failure: %#v", snapshot.Discovery)
	}
	if !snapshot.SourceSelectors.Available || snapshot.SourceSelectors.FactStatus != sourceSelectorFactAvailable || snapshot.SourceSelectors.RecommendationStatus != sourceSelectorFactPartial {
		t.Fatalf("source facts were not retained after inference failure: %#v", snapshot.SourceSelectors)
	}
	if len(snapshot.SourceSelectors.Interfaces) != 1 || len(snapshot.SourceSelectors.InterfaceLists) != 1 {
		t.Fatalf("source facts were lost after inference failure: %#v", snapshot.SourceSelectors)
	}
}

func containsSourceSelectorInterfaceList(values []SourceSelectorInterfaceList, name string) bool {
	for _, value := range values {
		if value.Name == name {
			return true
		}
	}
	return false
}
