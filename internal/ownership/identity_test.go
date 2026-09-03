package ownership

import "testing"

func TestIdentityIsScopedByManagerAndDevice(t *testing.T) {
	first := Identity("manager-a", "device-a", "access-forwarder")
	if first != Identity("manager-a", "device-a", "access-forwarder") {
		t.Fatal("identity is not deterministic")
	}
	if first == Identity("manager-b", "device-a", "access-forwarder") || first == Identity("manager-a", "device-b", "access-forwarder") || first == Identity("manager-a", "device-a", "other") {
		t.Fatalf("identity is not scoped: %q", first)
	}
	if !IsCanonical(first) || !IsCanonicalFor("manager-a", "device-a", first) || IsCanonicalFor("manager-b", "device-a", first) {
		t.Fatalf("canonical ownership checks failed for %q", first)
	}
}

func TestLegacyIdentityShapeIsNotCurrentOwnership(t *testing.T) {
	if !IsUnscopedLegacy("rb_437614df") || IsCanonical("rb_437614df") {
		t.Fatal("old unscoped identity shape was not classified conservatively")
	}
	if IsUnscopedLegacy(Identity("manager", "device", "logical")) {
		t.Fatal("scoped identity was classified as unscoped legacy")
	}
}

func TestPhysicalNamespaceIsScopedAndDistinctFromLegacyNames(t *testing.T) {
	first := Namespace("manager-a", "device-a")
	if first != Namespace("manager-a", "device-a") || first == Namespace("manager-b", "device-a") || first == Namespace("manager-a", "device-b") {
		t.Fatalf("physical namespace is not scoped: %q", first)
	}
	if !IsNamespace(first+"target_123456789abc") || !IsNamespaceFor("manager-a", "device-a", first+"target_123456789abc") {
		t.Fatalf("current physical namespace was not recognized: %q", first)
	}
	if IsNamespace(Identity("manager-a", "device-a", "logical")) {
		t.Fatalf("canonical comment identity was misclassified as a physical namespace")
	}
	if IsNamespaceFor("manager-b", "device-a", first+"target_123456789abc") || IsNamespace("rb_ac_123456789abc") {
		t.Fatalf("legacy physical namespace was not isolated: %q", first)
	}
}
