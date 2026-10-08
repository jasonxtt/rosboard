package containers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path"
	"strings"
	"time"

	"rosboard/internal/config"
	"rosboard/internal/routeros"
)

type DirectoryWriter interface {
	CreateDirectory(context.Context, string) error
	RenameDirectory(context.Context, string, string) error
	RemoveDirectory(context.Context, string) error
}

type DirectoryRequest struct {
	Action      string `json:"action"`
	RequestID   string `json:"requestId"`
	Parent      string `json:"parent"`
	Path        string `json:"path"`
	Name        string `json:"name"`
	ExpectedID  string `json:"expectedId"`
	ConfirmPath string `json:"confirmPath"`
}

type DirectoryMutation struct {
	Action       string `json:"action"`
	RequestID    string `json:"requestId"`
	Path         string `json:"path"`
	PreviousPath string `json:"previousPath,omitempty"`
	State        string `json:"state"`
}

type DirectoryError struct {
	Status        int
	Code, Message string
}

func (e *DirectoryError) Error() string { return e.Message }
func directoryError(status int, code, message string) *DirectoryError {
	return &DirectoryError{status, code, message}
}

type directoryRecord struct {
	deviceKey  string
	request    DirectoryRequest
	result     DirectoryMutation
	err        error
	release    func()
	processing bool
	created    time.Time
	oldRows    []routeros.RouterOSObject
}

func directoryDeviceKey(d config.DeviceConfig) string {
	k := sha256.Sum256([]byte(d.ID + "\x00" + d.RouterOS.BaseURL + "\x00" + d.RouterOS.Username + "\x00" + d.RouterOS.Password))
	return hex.EncodeToString(k[:])
}

func withinDirectory(p, root string) bool {
	if root == "/" {
		return strings.HasPrefix(p, "/")
	}
	return p == root || strings.HasPrefix(p, root+"/")
}
func writableDirectory(p string, disks []Disk) bool {
	if p == "/" {
		return false
	}
	for _, disk := range disks {
		root, err := DirectoryPath(disk.Name)
		if err == nil && root != "/" && disk.Writable && withinDirectory(p, root) {
			return true
		}
	}
	return false
}
func protectedDirectory(p string, rows []routeros.RouterOSObject, disks []Disk, refs []string) string {
	if p == "/" || path.Dir(p) == "/" {
		return "根目录、磁盘挂载点和系统目录不能重命名或删除"
	}
	for _, row := range rows {
		root, _ := DirectoryPath(row["name"])
		if row["type"] == "disk" && withinDirectory(root, p) {
			return "不能操作磁盘挂载点及其上级目录"
		}
	}
	for _, disk := range disks {
		root, _ := DirectoryPath(disk.Name)
		if root != "/" && withinDirectory(root, p) {
			return "不能操作磁盘挂载点及其上级目录"
		}
	}
	if !writableDirectory(p, disks) {
		return "此路径不在可写磁盘内"
	}
	for _, ref := range refs {
		if withinDirectory(ref, p) || withinDirectory(p, ref) {
			return "此目录与容器运行目录或挂载路径重叠，不能重命名或删除"
		}
	}
	return ""
}

func (s *Service) directoryReferences(ctx context.Context, d config.DeviceConfig) ([]string, error) {
	refs := []string{}
	reader := s.ReaderFor(d)
	for _, menu := range []routeros.ContainerMenu{routeros.ContainerList, routeros.ContainerMounts} {
		rows, err := reader.ContainerRead(ctx, menu)
		if err != nil {
			return nil, directoryError(502, "directory_references_failed", "无法确认容器使用中的路径，请检查 RouterOS 读取权限")
		}
		field := "root-dir"
		if menu == routeros.ContainerMounts {
			field = "src"
		}
		for _, row := range rows {
			if row[field] == "" {
				continue
			}
			p, err := DirectoryPath(row[field])
			if err != nil {
				return nil, directoryError(409, "directory_references_invalid", "容器路径无法安全解析，请先检查 RouterOS 配置")
			}
			refs = append(refs, p)
		}
	}
	return refs, nil
}

func directoryRow(rows []routeros.RouterOSObject, p string) routeros.RouterOSObject {
	for _, row := range rows {
		name, err := DirectoryPath(row["name"])
		if err == nil && name == p {
			return row
		}
	}
	return nil
}

