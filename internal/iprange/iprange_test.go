package iprange

import (
	"strings"
	"testing"
)

func TestPrefixesExpandsToMinimalCIDRs(t *testing.T) {
	cases := map[string]string{
		"10.0.0.100-10.0.0.199":                      "10.0.0.100/30 10.0.0.104/29 10.0.0.112/28 10.0.0.128/26 10.0.0.192/29",
		" 10.0.0.0 - 10.0.0.255 ":                    "10.0.0.0/24",
		"10.0.0.5-10.0.0.5":                          "10.0.0.5/32",
		"0.0.0.0-255.255.255.255":                    "0.0.0.0/0",
		"255.255.255.254-255.255.255.255":            "255.255.255.254/31",
		"2001:db8::1-2001:db8::4":                    "2001:db8::1/128 2001:db8::2/127 2001:db8::4/128",
		"::-ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff": "::/0",
	}
	for input, want := range cases {
		prefixes, err := Prefixes(input)
		if err != nil {
			t.Fatalf("%q: %v", input, err)
		}
		got := make([]string, len(prefixes))
		for i, prefix := range prefixes {
			got[i] = prefix.String()
		}
		if strings.Join(got, " ") != want {
			t.Fatalf("%q = %v, want %s", input, got, want)
		}
	}
}

func TestPrefixesRejectsInvalidRanges(t *testing.T) {
	cases := map[string]string{
		"10.0.0.199-10.0.0.100":      "起始地址不能大于结束地址",
		"10.0.0.1-2001:db8::1":       "两端必须同为 IPv4 或同为 IPv6",
		"10.0.0.1-":                  "结束地址无效",
		"-10.0.0.1":                  "起始地址无效",
		"10.0.0.0/24-10.0.1.0":       "起始地址无效",
		"fe80::1%eth0-fe80::2":       "起始地址无效",
		"10.0.0.1-10.0.0.2-10.0.0.3": "格式应为",
	}
	for input, want := range cases {
		_, err := Prefixes(input)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%q error = %v, want containing %q", input, err, want)
		}
	}
}
