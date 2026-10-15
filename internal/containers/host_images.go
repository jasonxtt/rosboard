package containers

import (
	"context"
	"errors"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"rosboard/internal/config"
	"rosboard/internal/downloadnet"
	"rosboard/internal/imagepull"
	"rosboard/internal/routeros"
)

type generatedImage struct {
	Snapshot         *downloadnet.Snapshot `json:"-"`
	TransferStarted  bool
	Artifact         imagepull.Artifact
	Revision         string
	TransferIdentity string
	Transfer         routeros.PreparedTransfer
	ContainerID      string
	Root             string
	Owner            string
	FinalOwner       string
	RuntimeSucceeded bool
	CleanupPending   bool
	Cleaned          bool
}

func (s *Service) imageDirectory(d config.DeviceConfig, id string) (string, string, error) {
	if _, e := uuid.Parse(id); e != nil {
		return "", "", errors.New("invalid image job")
	}
	base, e := filepath.Abs(filepath.Join(s.NativeStateDir, "container-staging"))
	if e != nil {
		return "", "", e
	}
	return base, filepath.Join(base, nativeKey(d), id), nil
}
func transferIdentity(d config.DeviceConfig) string {
	c := d.ContainerTransferCredentials()
	return downloadnet.Revision(d.RouterOS.BaseURL + "\x00" + c.Username + "\x00" + c.Password + "\x00" + c.HostKey + "\x00" + string(rune(c.Port)))
}
func (s *Service) hostImagePlan(ctx context.Context, d config.DeviceConfig, steps []nativeStep, snap Snapshot, id string) (*generatedImage, error) {
	if s.DownloadSnapshot == nil {
		return nil, nil
	}
	for _, step := range steps {
		if step.Menu != routeros.ContainerList || step.Command != "add" || step.Fields["remote-image"] == "" {
			continue
		}
		credentials := d.ContainerTransferCredentials()
		if credentials.HostKey == "" {
			return nil, operationError(409, "ssh_key_required", "镜像传输连接尚未初始化，请重新提交操作")
		}
		arch, e := imagepull.Platform(snap.Options.Architecture)
		if e != nil {
			return nil, e
		}
		reference, e := s.normalizedRegistry(ctx, d, step.Fields["remote-image"])
		if e != nil {
			return nil, e
		}

		root, e := DirectoryPath(step.Fields["root-dir"])
		if e != nil {
			return nil, e
		}
		disk := strings.Split(strings.TrimPrefix(root, "/"), "/")[0]
		mounted := false
		for _, candidate := range snap.Options.Disks {
			if candidate.Name == disk && candidate.Writable && candidate.FreeBytes > imagepull.Reserve {
				mounted = true
			}
		}
		if !mounted {
			return nil, operationError(400, "staging_disk_unavailable", "镜像暂存需要运行目录所在的已挂载可写磁盘")
		}
		finalOwner := step.Fields["comment"]
		for _, later := range steps {
			if later.Menu == routeros.ContainerList && later.Command == "set" && later.Match["root-dir"] == step.Fields["root-dir"] && later.Fields["comment"] != "" {
				finalOwner = later.Fields["comment"]
			}
		}
		snapshot, e := s.DownloadSnapshot()
		if e != nil {
			return nil, e
		}
		return &generatedImage{Snapshot: &snapshot, Artifact: imagepull.Artifact{Reference: reference, Platform: arch.OS + "/" + arch.Architecture}, Revision: snapshot.Revision, TransferIdentity: transferIdentity(d), Transfer: routeros.PreparedTransfer{Path: path.Join("/", disk, "rosboard/images/jobs", id, "image.tar")}, Root: step.Fields["root-dir"], Owner: step.Fields["comment"], FinalOwner: finalOwner}, nil
	}
	return nil, nil
}
func (s *Service) prepareHostImage(ctx context.Context, d config.DeviceConfig, n *nativeDevice, r *nativeRecord) error {
	g := r.Generated
	if g == nil {
		return nil
	}
	var snapshot downloadnet.Snapshot
	var e error
	if g.Snapshot != nil {
		snapshot = *g.Snapshot
	} else {
		snapshot, e = s.DownloadSnapshot()
		if e != nil {
			return e
		}
	}
	defer snapshot.Transport.CloseIdleConnections()
	if snapshot.Revision != g.Revision || transferIdentity(d) != g.TransferIdentity {
		return errors.New("下载代理或传输身份已变化，请显式恢复任务")
	}
	// Once the import is journaled, never regenerate/transfer or alter its steps.
	if g.Transfer.Published {
		n.mu.Lock()
		defer n.mu.Unlock()
		for i := range r.Steps {
			step := &r.Steps[i]
			if step.Menu == routeros.ContainerList && step.Command == "add" && step.Fields["remote-image"] != "" {
				delete(step.Fields, "remote-image")
				step.Fields["file"] = strings.TrimPrefix(g.Transfer.Path, "/")
			}
		}
		return n.save()
	}
	base, dir, e := s.imageDirectory(d, r.Job.ID)
	if e != nil {
		return e
	}
	phase := func(value string) {
		n.mu.Lock()
		r.Job.Phase = value
		r.Job.State = "running"
		_ = n.save()
		n.mu.Unlock()
	}
	downloadCtx, downloadCancel := context.WithTimeout(ctx, 45*time.Minute)
	defer downloadCancel()
	artifact, e := imagepull.Prepare(downloadCtx, base, dir, g.Artifact.Reference, strings.TrimPrefix(g.Artifact.Platform, "linux/"), g.Artifact.Digest, snapshot.Transport, func(a imagepull.Artifact) error {
		n.mu.Lock()
		defer n.mu.Unlock()
		g.Artifact = a
		return n.save()
	}, phase)
	if e != nil {
		return e
	}
	file, e := os.Open(filepath.Join(dir, "image.tar"))
	if e != nil {
		return e
	}
	defer file.Close()
	archive, e := InspectArchive(file, "image.tar", artifact.Bytes)
	if e != nil || archive.Architecture != strings.TrimPrefix(artifact.Platform, "linux/") {
		return imagepull.ErrPrepare
	}
	// Verify fresh free-space and separation immediately before SFTP writes.
	snap, e := s.fresh(ctx, d)
	if e != nil {
		return e
	}
	disk := strings.Split(strings.TrimPrefix(g.Transfer.Path, "/"), "/")[0]
	enough := false
	for _, candidate := range snap.Options.Disks {
		if candidate.Name == disk && candidate.Writable && candidate.FreeBytes > artifact.Bytes+artifact.Expanded+imagepull.Reserve {
			enough = true
		}
	}
	if !enough {
		return errors.New("RouterOS 暂存和展开空间不足；镜像已保留")
	}
	if e = s.protectGeneratedPath(ctx, d, g.Transfer.Path); e != nil {
		return e
	}
	n.mu.Lock()
	g.Artifact = artifact
	g.Transfer.Bytes = artifact.Bytes
	g.Transfer.SHA256 = artifact.SHA256
	e = n.save()
	n.mu.Unlock()
	if e != nil {
		return e
	}
	n.mu.Lock()
	g.TransferStarted = true
	e = n.save()
	n.mu.Unlock()
	if e != nil {
		return e
	}
	state := g.Transfer
	checkpoint := func() error { n.mu.Lock(); defer n.mu.Unlock(); g.Transfer = state; return n.save() }
	transferCtx, transferCancel := context.WithTimeout(ctx, 45*time.Minute)
	defer transferCancel()
	if e = s.PreparedUpload(transferCtx, d, &state, file, checkpoint, phase); e != nil {
		return e
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	for i := range r.Steps {
		step := &r.Steps[i]
		if step.Menu == routeros.ContainerList && step.Command == "add" && step.Fields["remote-image"] != "" {
			delete(step.Fields, "remote-image")
			step.Fields["file"] = strings.TrimPrefix(g.Transfer.Path, "/")
		}
	}
	return n.save()
}
func overlaps(a, b string) bool {
	a, _ = DirectoryPath(a)
	b, _ = DirectoryPath(b)
	return a != "/" && b != "/" && (a == b || strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/"))
}
func (s *Service) protectGeneratedPath(ctx context.Context, d config.DeviceConfig, p string) error {
	for _, menu := range []routeros.ContainerMenu{routeros.ContainerList, routeros.ContainerMounts} {
		rows, e := s.ReaderFor(d).ContainerRead(ctx, menu)
		if e != nil {
			return e
		}
		for _, row := range rows {
			key := "root-dir"
			if menu == routeros.ContainerMounts {
				key = "src"
			}
			if overlaps(p, row[key]) {
				return errors.New("镜像暂存路径与运行或挂载目录重叠")
			}
		}
	}
	return nil
}
func decorateGenerated(n *nativeDevice, snap *Snapshot) {
	for _, r := range n.state.Jobs {
		g := r.Generated
		if g == nil || g.ContainerID == "" {
			continue
		}
		for i := range snap.Items {
			item := &snap.Items[i]
			if item.ID == g.ContainerID && equivalent("root-dir", item.Config.RootDir, g.Root) && (item.OwnerComment == g.Owner || item.OwnerComment == g.FinalOwner) {
				// Ownership is validated from the raw item comment via its generated binding
				// in reconciliation; no name/path-only adoption is performed.
				item.Image = g.Artifact.Reference
				item.Config.Image = g.Artifact.Reference
				item.Config.ImageSource = "registry"
				item.Config.ArchiveFile = ""
				item.Config.ArchiveID = ""
				item.ImageDigest = g.Artifact.Digest
				item.ImagePlatform = g.Artifact.Platform
			}
		}
	}
}

var generatedFile = regexp.MustCompile(`^(?:[0-9a-f]{64}\.(?:blob|tar|json)|image\.tar|manifest\.json)(?:\.partial)?$`)

func (s *Service) cleanupLocalImage(d config.DeviceConfig, id string, owned []string) error {
	base, dir, e := s.imageDirectory(d, id)
	if e != nil {
		return e
	}
	if _, e = os.Lstat(dir); os.IsNotExist(e) {
		return nil
	}
	if e = imagepull.SafeDirectory(base, dir); e != nil {
		return e
	}
	root, e := os.OpenRoot(dir)
	if e != nil {
		return e
	}
	defer root.Close()
	entries, e := os.ReadDir(dir)
	if e != nil {
		return e
	}
	allowed := map[string]bool{}
	for _, name := range owned {
		allowed[name] = true
		allowed[name+".partial"] = true
	}
	for _, entry := range entries {
		if !allowed[entry.Name()] || !generatedFile.MatchString(entry.Name()) || !entry.Type().IsRegular() {
			return errors.New("暂存目录存在未知文件，拒绝清理")
		}
	}
	for _, entry := range entries {
		if e = root.Remove(entry.Name()); e != nil {
			return e
		}
	}
	return os.Remove(dir)
}

// reconcileImages is background/operation work, never a GET side effect. Caller
// holds the device gate. Cleanup intent is durable before either filesystem write.
func (s *Service) reconcileImages(ctx context.Context, d config.DeviceConfig, n *nativeDevice) error {
	rows, e := s.ReaderFor(d).ContainerRead(ctx, routeros.ContainerList)
	if e != nil {
		return e
	}
	n.mu.Lock()
	records := []*nativeRecord{}
	for _, r := range n.state.Jobs {
		if r.Generated != nil && !r.Generated.Cleaned {
			records = append(records, r)
		}
	}
	n.mu.Unlock()
	for _, r := range records {
		g := r.Generated
		if g.ContainerID == "" {
			continue
		}
		var row routeros.RouterOSObject
		for _, candidate := range rows {
			if candidate[".id"] == g.ContainerID {
				row = candidate
			}
		}
		if row != nil {
			if !equivalent("root-dir", row["root-dir"], g.Root) || (row["comment"] != g.Owner && row["comment"] != g.FinalOwner) {
				continue
			}
			healthy := containerStatus(row) == "running" && !isTrue(row["unhealthy"]) && !isTrue(row["download/extract failed"])
			if row["healthcheck-cmd"] != "" || row["default-healthcheck-cmd"] != "" || g.Artifact.HasHealthcheck {
				healthy = healthy && isTrue(row["healthy"])
			}
			if healthy {
				n.mu.Lock()
				g.RuntimeSucceeded = true
				n.mu.Unlock()
			}
		}
		// An absence is authoritative only after a successful container-list read.
		if row != nil && !g.RuntimeSucceeded {
			continue
		}
		shared := false
		for _, other := range rows {
			if other[".id"] != g.ContainerID && equivalent("file", other["file"], g.Transfer.Path) {
				shared = true
			}
		}
		if shared {
			continue
		}
		if e = s.protectGeneratedPath(ctx, d, g.Transfer.Path); e != nil {
			return e
		}
		n.mu.Lock()
		g.CleanupPending = true
		e = n.save()
		n.mu.Unlock()
		if e != nil {
			return e
		}
		if transferIdentity(d) != g.TransferIdentity {
			return errors.New("镜像清理的传输身份已变化")
		}
		if g.Transfer.NamespaceCreated {
			if e = s.PreparedRemove(ctx, d, g.Transfer); e != nil {
				return e
			}
		}
		if e = s.cleanupLocalImage(d, r.Job.ID, g.Artifact.Files); e != nil {
			return e
		}
		n.mu.Lock()
		g.Cleaned = true
		g.CleanupPending = false
		e = n.save()
		n.mu.Unlock()
		if e != nil {
			return e
		}
	}
	return nil
}
func (s *Service) imageCleanupLoop(ctx context.Context, d config.DeviceConfig, n *nativeDevice) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if s.DeviceFor != nil {
			current, ok := s.DeviceFor(d.ID)
			if !ok || !current.Enabled || current.Archived || !current.ContainerWrites || nativeKey(current) != nativeKey(d) {
				return
			}
			d = current
		}
		n.mu.Lock()
		busy := n.running || activeRecord(n) != nil
		n.mu.Unlock()
		if busy {
			continue
		}
		release, ok := s.native.gate.TryAcquire(d.ID)
		if !ok {
			continue
		}
		cleanupCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		if err := s.reconcileImages(cleanupCtx, d, n); err != nil {
			n.mu.Lock()
			for _, r := range n.state.Jobs {
				if r.Generated != nil && r.Generated.CleanupPending {
					r.Job.Warnings = []string{"关联镜像清理待恢复；下载与挂载数据已保留"}
				}
			}
			_ = n.save()
			n.mu.Unlock()
		}
		cancel()
		release()
	}
}

