package policyv2

import "testing"

func testPolicyEgress() Egress {
	return Egress{
		ID: "egress-a", Origin: EgressOriginPolicy, Name: "WAN A", Priority: 10,
		ListMode: ListModeShared, ListName: "display-only", DNSUpstream: "1.1.1.1",
		FakeAlias: "192.0.2.53", FailureMode: "strict", RouterOutput: true, Enabled: true,
		Revision: 4, Applied: true, Families: []EgressFamily{
			{Family: FamilyIPv4, Enabled: true, WANInterface: "wan-a", Gateway: "192.0.2.1", RouteTable: "custom-a", RouteMode: "strict", NATMode: "masquerade", WANSource: ""},
			{Family: FamilyIPv6, Enabled: false, WANInterface: "unused", Gateway: "2001:db8::1", RouteTable: "unused", RouteMode: "fallback", NATMode: "none", WANSource: "next-hop"},
		},
	}
}

func TestEgressExecutionSignatureIgnoresPresentationAndLifecycle(t *testing.T) {
	base := testPolicyEgress()
	other := base
	other.ID = "egress-b"
	other.Name = "A different display name"
	other.Priority = 999
	other.ListMode = ListModeDedicated
	other.ListName = "another-display-value"
	other.Revision = 99
	other.Applied = false
	other.PendingDeletion = true
	other.Families[1].WANInterface = "a completely different disabled family"
	if got, want := EgressExecutionSignature(other), EgressExecutionSignature(base); got != want {
		t.Fatalf("presentation/lifecycle fields changed execution signature: got %s want %s", got, want)
	}
}

func TestEgressExecutionSignatureIncludesExecutionSemantics(t *testing.T) {
	base := testPolicyEgress()
	variants := []struct {
		name   string
		mutate func(*Egress)
	}{
		{"gateway", func(value *Egress) { value.Families[0].Gateway = "192.0.2.2" }},
		{"dns", func(value *Egress) { value.DNSUpstream = "8.8.8.8" }},
		{"failure mode", func(value *Egress) { value.FailureMode = "fallback" }},
		{"nat mode", func(value *Egress) { value.Families[0].NATMode = "none" }},
		{"router output", func(value *Egress) { value.RouterOutput = false }},
		{"family", func(value *Egress) {
			value.Families = []EgressFamily{{Family: FamilyIPv6, Enabled: true, WANInterface: "wan6", Gateway: "2001:db8::1", RouteTable: "custom6"}}
		}},
		{"explicit route table", func(value *Egress) { value.Families[0].RouteTable = "custom-b" }},
		{"explicit alias", func(value *Egress) { value.FakeAlias = "192.0.2.54" }},
	}
	for _, variant := range variants {
		t.Run(variant.name, func(t *testing.T) {
			changed := base
			changed.Families = append([]EgressFamily(nil), base.Families...)
			variant.mutate(&changed)
			if got, want := EgressExecutionSignature(changed), EgressExecutionSignature(base); got == want {
				t.Fatalf("execution change was omitted from signature %s", got)
			}
		})
	}
}

func TestResolvePolicyEgressDoesNotReuseDifferentExecutionSemantics(t *testing.T) {
	base := testPolicyEgress()
	base.FakeAlias = ""
	variants := []struct {
		name   string
		mutate func(*Egress)
	}{
		{"dns", func(value *Egress) { value.DNSUpstream = "8.8.8.8" }},
		{"gateway", func(value *Egress) { value.Families[0].Gateway = "192.0.2.2" }},
		{"family", func(value *Egress) {
			value.Families = []EgressFamily{{Family: FamilyIPv6, Enabled: true, WANInterface: "wan6", Gateway: "2001:db8::1", RouteTable: "custom6"}}
		}},
		{"failure mode", func(value *Egress) { value.FailureMode = "fallback" }},
	}
	for _, variant := range variants {
		t.Run(variant.name, func(t *testing.T) {
			proposed := base
			proposed.ID = ""
			proposed.Families = append([]EgressFamily(nil), base.Families...)
			variant.mutate(&proposed)
			resolved, save := ResolvePolicyEgress(proposed, "", []Egress{base}, nil, "new-egress")
			if !save || resolved.ID != "new-egress" {
				t.Fatalf("different execution semantics were reused: %#v save=%v", resolved, save)
			}
		})
	}
}

