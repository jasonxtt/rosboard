package containers

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"rosboard/internal/config"
	"rosboard/internal/routeros"
)

type Reader interface {
	ContainerRead(context.Context, routeros.ContainerMenu) ([]routeros.RouterOSObject, error)
}
type cachedSnapshot struct {
	snapshot Snapshot
	expires  time.Time
}
type pendingSnapshot struct{ done chan struct{} }
type Service struct {
	ReaderFor func(config.DeviceConfig) Reader
	mu        sync.Mutex
	cache     map[[32]byte]cachedSnapshot
	pending   map[[32]byte]pendingSnapshot
}

// Snapshot coalesces reads per device and caches them briefly, so live default
// previews cannot issue a dozen RouterOS requests per keystroke. Failures are
// never cached; waiting requests retain their own cancellation boundary.
func (s *Service) Snapshot(ctx context.Context, d config.DeviceConfig) (Snapshot, error) {
	key := sha256.Sum256([]byte(d.ID + "\x00" + d.RouterOS.BaseURL + "\x00" + d.RouterOS.Username + "\x00" + d.RouterOS.Password))
	for {
		s.mu.Lock()
		if entry, ok := s.cache[key]; ok && time.Now().Before(entry.expires) {
			s.mu.Unlock()
			return entry.snapshot, nil
		}
		if pending, ok := s.pending[key]; ok {
			s.mu.Unlock()
			select {
			case <-ctx.Done():
				return Snapshot{}, ctx.Err()
			case <-pending.done:
				continue
			}
		}
		if s.pending == nil {
			s.pending = map[[32]byte]pendingSnapshot{}
		}
		pending := pendingSnapshot{done: make(chan struct{})}
		s.pending[key] = pending
		s.mu.Unlock()
		result, err := s.readSnapshot(ctx, d)
		s.mu.Lock()
		if err == nil {
			if s.cache == nil {
				s.cache = map[[32]byte]cachedSnapshot{}
			}
			for k, entry := range s.cache {
				if time.Now().After(entry.expires) {
					delete(s.cache, k)
				}
			}
			s.cache[key] = cachedSnapshot{snapshot: result, expires: time.Now().Add(5 * time.Second)}
		}
		delete(s.pending, key)
		close(pending.done)
		s.mu.Unlock()
		return result, err
	}
}

func NewService() *Service {
	return &Service{ReaderFor: func(d config.DeviceConfig) Reader {
		return routeros.NewClient(d.RouterOS.BaseURL, d.RouterOS.Username, d.RouterOS.Password)
	}}
}

