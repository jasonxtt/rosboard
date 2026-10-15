package containers

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"rosboard/internal/config"
	"rosboard/internal/downloadnet"
	"rosboard/internal/routeros"
)

func TestHostImageIndependentLab(t *testing.T) {
	cfgpath := os.Getenv("ROSBOARD_HOST_IMAGE_LAB_CONFIG")
	if cfgpath == "" {
		t.Skip("independent lab opt-in")
	}
	var cfg struct{ BaseURL, Username, Password, HostKey string }
	raw, e := os.ReadFile(cfgpath)
	if e != nil || json.Unmarshal(raw, &cfg) != nil || !strings.HasPrefix(cfg.BaseURL, "http://10.0.2.5") {
		t.Fatal("independent private lab configuration required")
	}
	d := config.DeviceConfig{ID: "host-image-lab", Enabled: true, ContainerWrites: true, ContainerSSHHostKey: cfg.HostKey, RouterOS: config.RouterOSConfig{BaseURL: cfg.BaseURL, Username: cfg.Username, Password: cfg.Password}}
	stateDir, _ := os.MkdirTemp("", "rosboard-host-native-lab-")
	t.Log("lab journal directory", stateDir)
	s := NewServiceWithState(stateDir)
	s.DownloadSnapshot = func() (downloadnet.Snapshot, error) { return downloadnet.New(os.Getenv("ROSBOARD_IMAGE_PROXY")) }
	ctx, cancel := context.WithTimeout(context.Background(), 9*time.Minute)
	defer cancel()
	snap, e := s.fresh(ctx, d)
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
		t.Fatal("lab disk unavailable")
	}
	token := strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
	parent := disk + "/rosboard-host-lab-" + token
	wait := func(svc *Service, j Job) Job {
		for attempt := 0; attempt < 1600; attempt++ {
			j, e = svc.NativeJob(d, j.ID)
			if e != nil {
				t.Fatal("job read failed")
			}
			if j.State == "succeeded" {
				return j
			}
			if j.State == "failed" || j.State == "unknown" {
				t.Fatalf("job state=%s phase=%s error=%s", j.State, j.Phase, j.Error)
			}
			time.Sleep(250 * time.Millisecond)
		}
		t.Fatal("lab job deadline")
		return j
	}
	submit := func(svc *Service, action, id string, draft Draft) Job {
		if action == "create" {
			snapshot, _ := svc.fresh(ctx, d)
			r := Resolve(draft, snapshot)
			if len(r.Errors) > 0 {
				t.Fatalf("draft validation: %v", r.Errors)
			}
		}
		j, e := svc.Submit(ctx, d, nil, ActionRequest{Action: action, TargetID: id, RequestID: uuid.NewString(), Draft: draft})
		if e != nil {
			t.Fatalf("submit %s: %v", action, e)
		}
		return wait(svc, j)
	}
	created := []string{}
	defer func() {
		cleanup, _ := s.fresh(context.Background(), d)
		for _, item := range cleanup.Items {
			if strings.HasPrefix(item.Name, "rb-host-"+token) {
				j, e := s.Submit(context.Background(), d, nil, ActionRequest{Action: "delete", TargetID: item.ID, RequestID: uuid.NewString()})
				if e == nil {
					for i := 0; i < 160; i++ {
						j, _ = s.NativeJob(d, j.ID)
						if j.State == "succeeded" || j.State == "failed" || j.State == "unknown" {
							break
						}
						time.Sleep(250 * time.Millisecond)
					}
				}
			}
		}
		// Only this generated fixture parent, after reference checks, may be removed.
		rows, _ := s.ReaderFor(d).ContainerRead(context.Background(), routeros.ContainerList)
		for _, row := range rows {
			if strings.HasPrefix(row["root-dir"], parent) {
				t.Error("fixture container remains")
				return
			}
		}
		files, _ := s.ReaderFor(d).ContainerRead(context.Background(), routeros.ContainerFiles)
		for _, f := range files {
			if f["name"] == parent {
				if e := routeros.NewMutationClient(cfg.BaseURL, cfg.Username, cfg.Password).RemoveDirectory(context.Background(), f[".id"]); e != nil {
					t.Error("fixture root cleanup failed")
				}
			}
		}
	}()
	for i, ref := range []string{"alpine:3.22", "ghcr.io/nginx/nginx-unprivileged:stable-alpine"} {
		draft := Draft{DraftID: uuid.NewString(), Name: "rb-host-" + token + string(rune('a'+i)), ImageSource: "registry", Image: ref, RootDir: "/" + parent + "/" + string(rune('a'+i)) + "/rootfs", Network: Network{VETH: "veth-host-" + token + string(rune('a'+i)), Bridge: "bridge1", Address: []string{"10.0.2.241/24", "10.0.2.242/24"}[i], Gateway: "10.0.2.1"}, StartAfterCreate: true, StartOnBoot: true, Logging: true, Health: Health{Mode: "inherit"}}
		if i == 0 {
			draft.Command = "sleep 3600"
		}
		j := submit(s, "create", "", draft)
		created = append(created, j.TargetID)
		n, _ := s.nativeDevice(d)
		n.mu.Lock()
		g := *n.state.Jobs[j.ID].Generated
		n.mu.Unlock()
		if !g.Cleaned || !g.RuntimeSucceeded {
			t.Fatal("first-runtime cleanup not confirmed")
		}
		_, dir, _ := s.imageDirectory(d, j.ID)
		if _, e := os.Stat(filepath.Join(dir, "image.tar")); !os.IsNotExist(e) {
			t.Fatal("local generated archive remains")
		}
		submit(s, "stop", j.TargetID, Draft{})
		submit(s, "start", j.TargetID, Draft{})
		submit(s, "restart", j.TargetID, Draft{})
		fresh, e := s.fresh(ctx, d)
		if e != nil {
			t.Fatal(e)
		}
		found := false
		for _, item := range fresh.Items {
			if item.ID == j.TargetID {
				found = item.Status == "running" && item.ImageDigest != "" && item.Config.ImageSource == "registry"
			}
		}
		if !found {
			t.Fatal("runtime/source metadata lost after tar deletion")
		}
		if i == 0 {
			updated := submit(s, "update", j.TargetID, Draft{})
			created[0] = updated.TargetID
		}
	}
	// A deliberately stopped import retains its recovery image until deletion.
	draft := Draft{DraftID: uuid.NewString(), Name: "rb-host-" + token + "stopped", ImageSource: "registry", Image: "alpine:3.22", RootDir: "/" + parent + "/stopped/rootfs", Network: Network{VETH: "veth-host-" + token + "s", Bridge: "bridge1", Address: "10.0.2.243/24", Gateway: "10.0.2.1"}, Command: "sleep 3600", Health: Health{Mode: "inherit"}}
	stopped := submit(s, "create", "", draft)
	n, _ := s.nativeDevice(d)
	n.mu.Lock()
	g := n.state.Jobs[stopped.ID].Generated
	retained := !g.Cleaned
	n.mu.Unlock()
	if !retained {
		t.Fatal("stopped image removed before first runtime")
	}
	// Keep a real sibling mount source while deleting the exclusive rootfs.
	dataPath := "/" + parent + "/stopped/data"
	native := routeros.NewMutationClient(cfg.BaseURL, cfg.Username, cfg.Password)
	if e := native.CreateDirectory(ctx, dataPath); e != nil {
		t.Fatal(e)
	}
	mountList := "rb-data-" + token
	if e := native.ContainerCommand(ctx, routeros.ContainerMounts, "add", routeros.RouterOSFields{"list": mountList, "src": dataPath, "dst": "/data", "comment": g.Owner}); e != nil {
		t.Fatal(e)
	}
	if e := native.ContainerCommand(ctx, routeros.ContainerList, "set", routeros.RouterOSFields{"numbers": stopped.TargetID, "mountlists": mountList}); e != nil {
		t.Fatal(e)
	}
	submit(s, "delete", stopped.TargetID, Draft{})
	fileRows, e := s.ReaderFor(d).ContainerRead(ctx, routeros.ContainerFiles)
	if e != nil {
		t.Fatal(e)
	}
	dataExists := false
	rootPath := strings.TrimPrefix(draft.RootDir, "/")
	for _, file := range fileRows {
		if file["name"] == strings.TrimPrefix(dataPath, "/") {
			dataExists = true
		}
		if file["name"] == rootPath || strings.HasPrefix(file["name"], rootPath+"/") || strings.HasPrefix(file["name"], rootPath+"-retained-") {
			t.Fatal("delete retained its root filesystem")
		}
	}
	if !dataExists {
		t.Fatal("delete removed independent mount data")
	}

	n.mu.Lock()
	cleaned := g.Cleaned
	n.mu.Unlock()
	if !cleaned {
		t.Fatal("delete-before-runtime left artifacts")
	}
	restored := NewServiceWithState(s.NativeStateDir)
	restored.DownloadSnapshot = s.DownloadSnapshot
	if e := restored.RestoreNative(d); e != nil {
		t.Fatal(e)
	}
	snap, e = restored.fresh(ctx, d)
	if e != nil {
		t.Fatal(e)
	}
	for _, id := range created {
		found := false
		for _, item := range snap.Items {
			if item.ID == id {
				found = item.ImageDigest != "" && item.Config.ImageSource == "registry"
			}
		}
		if !found {
			t.Fatal("origin not durable across restart")
		}
	}
	t.Log("Docker Hub Alpine and GHCR multilayer import/start/update, tar cleanup then stop/start/restart, stopped retention, delete removes rootfs while preserving sibling mount data, artifact cleanup and durable logical origin passed")
}
