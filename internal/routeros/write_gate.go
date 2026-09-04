package routeros

import (
	"strings"
	"sync"
)

// DeviceWriteGate serializes rosboard-owned RouterOS mutations per device
// while allowing different devices to reconcile independently.
type DeviceWriteGate struct {
	mu     sync.Mutex
	active map[string]bool
}

func NewDeviceWriteGate() *DeviceWriteGate {
	return &DeviceWriteGate{active: make(map[string]bool)}
}

func (gate *DeviceWriteGate) TryAcquire(deviceID string) (func(), bool) {
	if gate == nil {
		return func() {}, true
	}
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return nil, false
	}
	gate.mu.Lock()
	if gate.active[deviceID] {
		gate.mu.Unlock()
		return nil, false
	}
	gate.active[deviceID] = true
	gate.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			gate.mu.Lock()
			delete(gate.active, deviceID)
			gate.mu.Unlock()
		})
	}, true
}
