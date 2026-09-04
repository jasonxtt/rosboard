package main

import (
	"context"
	"io"
	"log"
	"testing"

	"rosboard/internal/config"
	"rosboard/internal/policyv2"
	"rosboard/internal/store"
)

func TestAssemblePolicyRuntimesScopesEnabledUnarchivedDevices(t *testing.T) {
	storage, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	cfg := config.Config{Devices: []config.DeviceConfig{
		{ID: "edge-a", Enabled: true, RouterOS: config.RouterOSConfig{BaseURL: "http://router-a.invalid", Username: "device-a", Password: "secret-a"}},
		{ID: "edge-b", Enabled: true, RouterOS: config.RouterOSConfig{BaseURL: "http://router-b.invalid", Username: "device-b", Password: "secret-b"}},
		{ID: "archived", Enabled: true, Archived: true, RouterOS: config.RouterOSConfig{BaseURL: "http://archived.invalid", Username: "device", Password: "secret"}},
		{ID: "disabled", RouterOS: config.RouterOSConfig{BaseURL: "http://disabled.invalid", Username: "device", Password: "secret"}},
		{ID: "no-access", Enabled: true, RouterOS: config.RouterOSConfig{BaseURL: "http://no-access.invalid"}},
	}}
	manager := policyv2.NewManager(log.New(io.Discard, "", 0))
	if err := assemblePolicyRuntimes(cfg, storage, nil, manager); err != nil {
		t.Fatal(err)
	}
	for _, deviceID := range []string{"edge-a", "edge-b"} {
		applier := manager.ApplierFor(deviceID)
		if applier == nil || applier.Reader == nil || applier.Mutation == nil || applier.Repo == nil {
			t.Fatalf("%s did not receive a complete V2 applier", deviceID)
		}
		if applier.Repo.DeviceID() != deviceID {
			t.Fatalf("%s received repository for %q", deviceID, applier.Repo.DeviceID())
		}
	}
	for _, deviceID := range []string{"archived", "disabled", "no-access"} {
		if manager.ApplierFor(deviceID) != nil {
			t.Fatalf("%s unexpectedly received a policy runtime", deviceID)
		}
	}
}

func TestAssemblePolicyRuntimePreservesInstallationIdentity(t *testing.T) {
	storage, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	want, err := storage.ManagerInstanceID(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{Devices: []config.DeviceConfig{{
		ID: config.DefaultDeviceID, Enabled: true,
		RouterOS: config.RouterOSConfig{BaseURL: "http://router.invalid", Username: "device", Password: "secret"},
	}}}
	manager := policyv2.NewManager(log.New(io.Discard, "", 0))
	if err := assemblePolicyRuntimes(cfg, storage, nil, manager); err != nil {
		t.Fatal(err)
	}
	applier := manager.ApplierFor(config.DefaultDeviceID)
	if applier == nil {
		t.Fatal("default device did not receive a V2 applier")
	}
	got, err := applier.Repo.ManagerInstanceID(context.Background())
	if err != nil || got != want {
		t.Fatalf("installation identity got=%q want=%q err=%v", got, want, err)
	}
}
