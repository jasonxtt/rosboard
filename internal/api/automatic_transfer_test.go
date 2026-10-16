package api

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"rosboard/internal/config"
	"testing"
)

func TestAutomaticTransferTrustIsBoundToEnabledDevice(t *testing.T) {
	d := config.DeviceConfig{ID: "lab", Enabled: true, RouterOS: config.RouterOSConfig{BaseURL: "http://router.fixture", Username: "user", Password: "private-fixture"}}
	s := NewServer(config.Config{Path: filepath.Join(t.TempDir(), "config.yaml"), Devices: []config.DeviceConfig{d}}, nil, nil)
	verified := d
	verified.ContainerSSHHostKey = "SHA256:fixture"
	if _, err := s.persistAutomaticTransfer(d, verified); err != nil {
		t.Fatal(err)
	}
	saved, _ := s.configSnapshot().Device(d.ID)
	if saved.ContainerSSHHostKey != "SHA256:fixture" || (saved.RouterOS.BaseURL != d.RouterOS.BaseURL || saved.RouterOS.Username != d.RouterOS.Username || saved.RouterOS.Password != d.RouterOS.Password) {
		t.Fatal("unexpected connection changes")
	}
	verified.ContainerSSHHostKey = "SHA256:changed"
	if _, err := s.persistAutomaticTransfer(d, verified); err == nil {
		t.Fatal("overwrote an existing pin")
	}
	if got, err := s.automaticTransfer(context.Background(), saved); err != nil || got.ContainerSSHHostKey != saved.ContainerSSHHostKey {
		t.Fatal("existing trust was rediscovered")
	}
	readonly := d
	readonly.Enabled = false
	if _, err := s.automaticTransfer(context.Background(), readonly); err == nil {
		t.Fatal("disabled device enrolled")
	}
}
func TestAutomaticTransferRejectsChangedConnection(t *testing.T) {
	d := config.DeviceConfig{ID: "lab", Enabled: true, RouterOS: config.RouterOSConfig{BaseURL: "http://router.fixture", Username: "user", Password: "private-fixture"}}
	changed := d
	changed.RouterOS.Password = "changed-fixture"
	s := NewServer(config.Config{Path: filepath.Join(t.TempDir(), "config.yaml"), Devices: []config.DeviceConfig{changed}}, nil, nil)
	verified := d
	verified.ContainerSSHHostKey = "SHA256:fixture"
	if _, err := s.persistAutomaticTransfer(d, verified); err == nil {
		t.Fatal("saved stale authority")
	}
	saved, _ := s.configSnapshot().Device(d.ID)
	if saved.ContainerSSHHostKey != "" {
		t.Fatal("persisted trust after identity change")
	}
}

func TestAutomaticTransferIndependentLab(t *testing.T) {
	p := os.Getenv("ROSBOARD_HOST_IMAGE_LAB_CONFIG")
	if p == "" {
		t.Skip("independent lab opt-in")
	}
	raw, err := os.ReadFile(p)
	var c struct{ BaseURL, Username, Password, HostKey string }
	if err != nil || json.Unmarshal(raw, &c) != nil || c.BaseURL != "http://10.0.2.5" {
		t.Fatal("independent lab configuration required")
	}
	d := config.DeviceConfig{ID: "lab", Enabled: true, RouterOS: config.RouterOSConfig{BaseURL: c.BaseURL, Username: c.Username, Password: c.Password}}
	s := NewServer(config.Config{Path: filepath.Join(t.TempDir(), "config.yaml"), Devices: []config.DeviceConfig{d}}, nil, nil)
	got, err := s.automaticTransfer(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	if got.ContainerSSHHostKey != c.HostKey {
		t.Fatal("first-use SSH identity differs from known lab key")
	}
	saved, _ := s.configSnapshot().Device(d.ID)
	if saved.ContainerSSHHostKey != c.HostKey {
		t.Fatal("first-use trust not persisted")
	}
	if _, err = s.automaticTransfer(context.Background(), saved); err != nil {
		t.Fatal(err)
	}
	t.Log("REST authentication, automatic SSH key observation, pinned SFTP read and first-use persistence passed without RouterOS writes")
}