// MutateDirectory never retries a write. A request ID replays a confirmed result
// or re-reads an ambiguous result while retaining the shared device write gate.
func (s *Service) MutateDirectory(ctx context.Context, d config.DeviceConfig, disks []Disk, gate *routeros.DeviceWriteGate, req DirectoryRequest) (DirectoryMutation, error) {
	if s.DirectoryWriterFor == nil {
		return DirectoryMutation{}, directoryError(403, "directory_read_only", "目录写入不可用")
	}
	if len(req.RequestID) < 8 || len(req.RequestID) > 128 || strings.ContainsAny(req.RequestID, "/\\\x00\r\n") {
		return DirectoryMutation{}, directoryError(400, "invalid_request_id", "缺少有效操作标识，请重新打开目录选择器")
	}
	if req.Action != "mkdir" && req.Action != "rename" && req.Action != "delete" && req.Action != "recover" {
		return DirectoryMutation{}, directoryError(400, "invalid_directory_action", "不支持此目录操作")
	}
	deviceKey := directoryDeviceKey(d)
	key := deviceKey + ":" + req.RequestID
	s.directoryMu.Lock()
	if s.directoryRecords == nil {
		s.directoryRecords = map[string]*directoryRecord{}
	}
	if record := s.directoryRecords[key]; record != nil {
		if req.Action != "recover" && record.request != req {
			s.directoryMu.Unlock()
			return DirectoryMutation{}, directoryError(409, "request_id_conflict", "操作标识已用于其他参数")
		}
		if record.processing {
			result := record.result
			s.directoryMu.Unlock()
			return result, directoryError(409, "directory_busy", "目录操作正在进行，请稍后刷新")
		}
		if record.result.State != "unknown" {
			result, err := record.result, record.err
			s.directoryMu.Unlock()
			return result, err
		}
		record.processing = true
		s.directoryMu.Unlock()
		return s.reconcileDirectory(ctx, d, record)
	}
	if req.Action == "recover" {
		s.directoryMu.Unlock()
		return DirectoryMutation{}, directoryError(404, "directory_operation_not_found", "没有待确认的目录操作，请刷新目录")
	}
	// Bound successful/failed replay history; unresolved records are never evicted.
	for k, record := range s.directoryRecords {
		if !record.processing && record.result.State != "unknown" && time.Since(record.created) > time.Hour {
			delete(s.directoryRecords, k)
		}
	}
	if len(s.directoryRecords) >= 1024 {
		s.directoryMu.Unlock()
		return DirectoryMutation{}, directoryError(429, "directory_operation_limit", "目录操作过于频繁，请稍后重试")
	}
	if gate == nil {
		if s.directoryGate == nil {
			s.directoryGate = routeros.NewDeviceWriteGate()
		}
		gate = s.directoryGate
	}
	release, ok := gate.TryAcquire(d.ID)
	if !ok {
		s.directoryMu.Unlock()
		return DirectoryMutation{}, directoryError(409, "directory_busy", "此设备有其他写入或待确认操作，请稍后刷新")
	}
	record := &directoryRecord{deviceKey: deviceKey, request: req, result: DirectoryMutation{Action: req.Action, RequestID: req.RequestID, State: "pending"}, release: release, processing: true, created: time.Now()}
	s.directoryRecords[key] = record
	s.directoryMu.Unlock()

	// Finish reconciliation even if the picker closes or its HTTP client times out.
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 25*time.Second)
	defer cancel()
	target, old, rows, err := s.prepareDirectory(writeCtx, d, disks, req)
	if err != nil {
		return s.finishDirectory(record, "failed", err)
	}
	s.directoryMu.Lock()
	record.result.Path, record.result.PreviousPath = target, old
	for _, row := range rows {
		copy := routeros.RouterOSObject{}
		for k, v := range row {
			copy[k] = v
		}
		record.oldRows = append(record.oldRows, copy)
	}
	s.directoryMu.Unlock()
	client := s.DirectoryWriterFor(d)
	switch req.Action {
	case "mkdir":
		err = client.CreateDirectory(writeCtx, target)
	case "rename":
		err = client.RenameDirectory(writeCtx, req.ExpectedID, target)
	case "delete":
		err = client.RemoveDirectory(writeCtx, req.ExpectedID)
	}
	if err != nil {
		var unknown *routeros.MutationOutcomeUnknownError
		if !errors.As(err, &unknown) {
			var remote *routeros.HTTPError
			if errors.As(err, &remote) && (remote.StatusCode == 401 || remote.StatusCode == 403) {
				return s.finishDirectory(record, "failed", directoryError(403, "directory_permission_denied", "RouterOS 拒绝目录写入，请检查账号权限"))
			}
			return s.finishDirectory(record, "failed", directoryError(502, "directory_write_failed", "RouterOS 拒绝目录操作，请检查权限、路径和存储状态"))
		}
	}
	// Fresh timeout for read-back, not a second write.
	readCtx, readCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer readCancel()
	return s.reconcileDirectory(readCtx, d, record)
}

