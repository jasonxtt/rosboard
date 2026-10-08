package containers

import (
	"fmt"
	"net"
	"net/netip"
	"path"
	"regexp"
	"strconv"
	"strings"
)

var slugPattern = regexp.MustCompile(`[^a-z0-9_.-]+`)
var namePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,62}$`)
var envPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
var cpuPattern = regexp.MustCompile(`^\d+(?:-\d+)?(?:,\d+(?:-\d+)?)*$`)
var durationPattern = regexp.MustCompile(`^(?:[0-9]+(?:ms|s|m|h|d|w))+$|^[0-9]{1,2}:[0-5][0-9]:[0-5][0-9](?:\.[0-9]+)?$`)
var memoryPattern = regexp.MustCompile(`^(?:unlimited|[1-9]\d*(?:[KMGTP]i?B?|[kmg])?)$`)

func NormalizeImage(image string) string {
	image = strings.TrimSpace(image)
	if image == "" || strings.Contains(image, "@") {
		return image
	}
	if !strings.Contains(image[strings.LastIndex(image, "/")+1:], ":") {
		image += ":latest"
	}
	return image
}

// Resolve performs no I/O and never fills network requirements. ExistingID
// resolves against this device's snapshot, rather than trusting client metadata.
func Resolve(d Draft, s Snapshot) Resolution {
	r := Resolution{Effective: d, Errors: map[string]string{}, Defaults: []string{}, ContainerFields: map[string]string{}}
	e := &r.Effective
	var existing *Item
	for i := range s.Items {
		if s.Items[i].ID == d.ExistingID {
			existing = &s.Items[i]
			break
		}
	}
	if d.ExistingID != "" && existing == nil {
		r.Errors["existingId"] = "容器不存在于当前设备"
	}
	if e.ImageSource == "" {
		e.ImageSource = "registry"
	}
	imageFile := ""
	switch e.ImageSource {
	case "registry":
		e.ArchiveID, e.ArchiveFile = "", ""
	case "archive":
		if existing != nil && e.ArchiveID == "" && e.ArchiveFile == existing.Config.ArchiveFile && e.ArchiveFile != "" {
			imageFile = existing.Config.ArchiveFile
			e.Image = existing.Image
		} else {
			for _, archive := range s.Options.Archives {
				if archive.ID == e.ArchiveID {
					imageFile = archive.RemotePath
					e.Image = archive.Reference
					e.ArchiveFile = archive.RemotePath
					if archive.Architecture != OCIArchitecture(s.Options.Architecture) {
						r.Errors["archiveId"] = "镜像架构与当前设备不匹配"
					}
					break
				}
			}
			if imageFile == "" {
				r.Errors["archiveId"] = "请先上传并校验本地镜像归档"
			}
		}
	default:
		r.Errors["imageSource"] = "镜像来源无效"
	}
	if e.ImageSource == "registry" && (existing == nil || d.Image != existing.Config.Image) {
		e.Image = NormalizeImage(e.Image)
	}
	if e.ImageSource == "registry" && e.Image == "" {
		r.Errors["image"] = "请填写镜像"
	} else if strings.ContainsAny(e.Image, " \t\r\n") || strings.HasPrefix(e.Image, "-") {
		r.Errors["image"] = "镜像引用不能包含空格"
	}
	if e.Name == "" {
		leaf := strings.Split(strings.Split(e.Image, "@")[0], "/")
		base := strings.Split(leaf[len(leaf)-1], ":")[0]
		base = strings.Trim(slugPattern.ReplaceAllString(strings.ToLower(base), "-"), "-.")
		if base == "" {
			base = "container"
		}
		if len(base) > 40 {
			base = base[:40]
		}
		suffix := slugPattern.ReplaceAllString(strings.ToLower(d.DraftID), "")
		if len(suffix) > 6 {
			suffix = suffix[:6]
		}
		if len(suffix) < 4 {
			r.Errors["draftId"] = "请重新打开创建表单以生成唯一标识"
		}
		e.Name = base + "-" + suffix
		r.Defaults = append(r.Defaults, "容器名称："+e.Name)
	}
	if e.Health.Mode == "override" {
		for key, value := range map[string]string{"interval": e.Health.Interval, "timeout": e.Health.Timeout, "startPeriod": e.Health.StartPeriod} {
			if value != "" && !durationPattern.MatchString(value) {
				r.Errors["health."+key] = "请使用有效时间，例如 30s、1m 或 00:00:30"
			}
		}
	}
	if !namePattern.MatchString(e.Name) {
		r.Errors["name"] = "名称需为 1–63 位字母、数字、点、下划线或连字符"
	}
	for _, item := range s.Items {
		if item.ID != d.ExistingID && item.Name == e.Name {
			r.Errors["name"] = "容器名称已存在"
		}
	}
	n := e.Network
	if n.VETH == "" {
		r.Errors["network.veth"] = "请填写专属 VETH 名称"
	} else if !namePattern.MatchString(n.VETH) {
		r.Errors["network.veth"] = "VETH 名称格式无效"
	}
	if existing == nil || n.VETH != existing.Network.VETH {
		for _, iface := range s.Options.Interfaces {
			if iface == n.VETH {
				r.Errors["network.veth"] = "接口名称已存在"
			}
		}
	}
	foundBridge := false
	for _, bridge := range s.Options.Bridges {
		if bridge == n.Bridge {
			foundBridge = true
		}
	}
	if !foundBridge {
		r.Errors["network.bridge"] = "请选择已有 bridge"
	}
	prefix, err := netip.ParsePrefix(n.Address)
	if err != nil || !prefix.Addr().Is4() {
		r.Errors["network.address"] = "请填写静态 IPv4 地址及掩码，例如 172.20.0.2/24"
	} else {
		addr := prefix.Addr()
		if addr.IsUnspecified() || addr.IsMulticast() || addr.IsLoopback() {
			r.Errors["network.address"] = "IPv4 地址不可用于容器"
		}
		for _, used := range s.Options.UsedIPs {
			ip := strings.Split(used, "/")[0]
			if ip == addr.String() && (existing == nil || strings.Split(existing.Network.Address, "/")[0] != ip) {
				r.Errors["network.address"] = "已知设备地址存在冲突"
			}
		}
	}
	gateway, gerr := netip.ParseAddr(n.Gateway)
	if gerr != nil || !gateway.Is4() {
		r.Errors["network.gateway"] = "请填写 IPv4 网关"
	} else if err == nil && (!prefix.Contains(gateway) || gateway == prefix.Addr()) {
		r.Errors["network.gateway"] = "网关必须在同一子网且与容器地址不同"
	}
	if n.Address6 != "" {
		p, err := netip.ParsePrefix(n.Address6)
		if err != nil || !p.Addr().Is6() {
			r.Errors["network.address6"] = "IPv6 地址及掩码格式无效"
		}
	}
	if n.Gateway6 != "" {
		g, err := netip.ParseAddr(n.Gateway6)
		if err != nil || !g.Is6() {
			r.Errors["network.gateway6"] = "IPv6 网关格式无效"
		} else if p, err := netip.ParsePrefix(n.Address6); !g.IsLinkLocalUnicast() && (err != nil || !p.Contains(g) || g == p.Addr()) {
			r.Errors["network.gateway6"] = "IPv6 网关需位于同一子网或为链路本地地址"
		}
	}
	if n.MAC != "" {
		mac, err := net.ParseMAC(n.MAC)
		if err != nil || len(mac) != 6 || mac[0]&1 != 0 {
			r.Errors["network.mac"] = "请填写有效的单播 MAC 地址"
		}
	}
	if existing != nil && len(existing.SharedVETH) > 0 && n != existing.Network {
		r.Errors["network.veth"] = "共享 VETH 的网络配置必须保持原值"
	}
	if e.RootDir == "" {
		var best *Disk
		for i := range s.Options.Disks {
			disk := &s.Options.Disks[i]
			if disk.Writable && disk.FreeBytes > 0 && (best == nil || disk.FreeBytes > best.FreeBytes) {
				best = disk
			}
		}
		if best == nil {
			r.Errors["rootDir"] = "没有可用磁盘，请指定存储目录"
		} else {
			e.RootDir = path.Join("/", best.Name, "rosboard", "containers", e.Name, "rootfs")
			r.Defaults = append(r.Defaults, "容器文件系统目录："+e.RootDir)
		}
	} else if existing == nil || e.RootDir != existing.Config.RootDir {
		normalized, err := DirectoryPath(e.RootDir)
		if err != nil || !strings.HasPrefix(e.RootDir, "/") || normalized == "/" {
			r.Errors["rootDir"] = "请使用独立的绝对目录，不能包含 .."
		} else {
			e.RootDir = normalized
			for _, disk := range s.Options.Disks {
				if normalized == "/"+strings.Trim(disk.Name, "/") {
					r.Errors["rootDir"] = "请选择磁盘中的专属文件夹，不能直接使用整个磁盘"
				}
			}
			for _, item := range s.Items {
				other, _ := DirectoryPath(item.Config.RootDir)
				if item.ID != d.ExistingID && item.Config.RootDir != "" && other == normalized {
					r.Errors["rootDir"] = "此目录已被其他容器使用"
				}
			}
		}
	}
	keys := map[string]bool{}
	for i, env := range e.Env {
		key := fmt.Sprintf("env.%d", i)
		if !envPattern.MatchString(env.Key) || keys[env.Key] {
			r.Errors[key] = "环境变量名称无效或重复"
		}
		keys[env.Key] = true
		if strings.ContainsRune(env.Value, 0) {
			r.Errors[key] = "变量值不能包含 NUL"
		}
	}
	targets := map[string]bool{}
	for i, mount := range e.Mounts {
		if mount.Mode != "" && mount.Mode != "rw" && mount.Mode != "ro" && mount.Mode != "rw,noexec" && mount.Mode != "ro,noexec" {
			r.Errors[fmt.Sprintf("mounts.%d", i)] = "不支持的挂载模式"
		}
		source, sourceErr := DirectoryPath(mount.Source)
		target, targetErr := DirectoryPath(mount.Target)
		existingSource := false
		if existing != nil {
			for _, old := range existing.Config.Mounts {
				if old.Source == mount.Source {
					existingSource = true
				}
			}
		}
		if (!strings.HasPrefix(mount.Source, "/") && !existingSource) || mount.Source == "" || sourceErr != nil || targetErr != nil || !strings.HasPrefix(mount.Target, "/") || target == "/" || targets[target] {
			r.Errors[fmt.Sprintf("mounts.%d", i)] = "挂载需填写绝对源目录和唯一的容器目标目录"
		}
		targets[target] = true
		root, _ := DirectoryPath(e.RootDir)
		if existing == nil && sourceErr == nil && (source == root || strings.HasPrefix(source, root+"/")) {
			r.Errors[fmt.Sprintf("mounts.%d", i)] = "持久化挂载源需放在容器运行目录之外"
		}
	}
	for key, value := range map[string]string{"memoryHigh": e.MemoryHigh, "memoryMax": e.MemoryMax} {
		if value != "" && !memoryPattern.MatchString(value) {
			r.Errors[key] = "内存使用字节数、M/G 单位或 unlimited"
		}
	}
	if e.CPUList != "" && !cpuPattern.MatchString(e.CPUList) {
		r.Errors["cpuList"] = "CPU 使用编号列表，例如 0 或 0,1"
	}
	if e.RestartPolicy == "" {
		e.RestartPolicy = "no"
	}
	if e.RestartPolicy != "no" && e.RestartPolicy != "always" && e.RestartPolicy != "on-failure" {
		r.Errors["restartPolicy"] = "不支持的重启策略"
	}
	if e.Health.Mode == "" {
		e.Health.Mode = "inherit"
	}
	if e.Health.Mode != "inherit" && e.Health.Mode != "override" {
		r.Errors["health.mode"] = "不支持的健康检查模式"
	}
	if e.Health.Mode == "override" {
		if strings.TrimSpace(e.Health.Command) == "" {
			r.Errors["health.command"] = "覆盖健康检查时需填写命令"
		}
		if e.Health.Retries != "" {
			n, err := strconv.Atoi(e.Health.Retries)
			if err != nil || n < 1 {
				r.Errors["health.retries"] = "重试次数需为正整数"
			}
		}
	}
	fields := r.ContainerFields
	fields["name"] = e.Name
	fields["remote-image"] = e.Image
	fields["interface"] = n.VETH
	fields["root-dir"] = e.RootDir
	fields["start-on-boot"] = yesNo(e.StartOnBoot)
	fields["logging"] = yesNo(e.Logging)
	fields["restart-policy"] = e.RestartPolicy
	for key, value := range map[string]string{"cmd": e.Command, "entrypoint": e.Entrypoint, "user": e.User, "workdir": e.Workdir, "cpu-list": e.CPUList, "memory-high": e.MemoryHigh, "memory-max": e.MemoryMax} {
		if value != "" {
			fields[key] = value
		}
	}
	high, max := e.MemoryHigh, e.MemoryMax
	if high == "" {
		high = s.Options.MemoryHigh
	}
	if max == "" {
		max = s.Options.MemoryMax
	}
	if high == "" {
		high = "unlimited"
	}
	if max == "" {
		max = "unlimited"
	}
	r.Defaults = append(r.Defaults, "镜像："+e.Image, "有效内存 high/max："+high+" / "+max)
	if e.Command == "" {
		r.Defaults = append(r.Defaults, "命令继承镜像")
	}
	if e.CPUList == "" {
		r.Defaults = append(r.Defaults, "CPU 使用 RouterOS 默认")
	}
	if e.Health.Mode == "override" {
		fields["healthcheck-cmd"] = e.Health.Command
		for key, value := range map[string]string{"healthcheck-interval": e.Health.Interval, "healthcheck-timeout": e.Health.Timeout, "healthcheck-retries": e.Health.Retries, "healthcheck-start-period": e.Health.StartPeriod} {
			if value != "" {
				fields[key] = value
			}
		}
	}
	if e.Health.Mode == "inherit" {
		r.Defaults = append(r.Defaults, "健康检查继承镜像")
	}
	if e.ImageSource == "archive" {
		delete(r.ContainerFields, "remote-image")
		if imageFile != "" {
			r.ContainerFields["file"] = imageFile
		}
	}
	return r
}
func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}
