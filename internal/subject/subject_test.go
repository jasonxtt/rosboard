package subject

import (
	"reflect"
	"testing"
)

func TestNormalizePrefixCanonicalizesAddressesAndNetworks(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: " 192.0.2.10 ", want: "192.0.2.10/32"},
		{input: "2001:0DB8:0:0::1", want: "2001:db8::1/128"},
		{input: "192.0.2.129/24", want: "192.0.2.0/24"},
		{input: "2001:db8:1:2::/48", want: "2001:db8:1::/48"},
	}
	for _, test := range tests {
		if got, err := NormalizePrefix(test.input); err != nil || got != test.want {
			t.Fatalf("NormalizePrefix(%q) = %q, %v; want %q", test.input, got, err, test.want)
		}
	}
}

func TestNormalizeSelectedSubjectDeduplicatesAndNormalizesEvidence(t *testing.T) {
	got, err := Normalize(Subject{
		Mode: ModeSelected,
		Members: []Member{{
			TerminalID: "terminal-a", Binding: BindingFixed,
			PinnedIPv4: []string{"192.0.2.10", "192.0.2.10"},
			PinnedIPv6: []string{"2001:0DB8::1"},
		}},
		Prefixes: []string{"192.0.2.129/24", "192.0.2.0/24", "2001:db8::1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := Subject{
		Mode: ModeSelected,
		Members: []Member{{
			TerminalID: "terminal-a", Binding: BindingFixed,
			PinnedIPv4: []string{"192.0.2.10"}, PinnedIPv6: []string{"2001:db8::1"}, LastIPv4: []string{}, LastIPv6: []string{},
		}},
		Prefixes: []string{"192.0.2.0/24", "2001:db8::1/128"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalized subject = %#v, want %#v", got, want)
	}
}

func TestNormalizeSubjectPreservesAutoLastTrustedAddresses(t *testing.T) {
	got, err := Normalize(Subject{Mode: ModeSelected, Members: []Member{{
		TerminalID: "terminal-a", Binding: BindingAuto, AnchorMAC: "aa:bb:cc:dd:ee:ff",
		LastIPv4: []string{"192.0.2.10"}, LastIPv6: []string{"2001:db8::10"},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	member := got.Members[0]
	if member.AnchorMAC != "AA:BB:CC:DD:EE:FF" || !reflect.DeepEqual(member.LastIPv4, []string{"192.0.2.10"}) || !reflect.DeepEqual(member.LastIPv6, []string{"2001:db8::10"}) {
		t.Fatalf("auto member evidence was not normalized: %#v", member)
	}
}

func TestNormalizeExcludedSubjectUsesTheSameAddressEvidenceShape(t *testing.T) {
	got, err := Normalize(Subject{Mode: ModeExcluded, Prefixes: []string{"192.0.2.129/24"}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode != ModeExcluded || !reflect.DeepEqual(got.Prefixes, []string{"192.0.2.0/24"}) {
		t.Fatalf("excluded subject = %#v", got)
	}
}

func TestNormalizeSubjectRejectsInvalidCombinations(t *testing.T) {
	tests := []Subject{
		{Mode: ModeAll, Prefixes: []string{"192.0.2.0/24"}},
		{Mode: ModeSelected},
		{Mode: ModeExcluded},
		{Mode: ModeSelected, Members: []Member{{TerminalID: "terminal-a", Binding: BindingAuto, PinnedIPv4: []string{"192.0.2.10"}}}},
		{Mode: ModeSelected, Members: []Member{{TerminalID: "terminal-a", Binding: BindingFixed}}},
	}
	for index, value := range tests {
		if _, err := Normalize(value); err == nil {
			t.Fatalf("invalid subject %d was accepted: %#v", index, value)
		}
	}
}

func TestNormalizeAddressesRejectsZonesAndSkipsLinkLocalIPv6(t *testing.T) {
	if got, err := NormalizeAddresses([]string{"fe80::1", "2001:db8::1"}, false); err != nil || !reflect.DeepEqual(got, []string{"2001:db8::1"}) {
		t.Fatalf("IPv6 address normalization = %#v, %v", got, err)
	}
	if _, err := NormalizePrefix("fe80::1%en0"); err == nil {
		t.Fatal("zoned prefix was accepted")
	}
}
