package containers

import (
	"context"
	"errors"
	"rosboard/internal/config"
	"rosboard/internal/routeros"
	"testing"
)

type fakeReader struct {
	data map[routeros.ContainerMenu][]routeros.RouterOSObject
	err  map[routeros.ContainerMenu]error
}

func (f fakeReader) ContainerRead(_ context.Context, m routeros.ContainerMenu) ([]routeros.RouterOSObject, error) {
	return f.data[m], f.err[m]
}
func TestReadFlagsTopologyAndSensitiveBoundary(t *testing.T) {
	f := fakeReader{data: map[routeros.ContainerMenu][]routeros.RouterOSObject{
		routeros.ContainerList: {{".id": "*7", "name": "dns", "interface": "container-dns", "stopped": "true", "logging": "false", "start-on-boot": "false", "envlists": "shared", "mountlists": "shared-mount", "default-cmd": "serve", "cmd": "exact", "remote-image": "dns:1"}, {".id": "*8", "name": "other", "interface": "container-dns", "running": "true"}},
		routeros.ContainerVETH: {{"name": "container-dns", "address": "172.20.0.2/24", "gateway": "172.20.0.1"}}, routeros.ContainerBridgePort: {{"interface": "container-dns", "bridge": "br-existing"}}, routeros.ContainerBridge: {{"name": "br-existing"}}, routeros.ContainerEnvs: {{"list": "shared", "key": "SPECIAL", "value": " \"$;中文 "}}, routeros.ContainerMounts: {{"list": "shared-mount", "src": "/sata1/config", "dst": "/etc/config", "mode": "ro,noexec"}}, routeros.ContainerConfig: {{"memory-high": "unlimited", "memory-max": "unlimited"}}, routeros.ContainerDisk: {{"slot": "sata1", "fs": "ext4", "free": "4 GiB"}}, routeros.ContainerLogs: {{".id": "*1", "container": "*7", "message": "safe"}, {"container": "*8", "message": "other device row"}},
	}}
	svc := &Service{ReaderFor: func(config.DeviceConfig) Reader { return f }}
	s, err := svc.Snapshot(context.Background(), config.DeviceConfig{})
	if err != nil {
		t.Fatal(err)
	}
	item := s.Items[0]
	if item.Status != "stopped" || item.Network.Bridge != "br-existing" || len(item.SharedVETH) != 1 || item.Config.Logging || item.Config.StartOnBoot || item.Config.Command != "exact" || item.ImageDefaults["cmd"] != "serve" || !item.Config.Mounts[0].ReadOnly {
		t.Fatalf("unexpected projection %+v", item)
	}
	if s.Capabilities.Writes || s.Capabilities.Mode != "read-only" || s.Options.Disks[0].FreeBytes != 4<<30 {
		t.Fatal("wrong capabilities/disks")
	}
	logs, err := svc.Logs(context.Background(), config.DeviceConfig{}, "*7", "dns")
	if err != nil || len(logs) != 1 {
		t.Fatalf("logs: %v %v", logs, err)
	}
}
func TestReadDistinguishesUnsupportedAndFailure(t *testing.T) {
	for _, tc := range []struct {
		err     error
		wantErr bool
	}{{&routeros.HTTPError{StatusCode: 404}, false}, {errors.New("transport failed"), true}, {&routeros.HTTPError{StatusCode: 401}, true}} {
		svc := &Service{ReaderFor: func(config.DeviceConfig) Reader {
			return fakeReader{err: map[routeros.ContainerMenu]error{routeros.ContainerList: tc.err}}
		}}
		s, err := svc.Snapshot(context.Background(), config.DeviceConfig{})
		if (err != nil) != tc.wantErr {
			t.Fatal(err)
		}
		if s.Capabilities.Supported {
			t.Fatal("unsupported request reported as supported")
		}
	}
}

// Coalescing must neither mix devices nor reuse snapshots after credentials
// change; client cancellation while waiting must not cancel the owner's read.
type blockingReader struct {
	started chan struct{}
	release chan struct{}
	calls   int
}

func (f *blockingReader) ContainerRead(ctx context.Context, menu routeros.ContainerMenu) ([]routeros.RouterOSObject, error) {
	if menu == routeros.ContainerList {
		f.calls++
		close(f.started)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-f.release:
		}
	}
	return []routeros.RouterOSObject{}, nil
}
func TestSnapshotCoalescingCancellationAndCredentialIsolation(t *testing.T) {
	reader := &blockingReader{started: make(chan struct{}), release: make(chan struct{})}
	svc := &Service{ReaderFor: func(config.DeviceConfig) Reader { return reader }}
	result := make(chan error)
	device := config.DeviceConfig{ID: "one"}
	go func() { _, err := svc.Snapshot(context.Background(), device); result <- err }()
	<-reader.started
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := svc.Snapshot(ctx, device); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	close(reader.release)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Snapshot(context.Background(), device); err != nil {
		t.Fatal(err)
	}
	if reader.calls != 1 {
		t.Fatal("cached/coalesced request repeated")
	}
	other := &fakeReader{data: map[routeros.ContainerMenu][]routeros.RouterOSObject{routeros.ContainerList: {{".id": "new", "name": "new-account"}}}}
	svc.ReaderFor = func(config.DeviceConfig) Reader { return other }
	device.RouterOS.Password = "changed-test-only"
	changed, err := svc.Snapshot(context.Background(), device)
	if err != nil || len(changed.Items) != 1 || changed.Items[0].Name != "new-account" {
		t.Fatal("credential change reused old snapshot", err)
	}
}
