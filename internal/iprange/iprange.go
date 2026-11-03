// Package iprange parses the "start-end" address range notation accepted by
// IP inputs and expands it into the minimal covering list of CIDR prefixes.
package iprange

import (
	"fmt"
	"net/netip"
	"strings"
)

// IsRange reports whether text uses the range notation. Neither IPv4 nor IPv6
// literals contain '-', so its presence is unambiguous.
func IsRange(text string) bool {
	return strings.Contains(text, "-")
}

// Prefixes parses "start-end" (both ends inclusive, same family) and returns
// the minimal list of masked prefixes that exactly covers the range.
func Prefixes(text string) ([]netip.Prefix, error) {
	text = strings.TrimSpace(text)
	startText, endText, ok := strings.Cut(text, "-")
	if !ok || strings.Contains(endText, "-") {
		return nil, fmt.Errorf("IP 段 %q 格式应为「起始地址-结束地址」", text)
	}
	start, err := parseEnd(startText)
	if err != nil {
		return nil, fmt.Errorf("IP 段 %q 的起始地址无效", text)
	}
	end, err := parseEnd(endText)
	if err != nil {
		return nil, fmt.Errorf("IP 段 %q 的结束地址无效", text)
	}
	if start.Is4() != end.Is4() {
		return nil, fmt.Errorf("IP 段 %q 两端必须同为 IPv4 或同为 IPv6", text)
	}
	if start.Compare(end) > 0 {
		return nil, fmt.Errorf("IP 段 %q 的起始地址不能大于结束地址", text)
	}

	var result []netip.Prefix
	for {
		// The largest aligned block starting at start that does not pass end.
		bits := start.BitLen()
		for bits > 0 {
			candidate := netip.PrefixFrom(start, bits-1).Masked()
			if candidate.Addr() != start || lastAddr(candidate).Compare(end) > 0 {
				break
			}
			bits--
		}
		block := netip.PrefixFrom(start, bits)
		result = append(result, block)
		last := lastAddr(block)
		if last == end {
			return result, nil
		}
		start = last.Next()
	}
}

func parseEnd(text string) (netip.Addr, error) {
	address, err := netip.ParseAddr(strings.TrimSpace(text))
	if err != nil {
		return netip.Addr{}, err
	}
	if address.Zone() != "" {
		return netip.Addr{}, fmt.Errorf("zone not allowed")
	}
	return address, nil
}

// lastAddr returns the highest address inside a masked prefix.
func lastAddr(prefix netip.Prefix) netip.Addr {
	bytes := prefix.Addr().As16()
	offset := 0
	if prefix.Addr().Is4() {
		offset = 12
	}
	for bit := prefix.Bits(); bit < prefix.Addr().BitLen(); bit++ {
		bytes[offset+bit/8] |= 0x80 >> (bit % 8)
	}
	if prefix.Addr().Is4() {
		return netip.AddrFrom4([4]byte(bytes[12:]))
	}
	return netip.AddrFrom16(bytes)
}
