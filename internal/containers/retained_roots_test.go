package containers

import (
	"context"
	"fmt"
	"maps"
	"strings"
	"testing"

	"rosboard/internal/config"
	"rosboard/internal/routeros"
)

type renamedRootWriter struct{ *nativeFake }

func (w renamedRootWriter) RenameDirectory(ctx context.Context, id, name string) error {
	if err := w.nativeFake.RenameDirectory(ctx, id, name); err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, row := range w.rows[routeros.ContainerFiles] {
		if row[".id"] == id {
			row[".id"] = "**renamed-" + name
		}
	}
	return nil
}

func retainedFixture(t *testing.T, count int) (*Service, config.DeviceConfig, *nativeFake, Job, []nativeStep) {
	t.Helper()
	s, d, f := nativeFixture(t)
	s.NativeWriterFor = func(config.DeviceConfig) NativeWriter { return renamedRootWriter{f} }
	draft := draftFixture()
	draft.Mounts = []Mount{{Source: "/sata1/independent/config", Target: "/config"}}
	j := nativeSubmit(t, s, d, ActionRequest{Action: "create", RequestID: "history-create", Draft: draft})
	for i := 0; i < count; i++ {
		j = nativeSubmit(t, s, d, ActionRequest{Action: "update", TargetID: j.TargetID, RequestID: fmt.Sprintf("history-update-%d", i)})
	}
	updates, err := s.retainedUpdates(d)
	if err != nil {
		t.Fatal(err)
	}
	var roots []nativeStep
	for _, update := range updates {
		if update.root != nil {
			roots = append(roots, *update.root)
		}
	}
	if len(roots) != count {
		t.Fatalf("history records: %d, want %d", len(roots), count)
	}
	for _, root := range roots {
		if root.ResultID == "" || root.ResultID == root.Match[".id"] {
			t.Fatal("post-rename ID was not persisted")
		}
	}
	return s, d, f, j, roots
}

func TestDeleteCleansMultipleUpdateRootsAfterRestart(t *testing.T) {
	s, d, f, updated, roots := retainedFixture(t, 2)
	// A similarly named directory without an exact update record stays untouched.
	foreign := roots[0].Fields["name"] + "-foreign"
	f.mu.Lock()
	f.rows[routeros.ContainerFiles] = append(f.rows[routeros.ContainerFiles],
		routeros.RouterOSObject{".id": "**foreign", "name": foreign, "type": "directory"},
		routeros.RouterOSObject{".id": "**mount-data", "name": "sata1/independent/config", "type": "directory"})
	// Names can change without changing the container's identity/update chain.
	for _, row := range f.rows[routeros.ContainerList] {
		if row[".id"] == updated.TargetID {
			row["name"] = "renamed-service"
		}
	}
	f.mu.Unlock()
	restored := NewServiceWithState(s.NativeStateDir)
	restored.ReaderFor, restored.NativeWriterFor = s.ReaderFor, s.NativeWriterFor
	if err := restored.RestoreNative(d); err != nil {
		t.Fatal(err)
	}
	deleted := nativeSubmit(t, restored, d, ActionRequest{Action: "delete", TargetID: updated.TargetID, RequestID: "history-delete"})
	files, _ := f.ContainerRead(context.Background(), routeros.ContainerFiles)
	for _, root := range roots {
		if len(findRows(files, map[string]string{"name": root.Fields["name"]})) != 0 {
			t.Fatal("historical root remains")
		}
	}
	if len(findRows(files, map[string]string{".id": "**foreign"})) != 1 || len(findRows(files, map[string]string{".id": "**mount-data"})) != 1 ||
		!contains(deleted.Retained, "sata1/independent/config") {
		t.Fatalf("foreign directory or independent mount data was removed: files=%+v retained=%v", files, deleted.Retained)
	}
}

