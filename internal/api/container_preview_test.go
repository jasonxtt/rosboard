package api

// The interactive simulator is compiled only by go test. No mock data or
// mutation handler is linked into cmd/rosboard or its embedded production UI.
import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"rosboard/internal/containers"
)

type simulatedJob struct {
	containers.Job
	draft    containers.Draft
	started  time.Time
	scenario string
	applied  bool
}
type simulatedRequest struct {
	Action    string           `json:"action"`
	TargetID  string           `json:"targetId"`
	RequestID string           `json:"requestId"`
	Draft     containers.Draft `json:"draft"`
	Scenario  string           `json:"scenario"`
}
type simulation struct {
	mu         sync.Mutex
	devices    map[string]containers.Snapshot
	jobs       map[string]*simulatedJob
	requests   map[string]string
	signatures map[string][32]byte
	resources  map[string]map[string][]string
	latest     map[string]string
	now        func() time.Time
}

func newSimulation() *simulation {
	m := &simulation{devices: map[string]containers.Snapshot{}, jobs: map[string]*simulatedJob{}, requests: map[string]string{}, signatures: map[string][32]byte{}, resources: map[string]map[string][]string{}, now: time.Now, latest: map[string]string{}}
	for _, id := range []string{"demo-router", "demo-edge"} {
		m.devices[id] = simulationFixture(id)
		m.resources[id] = map[string][]string{}
	}
	return m
}
func simulationFixture(device string) containers.Snapshot {
	s := containers.Snapshot{Items: []containers.Item{}, Options: containers.Options{Bridges: []string{"br-containers", "br-services"}, Interfaces: []string{"ether1", "br-containers", "br-services", "veth-shared"}, UsedIPs: []string{"172.20.0.1/24", "172.20.0.2/24"}, Disks: []containers.Disk{{Name: "usb1", FreeBytes: 1 << 30, Writable: true}, {Name: "sata1", FreeBytes: 8 << 30, Writable: true}}, MemoryHigh: "256M", MemoryMax: "512M"}, Capabilities: containers.Capabilities{Supported: true, Writes: true, Mode: "simulation", Version: "7.23.5", Logs: true, Fields: []string{"memory-high", "memory-max", "restart-policy", "cpu-list", "healthcheck-cmd"}, Warnings: []string{}}}
	names := []string{"mosdns", "dns-metrics", "nginx", "redis"}
	if device == "demo-edge" {
		names = []string{"edge-proxy"}
	}
	for i, name := range names {
		n := containers.Network{VETH: "veth-" + name, Bridge: "br-containers", Address: fmt.Sprintf("172.20.0.%d/24", 10+i), Gateway: "172.20.0.1"}
		shared := []string{}
		if i < 2 && device == "demo-router" {
			n.VETH = "veth-shared"
			n.Address = "172.20.0.2/24"
			shared = []string{names[1-i]}
		}
		d := containers.Draft{ExistingID: fmt.Sprintf("*%d", i+1), Name: name, Image: "ghcr.io/example/" + name + ":stable", Network: n, RootDir: "/sata1/existing/" + name, Command: "", Env: []containers.Environment{{Key: "TZ", Value: "Asia/Taipei"}}, Mounts: []containers.Mount{{Source: "/sata1/shared-config", Target: "/etc/config", ReadOnly: true}}, Ports: []containers.Port{}, StartOnBoot: i%2 == 0, Logging: i != 0, RestartPolicy: "no", Health: containers.Health{Mode: "inherit"}}
		state := "running"
		if i == 0 {
			state = "stopped"
		}
		s.Items = append(s.Items, containers.Item{ID: d.ExistingID, Name: name, Status: state, Image: d.Image, Network: n, CPU: fmt.Sprint(i * 3), Memory: fmt.Sprintf("%d MiB", 24+i*16), Ports: d.Ports, StartOnBoot: d.StartOnBoot, Ownership: "unmanaged", SharedVETH: shared, EnvLists: []string{"shared-env"}, MountLists: []string{"shared-config"}, Config: d, ImageDefaults: map[string]string{"cmd": "server --config /etc/config", "entrypoint": "/entrypoint.sh", "user": "1000", "workdir": "/app", "healthcheck-cmd": "CMD-SHELL curl -f localhost/health"}})
		if n.VETH != "veth-shared" {
			s.Options.Interfaces = append(s.Options.Interfaces, n.VETH)
			s.Options.UsedIPs = append(s.Options.UsedIPs, n.Address)
		}
	}
	return s
}
func (m *simulation) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w.Header().Set("Cache-Control", "no-store")
	if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+r.Host {
		writeAPIError(w, 403, "cross_origin_denied", "模拟服务仅接受同源请求")
		return
	}
	device := r.URL.Query().Get("device")
	s, ok := m.devices[device]
	if !ok {
		writeAPIError(w, 404, "device_not_found", "模拟设备不存在")
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/containers")
	if path == "/actions" && r.Method == http.MethodPost {
		m.action(w, r, device, s)
		return
	}
	if strings.HasPrefix(path, "/jobs/") {
		parts := strings.Split(strings.TrimPrefix(path, "/jobs/"), "/")
		job := m.jobs[parts[0]]
		if job == nil || job.DeviceID != device {
			writeAPIError(w, 404, "job_not_found", "任务不在此设备")
			return
		}
		if len(parts) == 2 && parts[1] == "recover" && r.Method == http.MethodPost {
			if job.State != "unknown" {
				writeAPIError(w, 409, "recovery_not_required", "任务不需要恢复")
				return
			}
			m.apply(job)
			job.State = "succeeded"
			job.Phase = "回读已确认，没有重复创建"
			job.Progress = 100
			job.Error = ""
			writeJSON(w, 200, job.Job)
			return
		}
		if len(parts) != 1 || r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		m.advance(job)
		writeJSON(w, 200, job.Job)
		return
	}
	if path == "/resolve" && r.Method == http.MethodPost {
		var d containers.Draft
		if decodeJSONBody(w, r, &d) != nil {
			return
		}
		writeJSON(w, 200, containers.Resolve(d, s))
		return
	}
	if (path == "/_reset" || path == "/_many") && r.Method == http.MethodPost {
		for _, job := range m.jobs {
			if job.DeviceID == device && (job.State == "queued" || job.State == "running" || job.State == "unknown") {
				writeAPIError(w, 409, "device_busy", "先完成或恢复当前任务")
				return
			}
		}
		m.devices[device] = simulationFixture(device)
		delete(m.latest, device)
		m.resources[device] = map[string][]string{}
		if path == "/_many" {
			s = m.devices[device]
			for i := 0; i < 160; i++ {
				item := s.Items[len(s.Items)-1]
				item.ID = fmt.Sprintf("*demo%d", i)
				item.Name = fmt.Sprintf("worker-%03d", i)
				item.Image = "ghcr.io/example/very-long-registry-path/services/processing/worker-with-a-long-reference:2026.10-stable"
				item.Config.ExistingID = item.ID
				item.Config.Name = item.Name
				item.Config.Image = item.Image
				item.Network.VETH = fmt.Sprintf("veth-worker-%03d", i)
				item.Network.Address = fmt.Sprintf("172.21.0.%d/16", i+10)
				item.Network.Gateway = "172.21.0.1"
				item.Config.Network = item.Network
				item.Config.RootDir = "/sata1/existing/" + item.Name
				item.SharedVETH = []string{}
				s.Options.Interfaces = append(s.Options.Interfaces, item.Network.VETH)
				s.Options.UsedIPs = append(s.Options.UsedIPs, item.Network.Address)
				s.Items = append(s.Items, item)
			}
			m.devices[device] = s
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	if path == "" || path == "/" {
		if id := m.latest[device]; id != "" {
			value := m.jobs[id].Job
			s.ActiveJob = &value
		}
		writeJSON(w, 200, s)
		return
	}
	if path == "/options" {
		writeJSON(w, 200, map[string]any{"options": s.Options, "capabilities": s.Capabilities})
		return
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for _, item := range s.Items {
		if item.ID != parts[0] {
			continue
		}
		if len(parts) == 1 {
			writeJSON(w, 200, item)
			return
		}
		if len(parts) == 2 && parts[1] == "logs" {
			logs := []containers.Log{}
			if item.Config.Logging {
				logs = append(logs, containers.Log{ID: "log1", Time: "2026-10-08 12:00:00", Message: item.Name + ": service ready (simulated)"}, containers.Log{ID: "log2", Time: "2026-10-08 12:00:01", Message: "健康检查通过 · <script>safe text</script>"})
			}
			writeJSON(w, 200, map[string]any{"logs": logs})
			return
		}
	}
	writeAPIError(w, 404, "not_found", "模拟资源不存在")
}
func (m *simulation) action(w http.ResponseWriter, r *http.Request, device string, s containers.Snapshot) {
	var req simulatedRequest
	if decodeJSONBody(w, r, &req) != nil {
		return
	}
	if req.RequestID == "" {
		writeAPIError(w, 400, "request_id_required", "缺少请求唯一标识")
		return
	}
	encoded, err := json.Marshal(req)
	if err != nil {
		writeAPIError(w, 400, "invalid_request", "请求无效")
		return
	}
	signature := sha256.Sum256(encoded)
	key := device + "/" + req.RequestID
	if id := m.requests[key]; id != "" {
		if m.signatures[key] != signature {
			writeAPIError(w, 409, "request_reused", "相同请求标识不能用于不同操作")
			return
		}
		writeJSON(w, 202, m.jobs[id].Job)
		return
	}
	for _, job := range m.jobs {
		if job.DeviceID == device && (job.State == "queued" || job.State == "running" || job.State == "unknown") {
			writeAPIError(w, 409, "device_busy", "此设备已有进行中的任务")
			return
		}
	}
	var selected *containers.Item
	for i := range s.Items {
		if s.Items[i].ID == req.TargetID {
			selected = &s.Items[i]
			break
		}
	}
	if req.Action != "create" && selected == nil {
		writeAPIError(w, 404, "container_not_found", "容器不存在")
		return
	}
	switch req.Action {
	case "create", "edit":
		if req.Action == "create" {
			req.Draft.ExistingID = ""
		} else {
			if selected.Ownership != "managed" {
				writeAPIError(w, 409, "adoption_required", "请先接管此容器")
				return
			}
			req.Draft.ExistingID = selected.ID
		}
		resolved := containers.Resolve(req.Draft, s)
		if len(resolved.Errors) > 0 {
			writeJSON(w, 400, map[string]any{"code": "invalid_draft", "error": "配置校验失败", "details": resolved.Errors})
			return
		}
		req.Draft = resolved.Effective
	case "start", "stop", "restart":
	case "update", "delete":
		if selected.Ownership != "managed" {
			writeAPIError(w, 409, "adoption_required", "请先接管此容器")
			return
		}
	case "adopt":
		if selected.Ownership == "managed" {
			writeAPIError(w, 409, "already_managed", "容器已接管")
			return
		}
	default:
		writeAPIError(w, 400, "invalid_action", "不支持的模拟操作")
		return
	}
	if req.Scenario != "success" && req.Scenario != "failure" && req.Scenario != "unknown" {
		writeAPIError(w, 400, "invalid_scenario", "不支持的模拟情景")
		return
	}
	job := &simulatedJob{Job: containers.Job{ID: uuid.NewString(), DeviceID: device, Action: req.Action, TargetID: req.TargetID, State: "queued", Phase: "等待执行", Progress: 0, Retained: []string{}}, draft: req.Draft, started: m.now(), scenario: req.Scenario}
	m.jobs[job.ID] = job
	m.latest[device] = job.ID
	m.requests[key] = job.ID
	m.signatures[key] = signature
	writeJSON(w, 202, job.Job)
}
func (m *simulation) advance(job *simulatedJob) {
	if job.State != "queued" && job.State != "running" {
		return
	}
	elapsed := m.now().Sub(job.started)
	job.State = "running"
	switch {
	case elapsed < time.Second:
		job.Phase = "准备与校验"
		job.Progress = 10
	case elapsed < 2*time.Second:
		if job.Action == "create" || job.Action == "update" {
			job.Phase = "下载镜像"
		} else {
			job.Phase = "读取现有配置"
		}
		job.Progress = 35
	case elapsed < 3*time.Second:
		job.Phase = "创建 / 应用配置"
		job.Progress = 70
	case elapsed < 4*time.Second && job.Action == "create" && job.draft.StartAfterCreate:
		job.Phase = "启动容器"
		job.Progress = 90
	default:
		if job.scenario == "failure" {
			job.State = "failed"
			job.Phase = "操作失败"
			job.Error = "模拟下载 / 执行失败，未更改已有对象"
			return
		}
		m.apply(job)
		if job.scenario == "unknown" {
			job.State = "unknown"
			job.Phase = "结果不明，等待回读"
			job.Error = "响应超时；保留设备操作锁，禁止重复创建"
			job.Progress = 85
			return
		}
		job.State = "succeeded"
		job.Phase = "已完成"
		job.Progress = 100
	}
}
func (m *simulation) apply(job *simulatedJob) {
	if job.applied {
		return
	}
	job.applied = true
	s := m.devices[job.DeviceID]
	if job.Action == "create" {
		d := job.draft
		id := "*" + job.ID
		d.ExistingID = id
		status := "stopped"
		if d.StartAfterCreate {
			status = "running"
		}
		item := containers.Item{ID: id, Name: d.Name, Image: d.Image, Status: status, Network: d.Network, CPU: "0", Memory: "16 MiB", Ports: d.Ports, StartOnBoot: d.StartOnBoot, Ownership: "managed", Config: d, SharedVETH: []string{}, EnvLists: []string{}, MountLists: []string{}, ImageDefaults: map[string]string{}}
		owned := []string{"veth:" + d.Network.VETH, "bridge-port:" + d.Network.VETH}
		if len(d.Env) > 0 {
			item.EnvLists = []string{"rosboard-env-" + job.ID}
			owned = append(owned, "env:"+item.EnvLists[0])
		}
		if len(d.Mounts) > 0 {
			item.MountLists = []string{"rosboard-mount-" + job.ID}
			owned = append(owned, "mount:"+item.MountLists[0])
		}
		for i := range d.Ports {
			owned = append(owned, fmt.Sprintf("nat:%s-%d", job.ID, i))
		}
		m.resources[job.DeviceID][id] = owned
		s.Items = append(s.Items, item)
		s.Options.Interfaces = append(s.Options.Interfaces, d.Network.VETH)
		s.Options.UsedIPs = append(s.Options.UsedIPs, d.Network.Address)
		job.TargetID = id
	} else {
		for i := range s.Items {
			item := &s.Items[i]
			if item.ID != job.TargetID {
				continue
			}
			switch job.Action {
			case "start", "restart":
				item.Status = "running"
			case "stop":
				item.Status = "stopped"
			case "update":
				job.Retained = append(job.Retained, item.Config.RootDir)
			case "adopt":
				item.Ownership = "managed"
				job.Retained = append(job.Retained, "existing-veth:"+item.Network.VETH)
			case "edit":
				// The simulator creates independently tracked replacement env/mount sets;
				// it never mutates pre-existing lists referenced by another container.
				old := item.Config
				owned := m.resources[job.DeviceID][item.ID]
				if old.Network != job.draft.Network {
					if old.Network.VETH != job.draft.Network.VETH {
						if simulationOwns(owned, "veth:"+old.Network.VETH) {
							s.Options.Interfaces = removeString(s.Options.Interfaces, old.Network.VETH)
							owned = removeString(owned, "veth:"+old.Network.VETH)
							owned = removeString(owned, "bridge-port:"+old.Network.VETH)
						} else {
							job.Retained = append(job.Retained, "veth:"+old.Network.VETH)
						}
						s.Options.Interfaces = append(s.Options.Interfaces, job.draft.Network.VETH)
						owned = append(owned, "veth:"+job.draft.Network.VETH, "bridge-port:"+job.draft.Network.VETH)
					}
					s.Options.UsedIPs = removeString(s.Options.UsedIPs, old.Network.Address)
					s.Options.UsedIPs = append(s.Options.UsedIPs, job.draft.Network.Address)
				}
				item.Config = job.draft
				item.Name = job.draft.Name
				item.Image = job.draft.Image
				item.Network = job.draft.Network
				item.Ports = job.draft.Ports
				item.StartOnBoot = job.draft.StartOnBoot
				if !reflect.DeepEqual(old.Env, job.draft.Env) {
					for _, list := range item.EnvLists {
						if simulationOwns(owned, "env:"+list) {
							owned = removeString(owned, "env:"+list)
						} else {
							job.Retained = append(job.Retained, list)
						}
					}
					item.EnvLists = []string{}
					if len(job.draft.Env) > 0 {
						item.EnvLists = []string{"rosboard-env-" + job.ID}
						owned = append(owned, "env:"+item.EnvLists[0])
					}
				}
				if !reflect.DeepEqual(old.Mounts, job.draft.Mounts) {
					for _, list := range item.MountLists {
						if simulationOwns(owned, "mount:"+list) {
							owned = removeString(owned, "mount:"+list)
						} else {
							job.Retained = append(job.Retained, list)
						}
					}
					item.MountLists = []string{}
					if len(job.draft.Mounts) > 0 {
						item.MountLists = []string{"rosboard-mount-" + job.ID}
						owned = append(owned, "mount:"+item.MountLists[0])
					}
				}
				if !reflect.DeepEqual(old.Ports, job.draft.Ports) {
					remaining := []string{}
					for _, resource := range owned {
						if !strings.HasPrefix(resource, "nat:") {
							remaining = append(remaining, resource)
						}
					}
					owned = remaining
					for i := range job.draft.Ports {
						owned = append(owned, fmt.Sprintf("nat:%s-%d", job.ID, i))
					}
				}
				m.resources[job.DeviceID][item.ID] = owned
			case "delete":
				job.Retained = append(job.Retained, item.Config.RootDir)
				owned := m.resources[job.DeviceID][item.ID]
				if len(item.SharedVETH) > 0 || !simulationOwns(owned, "veth:"+item.Network.VETH) {
					job.Retained = append(job.Retained, "veth:"+item.Network.VETH)
				} else {
					s.Options.Interfaces = removeString(s.Options.Interfaces, item.Network.VETH)
					s.Options.UsedIPs = removeString(s.Options.UsedIPs, item.Network.Address)
				}
				for _, list := range item.EnvLists {
					if !simulationOwns(owned, "env:"+list) {
						job.Retained = append(job.Retained, list)
					}
				}
				for _, list := range item.MountLists {
					if !simulationOwns(owned, "mount:"+list) {
						job.Retained = append(job.Retained, list)
					}
				}
				delete(m.resources[job.DeviceID], item.ID)
				s.Items = append(s.Items[:i], s.Items[i+1:]...)
			}
			break
		}
	}
	// Recompute relationships after deletion/edit rather than keep stale sharing.
	for i := range s.Items {
		s.Items[i].SharedVETH = []string{}
		for j := range s.Items {
			if i != j && s.Items[i].Network.VETH == s.Items[j].Network.VETH {
				s.Items[i].SharedVETH = append(s.Items[i].SharedVETH, s.Items[j].Name)
			}
		}
	}
	m.devices[job.DeviceID] = s
}
func simulationOwns(resources []string, value string) bool {
	for _, resource := range resources {
		if resource == value {
			return true
		}
	}
	return false
}
func removeString(values []string, value string) []string {
	result := []string{}
	for _, v := range values {
		if v != value {
			result = append(result, v)
		}
	}
	return result
}

func TestContainerPreview(t *testing.T) {
	if os.Getenv("ROSBOARD_CONTAINER_PREVIEW") != "1" {
		t.Skip("interactive preview only")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:8099")
	if err != nil {
		t.Fatal(err)
	}
	t.Log("Container simulation listening on 127.0.0.1:8099 (test-only, no RouterOS credentials)")
	t.Fatal(http.Serve(listener, newSimulation()))
}
