package containers

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"rosboard/internal/config"
	"rosboard/internal/routeros"
)

type nativeFake struct {
	mu      sync.Mutex
	rows    map[routeros.ContainerMenu][]routeros.RouterOSObject
	writes  int
	unknown bool
	block   chan struct{}
}

func (f *nativeFake) ContainerRead(_ context.Context, m routeros.ContainerMenu) ([]routeros.RouterOSObject, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []routeros.RouterOSObject{}
	for _, row := range f.rows[m] {
		copy := routeros.RouterOSObject{}
		for k, v := range row {
			copy[k] = v
		}
		out = append(out, copy)
	}
	return out, nil
}
func (f *nativeFake) ContainerCommand(ctx context.Context, m routeros.ContainerMenu, c string, fields routeros.RouterOSFields) error {
	if f.block != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-f.block:
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writes++
	if c == "add" {
		row := routeros.RouterOSObject{".id": fmt.Sprintf("*%X", f.writes)}
		for k, v := range fields {
			row[k] = v.(string)
			if k == "logging" || k == "start-on-boot" {
				if isTrue(row[k]) {
					row[k] = "true"
				} else {
					row[k] = "false"
				}
			}
		}
		if m == routeros.ContainerList {
			row["stopped"] = "true"
			if row["root-dir"] != "" {
				f.rows[routeros.ContainerFiles] = append(f.rows[routeros.ContainerFiles], routeros.RouterOSObject{".id": fmt.Sprintf("**root%d", f.writes), "name": strings.TrimPrefix(row["root-dir"], "/"), "type": "container store"})
			}
		}
		f.rows[m] = append(f.rows[m], row)
	} else {
		for i, row := range f.rows[m] {
			if row[".id"] != fields["numbers"] {
				continue
			}
			switch c {
			case "remove":
				if m == routeros.ContainerList {
					root := strings.TrimPrefix(row["root-dir"], "/")
					kept := []routeros.RouterOSObject{}
					for _, file := range f.rows[routeros.ContainerFiles] {
						if file["name"] != root && !strings.HasPrefix(file["name"], root+"/") {
							kept = append(kept, file)
						}
					}
					f.rows[routeros.ContainerFiles] = kept
				}
				f.rows[m] = append(f.rows[m][:i], f.rows[m][i+1:]...)
			case "set":
				for k, v := range fields {
					if k != "numbers" {
						row[k] = v.(string)
					}
				}
			case "start":
				delete(row, "stopped")
				row["running"] = "true"
			case "stop":
				delete(row, "running")
				row["stopped"] = "true"
			}
			break
		}
	}
	if f.unknown {
		return &routeros.MutationOutcomeUnknownError{Method: "POST", Path: "/rest/container/add"}
	}
	return nil
}
func nativeFixture(t *testing.T) (*Service, config.DeviceConfig, *nativeFake) {
	t.Helper()
	d := config.DeviceConfig{ID: "lab", Enabled: true, ContainerWrites: true}
	f := &nativeFake{rows: map[routeros.ContainerMenu][]routeros.RouterOSObject{
		routeros.ContainerBridge: {{"name": "br-containers"}}, routeros.ContainerDisk: {{"name": "sata1", "fs": "ext4", "free": "1073741824"}}, routeros.ContainerResource: {{"architecture-name": "x86_64", "version": "7.23.7"}},
	}}
	s := NewServiceWithState(t.TempDir())
	s.ReaderFor = func(config.DeviceConfig) Reader { return f }
	s.NativeWriterFor = func(config.DeviceConfig) NativeWriter { return f }
	return s, d, f
}
func waitNative(t *testing.T, s *Service, d config.DeviceConfig, id string) Job {
	t.Helper()
	for i := 0; i < 100; i++ {
		j, err := s.NativeJob(d, id)
		if err != nil {
			t.Fatal(err)
		}
		if j.State != "queued" && j.State != "running" {
			return j
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("job timed out")
	return Job{}
}
func nativeSubmit(t *testing.T, s *Service, d config.DeviceConfig, r ActionRequest) Job {
	t.Helper()
	j, err := s.Submit(context.Background(), d, nil, r)
	if err != nil {
		t.Fatal(err)
	}
	j = waitNative(t, s, d, j.ID)
	if j.State != "succeeded" {
		t.Fatalf("job failed: %+v", j)
	}
	return j
}
func TestNativeLifecycleReplayAndDurability(t *testing.T) {
	s, d, f := nativeFixture(t)
	draft := draftFixture()
	draft.Name = "test-native"
	draft.Env = []Environment{{Key: "VALUE", Value: " ' $() ; 中文\n "}}
	draft.Mounts = []Mount{{Source: "/sata1/data", Target: "/data", ReadOnly: true}}
	req := ActionRequest{Action: "create", RequestID: "create-one", Draft: draft}
	j := nativeSubmit(t, s, d, req)
	repeat, err := s.Submit(context.Background(), d, nil, req)
	if err != nil || repeat.ID != j.ID {
		t.Fatal("replay did not return original job", err)
	}
	req.Draft.Name = "another"
	if _, err = s.Submit(context.Background(), d, nil, req); err == nil {
		t.Fatal("request payload reuse accepted")
	}
	snap, err := s.fresh(context.Background(), d)
	if err != nil || len(snap.Items) != 1 || snap.Items[0].Ownership != "managed" || snap.Items[0].Status != "running" || snap.Items[0].Config.Env[0].Value != draft.Env[0].Value || !snap.Items[0].Config.Mounts[0].ReadOnly {
		t.Fatalf("snapshot: %+v %v", snap, err)
	}
	for _, a := range []string{"stop", "start", "restart"} {
		nativeSubmit(t, s, d, ActionRequest{Action: a, TargetID: j.TargetID, RequestID: a})
	}
	second := NewServiceWithState(s.NativeStateDir)
	second.ReaderFor = s.ReaderFor
	second.NativeWriterFor = s.NativeWriterFor
	replay, err := second.NativeJob(d, j.ID)
	if err != nil || replay.State != "succeeded" {
		t.Fatal("journal did not survive restart", err)
	}
	if _, err = second.NativeJob(config.DeviceConfig{ID: "other", ContainerWrites: true}, j.ID); err == nil {
		t.Fatal("job crossed device scope")
	}
	nativeSubmit(t, s, d, ActionRequest{Action: "delete", TargetID: j.TargetID, RequestID: "delete-one"})
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, menu := range []routeros.ContainerMenu{routeros.ContainerList, routeros.ContainerVETH, routeros.ContainerBridgePort, routeros.ContainerEnvs, routeros.ContainerMounts} {
		if len(f.rows[menu]) != 0 {
			t.Fatalf("owned objects leaked: %s", menu)
		}
	}
}
func TestNativeUnknownWriteReadbackNeverRetries(t *testing.T) {
	s, d, f := nativeFixture(t)
	f.unknown = true
	j := nativeSubmit(t, s, d, ActionRequest{Action: "create", RequestID: "unknown-create", Draft: draftFixture()})
	f.mu.Lock()
	writes := f.writes
	f.mu.Unlock()
	if writes != 4 {
		t.Fatalf("write was retried: %d", writes)
	}
	if j.TargetID == "" {
		t.Fatal("missing confirmed target")
	}
}
func TestNativeBusyAndAdoptionPreserveSharedObjects(t *testing.T) {
	s, d, f := nativeFixture(t)
	f.block = make(chan struct{})
	j, err := s.Submit(context.Background(), d, nil, ActionRequest{Action: "create", RequestID: "first", Draft: draftFixture()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Submit(context.Background(), d, nil, ActionRequest{Action: "create", RequestID: "second", Draft: draftFixture()}); err == nil {
		t.Fatal("parallel writes accepted")
	}
	close(f.block)
	if waitNative(t, s, d, j.ID).State != "succeeded" {
		t.Fatal("first failed")
	}
	// An unmanaged existing container shares the newly created network.
	f.mu.Lock()
	f.rows[routeros.ContainerList] = append(f.rows[routeros.ContainerList], routeros.RouterOSObject{".id": "*F0", "name": "existing", "interface": "new-veth", "root-dir": "/sata1/existing", "stopped": "true", "remote-image": "example:latest", "comment": "operator notes"})
	f.mu.Unlock()
	nativeSubmit(t, s, d, ActionRequest{Action: "adopt", TargetID: "*F0", RequestID: "adopt-existing"})
	nativeSubmit(t, s, d, ActionRequest{Action: "delete", TargetID: "*F0", RequestID: "delete-adopted"})
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.rows[routeros.ContainerVETH]) != 1 || len(f.rows[routeros.ContainerBridgePort]) != 1 {
		t.Fatal("adoption removed existing network")
	}
}
func TestNativeSetRenameAndNormalizedResources(t *testing.T) {
	step := nativeStep{Menu: routeros.ContainerList, Command: "set", Match: map[string]string{".id": "*1", "name": "old"}, Fields: map[string]string{"name": "new", "memory-max": "128M", "memory-high": ""}}
	ok, _ := desired(step, []routeros.RouterOSObject{{".id": "*1", "name": "new", "memory-max": "134217728", "memory-high": "unlimited"}})
	if !ok {
		t.Fatal("normalized readback not confirmed")
	}
}

func (f *nativeFake) RemoveDirectory(ctx context.Context, id string) error {
	return f.ContainerCommand(ctx, routeros.ContainerFiles, "remove", routeros.RouterOSFields{"numbers": id})
}

func (f *nativeFake) RenameDirectory(ctx context.Context, id, name string) error {
	return f.ContainerCommand(ctx, routeros.ContainerFiles, "set", routeros.RouterOSFields{"numbers": id, "name": name})
}
func TestNativeRootRetainedOnDeleteAndUpdate(t *testing.T) {
	s, d, f := nativeFixture(t)
	draft := draftFixture()
	draft.Name = "retain"
	created := nativeSubmit(t, s, d, ActionRequest{Action: "create", RequestID: "retain-create", Draft: draft})
	original, _ := s.fresh(context.Background(), d)
	oldRoot := original.Items[0].Config.RootDir
	updated := nativeSubmit(t, s, d, ActionRequest{Action: "update", TargetID: created.TargetID, RequestID: "retain-update"})
	if len(updated.Retained) == 0 || updated.Retained[0] == oldRoot {
		t.Fatalf("old runtime was not moved: %+v", updated)
	}
	deleted := nativeSubmit(t, s, d, ActionRequest{Action: "delete", TargetID: updated.TargetID, RequestID: "retain-delete"})
	files, _ := f.ContainerRead(context.Background(), routeros.ContainerFiles)
	count := 0
	for _, file := range files {
		if strings.Contains(file["name"], "-retained-") {
			count++
		}
	}
	if count != 2 || len(deleted.Retained) == 0 {
		t.Fatalf("root filesystems lost: %+v %+v", files, deleted)
	}
}

func TestNativeRetainRootReadbackUsesPathAfterOpaqueIDChanges(t *testing.T) {
	step := nativeStep{Command: "retain-root", Match: map[string]string{".id": "**old", "name": "sata1/app/rootfs"}, Fields: map[string]string{"name": "sata1/app/rootfs-retained-unique"}}
	if ok, _ := desired(step, []routeros.RouterOSObject{{".id": "**new", "name": step.Fields["name"], "type": "container store"}}); !ok {
		t.Fatal("confirmed rename lost after ID changed")
	}
	if ok, _ := desired(step, []routeros.RouterOSObject{{"name": step.Fields["name"], "type": "file"}}); ok {
		t.Fatal("ordinary file was accepted as retained filesystem")
	}
	if ok, _ := desired(step, []routeros.RouterOSObject{{"name": step.Fields["name"], "type": "directory"}, {"name": step.Match["name"], "type": "directory"}}); ok {
		t.Fatal("rename confirmed while source still exists")
	}
}
func TestNativeSharedRootStopsDeletion(t *testing.T) {
	s, d, f := nativeFixture(t)
	created := nativeSubmit(t, s, d, ActionRequest{Action: "create", RequestID: "shared-root-create", Draft: draftFixture()})
	f.mu.Lock()
	own := f.rows[routeros.ContainerList][0]
	f.rows[routeros.ContainerList] = append(f.rows[routeros.ContainerList], routeros.RouterOSObject{".id": "*FF", "name": "other", "root-dir": own["root-dir"], "stopped": "true"})
	f.mu.Unlock()
	j, err := s.Submit(context.Background(), d, nil, ActionRequest{Action: "delete", TargetID: created.TargetID, RequestID: "shared-root-delete"})
	if err != nil {
		t.Fatal(err)
	}
	if job := waitNative(t, s, d, j.ID); job.State != "failed" {
		t.Fatalf("shared root deletion allowed: %+v", job)
	}
	rows, _ := f.ContainerRead(context.Background(), routeros.ContainerList)
	if len(rows) != 2 {
		t.Fatal("shared container was deleted")
	}
}

func TestNativePendingRootRenameSurvivesRestartWithoutReplay(t *testing.T) {
	s, d, f := nativeFixture(t)
	created := nativeSubmit(t, s, d, ActionRequest{Action: "create", RequestID: "restart-root-create", Draft: draftFixture()})
	snap, _ := s.fresh(context.Background(), d)
	steps, retained, err := s.plan(context.Background(), d, ActionRequest{Action: "delete", TargetID: created.TargetID}, snap, "12345678-test")
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a crash after rename was applied, before its journal confirmation.
	for i := range steps {
		if steps[i].Command == "stop" {
			rows, _ := f.ContainerRead(context.Background(), steps[i].Menu)
			targets := findRows(rows, steps[i].Match)
			if err := f.ContainerCommand(context.Background(), steps[i].Menu, "stop", routeros.RouterOSFields{"numbers": targets[0][".id"]}); err != nil {
				t.Fatal(err)
			}
			steps[i].Done = true
		}
		if steps[i].Command == "retain-root" {
			if err := f.RenameDirectory(context.Background(), steps[i].Match[".id"], steps[i].Fields["name"]); err != nil {
				t.Fatal(err)
			}
		}
	}
	n, _ := s.nativeDevice(d)
	n.mu.Lock()
	n.state.Jobs["restart-delete"] = &nativeRecord{Job: Job{ID: "restart-delete", DeviceID: d.ID, TargetID: created.TargetID, Action: "delete", State: "running", Retained: retained}, Request: ActionRequest{Action: "delete", TargetID: created.TargetID, RequestID: "restart-delete"}, Steps: steps, Pending: true}
	n.state.Latest = "restart-delete"
	if err := n.save(); err != nil {
		t.Fatal(err)
	}
	n.mu.Unlock()
	f.mu.Lock()
	before := f.writes
	f.mu.Unlock()
	restored := NewServiceWithState(s.NativeStateDir)
	restored.ReaderFor = s.ReaderFor
	restored.NativeWriterFor = s.NativeWriterFor
	if err := restored.RestoreNative(d); err != nil {
		t.Fatal(err)
	}
	if release, ok := restored.NativeGate.TryAcquire(d.ID); ok {
		release()
		t.Fatal("restart left other device writes unlocked")
	}
	if _, err := restored.RecoverNative(d, nil, "restart-delete"); err != nil {
		t.Fatal(err)
	}
	if job := waitNative(t, restored, d, "restart-delete"); job.State != "succeeded" {
		t.Fatalf("recovery failed: %+v", job)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.writes-before != 3 {
		t.Fatalf("rename or stop replayed: %d writes", f.writes-before)
	}
}

func TestNativeExistingEmptyRootAndNonemptyProtection(t *testing.T) {
	for _, nonempty := range []bool{false, true} {
		t.Run(fmt.Sprint(nonempty), func(t *testing.T) {
			s, d, f := nativeFixture(t)
			draft := draftFixture()
			draft.RootDir = "/sata1/app/rootfs"
			f.rows[routeros.ContainerFiles] = []routeros.RouterOSObject{{".id": "**empty", "name": "sata1/app/rootfs", "type": "directory"}}
			if nonempty {
				f.rows[routeros.ContainerFiles] = append(f.rows[routeros.ContainerFiles], routeros.RouterOSObject{".id": "**child", "name": "sata1/app/rootfs/existing.txt", "type": ".txt file"})
			}
			j, err := s.Submit(context.Background(), d, nil, ActionRequest{Action: "create", RequestID: "existing-root", Draft: draft})
			if nonempty {
				if err == nil || f.writes != 0 {
					t.Fatal("nonempty root accepted or mutated")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if job := waitNative(t, s, d, j.ID); job.State != "succeeded" {
				t.Fatalf("empty-root preparation failed: %+v", job)
			}
			rows, _ := f.ContainerRead(context.Background(), routeros.ContainerFiles)
			if len(rows) != 1 || rows[0]["type"] != "container store" {
				t.Fatal("empty directory was not replaced by imported rootfs")
			}
		})
	}
}
