package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"rosboard/internal/containers"
)

func (s *Server) serveContainers(writer http.ResponseWriter, request *http.Request) {
	parts := strings.Split(strings.TrimPrefix(strings.TrimPrefix(request.URL.Path, "/api/containers"), "/"), "/")
	resolving := len(parts) == 1 && parts[0] == "resolve"
	if request.Method != http.MethodGet && !(resolving && request.Method == http.MethodPost) {
		writeAPIError(writer, http.StatusForbidden, "container_read_only", "当前阶段仅支持读取；真实 RouterOS 操作尚未启用")
		return
	}
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
		if _, err := containers.DirectoryPath(request.URL.Query().Get("path")); err != nil {
			writeAPIError(writer, 400, "invalid_path", "目录路径无效")
			return
		}
		listing, err := s.containers.Directories(ctx, device, snapshot.Options.Disks, request.URL.Query().Get("path"))
		if err != nil {
			writeAPIError(writer, 502, "directory_read_failed", "读取目录失败，请检查路径与 RouterOS 文件读取权限")
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
