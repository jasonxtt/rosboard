package applicationpreset

import (
	"errors"
	"reflect"
	"testing"
)

func TestResolveRequestedKindsUsesDomainFirstDefault(t *testing.T) {
	got, err := ResolveRequestedKinds(nil, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"domain"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("default kinds=%v, want %v", got, want)
	}
}

func TestResolveRequestedKindsFallsBackToIP(t *testing.T) {
	got, err := ResolveRequestedKinds(nil, false, true)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"ip"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("fallback kinds=%v, want %v", got, want)
	}
}

func TestResolveRequestedKindsCanonicalizesAndRejectsUnavailableKinds(t *testing.T) {
	got, err := ResolveRequestedKinds([]string{" ip ", "domain", "ip"}, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"domain", "ip"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("kinds=%v, want %v", got, want)
	}
	if _, err := ResolveRequestedKinds([]string{"ip"}, true, false); !errors.Is(err, ErrRequestedKindUnavailable) {
		t.Fatalf("unavailable kind error=%v, want ErrRequestedKindUnavailable", err)
	}
	if _, err := ResolveRequestedKinds([]string{"other"}, true, true); err == nil {
		t.Fatal("unsupported kind must be rejected")
	}
}

func TestResolveRequestedKindsRejectsEmptyYAML(t *testing.T) {
	if _, err := ResolveRequestedKinds(nil, false, false); !errors.Is(err, ErrRequestedKindUnavailable) {
		t.Fatalf("empty YAML error=%v, want ErrRequestedKindUnavailable", err)
	}
}