func (s *Service) StartImageWorkers(ctx context.Context, devices []config.DeviceConfig) {
	for _, d := range devices {
		if !d.Enabled || d.Archived || !d.ContainerWrites {
			continue
		}
		n, e := s.nativeDevice(d)
		if e != nil {
			continue
		}
		n.mu.Lock()
		if !n.cleanupStarted {
			n.cleanupStarted = true
			go s.imageCleanupLoop(ctx, d, n)
		}
		n.mu.Unlock()
	}
}

func (s *Service) cancelGenerated(ctx context.Context, d config.DeviceConfig, n *nativeDevice, r *nativeRecord) error {
	g := r.Generated
	if g == nil {
		return nil
	}
	n.mu.Lock()
	g.CleanupPending = true
	err := n.save()
	n.mu.Unlock()
	if err != nil {
		return err
	}
	if g.TransferStarted {
		if !g.Transfer.NamespaceCreated {
			c := d.ContainerTransferCredentials()
			if err = routeros.PreparedAbsent(ctx, d.RouterOS.BaseURL, c.Username, c.Password, c.HostKey, c.Port, g.Transfer); err != nil {
				return err
			}
		} else {
			if transferIdentity(d) != g.TransferIdentity {
				return routeros.ErrTransferUnknown
			}
			if err = s.PreparedRemove(ctx, d, g.Transfer); err != nil {
				return err
			}
		}
	}
	if err = s.cleanupLocalImage(d, r.Job.ID, g.Artifact.Files); err != nil {
		return err
	}
	n.mu.Lock()
	g.Cleaned = true
	g.CleanupPending = false
	err = n.save()
	n.mu.Unlock()
	return err
}
func (s *Service) CancelNative(d config.DeviceConfig, id string) (Job, error) {
	n, e := s.nativeDevice(d)
	if e != nil {
		return Job{}, e
	}
	n.mu.Lock()
	r := n.state.Jobs[id]
	if r == nil || r.Generated == nil {
		n.mu.Unlock()
		return Job{}, operationError(409, "cancel_unavailable", "此任务不支持取消镜像准备")
	}
	for _, step := range r.Steps {
		if step.Done {
			n.mu.Unlock()
			return Job{}, operationError(409, "import_started", "导入开始后请回读恢复，再使用受保护的容器删除操作")
		}
	}
	if r.Pending || n.running && !r.Preparing {
		n.mu.Unlock()
		return Job{}, operationError(409, "import_started", "导入已开始，请回读恢复")
	}
	if r.Job.State == "succeeded" || r.Job.State == "cancelled" {
		j := recordJob(r)
		n.mu.Unlock()
		return j, nil
	}
	r.CancelRequested = true
	r.Job.Phase = "正在取消镜像准备"
	if e = n.save(); e != nil {
		n.mu.Unlock()
		return Job{}, e
	}
	running := n.running
	if n.cancel != nil {
		n.cancel()
	}
	j := recordJob(r)
	n.mu.Unlock()
	if !running {
		return s.RecoverNative(d, nil, id)
	}
	return j, nil
}