func TestEgressExecutionSignatureIgnoresGeneratedRouteTableIdentity(t *testing.T) {
	left := testPolicyEgress()
	right := left
	left.Families = append([]EgressFamily(nil), left.Families...)
	right.Families = append([]EgressFamily(nil), right.Families...)
	left.Families[0].RouteTable = "rb_0123456789ab_012345674"
	right.Families[0].RouteTable = "rb_abcdefabcdef_fedcba986"
	if got, want := EgressExecutionSignature(left), EgressExecutionSignature(right); got != want {
		t.Fatalf("generated route-table identity changed execution signature: got %s want %s", got, want)
	}
}

func TestResolvePolicyEgressReusesAndAvoidsUnchangedChurn(t *testing.T) {
	current := testPolicyEgress()
	proposed := current
	proposed.ID = ""
	proposed.Name = "Policy B exit"
	proposed.FakeAlias = ""
	resolved, save := ResolvePolicyEgress(proposed, "", []Egress{current}, nil, "new-egress")
	if save || resolved.ID != current.ID {
		t.Fatalf("equivalent new policy did not reuse current egress: %#v save=%v", resolved, save)
	}

	unchanged, save := ResolvePolicyEgress(current, current.ID, []Egress{current}, map[string]int{current.ID: 1}, "new-egress")
	if save || unchanged.ID != current.ID {
		t.Fatalf("unchanged edit caused egress churn: %#v save=%v", unchanged, save)
	}
}

func TestResolvePolicyEgressUsesCopyOnWriteForSharedPolicyEgress(t *testing.T) {
	current := testPolicyEgress()
	proposed := current
	proposed.ID = current.ID
	proposed.Families = append([]EgressFamily(nil), current.Families...)
	proposed.Families[0].Gateway = "192.0.2.2"
	resolved, save := ResolvePolicyEgress(proposed, current.ID, []Egress{current}, map[string]int{current.ID: 2}, "egress-b")
	if !save || resolved.ID != "egress-b" || resolved.Origin != EgressOriginPolicy || resolved.Revision != 0 {
		t.Fatalf("shared egress edit did not copy-on-write: %#v save=%v", resolved, save)
	}
	if current.Families[0].Gateway != "192.0.2.1" {
		t.Fatalf("copy-on-write mutated the shared source: %#v", current)
	}
}

func TestResolvePolicyEgressMutatesOnlySolePolicyOwner(t *testing.T) {
	current := testPolicyEgress()
	proposed := current
	proposed.Families = append([]EgressFamily(nil), current.Families...)
	proposed.Families[0].Gateway = "192.0.2.2"
	resolved, save := ResolvePolicyEgress(proposed, current.ID, []Egress{current}, map[string]int{current.ID: 1}, "egress-b")
	if !save || resolved.ID != current.ID || resolved.Revision != current.Revision || resolved.Origin != current.Origin {
		t.Fatalf("sole policy owner was not updated in place: %#v save=%v", resolved, save)
	}
}

func TestResolvePolicyEgressNeverMutatesLegacyOrphan(t *testing.T) {
	current := testPolicyEgress()
	current.Origin = EgressOriginLegacy
	proposed := current
	proposed.Families = append([]EgressFamily(nil), current.Families...)
	proposed.Families[0].Gateway = "192.0.2.2"
	resolved, save := ResolvePolicyEgress(proposed, current.ID, []Egress{current}, map[string]int{current.ID: 0}, "egress-new")
	if !save || resolved.ID != "egress-new" || resolved.Origin != EgressOriginPolicy {
		t.Fatalf("legacy egress was mutated/reused for a changed policy: %#v save=%v", resolved, save)
	}
}

func TestResolvePolicyEgressDoesNotMutateUnrelatedExistingCandidate(t *testing.T) {
	current := testPolicyEgress()
	proposed := current
	proposed.Families = append([]EgressFamily(nil), current.Families...)
	proposed.Families[0].Gateway = "192.0.2.2"
	resolved, save := ResolvePolicyEgress(proposed, "", []Egress{current}, map[string]int{current.ID: 1}, "egress-new")
	if !save || resolved.ID != "egress-new" {
		t.Fatalf("unrelated candidate was mutated instead of copied: %#v save=%v", resolved, save)
	}
}

func TestResolvePolicyEgressRecomputesGeneratedRouteTableForNewIdentity(t *testing.T) {
	proposed := testPolicyEgress()
	proposed.ID = ""
	proposed.Families = append([]EgressFamily(nil), proposed.Families...)
	proposed.Families[0].RouteTable = "rb_0123456789ab_012345674"
	resolved, save := ResolvePolicyEgress(proposed, "", nil, nil, "egress-new")
	if !save || resolved.Families[0].RouteTable != "" {
		t.Fatalf("generated route table was retained across a new identity: %#v save=%v", resolved, save)
	}
}
