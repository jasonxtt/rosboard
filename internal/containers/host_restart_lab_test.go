package containers

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"rosboard/internal/config"
	"rosboard/internal/downloadnet"
	"rosboard/internal/routeros"
)

type restartFixture struct {
	State   string
	Request ActionRequest
	Device  config.DeviceConfig
}

func restartService(f restartFixture) *Service {
	s := NewServiceWithState(f.State)
	s.DownloadSnapshot = func() (downloadnet.Snapshot, error) { return downloadnet.New(os.Getenv("ROSBOARD_IMAGE_PROXY")) }
	return s
}
func TestHostImageRestartWorker(t *testing.T) {
	p := os.Getenv("ROSBOARD_HOST_RESTART_FIXTURE")
	if p == "" {
		t.Skip("subprocess fixture only")
	}
	var f restartFixture
	raw, e := os.ReadFile(p)
	if e != nil || json.Unmarshal(raw, &f) != nil {
		t.Fatal("private restart fixture unavailable")
	}
	s := restartService(f)
	j, e := s.Submit(context.Background(), f.Device, nil, f.Request)
	if e != nil {
		t.Fatal(e)
	}
	_ = j
	for {
		time.Sleep(time.Second)
	}
}
func TestHostImageRestartIndependentLab(t *testing.T) {
	p := os.Getenv("ROSBOARD_HOST_IMAGE_LAB_CONFIG")
	if p == "" {
		t.Skip("independent restart lab opt-in")
	}
	var c struct{ BaseURL, Username, Password, HostKey string }
	raw, e := os.ReadFile(p)
	if e != nil || json.Unmarshal(raw, &c) != nil || c.BaseURL != "http://10.0.2.5" {
		t.Fatal("independent private lab config required")
	}
	d := config.DeviceConfig{ID: "host-restart-lab", Enabled: true, ContainerSSHHostKey: c.HostKey, RouterOS: config.RouterOSConfig{BaseURL: c.BaseURL, Username: c.Username, Password: c.Password}}
	state, _ := os.MkdirTemp("", "rosboard-image-restart-lab-")
	t.Log("durable lab state", state)
	s := NewServiceWithState(state)
	snap, e := s.fresh(context.Background(), d)
	if e != nil || len(snap.Options.Disks) == 0 {
		t.Fatal("lab snapshot unavailable")
	}
	disk := ""
	for _, candidate := range snap.Options.Disks {
		if candidate.Writable && candidate.FreeBytes > 512<<20 {
			disk = candidate.Name
			break
		}
	}
	if disk == "" {
		t.Fatal("writable lab disk unavailable")
	}
	token := strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
	parent := disk + "/rosboard-restart-" + token
	fixture := restartFixture{State: state, Device: d, Request: ActionRequest{Action: "create", RequestID: uuid.NewString(), Draft: Draft{DraftID: uuid.NewString(), Name: "rb-restart-" + token, Image: "ghcr.io/nginx/nginx-unprivileged:stable-alpine", ImageSource: "registry", RootDir: "/" + parent + "/rootfs", Network: Network{VETH: "veth-restart-" + token, Bridge: "bridge1", Address: "10.0.2.244/24", Gateway: "10.0.2.1"}, Health: Health{Mode: "inherit"}}}}
	private := filepath.Join(state, "fixture-private.json")
	payload, _ := json.Marshal(fixture)
	os.WriteFile(private, payload, 0600)
	defer os.Remove(private)
	executable, _ := os.Executable()
	child := exec.Command(executable, "-test.run=^TestHostImageRestartWorker$", "-test.v")
	child.Env = append(os.Environ(), "ROSBOARD_HOST_RESTART_FIXTURE="+private)
	var output strings.Builder
	child.Stdout = &output
	child.Stderr = &output
	if e = child.Start(); e != nil {
		t.Fatal(e)
	}
	defer func() {
		if child.Process != nil {
			child.Process.Kill()
			child.Wait()
		}
	}()
	var jobID string
	interrupted := false
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		journals, _ := filepath.Glob(filepath.Join(state, "containers", "*.json"))
		if len(journals) > 0 {
			data, _ := os.ReadFile(journals[0])
			var ns nativeState
			if json.Unmarshal(data, &ns) == nil {
				jobID = ns.Latest
				r := ns.Jobs[jobID]
				if r != nil && r.Pending {
					for _, step := range r.Steps {
						if step.Done {
							continue
						}
						if step.Menu == routeros.ContainerList && step.Command == "add" {
							rows, _ := s.ReaderFor(d).ContainerRead(context.Background(), routeros.ContainerList)
							for _, row := range rows {
								if row["name"] == fixture.Request.Draft.Name {
									interrupted = true
								}
							}
						}
						break
					}
				}
			}
		}
		if interrupted {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	child.Process.Kill()
	child.Wait()
	child.Process = nil
	if !interrupted {
		t.Fatalf("did not observe a pending actual import: %s", output.String())
	}
	s = restartService(fixture)
	if e = s.RestoreNative(d); e != nil {
		t.Fatal(e)
	}
	job, e := s.RecoverNative(d, nil, jobID)
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 1200; i++ {
		job, e = s.NativeJob(d, jobID)
		if e != nil {
			t.Fatal(e)
		}
		if job.State == "succeeded" {
			break
		}
		if job.State == "failed" || job.State == "unknown" {
			t.Fatalf("restart recovery: %s %s", job.Phase, job.Error)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if job.State != "succeeded" {
		t.Fatal("recovery deadline")
	}
	rows, e := s.ReaderFor(d).ContainerRead(context.Background(), routeros.ContainerList)
	if e != nil {
		t.Fatal(e)
	}
	count := 0
	for _, row := range rows {
		if row["name"] == fixture.Request.Draft.Name {
			count++
		}
	}
	if count != 1 {
		t.Fatal("container/add replayed")
	}
	remove, e := s.Submit(context.Background(), d, nil, ActionRequest{Action: "delete", TargetID: job.TargetID, RequestID: uuid.NewString()})
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 300; i++ {
		remove, _ = s.NativeJob(d, remove.ID)
		if remove.State == "succeeded" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if remove.State != "succeeded" {
		t.Fatal("restart fixture cleanup failed")
	}
	files, _ := s.ReaderFor(d).ContainerRead(context.Background(), routeros.ContainerFiles)
	for _, f := range files {
		if f["name"] == parent {
			if e := routeros.NewMutationClient(c.BaseURL, c.Username, c.Password).RemoveDirectory(context.Background(), f[".id"]); e != nil {
				t.Fatal("restart fixture root cleanup failed")
			}
		}
	}
	t.Log("actual import interrupted by coordinator process exit; restart read-back completed with exactly one container/add and durable cleanup")
}
