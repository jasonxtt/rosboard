package containers

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/google/uuid"
	"rosboard/internal/config"
	"rosboard/internal/downloadnet"
	"rosboard/internal/routeros"
)

func hostFixture(t *testing.T) (*Service, config.DeviceConfig, *nativeFake, *atomic.Int32) {
	t.Helper()
	s, d, f := nativeFixture(t)
	d.RouterOS.BaseURL = "https://router.fixture"
	d.RouterOS.Username = "fixture"
	d.ContainerSSHHostKey = "SHA256:fixture"
	server := httptest.NewTLSServer(registry.New())
	t.Cleanup(server.Close)
	tr := server.Client().Transport.(*http.Transport).Clone()
	tr.TLSClientConfig = tr.TLSClientConfig.Clone()
	tr.TLSClientConfig.ServerName = server.Certificate().DNSNames[0]
	tr.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}
	s.DownloadSnapshot = func() (downloadnet.Snapshot, error) {
		return downloadnet.Snapshot{Revision: "fixture", Transport: tr.Clone()}, nil
	}
	var layerTar bytes.Buffer
	tw := tar.NewWriter(&layerTar)
	tw.WriteHeader(&tar.Header{Name: "file", Size: 7, Mode: 0644})
	tw.Write([]byte("content"))
	tw.Close()
	layer, e := tarball.LayerFromReader(bytes.NewReader(layerTar.Bytes()))
	if e != nil {
		t.Fatal(e)
	}
	img, _ := mutate.AppendLayers(empty.Image, layer)
	cfg, _ := img.ConfigFile()
	cfg.OS = "linux"
	cfg.Architecture = "amd64"
	img, _ = mutate.ConfigFile(img, cfg)
	tag, _ := name.NewTag("registry.fixture/library/test:latest")
	if e = remote.Write(tag, img, remote.WithTransport(tr)); e != nil {
		t.Fatal(e)
	}
	var uploads atomic.Int32
	s.PreparedUpload = func(ctx context.Context, d config.DeviceConfig, state *routeros.PreparedTransfer, source io.ReadSeeker, checkpoint func() error, phase func(string)) error {
		uploads.Add(1)
		state.NamespaceCreated = true
		state.PartialCreated = true
		state.Verified = true
		state.Published = true
		return checkpoint()
	}
	s.PreparedRemove = func(context.Context, config.DeviceConfig, routeros.PreparedTransfer) error { return nil }
	return s, d, f, &uploads
}
func hostDraft() Draft {
	return Draft{DraftID: "host-test", Name: "host-test", ImageSource: "registry", Image: "registry.fixture/library/test:latest", RootDir: "/sata1/app/rootfs", Network: Network{VETH: "veth-host", Bridge: "br-containers", Address: "172.20.0.20/24", Gateway: "172.20.0.1"}, Health: Health{Mode: "inherit"}}
}
func TestHostImageRetainsBeforeRuntimeAndCleanupRecoversWithoutRemoveReplay(t *testing.T) {
	s, d, f, uploads := hostFixture(t)
	j := nativeSubmit(t, s, d, ActionRequest{Action: "create", RequestID: uuid.NewString(), Draft: hostDraft()})
	n, _ := s.nativeDevice(d)
	g := n.state.Jobs[j.ID].Generated
	if uploads.Load() != 1 || g.Cleaned || g.Artifact.Digest == "" {
		t.Fatal("stopped image provenance/retention lost")
	}
	_, dir, _ := s.imageDirectory(d, j.ID)
	if _, e := os.Stat(filepath.Join(dir, "image.tar")); e != nil {
		t.Fatal(e)
	}
	f.mu.Lock()
	for _, row := range f.rows[routeros.ContainerList] {
		if row["remote-image"] != "" || row["file"] == "" {
			t.Fatal("native registry path was not replaced")
		}
	}
	f.mu.Unlock()
	s.PreparedRemove = func(context.Context, config.DeviceConfig, routeros.PreparedTransfer) error {
		return errors.New("cleanup offline")
	}
	deletion, e := s.Submit(context.Background(), d, nil, ActionRequest{Action: "delete", TargetID: j.TargetID, RequestID: uuid.NewString()})
	if e != nil {
		t.Fatal(e)
	}
	deletion = waitNative(t, s, d, deletion.ID)
	if deletion.State != "unknown" || !g.CleanupPending || !strings.Contains(deletion.Error, "容器已删除") {
		t.Fatal("delete/cleanup ambiguity hidden")
	}
	f.mu.Lock()
	before := f.writes
	remaining := len(f.rows[routeros.ContainerList])
	f.mu.Unlock()
	if remaining != 0 {
		t.Fatal("container was not deleted")
	}
	s.PreparedRemove = func(context.Context, config.DeviceConfig, routeros.PreparedTransfer) error { return nil }
	if _, e = s.RecoverNative(d, nil, deletion.ID); e != nil {
		t.Fatal(e)
	}
	deletion = waitNative(t, s, d, deletion.ID)
	f.mu.Lock()
	after := f.writes
	f.mu.Unlock()
	if deletion.State != "succeeded" || !g.Cleaned || after != before {
		t.Fatal("cleanup recovery repeated native writes")
	}
	if _, e = os.Stat(filepath.Join(dir, "image.tar")); !os.IsNotExist(e) {
		t.Fatal("cleanup left generated tar")
	}
}
func TestHostImageTransferFailureRecoveryPinsDigestBeforeNativeWrites(t *testing.T) {
	s, d, f, _ := hostFixture(t)
	normal := s.PreparedUpload
	var attempts atomic.Int32
	s.PreparedUpload = func(ctx context.Context, d config.DeviceConfig, state *routeros.PreparedTransfer, source io.ReadSeeker, checkpoint func() error, phase func(string)) error {
		if attempts.Add(1) == 1 {
			state.NamespaceCreated = true
			state.PartialCreated = true
			checkpoint()
			return routeros.ErrTransferUnknown
		}
		return normal(ctx, d, state, source, checkpoint, phase)
	}
	j, e := s.Submit(context.Background(), d, nil, ActionRequest{Action: "create", RequestID: uuid.NewString(), Draft: hostDraft()})
	if e != nil {
		t.Fatal(e)
	}
	j = waitNative(t, s, d, j.ID)
	if j.State != "unknown" {
		t.Fatal("transfer ambiguity released gate")
	}
	f.mu.Lock()
	writes := f.writes
	f.mu.Unlock()
	if writes != 0 {
		t.Fatal("native writes occurred before upload verification")
	}
	n, _ := s.nativeDevice(d)
	digest := n.state.Jobs[j.ID].Generated.Artifact.Digest
	restored := NewServiceWithState(s.NativeStateDir)
	restored.ReaderFor = s.ReaderFor
	restored.NativeWriterFor = s.NativeWriterFor
	restored.DownloadSnapshot = s.DownloadSnapshot
	restored.PreparedUpload = s.PreparedUpload
	restored.PreparedRemove = s.PreparedRemove
	restored.RegistryDNSLookup = nil
	if e = restored.RestoreNative(d); e != nil {
		t.Fatal(e)
	}
	if _, e = restored.RecoverNative(d, nil, j.ID); e != nil {
		t.Fatal(e)
	}
	j = waitNative(t, restored, d, j.ID)
	nr, _ := restored.nativeDevice(d)
	if j.State != "succeeded" || nr.state.Jobs[j.ID].Generated.Artifact.Digest != digest {
		t.Fatal("recovery lost immutable digest")
	}
	f.mu.Lock()
	count := len(f.rows[routeros.ContainerList])
	f.mu.Unlock()
	if count != 1 {
		t.Fatal("duplicate native add")
	}
}
func TestHostImageCancellationIsDurableAndReadOnlyUntilExplicitRequest(t *testing.T) {
	s, d, f, _ := hostFixture(t)
	started := make(chan struct{})
	s.DownloadSnapshot = func() (downloadnet.Snapshot, error) {
		return downloadnet.Snapshot{Revision: "cancel", Transport: &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			select {
			case started <- struct{}{}:
			default:
			}
			<-ctx.Done()
			return nil, ctx.Err()
		}}}, nil
	}
	j, e := s.Submit(context.Background(), d, nil, ActionRequest{Action: "create", RequestID: uuid.NewString(), Draft: hostDraft()})
	if e != nil {
		t.Fatal(e)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("download did not start")
	}
	if _, e = s.CancelNative(d, j.ID); e != nil {
		t.Fatal(e)
	}
	j = waitNative(t, s, d, j.ID)
	if j.State != "cancelled" {
		t.Fatalf("cancel state %s %s", j.State, j.Phase)
	}
	f.mu.Lock()
	writes := f.writes
	f.mu.Unlock()
	if writes != 0 {
		t.Fatal("cancelled host preparation mutated router")
	}
	n, _ := s.nativeDevice(d)
	if !n.state.Jobs[j.ID].CancelRequested {
		t.Fatal("cancellation not persisted")
	}
}

func TestHostImageKeepsAdmissionProxySnapshot(t *testing.T) {
	s, d, _, uploads := hostFixture(t)
	original := s.DownloadSnapshot
	var captures atomic.Int32
	s.DownloadSnapshot = func() (downloadnet.Snapshot, error) {
		if captures.Add(1) > 1 {
			return downloadnet.Snapshot{}, errors.New("global proxy changed after admission")
		}
		return original()
	}
	j := nativeSubmit(t, s, d, ActionRequest{Action: "create", RequestID: uuid.NewString(), Draft: hostDraft()})
	if j.State != "succeeded" || uploads.Load() != 1 || captures.Load() != 1 {
		t.Fatalf("job did not retain admission transport: state=%s captures=%d uploads=%d", j.State, captures.Load(), uploads.Load())
	}
}