func (s *Service) readSnapshot(ctx context.Context, d config.DeviceConfig) (Snapshot, error) {
	result := Snapshot{Items: []Item{}, Options: Options{Bridges: []string{}, Interfaces: []string{}, UsedIPs: []string{}, Disks: []Disk{}}, Capabilities: Capabilities{Mode: "read-only", Fields: []string{}, Warnings: []string{}}}
	reader := s.ReaderFor(d)
	rows, err := reader.ContainerRead(ctx, routeros.ContainerList)
	if unsupported(err) {
		result.Capabilities.Warnings = append(result.Capabilities.Warnings, "此设备未提供 Container 读取接口，请检查 container 软件包与权限")
		return result, nil
	}
	if err != nil {
		return result, fmt.Errorf("read containers: %w", err)
	}
	result.Capabilities.Supported = true
	data := map[routeros.ContainerMenu][]routeros.RouterOSObject{}
	for _, menu := range []routeros.ContainerMenu{routeros.ContainerResource, routeros.ContainerConfig, routeros.ContainerVETH, routeros.ContainerBridge, routeros.ContainerBridgePort, routeros.ContainerInterfaces, routeros.ContainerAddresses, routeros.ContainerDisk, routeros.ContainerEnvs, routeros.ContainerMounts, routeros.ContainerLogs} {
		objects, err := reader.ContainerRead(ctx, menu)
		if unsupported(err) {
			result.Capabilities.Warnings = append(result.Capabilities.Warnings, "无法读取 "+string(menu)+"；相关信息不可用")
			continue
		}
		if err != nil {
			return result, fmt.Errorf("read %s: %w", menu, err)
		}
		data[menu] = objects
	}
	for _, row := range data[routeros.ContainerResource] {
		result.Capabilities.Version = row["version"]
		result.Options.Architecture = row["architecture-name"]
	}
	for _, row := range data[routeros.ContainerConfig] {
		result.Options.MemoryHigh = row["memory-high"]
		result.Options.MemoryMax = row["memory-max"]
	}
	for _, row := range data[routeros.ContainerBridge] {
		if !isTrue(row["disabled"]) {
			result.Options.Bridges = append(result.Options.Bridges, row["name"])
		}
	}
	for _, row := range data[routeros.ContainerInterfaces] {
		result.Options.Interfaces = append(result.Options.Interfaces, row["name"])
	}
	for _, row := range data[routeros.ContainerAddresses] {
		result.Options.UsedIPs = append(result.Options.UsedIPs, row["address"])
	}
	for _, row := range data[routeros.ContainerDisk] {
		name := row["mount-point"]
		if name == "" {
			name = row["slot"]
		}
		if name == "" {
			name = row["name"]
		}
		name = strings.Trim(name, "/")
		result.Options.Disks = append(result.Options.Disks, Disk{Name: name, FreeBytes: byteCount(row["free"]), Writable: name != "" && !isTrue(row["disabled"]) && !isTrue(row["formatting"]) && !isTrue(row["empty"]) && (row["mounted"] == "" || isTrue(row["mounted"])) && (row["fs"] != "" || row["type"] == "ext4")})
	}
	for _, row := range rows {
		n := Network{VETH: row["interface"]}
		for _, veth := range data[routeros.ContainerVETH] {
			if veth["name"] != n.VETH {
				continue
			}
			for _, address := range strings.Split(veth["address"], ",") {
				if strings.Contains(address, ":") {
					n.Address6 = address
				} else {
					n.Address = address
				}
			}
			n.Gateway = veth["gateway"]
			n.Gateway6 = veth["gateway6"]
			n.MAC = veth["mac-address"]
			result.Options.UsedIPs = append(result.Options.UsedIPs, n.Address)
		}
		for _, port := range data[routeros.ContainerBridgePort] {
			if port["interface"] == n.VETH && !isTrue(port["disabled"]) {
				n.Bridge = port["bridge"]
			}
		}
		draft := Draft{ExistingID: row[".id"], Name: row["name"], Image: row["remote-image"], Network: n, RootDir: row["root-dir"], Command: row["cmd"], Entrypoint: row["entrypoint"], User: row["user"], Workdir: row["workdir"], Env: []Environment{}, Mounts: []Mount{}, MemoryHigh: row["memory-high"], MemoryMax: row["memory-max"], CPUList: row["cpu-list"], StartOnBoot: isTrue(row["start-on-boot"]), Logging: isTrue(row["logging"]), RestartPolicy: row["restart-policy"], Health: Health{Mode: "inherit"}}
		draft.ImageSource = "registry"
		if row["file"] != "" && row["remote-image"] == "" {
			draft.ImageSource = "archive"
			draft.ArchiveFile = row["file"]
		}
		if draft.Image == "" {
			draft.Image = row["tag"]
			if draft.Image == "" && draft.ArchiveFile != "" {
				draft.Image = draft.ArchiveFile
			}
		}
		if row["healthcheck-cmd"] != "" {
			draft.Health = Health{Mode: "override", Command: row["healthcheck-cmd"], Interval: row["healthcheck-interval"], Timeout: row["healthcheck-timeout"], Retries: row["healthcheck-retries"], StartPeriod: row["healthcheck-start-period"]}
		}
		envLists := splitLists(first(row["envlists"], row["envlist"]))
		mountLists := splitLists(first(row["mountlists"], row["mounts"]))
		for _, env := range data[routeros.ContainerEnvs] {
			if contains(envLists, env["list"]) {
				draft.Env = append(draft.Env, Environment{Key: env["key"], Value: env["value"]})
			}
		}
		for _, mount := range data[routeros.ContainerMounts] {
			if contains(mountLists, first(mount["list"], mount["name"])) {
				draft.Mounts = append(draft.Mounts, Mount{Source: mount["src"], Target: mount["dst"], ReadOnly: strings.HasPrefix(mount["mode"], "ro"), Mode: mount["mode"]})
			}
		}
		item := Item{ID: row[".id"], Name: row["name"], Image: draft.Image, Network: n, Status: containerStatus(row), CPU: row["cpu-usage"], Memory: row["memory-usage"], StartOnBoot: draft.StartOnBoot, Ownership: "unmanaged", SharedVETH: []string{}, EnvLists: envLists, MountLists: mountLists, Config: draft, ImageDefaults: map[string]string{}}
		for _, other := range rows {
			if other[".id"] != item.ID && n.VETH != "" && other["interface"] == n.VETH {
				item.SharedVETH = append(item.SharedVETH, other["name"])
			}
		}
		for _, field := range []string{"cmd", "entrypoint", "user", "workdir", "healthcheck-cmd"} {
			if value := row["default-"+field]; value != "" {
				item.ImageDefaults[field] = value
			}
		}
		for _, field := range []string{"restart-policy", "memory-high", "memory-max", "cpu-list", "healthcheck-cmd"} {
			if _, ok := row[field]; ok && !contains(result.Capabilities.Fields, field) {
				result.Capabilities.Fields = append(result.Capabilities.Fields, field)
			}
		}
		result.Items = append(result.Items, item)
	}
	_, result.Capabilities.Logs = data[routeros.ContainerLogs]
	return result, nil
}
func (s *Service) Logs(ctx context.Context, d config.DeviceConfig, id, name string) ([]Log, error) {
	rows, err := s.ReaderFor(d).ContainerRead(ctx, routeros.ContainerLogs)
	if err != nil {
		return nil, err
	}
	result := []Log{}
	for _, row := range rows {
		if row["container"] == id || (name != "" && row["container"] == name) {
			result = append(result, Log{ID: row[".id"], Time: row["time"], Message: row["message"]})
		}
	}
	if len(result) > 500 {
		result = result[len(result)-500:]
	}
	return result, nil
}
func unsupported(err error) bool {
	var e *routeros.HTTPError
	return errors.As(err, &e) && (e.StatusCode == 404 || e.StatusCode == 400)
}
func isTrue(value string) bool { return value == "true" || value == "yes" }
func first(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}
func splitLists(value string) []string {
	list := []string{}
	for _, part := range strings.Split(value, ",") {
		if part = strings.TrimSpace(part); part != "" {
			list = append(list, part)
		}
	}
	return list
}
func containerStatus(row routeros.RouterOSObject) string {
	for _, status := range []string{"error", "downloading", "extracting", "starting", "stopping", "running", "stopped"} {
		if isTrue(row[status]) {
			return status
		}
	}
	return first(row["status"], "unknown")
}
func byteCount(value string) int64 {
	value = strings.TrimSpace(value)
	for _, u := range []struct {
		suffix   string
		multiple float64
	}{{"GiB", 1 << 30}, {"MiB", 1 << 20}, {"KiB", 1 << 10}, {"GB", 1e9}, {"MB", 1e6}, {"kB", 1e3}} {
		if strings.HasSuffix(value, u.suffix) {
			n, _ := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(value, u.suffix)), 64)
			return int64(n * u.multiple)
		}
	}
	n, _ := strconv.ParseInt(value, 10, 64)
	return n
}