func TestDeleteRejectsUnsafeHistoricalRootsBeforeAnyWrite(t *testing.T) {
	for _, scenario := range []string{"replacement", "mount", "shared", "legacy-changed-id", "recycled-container-id", "foreign-owner", "missing-history-guard"} {
		t.Run(scenario, func(t *testing.T) {
			s, d, f, updated, roots := retainedFixture(t, 1)
			root := roots[0]
			switch scenario {
			case "replacement":
				f.mu.Lock()
				for _, row := range f.rows[routeros.ContainerFiles] {
					if row["name"] == root.Fields["name"] {
						row[".id"] = "**replacement"
					}
				}
				f.mu.Unlock()
			case "mount":
				f.mu.Lock()
				f.rows[routeros.ContainerMounts] = append(f.rows[routeros.ContainerMounts], routeros.RouterOSObject{".id": "*FA", "src": root.Fields["name"] + "/data"})
				f.mu.Unlock()
			case "shared":
				f.mu.Lock()
				f.rows[routeros.ContainerList] = append(f.rows[routeros.ContainerList], routeros.RouterOSObject{".id": "*FA", "root-dir": root.Fields["name"]})
				f.mu.Unlock()
			default:
				n, _ := s.nativeDevice(d)
				n.mu.Lock()
				for _, record := range n.state.Jobs {
					if record.Request.Action != "update" {
						continue
					}
					for i := range record.Steps {
						step := &record.Steps[i]
						if step.Command == "retain-root" {
							if scenario == "legacy-changed-id" {
								step.ResultID = ""
							} else if scenario == "missing-history-guard" {
								step.Guard = nil
							}
						}
					}
				}
				n.mu.Unlock()
				if scenario == "recycled-container-id" || scenario == "foreign-owner" {
					// The same ROS ID must not claim another output root/owner.
					f.mu.Lock()
					for _, row := range f.rows[routeros.ContainerList] {
						if row[".id"] == updated.TargetID {
							if scenario == "recycled-container-id" {
								row["root-dir"] = "/sata1/other/rootfs"
							} else {
								row["comment"] = managedComment(d, "another-owner")
							}
						}
					}
					f.mu.Unlock()
				}
			}
			f.mu.Lock()
			before := f.writes
			f.mu.Unlock()
			snap, err := s.fresh(context.Background(), d)
			if err != nil {
				t.Fatal(err)
			}
			steps, _, err := s.plan(context.Background(), d, ActionRequest{Action: "delete", TargetID: updated.TargetID}, snap, "delete-history")
			if scenario == "recycled-container-id" || scenario == "foreign-owner" {
				if err != nil {
					t.Fatal(err)
				}
				for _, step := range steps {
					if step.Match["name"] == root.Fields["name"] {
						t.Fatal("unrelated historical root was claimed")
					}
				}
			} else if err == nil {
				t.Fatal("unsafe history admitted")
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.writes != before {
				t.Fatal("history validation wrote to RouterOS")
			}
		})
	}
}

func TestDeleteRechecksHistoryReferencesAfterPlanning(t *testing.T) {
	s, d, f, updated, roots := retainedFixture(t, 1)
	root := roots[0]
	s.NativeWriterFor = func(config.DeviceConfig) NativeWriter {
		return rootReplacementWriter{NativeWriter: f, afterRemove: func() {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.rows[routeros.ContainerMounts] = append(f.rows[routeros.ContainerMounts], routeros.RouterOSObject{".id": "*late", "src": root.Fields["name"]})
		}}
	}
	j, err := s.Submit(context.Background(), d, nil, ActionRequest{Action: "delete", TargetID: updated.TargetID, RequestID: "late-mount-delete"})
	if err != nil {
		t.Fatal(err)
	}
	j = waitNative(t, s, d, j.ID)
	if j.State != "failed" || !strings.Contains(j.Error, "挂载") {
		t.Fatalf("late reference not rejected: %+v", j)
	}
	files, _ := f.ContainerRead(context.Background(), routeros.ContainerFiles)
	if len(findRows(files, map[string]string{"name": root.Fields["name"]})) != 1 {
		t.Fatal("newly shared history removed")
	}
}

func TestPendingHistoricalRemovalRecoversWithoutReplayingDelete(t *testing.T) {
	s, d, f, updated, roots := retainedFixture(t, 1)
	snap, _ := s.fresh(context.Background(), d)
	steps, retained, err := s.plan(context.Background(), d, ActionRequest{Action: "delete", TargetID: updated.TargetID}, snap, "pending-history")
	if err != nil {
		t.Fatal(err)
	}
	remaining := 0
	for i := range steps {
		step := &steps[i]
		if step.Command == "remove-root" && step.Match["name"] == roots[0].Fields["name"] {
			if err := f.RemoveDirectory(context.Background(), step.Match[".id"]); err != nil {
				t.Fatal(err)
			}
			remaining = len(steps) - i - 1
			break // Simulate lost acknowledgement before Done is saved.
		}
		if step.Menu == routeros.ContainerList {
			if err := f.ContainerCommand(context.Background(), step.Menu, step.Command, routeros.RouterOSFields{"numbers": step.Match[".id"]}); err != nil {
				t.Fatal(err)
			}
		}
		step.Done = true
	}
	n, _ := s.nativeDevice(d)
	n.mu.Lock()
	n.state.Jobs["pending-history"] = &nativeRecord{Job: Job{ID: "pending-history", DeviceID: d.ID, Action: "delete", TargetID: updated.TargetID, State: "unknown", Retained: retained}, Request: ActionRequest{Action: "delete", TargetID: updated.TargetID}, Steps: steps, Pending: true}
	if err := n.save(); err != nil {
		t.Fatal(err)
	}
	n.mu.Unlock()
	f.mu.Lock()
	before := f.writes
	f.mu.Unlock()
	restored := NewServiceWithState(s.NativeStateDir)
	restored.ReaderFor, restored.NativeWriterFor = s.ReaderFor, s.NativeWriterFor
	if err := restored.RestoreNative(d); err != nil {
		t.Fatal(err)
	}
	if _, err := restored.RecoverNative(d, nil, "pending-history"); err != nil {
		t.Fatal(err)
	}
	if j := waitNative(t, restored, d, "pending-history"); j.State != "succeeded" {
		t.Fatalf("recovery failed: %+v", j)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	// Only the later owned auxiliary objects remain to clean up.
	if f.writes-before != remaining {
		t.Fatalf("root removal replayed: %d writes", f.writes-before)
	}
}

func TestUnrelatedUpdateHistoryDoesNotBlockDelete(t *testing.T) {
	s, d, _, updated, _ := retainedFixture(t, 1)
	n, _ := s.nativeDevice(d)
	n.mu.Lock()
	for _, record := range n.state.Jobs {
		if record.Request.Action == "update" {
			copy := *record
			copy.Steps = append([]nativeStep(nil), record.Steps...)
			copy.Job.TargetID = "*unrelated"
			for i := range copy.Steps {
				copy.Steps[i].Guard = maps.Clone(copy.Steps[i].Guard)
				if copy.Steps[i].Command == "retain-root" {
					copy.Steps[i].Guard = nil
				}
			}
			n.state.Jobs["unrelated"] = &copy
			break
		}
	}
	n.mu.Unlock()
	nativeSubmit(t, s, d, ActionRequest{Action: "delete", TargetID: updated.TargetID, RequestID: "unrelated-delete"})
}

func TestLegacyHistoryWithUnchangedIDAndMissingRoots(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(fmt.Sprint(missing), func(t *testing.T) {
			s, d, f := nativeFixture(t)
			created := nativeSubmit(t, s, d, ActionRequest{Action: "create", RequestID: "legacy-create", Draft: draftFixture()})
			updated := nativeSubmit(t, s, d, ActionRequest{Action: "update", TargetID: created.TargetID, RequestID: "legacy-update"})
			n, _ := s.nativeDevice(d)
			n.mu.Lock()
			root := ""
			for i := range n.state.Jobs[updated.ID].Steps {
				step := &n.state.Jobs[updated.ID].Steps[i]
				if step.Command == "retain-root" {
					step.ResultID = ""
					root = step.Fields["name"]
					if missing {
						if err := f.RemoveDirectory(context.Background(), step.Match[".id"]); err != nil {
							t.Fatal(err)
						}
					}
				}
			}
			n.mu.Unlock()
			nativeSubmit(t, s, d, ActionRequest{Action: "delete", TargetID: updated.TargetID, RequestID: "legacy-delete"})
			files, _ := f.ContainerRead(context.Background(), routeros.ContainerFiles)
			if root == "" || len(findRows(files, map[string]string{"name": root})) != 0 {
				t.Fatal("legacy root was not cleaned")
			}
		})
	}
}

func TestUnattemptedRetentionCannotClaimIndependentTarget(t *testing.T) {
	s, d, f := nativeFixture(t)
	created := nativeSubmit(t, s, d, ActionRequest{Action: "create", RequestID: "foreign-retain-create", Draft: draftFixture()})
	snap, _ := s.fresh(context.Background(), d)
	steps, retained, err := s.plan(context.Background(), d, ActionRequest{Action: "update", TargetID: created.TargetID}, snap, "12345678-1234-1234-1234-123456789012")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for i := range steps {
		step := &steps[i]
		if step.Command == "retain-root" {
			f.mu.Lock()
			for _, row := range f.rows[routeros.ContainerFiles] {
				if row["name"] == step.Match["name"] {
					row["name"], row[".id"] = step.Fields["name"], "**independent-target"
				}
			}
			f.mu.Unlock()
			found = true
			break
		}
		fields := routeros.RouterOSFields{}
		for k, v := range step.Fields {
			fields[k] = v
		}
		if step.Command != "add" {
			rows, _ := f.ContainerRead(context.Background(), step.Menu)
			matches := findRows(rows, step.Match)
			if len(matches) != 1 {
				t.Fatal("fixture step target missing")
			}
			fields["numbers"] = matches[0][".id"]
		}
		if err := f.ContainerCommand(context.Background(), step.Menu, step.Command, fields); err != nil {
			t.Fatal(err)
		}
		step.Done = true
	}
	if !found {
		t.Fatal("fixture has no retention step")
	}
	r := &nativeRecord{Job: Job{ID: "12345678-1234-1234-1234-123456789012", DeviceID: d.ID, Action: "update", State: "running", Retained: retained}, Request: ActionRequest{Action: "update", TargetID: created.TargetID}, Steps: steps}
	n, _ := s.nativeDevice(d)
	n.mu.Lock()
	n.state.Jobs[r.Job.ID] = r
	n.mu.Unlock()
	f.mu.Lock()
	before := f.writes
	f.mu.Unlock()
	s.executeNative(d, n, r)
	if r.Job.State != "failed" || !strings.Contains(r.Job.Error, "非预期") {
		t.Fatalf("foreign target claimed: %+v", r.Job)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.writes != before {
		t.Fatal("unattempted retention continued writing")
	}
}
