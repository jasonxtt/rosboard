package containers

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"rosboard/internal/config"
	"rosboard/internal/routeros"
)

type directoryFixture struct {
	mu             sync.Mutex
	rows           []routeros.RouterOSObject
	refs           []routeros.RouterOSObject
	mounts         []routeros.RouterOSObject
	writeErr       error
	failReads      bool
	failAfterWrite bool
	writes         int
}

func (f *directoryFixture) ContainerRead(_ context.Context, menu routeros.ContainerMenu) ([]routeros.RouterOSObject, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if menu == routeros.ContainerList {
		return f.refs, nil
	}
	if menu == routeros.ContainerMounts {
		return f.mounts, nil
	}
	if f.failReads {
		return nil, errors.New("connection closed")
	}
	rows := []routeros.RouterOSObject{}
	for _, row := range f.rows {
		copy := routeros.RouterOSObject{}
		for k, v := range row {
			copy[k] = v
		}
		rows = append(rows, copy)
	}
	return rows, nil
}
func (f *directoryFixture) CreateDirectory(_ context.Context, p string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writes++
	f.rows = append(f.rows, routeros.RouterOSObject{".id": fmt.Sprintf("*%X", 100+f.writes), "name": strings.TrimPrefix(p, "/"), "type": "directory"})
	f.failReads = f.failAfterWrite
	return f.writeErr
}
func (f *directoryFixture) RenameDirectory(_ context.Context, id, target string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writes++
	old := ""
	for _, row := range f.rows {
		if row[".id"] == id {
			old = "/" + row["name"]
		}
	}
	for _, row := range f.rows {
		p := "/" + row["name"]
		if withinDirectory(p, old) {
			row["name"] = strings.TrimPrefix(target+strings.TrimPrefix(p, old), "/")
			row[".id"] = "*AB" + strings.TrimPrefix(row[".id"], "*")
		}
	}
	return f.writeErr
}
func (f *directoryFixture) RemoveDirectory(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writes++
	old := ""
	for _, row := range f.rows {
		if row[".id"] == id {
			old = "/" + row["name"]
		}
	}
	rows := []routeros.RouterOSObject{}
	for _, row := range f.rows {
		if !withinDirectory("/"+row["name"], old) {
			rows = append(rows, row)
		}
	}
	f.rows = rows
	return f.writeErr
}
func directoryTestService() (*Service, *directoryFixture, config.DeviceConfig, []Disk) {
	f := &directoryFixture{rows: []routeros.RouterOSObject{{".id": "*1", "name": "sata1", "type": "disk"}, {".id": "*2", "name": "sata1/docker", "type": "directory"}}}
	s := &Service{ReaderFor: func(config.DeviceConfig) Reader { return f }, DirectoryWriterFor: func(config.DeviceConfig) DirectoryWriter { return f }}
	return s, f, config.DeviceConfig{ID: "test-device"}, []Disk{{Name: "sata1", Writable: true}}
}
func requireDirectoryCode(t *testing.T, err error, code string) {
	t.Helper()
	var e *DirectoryError
	if !errors.As(err, &e) || e.Code != code {
		t.Fatalf("want %s, got %v", code, err)
	}
}
func TestDirectoryMutationUnicodeRenameIDChangeRecursiveDeleteAndReplay(t *testing.T) {
	s, f, d, disks := directoryTestService()
	ctx := context.Background()
	req := DirectoryRequest{Action: "mkdir", RequestID: "mkdir-unique", Parent: "/sata1/docker", Name: "配置 文件夹", ExpectedID: "*2"}
	created, err := s.MutateDirectory(ctx, d, disks, nil, req)
	if err != nil || created.Path != "/sata1/docker/配置 文件夹" {
		t.Fatal(created, err)
	}
	s.MutateDirectory(ctx, d, disks, nil, req)
	if f.writes != 1 {
		t.Fatal("replayed write")
	}
	changed := req
	changed.Name = "other"
	_, err = s.MutateDirectory(ctx, d, disks, nil, changed)
	requireDirectoryCode(t, err, "request_id_conflict")
	f.rows = append(f.rows, routeros.RouterOSObject{".id": "*9", "name": strings.TrimPrefix(created.Path, "/") + "/config.yaml", "type": ".yaml file", "size": "128"})
	renamed, err := s.MutateDirectory(ctx, d, disks, nil, DirectoryRequest{Action: "rename", RequestID: "rename-unique", Path: created.Path, Name: "改名目录", ExpectedID: "*65"})
	if err != nil || renamed.State != "succeeded" {
		t.Fatal(renamed, err)
	}
	row := directoryRow(f.rows, renamed.Path)
	if row[".id"] == "*65" || directoryRow(f.rows, renamed.Path+"/config.yaml") == nil {
		t.Fatal("rename must tolerate changed opaque IDs and retain descendants")
	}
	_, err = s.MutateDirectory(ctx, d, disks, nil, DirectoryRequest{Action: "delete", RequestID: "stale-id-delete", Path: renamed.Path, ExpectedID: "*65", ConfirmPath: renamed.Path})
	requireDirectoryCode(t, err, "directory_changed")
	_, err = s.MutateDirectory(ctx, d, disks, nil, DirectoryRequest{Action: "delete", RequestID: "delete-confirmed", Path: renamed.Path, ExpectedID: row[".id"], ConfirmPath: renamed.Path})
	if err != nil || directoryRow(f.rows, renamed.Path+"/config.yaml") != nil || len(f.rows) != 2 {
		t.Fatal(err, f.rows)
	}
}
func TestDirectoryProtectsRootDisksFilesTraversalAndFreshContainerPaths(t *testing.T) {
	s, f, d, disks := directoryTestService()
	ctx := context.Background()
	f.rows = append(f.rows, routeros.RouterOSObject{".id": "*3", "name": "sata1/docker/rootdir", "type": "directory"}, routeros.RouterOSObject{".id": "*4", "name": "sata1/docker/config", "type": "directory"}, routeros.RouterOSObject{".id": "*5", "name": "sata1/plain.txt", "type": ".txt file"})
	f.refs = []routeros.RouterOSObject{{"root-dir": "sata1/docker/rootdir"}}
	f.mounts = []routeros.RouterOSObject{{"src": "sata1/docker/config"}}
	for i, p := range []string{"/", "/sata1", "/sata1/docker", "/sata1/docker/rootdir", "/sata1/docker/config"} {
		_, err := s.MutateDirectory(ctx, d, disks, nil, DirectoryRequest{Action: "delete", RequestID: fmt.Sprintf("protected-%d", i), Path: p, ExpectedID: "*2", ConfirmPath: p})
		if p != "/" && p != "/sata1" && p != "/sata1/docker" {
			requireDirectoryCode(t, err, "directory_changed")
		} else {
			requireDirectoryCode(t, err, "directory_protected")
		}
	}
	_, err := s.MutateDirectory(ctx, d, disks, nil, DirectoryRequest{Action: "rename", RequestID: "protected-config", Path: "/sata1/docker/config", ExpectedID: "*4", Name: "data"})
	requireDirectoryCode(t, err, "directory_protected")
	_, err = s.MutateDirectory(ctx, d, disks, nil, DirectoryRequest{Action: "delete", RequestID: "ordinary-file", Path: "/sata1/plain.txt", ExpectedID: "*5", ConfirmPath: "/sata1/plain.txt"})
	requireDirectoryCode(t, err, "directory_not_found")
	for i, name := range []string{"", "..", ".", "a/b", "a\\b", "bad\tname", "\x00"} {
		_, err = s.MutateDirectory(ctx, d, disks, nil, DirectoryRequest{Action: "mkdir", RequestID: fmt.Sprintf("invalid-name-%d", i), Parent: "/sata1/docker", Name: name})
		requireDirectoryCode(t, err, "invalid_directory_name")
	}
	_, err = s.MutateDirectory(ctx, d, disks, nil, DirectoryRequest{Action: "mkdir", RequestID: "bad-path-request", Parent: "/sata1/../escape", Name: "folder"})
	requireDirectoryCode(t, err, "invalid_path")
	if f.writes != 0 {
		t.Fatal("invalid request mutated device")
	}
	listing, err := s.Directories(ctx, d, disks, "/sata1/docker")
	if err != nil || listing.Entries[0].Protected == "" || listing.Entries[1].Protected == "" {
		t.Fatal(listing, err)
	}
}
func TestDirectoryUnknownResultRetainsSharedGateAndOnlyReadsOnRecovery(t *testing.T) {
	s, f, d, disks := directoryTestService()
	ctx := context.Background()
	gate := routeros.NewDeviceWriteGate()
	f.writeErr = &routeros.MutationOutcomeUnknownError{Method: "POST", Path: "/rest/file/add"}
	f.failAfterWrite = true
	req := DirectoryRequest{Action: "mkdir", RequestID: "uncertain-directory", Parent: "/sata1/docker", Name: "created"}
	_, err := s.MutateDirectory(ctx, d, disks, gate, req)
	requireDirectoryCode(t, err, "directory_outcome_unknown")
	if release, ok := gate.TryAcquire(d.ID); ok {
		release()
		t.Fatal("unknown outcome released device gate")
	}
	other := req
	other.RequestID = "other-directory"
	_, err = s.MutateDirectory(ctx, d, disks, gate, other)
	requireDirectoryCode(t, err, "directory_busy")
	f.failReads = false
	listing, err := s.Directories(ctx, d, disks, "/sata1/docker")
	if err != nil || listing.Pending == nil || listing.Pending.RequestID != req.RequestID {
		t.Fatal(listing, err)
	}
	result, err := s.MutateDirectory(ctx, d, disks, gate, DirectoryRequest{Action: "recover", RequestID: req.RequestID})
	if err != nil || result.State != "succeeded" || f.writes != 1 {
		t.Fatal(result, err, f.writes)
	}
	if release, ok := gate.TryAcquire(d.ID); !ok {
		t.Fatal("confirmed result retained gate")
	} else {
		release()
	}
}
func TestDirectoryPermissionFailureAndMissingDeleteConfirmation(t *testing.T) {
	s, f, d, disks := directoryTestService()
	ctx := context.Background()
	_, err := s.MutateDirectory(ctx, d, disks, nil, DirectoryRequest{Action: "delete", RequestID: "no-confirmation", Path: "/sata1/docker", ExpectedID: "*2"})
	requireDirectoryCode(t, err, "directory_confirmation_required")
	f.writeErr = &routeros.HTTPError{StatusCode: 403}
	_, err = s.MutateDirectory(ctx, d, disks, nil, DirectoryRequest{Action: "mkdir", RequestID: "permission-denied", Parent: "/sata1/docker", Name: "new"})
	requireDirectoryCode(t, err, "directory_permission_denied")
}