func (s *Service) normalizedRegistry(ctx context.Context, d config.DeviceConfig, reference string) (string, error) {
	firstPart := strings.SplitN(reference, "/", 2)
	explicit := len(firstPart) == 2 && strings.ContainsAny(firstPart[0], ".:")
	if !explicit {
		rows, e := s.ReaderFor(d).ContainerRead(ctx, routeros.ContainerRegistryConfig)
		if e != nil {
			return "", operationError(502, "registry_config_unavailable", "无法读取目标设备的默认镜像仓库")
		}
		if len(rows) > 0 {
			raw := first(rows[0]["registry-url"], rows[0]["assumed-registry-url"])
			if raw != "" {
				if !strings.Contains(raw, "://") {
					raw = "https://" + raw
				}
				u, e := url.Parse(raw)
				if e != nil || u.Scheme != "https" || u.User != nil || u.Hostname() == "" || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
					return "", operationError(400, "invalid_registry", "默认镜像仓库必须为不含凭据的 HTTPS 地址")
				}
				if u.Host != "docker.io" && u.Host != "registry-1.docker.io" && u.Host != "index.docker.io" {
					reference = u.Host + "/" + reference
				}
			}
		}
	}
	return imagepull.NormalizeReference(reference)
}
func (s *Service) ResolveForDevice(ctx context.Context, d config.DeviceConfig, draft Draft, snap Snapshot) Resolution {
	resolved := Resolve(draft, snap)
	if s.DownloadSnapshot != nil && resolved.Effective.ImageSource == "registry" {
		normalized, e := s.normalizedRegistry(ctx, d, resolved.Effective.Image)
		if e != nil {
			resolved.Errors["image"] = "无法确认默认镜像仓库；请检查配置或填写完整 HTTPS 仓库引用"
		} else {
			resolved.Effective.Image = normalized
			resolved.ContainerFields["remote-image"] = normalized
		}
	}
	return resolved
}

func transferPrepared(ctx context.Context, d config.DeviceConfig, state *routeros.PreparedTransfer, source io.ReadSeeker, checkpoint func() error, phase func(string)) error {
	c := d.ContainerTransferCredentials()
	return routeros.TransferPrepared(ctx, d.RouterOS.BaseURL, c.Username, c.Password, c.HostKey, c.Port, state, source, checkpoint, phase)
}
func removePrepared(ctx context.Context, d config.DeviceConfig, state routeros.PreparedTransfer) error {
	c := d.ContainerTransferCredentials()
	return routeros.RemovePrepared(ctx, d.RouterOS.BaseURL, c.Username, c.Password, c.HostKey, c.Port, state)
}
