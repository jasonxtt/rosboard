package containers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path"
	"strings"

	"github.com/google/uuid"
	"rosboard/internal/config"
	"rosboard/internal/routeros"
)

func (s *Service) UploadArchive(ctx context.Context, d config.DeviceConfig, gate *routeros.DeviceWriteGate, name string, reader io.Reader) (ImageArchive, error) {
	n, err := s.nativeDevice(d)
	if err != nil {
		return ImageArchive{}, err
	}
	if d.ContainerSSHHostKey == "" {
		return ImageArchive{}, operationError(409, "ssh_key_required", "上传需要管理员配置并核验设备 SSH 指纹")
	}
	n.mu.Lock()
	if activeRecord(n) != nil {
		n.mu.Unlock()
		return ImageArchive{}, operationError(409, "device_busy", "设备有未完成任务")
	}
	n.mu.Unlock()
	if gate == nil {
		gate = s.native.gate
	}
	release, ok := gate.TryAcquire(d.ID)
	if !ok {
		return ImageArchive{}, operationError(409, "device_busy", "设备正在写入")
	}
	defer release()
	file, err := os.CreateTemp(path.Dir(n.file), ".image-")
	if err != nil {
		return ImageArchive{}, err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	hash := sha256.New()
	size, err := io.Copy(io.MultiWriter(file, hash), io.LimitReader(reader, MaxArchiveBytes+1))
	if err != nil {
		return ImageArchive{}, err
	}
	archive, err := InspectArchive(file, name, size)
	if err != nil {
		return ImageArchive{}, operationError(400, "invalid_archive", err.Error())
	}
	snap, err := s.fresh(ctx, d)
	if err != nil {
		return ImageArchive{}, err
	}
	if archive.Architecture != OCIArchitecture(snap.Options.Architecture) {
		return ImageArchive{}, operationError(400, "archive_architecture", "镜像架构与所选设备不匹配")
	}
	var disk *Disk
	for i := range snap.Options.Disks {
		candidate := &snap.Options.Disks[i]
		if candidate.Writable && candidate.FreeBytes > size*2 && (disk == nil || candidate.FreeBytes > disk.FreeBytes) {
			disk = candidate
		}
	}
	if disk == nil {
		return ImageArchive{}, operationError(400, "archive_space", "可用磁盘空间不足，请至少保留归档大小两倍的空间")
	}
	archive.ID = uuid.NewString()
	archive.SHA256 = hex.EncodeToString(hash.Sum(nil))
	archive.RemotePath = path.Join("/", disk.Name, "rosboard", "images", archive.ID+".tar")
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return ImageArchive{}, err
	}
	if s.ArchiveTransfer == nil {
		return ImageArchive{}, errors.New("archive transfer unavailable")
	}
	err = s.ArchiveTransfer(ctx, d, archive, file)
	if err != nil {
		return ImageArchive{}, operationError(502, "archive_transfer_failed", "镜像传输或校验失败，请检查 SSH 指纹、权限与连接；临时文件不会用于创建")
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	n.state.Archives = append(n.state.Archives, archive)
	if err = n.save(); err != nil {
		return ImageArchive{}, err
	}
	return archive, nil
}
func transferArchive(ctx context.Context, d config.DeviceConfig, archive ImageArchive, reader io.Reader) error {
	return routeros.UploadContainerArchive(ctx, d.RouterOS.BaseURL, d.RouterOS.Username, d.RouterOS.Password, strings.TrimSpace(d.ContainerSSHHostKey), archive.RemotePath, reader, archive.Bytes, archive.SHA256)
}
