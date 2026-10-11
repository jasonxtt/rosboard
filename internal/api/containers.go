package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"rosboard/internal/containers"
	"rosboard/internal/routeros"
)

func (s *Server) serveContainers(writer http.ResponseWriter, request *http.Request) {
	parts := strings.Split(strings.TrimPrefix(strings.TrimPrefix(request.URL.Path, "/api/containers"), "/"), "/")
	resolving := len(parts) == 1 && parts[0] == "resolve"
	directories := len(parts) == 1 && parts[0] == "directories"
	deviceID := strings.TrimSpace(request.URL.Query().Get("device"))
	if deviceID == "" {
		writeAPIError(writer, 400, "device_required", "缺少 device 参数")
		return
	}
	device, found := s.configSnapshot().Device(deviceID)
	if !found || device.Archived || !device.Enabled {
		writeAPIError(writer, 404, "device_not_found", "设备不存在或未启用")
		return
	}
	// Only a private server configuration can opt a device into native writes.
	mutation := request.Method != http.MethodGet && !((resolving || directories) && request.Method == http.MethodPost)
	if mutation && !device.ContainerWrites {
		writeAPIError(writer, 403, "container_read_only", "此设备未启用容器写入")
		return
	}
	var gate *routeros.DeviceWriteGate
	if s.policy != nil {
		gate = s.policy.WriteGate()
	}
	if len(parts) >= 1 && parts[0] == "jobs" {
		if len(parts) < 2 {
			writeAPIError(writer, 404, "not_found", "接口不存在")
			return
		}
		var job containers.Job
		var err error
		if len(parts) == 3 && parts[2] == "recover" && request.Method == http.MethodPost {
			job, err = s.containers.RecoverNative(device, gate, parts[1])
		} else if len(parts) == 2 && request.Method == http.MethodGet {
			job, err = s.containers.NativeJob(device, parts[1])
		} else {
			methodNotAllowed(writer, http.MethodGet)
			return
		}
		if err != nil {
			writeDirectoryError(writer, err)
			return
		}
		writeJSON(writer, 200, job)
		return
	}
	if len(parts) == 1 && parts[0] == "actions" && request.Method == http.MethodPost {
		var operation containers.ActionRequest
		if decodeJSONBody(writer, request, &operation) != nil {
			return
		}
		ctx, cancel := context.WithTimeout(request.Context(), 20*time.Second)
		defer cancel()
		job, err := s.containers.Submit(ctx, device, gate, operation)
		if err != nil {
			writeDirectoryError(writer, err)
			return
		}
		writeJSON(writer, 202, job)
		return
	}
	if len(parts) == 2 && parts[0] == "images" && parts[1] == "upload" && request.Method == http.MethodPost {
		request.Body = http.MaxBytesReader(writer, request.Body, containers.MaxArchiveBytes+(1<<20))
		multipart, err := request.MultipartReader()
		if err != nil {
			writeAPIError(writer, 400, "invalid_archive", "请选择镜像 tar 文件")
			return
		}
		part, err := multipart.NextPart()
		if err != nil || part.FormName() != "file" || part.FileName() == "" {
			writeAPIError(writer, 400, "invalid_archive", "缺少镜像文件")
			return
		}
		defer part.Close()
		ctx, cancel := context.WithTimeout(request.Context(), 5*time.Minute)
		defer cancel()
		archive, err := s.containers.UploadArchive(ctx, device, gate, part.FileName(), part)
		if err != nil {
			writeDirectoryError(writer, err)
			return
		}
		writeJSON(writer, 201, archive)
		return
	}
	if mutation {
		writeAPIError(writer, 403, "container_read_only", "不支持此写入接口")
		return
	}
	var draft containers.Draft
	if resolving && decodeJSONBody(writer, request, &draft) != nil {
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 20*time.Second)
	defer cancel()
	snapshot, err := s.containers.Snapshot(ctx, device)
	if err != nil {
		writeAPIError(writer, 502, "container_read_failed", "读取容器信息失败，请检查设备连接与权限")
		return
	}
	switch {
	case len(parts) == 1 && parts[0] == "directories":
		if request.Method == http.MethodPost {
			var operation containers.DirectoryRequest
			if decodeJSONBody(writer, request, &operation) != nil {
				return
			}
			var gate *routeros.DeviceWriteGate
			if s.policy != nil {
				gate = s.policy.WriteGate()
			}
			result, err := s.containers.MutateDirectory(ctx, device, snapshot.Options.Disks, gate, operation)
			if err != nil {
				writeDirectoryError(writer, err)
				return
			}
			status := http.StatusOK
			if operation.Action == "mkdir" {
				status = http.StatusCreated
			}
			writeJSON(writer, status, result)
			return
		}
		if _, err := containers.DirectoryPath(request.URL.Query().Get("path")); err != nil {
			writeAPIError(writer, 400, "invalid_path", "目录路径无效")
			return
		}
		listing, err := s.containers.Directories(ctx, device, snapshot.Options.Disks, request.URL.Query().Get("path"))
		if err != nil {
			writeDirectoryError(writer, err)
			return
		}
		writeJSON(writer, 200, listing)
	case resolving:
		writeJSON(writer, 200, containers.Resolve(draft, snapshot))
	case len(parts) == 1 && parts[0] == "options":
		writeJSON(writer, 200, map[string]any{"options": snapshot.Options, "capabilities": snapshot.Capabilities})
	case request.URL.Path == "/api/containers" || (len(parts) == 1 && parts[0] == ""):
		writeJSON(writer, 200, snapshot)
	case len(parts) == 1 || (len(parts) == 2 && parts[1] == "logs"):
		var selected *containers.Item
		for i := range snapshot.Items {
			if snapshot.Items[i].ID == parts[0] {
				selected = &snapshot.Items[i]
				break
			}
		}
		if selected == nil {
			writeAPIError(writer, 404, "container_not_found", "当前设备没有此容器")
			return
		}
		if len(parts) == 1 {
			writeJSON(writer, 200, selected)
			return
		}
		logs, err := s.containers.Logs(ctx, device, selected.ID, selected.Name)
		if err != nil {
			writeAPIError(writer, 502, "container_logs_unavailable", "容器日志不可用，请检查 RouterOS 版本与权限")
			return
		}
		writeJSON(writer, 200, map[string]any{"logs": logs})
	default:
		writeAPIError(writer, 404, "not_found", "接口不存在")
	}
}

func writeDirectoryError(writer http.ResponseWriter, err error) {
	var directory *containers.DirectoryError
	if errors.As(err, &directory) {
		writeAPIError(writer, directory.Status, directory.Code, directory.Message)
		return
	}
	writeAPIError(writer, 502, "directory_read_failed", "读取目录失败，请检查路径与 RouterOS 文件读取权限")
}
