package service

import (
	"net"
	"testing"

	"rosboard/internal/routeros"
)

func TestSelectTrafficInterfacesPrefersPPPoE(t *testing.T) {
	interfaces := []routeros.Interface{
		{Name: "lan", Type: "ether", Running: "true"},
		{Name: "wan", Type: "ether", Running: "true"},
		{Name: "pppoe-out1", Type: "pppoe-out", Running: "true"},
	}

	selected := selectTrafficInterfaces(nil, interfaces)
	if len(selected) != 1 || selected[0] != "pppoe-out1" {
		t.Fatalf("expected pppoe-out1, got %#v", selected)
	}
}

func TestOrientConnectionMapsUploadAndDownloadForLocalSource(t *testing.T) {
	_, network, _ := net.ParseCIDR("10.0.0.0/24")

	view, ok := orientConnection("ipv4", routeros.FirewallConnection{
		Protocol:        "tcp",
		SrcAddress:      "10.0.0.31",
		SrcPort:         "51998",
		DstAddress:      "8.8.8.8",
		DstPort:         "443",
		ReplySrcAddress: "8.8.8.8",
		ReplySrcPort:    "443",
		ReplyDstAddress: "10.0.0.31",
		ReplyDstPort:    "51998",
		OrigBytes:       "2048",
		ReplBytes:       "8192",
		OrigRate:        "128000",
		ReplRate:        "512000",
	}, []*net.IPNet{network})
	if !ok {
		t.Fatal("expected connection to be oriented")
	}
	if view.LocalAddress != "10.0.0.31" {
		t.Fatalf("unexpected local address: %s", view.LocalAddress)
	}
	if view.CurrentUploadBytes != 2048 || view.CurrentDownloadBytes != 8192 {
		t.Fatalf("unexpected byte mapping: %#v", view)
	}
	if view.UploadBps != 128000 || view.DownloadBps != 512000 {
		t.Fatalf("unexpected rate mapping: %#v", view)
	}
}

func TestOrientConnectionMapsReplySideWhenReplySourceIsLocal(t *testing.T) {
	_, network, _ := net.ParseCIDR("10.0.0.0/24")

	view, ok := orientConnection("ipv4", routeros.FirewallConnection{
		Protocol:        "tcp",
		SrcAddress:      "8.8.8.8",
		SrcPort:         "443",
		DstAddress:      "203.0.113.9",
		DstPort:         "55000",
		ReplySrcAddress: "10.0.0.50",
		ReplySrcPort:    "55000",
		ReplyDstAddress: "8.8.8.8",
		ReplyDstPort:    "443",
		OrigBytes:       "12000",
		ReplBytes:       "4000",
		OrigRate:        "640000",
		ReplRate:        "128000",
	}, []*net.IPNet{network})
	if !ok {
		t.Fatal("expected connection to be oriented")
	}
	if view.LocalAddress != "10.0.0.50" {
		t.Fatalf("unexpected local address: %s", view.LocalAddress)
	}
	if view.CurrentUploadBytes != 4000 || view.CurrentDownloadBytes != 12000 {
		t.Fatalf("unexpected byte mapping: %#v", view)
	}
	if view.UploadBps != 128000 || view.DownloadBps != 640000 {
		t.Fatalf("unexpected rate mapping: %#v", view)
	}
}
