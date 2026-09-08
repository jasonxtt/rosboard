package policyv2

import "testing"

func TestDeviceStateApplied(t *testing.T) {
	cases := []struct {
		name            string
		desiredRevision int64
		appliedRevision int64
		want            bool
	}{
		{name: "fresh state is in sync", desiredRevision: 0, appliedRevision: 0, want: true},
		{name: "desired saved but not applied", desiredRevision: 1, appliedRevision: 0, want: false},
		{name: "desired applied", desiredRevision: 1, appliedRevision: 1, want: true},
		{name: "apply ahead of desired is inconsistent", desiredRevision: 1, appliedRevision: 2, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state := DeviceState{DesiredRevision: tc.desiredRevision, AppliedRevision: tc.appliedRevision}
			if got := state.Applied(); got != tc.want {
				t.Fatalf("Applied() = %v, want %v", got, tc.want)
			}
		})
	}
}
