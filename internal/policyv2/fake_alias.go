package policyv2

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"
)

type FakeAliasRequest struct {
	EgressID       string
	Family         AddressFamily
	PersistedAlias string
	UsedAliases    []string
}

func AllocateFakeDNSAlias(request FakeAliasRequest) (string, error) {
	if strings.TrimSpace(request.EgressID) == "" {
		return "", errors.New("fake DNS alias requires an egress ID")
	}
	if request.Family != FamilyIPv4 && request.Family != FamilyIPv6 {
		return "", fmt.Errorf("unsupported fake DNS alias family %q", request.Family)
	}
	used := make(map[netip.Addr]bool)
	for _, value := range request.UsedAliases {
		if address, err := netip.ParseAddr(strings.TrimSpace(value)); err == nil {
			used[address] = true
		}
	}
	if value := strings.TrimSpace(request.PersistedAlias); value != "" {
		address, err := netip.ParseAddr(value)
		if err != nil || !fakeAliasAllowed(address, request.Family) {
			return "", fmt.Errorf("fake DNS alias %q is outside the documentation pool", value)
		}
		if used[address] {
			return "", fmt.Errorf("fake DNS alias %q is already used", value)
		}
		return address.String(), nil
	}
	if request.Family == FamilyIPv4 {
		for _, prefixText := range []string{"192.0.2.0/24", "198.51.100.0/24", "203.0.113.0/24"} {
			prefix := netip.MustParsePrefix(prefixText)
			base := prefix.Addr().As4()
			for host := 1; host < 255; host++ {
				candidate := netip.AddrFrom4([4]byte{base[0], base[1], base[2], byte(host)})
				if !used[candidate] {
					return candidate.String(), nil
				}
			}
		}
	} else {
		prefix := netip.MustParsePrefix("2001:db8::/32")
		for host := 1; host <= 65535; host++ {
			candidate := netip.MustParseAddr(fmt.Sprintf("2001:db8::%x", host))
			if prefix.Contains(candidate) && !used[candidate] {
				return candidate.String(), nil
			}
		}
	}
	return "", fmt.Errorf("no collision-free documentation fake DNS alias is available for %s", request.Family)
}

func fakeAliasAllowed(address netip.Addr, family AddressFamily) bool {
	if family == FamilyIPv4 {
		for _, value := range []string{"192.0.2.0/24", "198.51.100.0/24", "203.0.113.0/24"} {
			if netip.MustParsePrefix(value).Contains(address) {
				return true
			}
		}
		return false
	}
	return netip.MustParsePrefix("2001:db8::/32").Contains(address)
}