func TestDirectoryRejectsOversizedFinalPathBeforeWriting(t *testing.T) {
	s, f, d, disks := directoryTestService()
	parent := "sata1/" + strings.Repeat("x", 1017)
	f.rows = append(f.rows, routeros.RouterOSObject{".id": "*FA", "name": parent, "type": "directory"})
	_, err := s.MutateDirectory(context.Background(), d, disks, nil, DirectoryRequest{Action: "mkdir", RequestID: "oversized-final-path", Parent: parent, Name: "folder"})
	requireDirectoryCode(t, err, "invalid_path")
	if f.writes != 0 {
		t.Fatal("oversized path reached mutation client")
	}
}

type blockingDirectoryWriter struct {
	*directoryFixture
	entered, proceed chan struct{}
}

func (f blockingDirectoryWriter) CreateDirectory(ctx context.Context, p string) error {
	close(f.entered)
	select {
	case <-f.proceed:
		return f.directoryFixture.CreateDirectory(ctx, p)
	case <-ctx.Done():
		return ctx.Err()
	}
}
func TestDirectoryInFlightRequestBlocksDuplicateAndExposesRecoveryAfterPickerCloses(t *testing.T) {
	s, f, d, disks := directoryTestService()
	w := blockingDirectoryWriter{f, make(chan struct{}), make(chan struct{})}
	s.DirectoryWriterFor = func(config.DeviceConfig) DirectoryWriter { return w }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := DirectoryRequest{Action: "mkdir", RequestID: "inflight-directory", Parent: "/sata1/docker", Name: "pending"}
	done := make(chan error, 1)
	go func() { _, err := s.MutateDirectory(ctx, d, disks, nil, req); done <- err }()
	<-w.entered
	_, err := s.MutateDirectory(context.Background(), d, disks, nil, req)
	requireDirectoryCode(t, err, "directory_busy")
	listing, err := s.Directories(context.Background(), d, disks, "/sata1/docker")
	if err != nil || listing.Pending == nil || listing.Pending.State != "pending" {
		t.Fatal(listing, err)
	}
	cancel()
	close(w.proceed)
	if err := <-done; err != nil {
		t.Fatal("closing the picker canceled result reconciliation", err)
	}
	if f.writes != 1 {
		t.Fatal("duplicate write", f.writes)
	}
}
