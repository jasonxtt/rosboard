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

func containsSourceSelectorInterfaceList(values []SourceSelectorInterfaceList, name string) bool {
	for _, value := range values {
		if value.Name == name {
			return true
		}
	}
	return false
}
