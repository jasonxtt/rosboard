package routeros

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVerifyRequiresCoreDataAndReturnsCandidatesWithOptionalWarnings(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/rest/system/resource":
			_, _ = writer.Write([]byte(`{"board-name":"CCR","version":"7.20","platform":"MikroTik"}`))
		case "/rest/interface":
			_, _ = writer.Write([]byte(`[{"name":"ether1","type":"ether","running":"true"}]`))
		case "/rest/ip/address":
			_, _ = writer.Write([]byte(`[{"address":"10.0.0.7/24","interface":"ether1"}]`))
		case "/rest/ipv6/address":
			_, _ = writer.Write([]byte(`[{"address":"fd00::7/64","interface":"ether1"}]`))
		case "/rest/ip/dhcp-server/lease", "/rest/ip/arp", "/rest/ip/firewall/connection":
			_, _ = writer.Write([]byte(`[]`))
		default:
			http.Error(writer, "optional unavailable", http.StatusForbidden)
		}
	}))
	defer server.Close()

	result, err := NewClient(server.URL, "admin", "secret").Verify(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Identity.RouterName != "CCR" || len(result.Interfaces) != 1 {
		t.Fatalf("unexpected verification result: %#v", result)
	}
	if len(result.CIDRCandidates) != 2 || result.CIDRCandidates[0].CIDR != "10.0.0.0/24" || result.CIDRCandidates[1].CIDR != "fd00::/64" {
		t.Fatalf("unexpected CIDR candidates: %#v", result.CIDRCandidates)
	}
	if len(result.Warnings) == 0 {
		t.Fatal("optional probe failures should be returned as warnings")
	}
}

func TestVerifyClassifiesCoreAuthenticationFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, "denied", http.StatusUnauthorized)
	}))
	defer server.Close()

	_, err := NewClient(server.URL, "admin", "wrong").Verify(context.Background())
	var verificationError *VerificationError
	if !errors.As(err, &verificationError) || verificationError.Kind != "authentication" {
		t.Fatalf("unexpected error: %#v", err)
	}
}
