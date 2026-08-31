package routeros

import "testing"

func TestDeviceWriteGateSerializesPerDevice(t *testing.T) {
	gate := NewDeviceWriteGate()
	releaseA, ok := gate.TryAcquire("router-a")
	if !ok {
		t.Fatal("first router-a acquire failed")
	}
	if _, ok := gate.TryAcquire("router-a"); ok {
		t.Fatal("second router-a acquire succeeded while active")
	}
	releaseB, ok := gate.TryAcquire("router-b")
	if !ok {
		t.Fatal("router-b should be independent")
	}
	releaseA()
	releaseA()
	if releaseAgain, ok := gate.TryAcquire("router-a"); !ok {
		t.Fatal("router-a was not released")
	} else {
		releaseAgain()
	}
	releaseB()
}
