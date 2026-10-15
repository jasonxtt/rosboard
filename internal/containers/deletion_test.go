package containers

import (
	"context"
	"rosboard/internal/config"
	"rosboard/internal/routeros"
	"strings"
	"testing"
)

func TestMemoryLimitsRejectAccidentalBytes(t *testing.T) {
	for _, value := range []string{"128", "1024", "512K"} {
		d := draftFixture()
		d.MemoryHigh = value
		d.MemoryMax = value
		r := Resolve(d, optionsFixture())
		if r.Errors["memoryMax"] == "" || r.Errors["memoryHigh"] == "" {
			t.Fatalf("unsafe byte limit accepted: %s", value)
		}
	}
	for _, value := range []string{"128M", "1MiB", "1048576", "unlimited"} {
		d := draftFixture()
		d.MemoryHigh = value
		d.MemoryMax = value
		r := Resolve(d, optionsFixture())
		if r.Errors["memoryMax"] != "" || r.Errors["memoryHigh"] != "" {
			t.Fatalf("valid limit rejected: %s", value)
		}
	}
}
func TestNativeFailureReasonRedactsCredentialsAndEnv(t *testing.T) {
	_, d, _ := nativeFixture(t)
	d.RouterOS.Username = "sensitive-user"
	d.RouterOS.Password = "sensitive-password"
	r := &nativeRecord{Steps: []nativeStep{{Fields: map[string]string{"value": "sensitive-env"}}}}
	text := nativeFailureMessage(&routeros.HTTPError{StatusCode: 400, Detail: "memory-max < 1MB sensitive-user sensitive-password sensitive-env"}, d, r)
	if !strings.Contains(text, "memory-max < 1MB") || strings.Contains(text, "sensitive-") {
		t.Fatal("unsafe or unhelpful failure reason")
	}
}
func TestDeleteProtectsMountedRootAndLeavesContainer(t *testing.T) {
	s, d, f := nativeFixture(t)
	created := nativeSubmit(t, s, d, ActionRequest{Action: "create", RequestID: "root-mount-create", Draft: draftFixture()})
	f.mu.Lock()
	root := f.rows[routeros.ContainerList][0]["root-dir"]
	f.rows[routeros.ContainerMounts] = append(f.rows[routeros.ContainerMounts], routeros.RouterOSObject{".id": "*FA", "src": root + "/data"})
	f.mu.Unlock()
	j, e := s.Submit(context.Background(), d, nil, ActionRequest{Action: "delete", TargetID: created.TargetID, RequestID: "root-mount-delete"})
	if e != nil {
		t.Fatal(e)
	}
	j = waitNative(t, s, d, j.ID)
	if j.State != "failed" || !strings.Contains(j.Error, "挂载") {
		t.Fatalf("mounted root removal not blocked: %+v", j)
	}
	rows, _ := f.ContainerRead(context.Background(), routeros.ContainerList)
	if len(rows) != 1 {
		t.Fatal("container removed despite mounted root")
	}
}

type rootReplacementWriter struct {
	NativeWriter
	afterRemove func()
}

func (w rootReplacementWriter) ContainerCommand(ctx context.Context, m routeros.ContainerMenu, c string, f routeros.RouterOSFields) error {
	e := w.NativeWriter.ContainerCommand(ctx, m, c, f)
	if e == nil && m == routeros.ContainerList && c == "remove" {
		w.afterRemove()
	}
	return e
}
func TestDeleteDoesNotRemoveSamePathReplacement(t *testing.T) {
	s, d, f := nativeFixture(t)
	created := nativeSubmit(t, s, d, ActionRequest{Action: "create", RequestID: "replacement-create", Draft: draftFixture()})
	f.mu.Lock()
	root := f.rows[routeros.ContainerList][0]["root-dir"]
	f.mu.Unlock()
	s.NativeWriterFor = func(_ config.DeviceConfig) NativeWriter {
		return rootReplacementWriter{NativeWriter: f, afterRemove: func() {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.rows[routeros.ContainerFiles] = append(f.rows[routeros.ContainerFiles], routeros.RouterOSObject{".id": "**replacement", "name": strings.TrimPrefix(root, "/"), "type": "directory"})
		}}
	}
	j, e := s.Submit(context.Background(), d, nil, ActionRequest{Action: "delete", TargetID: created.TargetID, RequestID: "replacement-delete"})
	if e != nil {
		t.Fatal(e)
	}
	j = waitNative(t, s, d, j.ID)
	if j.State != "failed" {
		t.Fatalf("replacement was treated as the owned root: %+v", j)
	}
	rows, _ := f.ContainerRead(context.Background(), routeros.ContainerFiles)
	if len(findRows(rows, map[string]string{".id": "**replacement"})) != 1 {
		t.Fatal("foreign replacement was removed")
	}
}
