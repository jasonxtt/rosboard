package containers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"rosboard/internal/config"
	"rosboard/internal/routeros"
)

type NativeWriter interface {
	ContainerCommand(context.Context, routeros.ContainerMenu, string, routeros.RouterOSFields) error
	RemoveDirectory(context.Context, string) error
	RenameDirectory(context.Context, string, string) error
}
type ActionRequest struct {
	Action    string `json:"action"`
	TargetID  string `json:"targetId"`
	RequestID string `json:"requestId"`
	Draft     Draft  `json:"draft"`
}
type nativeStep struct {
	Menu    routeros.ContainerMenu
	Command string
	Match   map[string]string
	Fields  map[string]string
	Done    bool
	Guard   map[string]string
}
type nativeRecord struct {
	Job       Job
	Request   ActionRequest
	Signature string
	Steps     []nativeStep
	Pending   bool
}
type nativeState struct {
	Latest   string
	Jobs     map[string]*nativeRecord
	Archives []ImageArchive
}
type nativeDevice struct {
	mu      sync.Mutex
	state   nativeState
	file    string
	release func()
	running bool
}
type nativeRuntime struct {
	mu      sync.Mutex
	devices map[string]*nativeDevice
	gate    *routeros.DeviceWriteGate
}

func nativeKey(d config.DeviceConfig) string {
	sum := sha256.Sum256([]byte(d.ID + "\x00" + d.RouterOS.BaseURL + "\x00" + d.RouterOS.Username + "\x00" + d.RouterOS.Password))
	return hex.EncodeToString(sum[:])
}
func (s *Service) nativeDevice(d config.DeviceConfig) (*nativeDevice, error) {
	if !d.ContainerWrites || s.NativeStateDir == "" {
		return nil, errors.New("container writes are disabled")
	}
	s.native.mu.Lock()
	defer s.native.mu.Unlock()
	if s.native.devices == nil {
		s.native.devices = map[string]*nativeDevice{}
		s.native.gate = s.NativeGate
		if s.native.gate == nil {
			s.native.gate = routeros.NewDeviceWriteGate()
		}
	}
	key := nativeKey(d)
	if n := s.native.devices[key]; n != nil {
		return n, nil
	}
	dir := filepath.Join(s.NativeStateDir, "containers")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	n := &nativeDevice{file: filepath.Join(dir, key+".json"), state: nativeState{Jobs: map[string]*nativeRecord{}, Archives: []ImageArchive{}}}
	data, err := os.ReadFile(n.file)
	if err == nil {
		if err = json.Unmarshal(data, &n.state); err != nil || n.state.Jobs == nil {
			return nil, errors.New("invalid container operation journal")
		}
		for _, r := range n.state.Jobs {
			if r.Job.State == "running" || r.Job.State == "queued" {
				r.Job.State = "unknown"
				r.Job.Phase = "服务重启，请回读恢复"
			}
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if n.state.Latest == "" {
		for id, r := range n.state.Jobs {
			if r.Job.State == "failed" {
				n.state.Latest = id
			}
		}
	}
	if activeRecord(n) != nil {
		release, ok := s.native.gate.TryAcquire(d.ID)
		if !ok {
			return nil, errors.New("unresolved container job cannot acquire device gate")
		}
		n.release = release
	}
	s.native.devices[key] = n
	return n, nil
}

// Journal is private, atomically replaced and synced before every side effect.
func (n *nativeDevice) save() error {
	data, err := json.Marshal(n.state)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(n.file), ".journal-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err = file.Chmod(0600); err == nil {
		_, err = file.Write(data)
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(file.Name(), n.file); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(n.file))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
func cloneJob(j Job) Job {
	j.Retained = append([]string{}, j.Retained...)
	j.Warnings = append([]string{}, j.Warnings...)
	return j
}
func activeRecord(n *nativeDevice) *nativeRecord {
	for _, r := range n.state.Jobs {
		if r.Job.State == "queued" || r.Job.State == "running" || r.Job.State == "unknown" {
			return r
		}
	}
	return nil
}
func (s *Service) decorateNative(d config.DeviceConfig, snapshot *Snapshot) error {
	snapshot.Capabilities.DirectoryWrites = snapshot.Capabilities.Supported && d.ContainerWrites && s.DirectoryWriterFor != nil
	if !d.ContainerWrites {
		return nil
	}
	n, err := s.nativeDevice(d)
	if err != nil {
		return err
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	snapshot.Options.Archives = append([]ImageArchive{}, n.state.Archives...)
	snapshot.Capabilities.Writes = snapshot.Capabilities.Supported && s.NativeWriterFor != nil
	snapshot.Capabilities.Mode = "native"
	if r := activeRecord(n); r != nil {
		j := cloneJob(r.Job)
		snapshot.ActiveJob = &j
	} else if r := n.state.Jobs[n.state.Latest]; r != nil {
		j := cloneJob(r.Job)
		snapshot.ActiveJob = &j
	}
	return nil
}
func (s *Service) fresh(ctx context.Context, d config.DeviceConfig) (Snapshot, error) {
	snap, err := s.readSnapshot(ctx, d)
	if err != nil {
		return snap, err
	}
	if err = s.decorateNative(d, &snap); err != nil {
		return Snapshot{}, err
	}
	return snap, nil
}
func managedComment(d config.DeviceConfig, token string) string {
	sum := sha256.Sum256([]byte(d.ID))
	return "rosboard-container:" + hex.EncodeToString(sum[:8]) + ":" + token
}
func ownedComment(d config.DeviceConfig, comment string) bool {
	return strings.HasPrefix(comment, managedComment(d, ""))
}
func operationError(status int, code, message string) error {
	return &DirectoryError{Status: status, Code: code, Message: message}
}

func (s *Service) Submit(ctx context.Context, d config.DeviceConfig, gate *routeros.DeviceWriteGate, req ActionRequest) (Job, error) {
	n, err := s.nativeDevice(d)
	if err != nil {
		return Job{}, err
	}
	if !namePattern.MatchString(req.RequestID) {
		return Job{}, operationError(400, "request_id_required", "请求标识无效")
	}
	raw, _ := json.Marshal(req)
	sum := sha256.Sum256(raw)
	sig := hex.EncodeToString(sum[:])
	n.mu.Lock()
	for _, r := range n.state.Jobs {
		if r.Request.RequestID == req.RequestID {
			jobID := r.Job.ID
			n.mu.Unlock()
			if r.Signature != sig {
				return Job{}, operationError(409, "request_reused", "相同请求标识不能用于不同操作")
			}
			return s.NativeJob(d, jobID)
		}
	}
	if activeRecord(n) != nil {
		n.mu.Unlock()
		return Job{}, operationError(409, "device_busy", "先完成或恢复当前设备任务")
	}
	if gate == nil {
		gate = s.native.gate
	}
	release, ok := gate.TryAcquire(d.ID)
	if !ok {
		n.mu.Unlock()
		return Job{}, operationError(409, "device_busy", "设备正在写入，请稍后重试")
	}
	n.release = release
	n.mu.Unlock()
	// Validate a fresh snapshot while holding the same gate as policy/directory writes.
	snap, err := s.fresh(ctx, d)
	if err != nil {
		release()
		return Job{}, err
	}
	id := uuid.NewString()
	steps, retained, err := s.plan(ctx, d, req, snap, id)
	if err != nil {
		release()
		return Job{}, err
	}
	r := &nativeRecord{Job: Job{ID: id, DeviceID: d.ID, Action: req.Action, TargetID: req.TargetID, State: "queued", Phase: "已校验，等待写入", Retained: retained}, Request: req, Signature: sig, Steps: steps}
	n.mu.Lock()
	n.state.Jobs[id] = r
	n.state.Latest = id
	if err = n.save(); err != nil {
		delete(n.state.Jobs, id)
		n.mu.Unlock()
		release()
		return Job{}, err
	}
	n.running = true
	j := cloneJob(r.Job)
	n.mu.Unlock()
	go s.executeNative(d, n, r)
	return j, nil
}
func (s *Service) NativeJob(d config.DeviceConfig, id string) (Job, error) {
	n, err := s.nativeDevice(d)
	if err != nil {
		return Job{}, err
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	r := n.state.Jobs[id]
	if r == nil {
		return Job{}, operationError(404, "job_not_found", "任务不属于当前设备")
	}
	return cloneJob(r.Job), nil
}
func (s *Service) RecoverNative(d config.DeviceConfig, gate *routeros.DeviceWriteGate, id string) (Job, error) {
	n, err := s.nativeDevice(d)
	if err != nil {
		return Job{}, err
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	r := n.state.Jobs[id]
	if r == nil {
		return Job{}, operationError(404, "job_not_found", "任务不属于当前设备")
	}
	if n.running || (r.Job.State != "unknown" && r.Job.State != "failed") || (activeRecord(n) != nil && activeRecord(n) != r) {
		return Job{}, operationError(409, "recovery_not_required", "当前任务无需恢复")
	}
	newlyLocked := false
	defer func() {
		if newlyLocked && !n.running && n.release != nil {
			n.release()
			n.release = nil
		}
	}()
	if n.release == nil {
		if gate == nil {
			gate = s.native.gate
		}
		release, ok := gate.TryAcquire(d.ID)
		if !ok {
			return Job{}, operationError(409, "device_busy", "设备正在写入")
		}
		n.release = release
		newlyLocked = true
	}
	if r.Job.State == "failed" {
		for i, step := range r.Steps {
			if step.Done {
				continue
			}
			if step.Menu == routeros.ContainerList && step.Command == "add" && !r.Pending {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				files, readErr := s.ReaderFor(d).ContainerRead(ctx, routeros.ContainerFiles)
				cancel()
				if readErr != nil {
					return Job{}, readErr
				}
				root, _ := DirectoryPath(step.Fields["root-dir"])
				for _, file := range files {
					p, _ := DirectoryPath(file["name"])
					if strings.HasPrefix(p, root+"/") {
						return Job{}, operationError(409, "root_not_empty", "运行目录非空，不能重新导入")
					}
				}
				for _, file := range files {
					p, _ := DirectoryPath(file["name"])
					if p == root && file["type"] == "directory" {
						prep := nativeStep{Menu: routeros.ContainerFiles, Command: "remove-empty-root", Match: map[string]string{".id": file[".id"], "name": file["name"], "type": "directory"}, Fields: map[string]string{}}
						r.Steps = append(r.Steps[:i], append([]nativeStep{prep}, r.Steps[i:]...)...)
						break
					}
				}
			}
			break
		}
	}
	r.Job.Error = ""

	n.running = true
	r.Job.State = "running"
	r.Job.Phase = "回读恢复，不重复写入"
	if err = n.save(); err != nil {
		n.running = false
		r.Job.State = "unknown"
		return Job{}, err
	}
	j := cloneJob(r.Job)
	go s.executeNative(d, n, r)
	return j, nil
}
func matchFields(row routeros.RouterOSObject, fields map[string]string) bool {
	for k, v := range fields {
		if !equivalent(k, row[k], v) {
			return false
		}
	}
	return true
}
func equivalent(k, actual, wanted string) bool {
	if actual == wanted {
		return true
	}
	if k == "logging" || k == "start-on-boot" {
		return isTrue(actual) == isTrue(wanted)
	}
	if k == "mac-address" {
		return strings.EqualFold(actual, wanted)
	}
	if k == "cpu-list" {
		converted, err := routeros.ContainerCPUList(wanted)
		return err == nil && actual == converted
	}
	if k == "memory-high" || k == "memory-max" {
		if wanted == "" && actual == "unlimited" {
			return true
		}
		parse := func(v string) int64 {
			for _, suffix := range []string{"KiB", "MiB", "GiB", "K", "M", "G", "k", "m", "g"} {
				if strings.HasSuffix(v, suffix) {
					n, _ := strconv.ParseInt(strings.TrimSuffix(v, suffix), 10, 64)
					factor := int64(1024)
					if strings.HasPrefix(strings.ToUpper(suffix), "M") {
						factor = 1 << 20
					}
					if strings.HasPrefix(strings.ToUpper(suffix), "G") {
						factor = 1 << 30
					}
					return n * factor
				}
			}
			n, _ := strconv.ParseInt(v, 10, 64)
			return n
		}
		return parse(actual) > 0 && parse(actual) == parse(wanted)
	}
	if k == "file" || k == "src" || k == "root-dir" {
		return strings.TrimPrefix(actual, "/") == strings.TrimPrefix(wanted, "/")
	}
	return false
}
func findRows(rows []routeros.RouterOSObject, fields map[string]string) []routeros.RouterOSObject {
	matches := []routeros.RouterOSObject{}
	for _, row := range rows {
		if matchFields(row, fields) {
			matches = append(matches, row)
		}
	}
	return matches
}
func desired(step nativeStep, rows []routeros.RouterOSObject) (bool, string) {
	if step.Command == "retain-root" {
		target := findRows(rows, map[string]string{"name": step.Fields["name"]})
		return len(target) == 1 && (target[0]["type"] == "container store" || target[0]["type"] == "directory") && len(findRows(rows, map[string]string{"name": step.Match["name"]})) == 0, ""
	}
	selector := step.Match
	if step.Command == "set" {
		selector = map[string]string{}
		for k, v := range step.Match {
			if _, changed := step.Fields[k]; !changed {
				selector[k] = v
			}
		}
	}
	matches := findRows(rows, selector)
	if step.Command == "remove" || step.Command == "remove-empty-root" {
		return len(matches) == 0, ""
	}
	if len(matches) != 1 {
		return false, ""
	}
	row := matches[0]
	switch step.Command {
	case "unset":
		_, overridden := row[step.Fields["value-name"]]
		return !overridden, row[".id"]
	case "start":
		return containerStatus(row) == "running", row[".id"]
	case "stop":
		return containerStatus(row) == "stopped", row[".id"]
	case "add", "set":
		return matchFields(row, step.Fields), row[".id"]
	}
	return false, ""
}
func (s *Service) executeNative(d config.DeviceConfig, n *nativeDevice, r *nativeRecord) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	failure := false
	unknown := false
	defer func() {
		if !unknown {
			s.stopRegistryDNS(d, r.Job.ID)
		}
		n.mu.Lock()
		defer n.mu.Unlock()
		n.running = false
		if failure {
			if unknown {
				r.Job.State = "unknown"
				r.Job.Error = "写入结果尚未确认。请回读恢复，勿重复提交。"
			} else {
				r.Job.State = "failed"
				r.Job.Error = "操作失败；已完成的对象和数据已保留，请查看设备日志。"
			}
		} else {
			r.Job.State = "succeeded"
			r.Job.Phase = "写入及回读确认完成"
			r.Job.Progress = 100
		}
		if err := n.save(); err != nil {
			r.Job.State = "unknown"
			r.Job.Error = "操作日志保存失败，请回读核对"
			unknown = true
		}
		if !unknown && n.release != nil {
			n.release()
			n.release = nil
		}
		// Invalidate even on partial failures; the next list must reflect the device.
		s.mu.Lock()
		delete(s.cache, sha256.Sum256([]byte(d.ID+"\x00"+d.RouterOS.BaseURL+"\x00"+d.RouterOS.Username+"\x00"+d.RouterOS.Password)))
		s.mu.Unlock()
	}()
	reader := s.ReaderFor(d)
	writer := s.NativeWriterFor(d)
	for i := range r.Steps {
		n.mu.Lock()
		step := r.Steps[i]
		pending := r.Pending
		done := step.Done
		n.mu.Unlock()
		if done {
			continue
		}
		if step.Menu == routeros.ContainerList && step.Command == "add" && step.Fields["remote-image"] != "" && step.Fields["file"] == "" {
			n.mu.Lock()
			r.Job.Phase = "准备镜像域名解析"
			n.mu.Unlock()
			s.beginRegistryDNS(d, n, r, step)
		}
		rows, err := reader.ContainerRead(ctx, step.Menu)
		if err != nil {
			failure = true
			unknown = pending
			return
		}

		// Recheck current references immediately before deleting an owned auxiliary
		// object. An operator may have started sharing it after planning.
		if step.Command == "remove" && step.Menu != routeros.ContainerList {
			objects := findRows(rows, step.Match)
			if len(objects) == 1 {
				shared := false
				current, readErr := reader.ContainerRead(ctx, routeros.ContainerList)
				if readErr != nil {
					failure = true
					unknown = pending
					return
				}
				object := objects[0]
				for _, container := range current {
					if (step.Menu == routeros.ContainerVETH && container["interface"] == object["name"]) || (step.Menu == routeros.ContainerBridgePort && container["interface"] == object["interface"]) || (step.Menu == routeros.ContainerEnvs && contains(splitLists(first(container["envlists"], container["envlist"])), object["list"])) || (step.Menu == routeros.ContainerMounts && contains(splitLists(first(container["mountlists"], container["mounts"])), first(object["list"], object["name"]))) {
						shared = true
					}
				}
				if shared {
					n.mu.Lock()
					r.Job.Retained = append(r.Job.Retained, string(step.Menu)+":"+object[".id"])
					r.Steps[i].Done = true
					r.Pending = false
					err = n.save()
					n.mu.Unlock()
					if err != nil {
						failure = true
						unknown = true
						return
					}
					continue
				}
			}
		}
		confirmed, target := desired(step, rows)
		if pending {
			if !confirmed {
				failure = true
				unknown = true
				return
			}
		} else {
			targets := findRows(rows, step.Match)
			// Creating or setting a previously satisfied desired state is idempotent.
			if !confirmed {
				if step.Command == "add" && len(targets) != 0 || step.Command != "add" && len(targets) != 1 {
					failure = true
					return
				}
				if step.Command == "retain-root" {
					if err := s.checkRootRetention(ctx, d, step); err != nil {
						failure = true
						return
					}
				}
				fields := routeros.RouterOSFields{}
				for k, v := range step.Fields {
					fields[k] = v
				}
				if step.Command != "add" {
					fields["numbers"] = targets[0][".id"]
				}
				n.mu.Lock()
				r.Pending = true
				r.Job.State = "running"
				r.Job.Phase = fmt.Sprintf("%s / %s · %d/%d", step.Menu, step.Command, i+1, len(r.Steps))
				r.Job.Progress = i * 90 / len(r.Steps)
				err = n.save()
				n.mu.Unlock()
				if err != nil {
					failure = true
					unknown = true
					return
				}
				if step.Command == "retain-root" {
					err = writer.RenameDirectory(ctx, targets[0][".id"], step.Fields["name"])
				} else if step.Command == "remove-empty-root" {
					root, _ := DirectoryPath(targets[0]["name"])
					for _, file := range rows {
						p, _ := DirectoryPath(file["name"])
						if strings.HasPrefix(p, root+"/") {
							failure = true
							n.mu.Lock()
							r.Pending = false
							n.mu.Unlock()
							return
						}
					}
					containers, e := reader.ContainerRead(ctx, routeros.ContainerList)
					if e != nil {
						failure = true
						unknown = true
						return
					}
					for _, container := range containers {
						p, _ := DirectoryPath(container["root-dir"])
						if p != "/" && (p == root || strings.HasPrefix(root, p+"/") || strings.HasPrefix(p, root+"/")) {
							failure = true
							n.mu.Lock()
							r.Pending = false
							n.mu.Unlock()
							return
						}
					}
					mounts, e := reader.ContainerRead(ctx, routeros.ContainerMounts)
					if e != nil {
						failure = true
						unknown = true
						return
					}
					for _, mount := range mounts {
						p, _ := DirectoryPath(mount["src"])
						if p != "/" && (p == root || strings.HasPrefix(root, p+"/") || strings.HasPrefix(p, root+"/")) {
							failure = true
							n.mu.Lock()
							r.Pending = false
							n.mu.Unlock()
							return
						}
					}
					err = writer.RemoveDirectory(ctx, targets[0][".id"])
				} else {
					err = writer.ContainerCommand(ctx, step.Menu, step.Command, fields)
				}
				if err != nil {
					var outcome *routeros.MutationOutcomeUnknownError
					if !errors.As(err, &outcome) {
						n.mu.Lock()
						r.Pending = false
						n.mu.Unlock()
						failure = true
						return
					}
				}
				// Poll reads only; never retransmit commands, even after a transport failure.
				for attempt := 0; attempt < 180; attempt++ {
					rows, readErr := reader.ContainerRead(ctx, step.Menu)
					if readErr == nil {
						confirmed, target = desired(step, rows)
						if confirmed {
							break
						}
						if step.Menu == routeros.ContainerList {
							for _, row := range findRows(rows, step.Match) {
								if containerStatus(row) == "error" {
									failure = true
									n.mu.Lock()
									r.Pending = false
									n.mu.Unlock()
									return
								}
							}
						}
					}
					select {
					case <-ctx.Done():
						failure = true
						unknown = true
						return
					case <-time.After(time.Second):
					}
				}
				if !confirmed {
					failure = true
					unknown = true
					return
				}
			}
		}
		// A container add is confirmed only once extraction is complete.
		if step.Menu == routeros.ContainerList && step.Command == "add" {
			for attempt := 0; attempt < 360; attempt++ {
				rows, err = reader.ContainerRead(ctx, step.Menu)
				if err != nil {
					failure = true
					unknown = true
					return
				}
				matches := findRows(rows, step.Match)
				if len(matches) != 1 {
					failure = true
					unknown = true
					return
				}
				status := containerStatus(matches[0])
				if status == "error" {
					failure = true
					n.mu.Lock()
					r.Pending = false
					n.mu.Unlock()
					return
				}
				if status == "stopped" || status == "running" {
					break
				}
				if attempt == 359 {
					failure = true
					unknown = true
					return
				}
				select {
				case <-ctx.Done():
					failure = true
					unknown = true
					return
				case <-time.After(time.Second):
				}
			}
		}
		n.mu.Lock()
		r.Steps[i].Done = true
		r.Pending = false
		if step.Menu == routeros.ContainerList && step.Command == "add" {
			r.Job.TargetID = target
		}
		err = n.save()
		n.mu.Unlock()
		if err != nil {
			failure = true
			unknown = true
			return
		}
	}
}
func addStep(menu routeros.ContainerMenu, fields map[string]string) nativeStep {
	match := map[string]string{"comment": fields["comment"]}
	if fields["name"] != "" {
		match["name"] = fields["name"]
	}
	if fields["key"] != "" {
		match["key"] = fields["key"]
	}
	if fields["dst"] != "" {
		match["dst"] = fields["dst"]
	}
	return nativeStep{Menu: menu, Command: "add", Match: match, Fields: fields}
}
func actionStep(command string, match map[string]string) nativeStep {
	return nativeStep{Menu: routeros.ContainerList, Command: command, Match: match, Fields: map[string]string{}}
}
func (s *Service) plan(ctx context.Context, d config.DeviceConfig, req ActionRequest, snap Snapshot, token string) ([]nativeStep, []string, error) {
	steps := []nativeStep{}
	retained := []string{}
	var selected *Item
	for i := range snap.Items {
		if snap.Items[i].ID == req.TargetID {
			selected = &snap.Items[i]
		}
	}
	if req.Action != "create" && selected == nil {
		return nil, nil, operationError(404, "container_not_found", "容器不存在")
	}
	rows, err := s.ReaderFor(d).ContainerRead(ctx, routeros.ContainerList)
	if err != nil {
		return nil, nil, err
	}
	var original routeros.RouterOSObject
	if selected != nil {
		for _, row := range rows {
			if row[".id"] == selected.ID {
				original = row
			}
		}
		if original == nil {
			return nil, nil, operationError(409, "container_changed", "容器已变更，请刷新")
		}
		if req.Action != "adopt" && !ownedComment(d, original["comment"]) {
			return nil, nil, operationError(409, "adoption_required", "请先接管此容器")
		}
	}
	match := map[string]string{}
	if selected != nil {
		match = map[string]string{".id": selected.ID, "name": original["name"], "root-dir": original["root-dir"], "interface": original["interface"], "comment": original["comment"]}
	}
	switch req.Action {
	case "adopt":
		if selected.Ownership == "managed" {
			return nil, nil, operationError(409, "already_managed", "此容器已接管")
		}
		// Adoption changes only the container comment; related objects keep ownership.
		steps = append(steps, nativeStep{Menu: routeros.ContainerList, Command: "set", Match: match, Fields: map[string]string{"comment": managedComment(d, token) + " " + original["comment"]}})
	case "start", "stop":
		steps = append(steps, actionStep(req.Action, match))
	case "restart":
		steps = append(steps, actionStep("stop", match), actionStep("start", match))
	case "delete":
		if selected.Config.ArchiveFile != "" {
			retained = append(retained, selected.Config.ArchiveFile)
		}
		for _, m := range selected.Config.Mounts {
			retained = append(retained, m.Source)
		}
		if selected.Status == "running" || selected.Status == "starting" || selected.Status == "stopping" {
			steps = append(steps, actionStep("stop", match))
		}
		retain, retainedPath, err := s.planRootRetention(ctx, d, match, token)
		if err != nil {
			return nil, nil, err
		}
		if retain != nil {
			steps = append(steps, *retain)
			retained = append(retained, retainedPath)
		}
		steps = append(steps, actionStep("remove", match))
		// Only dedicated objects bearing this container's exact creation marker may
		// be removed. Adoption never marks its network, env or mounts as owned.
		owner := strings.Fields(original["comment"])[0]
		lineage := s.containerLineage(d, selected.ID, owner)
		for _, menu := range []routeros.ContainerMenu{routeros.ContainerBridgePort, routeros.ContainerVETH, routeros.ContainerEnvs, routeros.ContainerMounts} {
			objects, e := s.ReaderFor(d).ContainerRead(ctx, menu)
			if e != nil {
				return nil, nil, e
			}
			for _, row := range objects {
				belongs := row["comment"] == owner
				if menu == routeros.ContainerEnvs {
					belongs = lineage[row["comment"]]
				}
				if menu == routeros.ContainerMounts {
					belongs = lineage[row["comment"]]
				}
				if !belongs {
					continue
				}
				shared := false
				for _, other := range snap.Items {
					if other.ID == selected.ID {
						continue
					}
					if (menu == routeros.ContainerVETH || menu == routeros.ContainerBridgePort) && other.Network.VETH == selected.Network.VETH {
						shared = true
					}
					if menu == routeros.ContainerEnvs && contains(other.EnvLists, row["list"]) {
						shared = true
					}
					if menu == routeros.ContainerMounts && contains(other.MountLists, first(row["list"], row["name"])) {
						shared = true
					}
				}
				if shared {
					retained = append(retained, string(menu)+":"+row[".id"])
					continue
				}
				steps = append(steps, nativeStep{Menu: menu, Command: "remove", Match: map[string]string{".id": row[".id"], "comment": row["comment"]}, Fields: map[string]string{}})
			}
		}
	case "create", "edit", "update":
		draft := req.Draft
		if req.Action == "create" {
			draft.ExistingID = ""
		} else {
			draft.ExistingID = selected.ID
		}
		if req.Action == "update" {
			draft = selected.Config
			draft.StartAfterCreate = selected.Status == "running"
		}
		resolved := Resolve(draft, snap)
		if len(resolved.Errors) != 0 {
			return nil, nil, operationError(400, "invalid_draft", "配置校验未通过，请检查必填项")
		}
		e := resolved.Effective
		fields := resolved.ContainerFields
		if f := fields["file"]; f != "" {
			fields["file"] = strings.TrimPrefix(f, "/")
		}
		marker := managedComment(d, token)
		if req.Action == "create" {
			root, _ := DirectoryPath(e.RootDir)
			for _, item := range snap.Items {
				old, _ := DirectoryPath(item.Config.RootDir)
				if old != "/" && (root == old || strings.HasPrefix(root, old+"/") || strings.HasPrefix(old, root+"/")) {
					return nil, nil, operationError(409, "root_in_use", "运行目录与已有容器重叠")
				}
				for _, m := range item.Config.Mounts {
					src, _ := DirectoryPath(m.Source)
					if src != "/" && (root == src || strings.HasPrefix(root, src+"/") || strings.HasPrefix(src, root+"/")) {
						return nil, nil, operationError(409, "root_in_use", "运行目录与挂载数据重叠")
					}
				}
			}
			files, e := s.ReaderFor(d).ContainerRead(ctx, routeros.ContainerFiles)
			if e != nil {
				return nil, nil, e
			}
			for _, file := range files {
				p, _ := DirectoryPath(file["name"])
				if strings.HasPrefix(p, root+"/") || p == root && file["type"] != "directory" {
					return nil, nil, operationError(409, "root_not_empty", "新容器运行目录必须为空，避免覆盖现有文件")
				}
			}
		}
		if req.Action == "create" {
			files, err := s.ReaderFor(d).ContainerRead(ctx, routeros.ContainerFiles)
			if err != nil {
				return nil, nil, err
			}
			for _, file := range files {
				p, _ := DirectoryPath(file["name"])
				root, _ := DirectoryPath(e.RootDir)
				if p == root {
					steps = append(steps, nativeStep{Menu: routeros.ContainerFiles, Command: "remove-empty-root", Match: map[string]string{".id": file[".id"], "name": file["name"], "type": "directory"}, Fields: map[string]string{}})
				}
			}
		}

		if req.Action == "edit" {
			if e.RootDir != selected.Config.RootDir || e.ImageSource != selected.Config.ImageSource || e.Image != selected.Config.Image || e.ArchiveFile != selected.Config.ArchiveFile {
				return nil, nil, operationError(409, "image_update_required", "运行目录和镜像不能通过编辑修改，请使用镜像更新")
			}
			if !reflect.DeepEqual(e.Network, selected.Network) {
				if len(selected.SharedVETH) > 0 || e.Network.VETH != selected.Network.VETH {
					return nil, nil, operationError(409, "network_recreate_required", "共享网络或 VETH 名称不可修改")
				}
				objects, readErr := s.ReaderFor(d).ContainerRead(ctx, routeros.ContainerVETH)
				if readErr != nil {
					return nil, nil, readErr
				}
				veths := findRows(objects, map[string]string{"name": selected.Network.VETH})
				if len(veths) != 1 || veths[0]["comment"] != strings.Fields(original["comment"])[0] {
					return nil, nil, operationError(409, "network_not_owned", "已有网络保持原归属，不能修改")
				}
				address := e.Network.Address
				if e.Network.Address6 != "" {
					address += "," + e.Network.Address6
				}
				vf := map[string]string{"address": address, "gateway": e.Network.Gateway, "gateway6": e.Network.Gateway6}
				if e.Network.MAC != "" {
					vf["mac-address"] = e.Network.MAC
				}
				// Network changes happen after the stop step below.
				steps = append(steps, nativeStep{Menu: routeros.ContainerVETH, Command: "set", Match: map[string]string{".id": veths[0][".id"], "comment": veths[0]["comment"]}, Fields: vf})
				if e.Network.Bridge != selected.Network.Bridge {
					ports, readErr := s.ReaderFor(d).ContainerRead(ctx, routeros.ContainerBridgePort)
					if readErr != nil {
						return nil, nil, readErr
					}
					matches := findRows(ports, map[string]string{"interface": selected.Network.VETH, "comment": veths[0]["comment"]})
					if len(matches) != 1 {
						return nil, nil, operationError(409, "network_not_owned", "bridge port 归属无法确认")
					}
					steps = append(steps, nativeStep{Menu: routeros.ContainerBridgePort, Command: "set", Match: map[string]string{".id": matches[0][".id"], "comment": veths[0]["comment"]}, Fields: map[string]string{"bridge": e.Network.Bridge}})
				}
			}
			delete(fields, "remote-image")
			delete(fields, "file")
			delete(fields, "root-dir")
			delete(fields, "interface")
			for k, v := range map[string]string{"cmd": e.Command, "entrypoint": e.Entrypoint, "user": e.User, "workdir": e.Workdir, "cpu-list": e.CPUList, "memory-high": e.MemoryHigh, "memory-max": e.MemoryMax} {
				fields[k] = v
			}
			// RouterOS cannot unset these integer fields: an empty set becomes
			// unlimited, not the current creation defaults. Resolve inheritance
			// explicitly when replacing existing per-container limits.
			fields["memory-high"] = first(e.MemoryHigh, first(snap.Options.MemoryHigh, "unlimited"))
			fields["memory-max"] = first(e.MemoryMax, first(snap.Options.MemoryMax, "unlimited"))
			// Empty strings override image defaults on RouterOS. Remove the
			// override with unset, whose outcome is observable as field absence.
			for _, key := range []string{"cmd", "entrypoint", "user", "workdir"} {
				if fields[key] == "" {
					delete(fields, key)
					if _, overridden := original[key]; overridden {
						steps = append(steps, nativeStep{Menu: routeros.ContainerList, Command: "unset", Match: match, Fields: map[string]string{"value-name": key}})
					}
				}
			}
			if e.Health.Mode == "inherit" {
				fields["healthcheck-cmd"] = ""
			}
			if selected.Status == "running" {
				steps = append([]nativeStep{actionStep("stop", match)}, steps...)
			}
		} else if req.Action == "create" {
			n := e.Network
			address := n.Address
			if n.Address6 != "" {
				address += "," + n.Address6
			}
			vf := map[string]string{"name": n.VETH, "address": address, "gateway": n.Gateway, "comment": marker}
			if n.Gateway6 != "" {
				vf["gateway6"] = n.Gateway6
			}
			if n.MAC != "" {
				vf["mac-address"] = n.MAC
			}
			steps = append(steps, addStep(routeros.ContainerVETH, vf), addStep(routeros.ContainerBridgePort, map[string]string{"interface": n.VETH, "bridge": n.Bridge, "comment": marker}))
		} else {
			// Stage an independent rootfs while the old container remains usable.
			root, _ := DirectoryPath(e.RootDir)
			fields["root-dir"] = path.Join(path.Dir(root), "rootfs-update-"+token[:8])
			fields["name"] = e.Name + "-update-" + token[:8]
			if len(fields["name"]) > 63 {
				fields["name"] = e.Name[:40] + "-update-" + token[:8]
			}
		}
		list := "rb-" + strings.ReplaceAll(token, "-", "")[:20]
		for _, env := range e.Env {
			steps = append(steps, addStep(routeros.ContainerEnvs, map[string]string{"list": list, "key": env.Key, "value": env.Value, "comment": marker}))
		}
		for _, m := range e.Mounts {
			mode := m.Mode
			if mode == "" {
				mode = "rw"
				if m.ReadOnly {
					mode = "ro"
				}
			}
			steps = append(steps, addStep(routeros.ContainerMounts, map[string]string{"list": list, "src": strings.TrimPrefix(m.Source, "/"), "dst": m.Target, "mode": mode, "comment": marker}))
			retained = append(retained, m.Source)
		}
		fields["envlists"] = ""
		fields["mountlists"] = ""
		if len(e.Env) > 0 {
			fields["envlists"] = list
		}
		if len(e.Mounts) > 0 {
			fields["mountlists"] = list
		}
		if req.Action == "edit" {
			retained = append(retained, selected.EnvLists...)
			retained = append(retained, selected.MountLists...)
			// Old lists are intentionally retained; shared lists are never overwritten.
			steps = append(steps, nativeStep{Menu: routeros.ContainerList, Command: "set", Match: match, Fields: fields})
			if selected.Status == "running" {
				steps = append(steps, actionStep("start", map[string]string{".id": selected.ID, "root-dir": original["root-dir"], "interface": original["interface"]}))
			}
		} else {
			fields["comment"] = marker
			steps = append(steps, addStep(routeros.ContainerList, fields))
			newMatch := map[string]string{"name": fields["name"], "comment": marker}
			if req.Action == "update" {
				steps = append(steps, actionStep("stop", match))
			}
			if e.StartAfterCreate {
				steps = append(steps, actionStep("start", newMatch))
			}
			if req.Action == "update" {
				retain, retainedPath, err := s.planRootRetention(ctx, d, match, token)
				if err != nil {
					return nil, nil, err
				}
				if retain != nil {
					steps = append(steps, *retain)
					retained = append(retained, retainedPath)
				}
				steps = append(steps, actionStep("remove", match), nativeStep{Menu: routeros.ContainerList, Command: "set", Match: map[string]string{"root-dir": fields["root-dir"], "interface": fields["interface"], "comment": marker}, Fields: map[string]string{"name": selected.Name, "comment": original["comment"]}})
			}
		}
	default:
		return nil, nil, operationError(400, "invalid_action", "不支持的操作")
	}
	return steps, retained, nil
}

// RouterOS removes root-dir implicitly with the container. Preserve the complete
// stopped filesystem under a unique sibling name before issuing container/remove.
func (s *Service) planRootRetention(ctx context.Context, d config.DeviceConfig, guard map[string]string, token string) (*nativeStep, string, error) {
	root, err := DirectoryPath(guard["root-dir"])
	if err != nil || root == "/" {
		return nil, "", nil
	}
	rows, err := s.ReaderFor(d).ContainerRead(ctx, routeros.ContainerFiles)
	if err != nil {
		return nil, "", err
	}
	matches := findRows(rows, map[string]string{"name": strings.TrimPrefix(root, "/")})
	if len(matches) == 0 {
		return nil, "", nil
	}
	if len(matches) != 1 || (matches[0]["type"] != "container store" && matches[0]["type"] != "directory") || len(strings.Split(strings.TrimPrefix(root, "/"), "/")) < 2 {
		return nil, "", operationError(409, "unsafe_root", "运行目录无法安全保留，操作已阻止")
	}
	target := strings.TrimPrefix(root, "/") + "-retained-" + token[:8]
	if len(findRows(rows, map[string]string{"name": target})) != 0 {
		return nil, "", operationError(409, "retained_root_exists", "保留目录已存在，请刷新")
	}
	step := nativeStep{Menu: routeros.ContainerFiles, Command: "retain-root", Match: map[string]string{".id": matches[0][".id"], "name": matches[0]["name"], "type": matches[0]["type"]}, Fields: map[string]string{"name": target}, Guard: guard}
	return &step, "/" + target, nil
}
func (s *Service) checkRootRetention(ctx context.Context, d config.DeviceConfig, step nativeStep) error {
	root, _ := DirectoryPath(step.Match["name"])
	overlaps := func(v string) bool {
		p, e := DirectoryPath(v)
		return e == nil && p != "/" && (p == root || strings.HasPrefix(p, root+"/") || strings.HasPrefix(root, p+"/"))
	}
	containers, err := s.ReaderFor(d).ContainerRead(ctx, routeros.ContainerList)
	if err != nil {
		return err
	}
	matches := findRows(containers, step.Guard)
	if len(matches) != 1 || !ownedComment(d, matches[0]["comment"]) || containerStatus(matches[0]) != "stopped" {
		return operationError(409, "root_in_use", "容器必须停止且归属未变更，才能保留运行目录")
	}
	for _, c := range containers {
		if c[".id"] != step.Guard[".id"] && overlaps(c["root-dir"]) {
			return operationError(409, "shared_root", "共享运行目录不能重命名，操作已阻止")
		}
	}
	mounts, err := s.ReaderFor(d).ContainerRead(ctx, routeros.ContainerMounts)
	if err != nil {
		return err
	}
	for _, m := range mounts {
		if overlaps(m["src"]) {
			return operationError(409, "mounted_root", "运行目录与挂载目录重叠，操作已阻止")
		}
	}
	files, err := s.ReaderFor(d).ContainerRead(ctx, routeros.ContainerFiles)
	if err != nil {
		return err
	}
	if len(findRows(files, map[string]string{"name": step.Fields["name"]})) != 0 {
		return operationError(409, "retained_root_exists", "保留目录已存在，操作已阻止")
	}
	return nil
}

// Only journals linking this exact container/update lineage authorize cleanup of
// obsolete lists. Device-wide markers alone never authorize another container's objects.
func (s *Service) containerLineage(d config.DeviceConfig, id, owner string) map[string]bool {
	markers := map[string]bool{owner: true}
	n, err := s.nativeDevice(d)
	if err != nil {
		return markers
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	ids := map[string]bool{id: true}
	for changed := true; changed; {
		changed = false
		for _, r := range n.state.Jobs {
			if ids[r.Job.TargetID] && r.Request.TargetID != "" && !ids[r.Request.TargetID] {
				ids[r.Request.TargetID] = true
				changed = true
			}
		}
	}
	for _, r := range n.state.Jobs {
		if ids[r.Job.TargetID] || (r.Request.TargetID != "" && ids[r.Request.TargetID]) {
			markers[managedComment(d, r.Job.ID)] = true
		}
	}
	return markers
}

// RestoreNative loads unresolved jobs before the HTTP server accepts policy or
// Files writes. A corrupt journal blocks that device until an administrator
// repairs its private state; it must never silently discard an unknown write.
func (s *Service) RestoreNative(d config.DeviceConfig) error {
	n, err := s.nativeDevice(d)
	if err == nil {
		n.mu.Lock()
		for _, r := range n.state.Jobs {
			if r.Pending && (r.Job.State == "unknown" || r.Job.State == "running") {
				for _, step := range r.Steps {
					if step.Done {
						continue
					}
					if step.Menu == routeros.ContainerList && step.Command == "add" && step.Fields["remote-image"] != "" {
						go s.beginRegistryDNS(d, n, r, step)
					}
					break
				}
			}
		}
		n.mu.Unlock()
	}
	if err != nil && s.NativeGate != nil {
		s.NativeGate.TryAcquire(d.ID)
	}
	return err
}