func (s *Service) prepareDirectory(ctx context.Context, d config.DeviceConfig, disks []Disk, req DirectoryRequest) (string, string, []routeros.RouterOSObject, error) {
	p, err := DirectoryPath(req.Path)
	if req.Action == "mkdir" {
		p, err = DirectoryPath(req.Parent)
	}
	if err != nil {
		return "", "", nil, directoryError(400, "invalid_path", "目录路径无效，不能包含 ..、控制字符或反斜杠")
	}
	if p == "/" || (req.Action != "mkdir" && path.Dir(p) == "/") || !writableDirectory(p, disks) {
		return "", "", nil, directoryError(403, "directory_protected", "根目录、磁盘挂载点和不可写路径不能执行此操作")
	}
	if req.Action != "delete" && routeros.DirectoryName(req.Name) != nil {
		return "", "", nil, directoryError(400, "invalid_directory_name", "文件夹名称不能为空，不能包含 /、\\ 或控制字符，不能为 . 或 ..")
	}
	rows, err := s.ReaderFor(d).ContainerRead(ctx, routeros.ContainerFiles)
	if err != nil {
		return "", "", nil, directoryError(502, "directory_read_failed", "无法确认当前目录，请检查 RouterOS 连接和读取权限")
	}
	row := directoryRow(rows, p)
	if row == nil || (row["type"] != "directory" && !(req.Action == "mkdir" && row["type"] == "disk")) {
		return "", "", nil, directoryError(404, "directory_not_found", "目录已不存在，请刷新后重新选择")
	}
	if req.Action != "mkdir" && (req.ExpectedID == "" || row[".id"] != req.ExpectedID) {
		return "", "", nil, directoryError(409, "directory_changed", "目录已被修改或替换，请刷新后重新选择")
	}
	if req.Action == "mkdir" && req.ExpectedID != "" && row[".id"] != req.ExpectedID {
		return "", "", nil, directoryError(409, "directory_changed", "父目录已被修改，请刷新后重新选择")
	}
	var refs []string
	if req.Action != "mkdir" {
		refs, err = s.directoryReferences(ctx, d)
		if err != nil {
			return "", "", nil, err
		}
		if reason := protectedDirectory(p, rows, disks, refs); reason != "" {
			return "", "", nil, directoryError(403, "directory_protected", reason)
		}
	}
	if req.Action == "delete" {
		if req.ConfirmPath != p {
			return "", "", nil, directoryError(400, "directory_confirmation_required", "请确认完整目录路径及其中全部内容的删除")
		}
		return p, p, rows, nil
	}
	parent := p
	if req.Action == "rename" {
		parent = path.Dir(p)
	}
	target := path.Join(parent, req.Name)
	if _, err := DirectoryPath(target); err != nil {
		return "", "", nil, directoryError(400, "invalid_path", "最终目录路径过长或无效")
	}
	if req.Action == "rename" {
		if reason := protectedDirectory(target, rows, disks, refs); reason != "" {
			return "", "", nil, directoryError(403, "directory_protected", reason)
		}
	}
	if directoryRow(rows, target) != nil {
		return "", "", nil, directoryError(409, "directory_exists", "同名文件或文件夹已存在")
	}
	return target, p, rows, nil
}

func (s *Service) reconcileDirectory(ctx context.Context, d config.DeviceConfig, record *directoryRecord) (DirectoryMutation, error) {
	rows, err := s.ReaderFor(d).ContainerRead(ctx, routeros.ContainerFiles)
	confirmed := err == nil
	result := record.result
	switch result.Action {
	case "mkdir":
		row := directoryRow(rows, result.Path)
		confirmed = confirmed && row != nil && row["type"] == "directory"
	case "rename":
		row := directoryRow(rows, result.Path)
		confirmed = confirmed && row != nil && row["type"] == "directory" && directoryRow(rows, result.PreviousPath) == nil
		for _, old := range record.oldRows {
			p, _ := DirectoryPath(old["name"])
			if withinDirectory(p, result.PreviousPath) && p != result.PreviousPath {
				confirmed = confirmed && directoryRow(rows, result.Path+strings.TrimPrefix(p, result.PreviousPath)) != nil && directoryRow(rows, p) == nil
			}
		}
	case "delete":
		for _, row := range rows {
			p, _ := DirectoryPath(row["name"])
			if withinDirectory(p, result.Path) {
				confirmed = false
			}
		}
	}
	if confirmed {
		return s.finishDirectory(record, "succeeded", nil)
	}
	return s.finishDirectory(record, "unknown", directoryError(409, "directory_outcome_unknown", "操作结果尚未确认。请刷新确认结果；系统不会重复执行写入"))
}

func (s *Service) finishDirectory(record *directoryRecord, state string, err error) (DirectoryMutation, error) {
	s.directoryMu.Lock()
	defer s.directoryMu.Unlock()
	record.result.State, record.err, record.processing = state, err, false
	if state != "unknown" {
		record.oldRows = nil
	}
	if state != "unknown" && record.release != nil {
		record.release()
		record.release = nil
	}
	return record.result, err
}
