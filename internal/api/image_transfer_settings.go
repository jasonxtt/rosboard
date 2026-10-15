package api

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"time"

	"rosboard/internal/config"
	"rosboard/internal/downloadnet"
	"rosboard/internal/routeros"
)

func transferSettingsRevision(d config.DeviceConfig) string {
	c := d.ContainerTransferCredentials()
	return downloadnet.Revision(fmt.Sprintf("%x", connectionFingerprint(d.RouterOS.BaseURL, d.RouterOS.Username, d.RouterOS.Password)) + c.Username + c.Password + c.HostKey + string(rune(c.Port)))
}
func transferSettingsStatus(d config.DeviceConfig) any {
	c := d.ContainerTransferCredentials()
	mode := "connection"
	if d.ContainerTransfer != nil {
		mode = "dedicated"
	}
	return map[string]any{"mode": mode, "username": c.Username, "passwordSet": c.Password != "", "port": c.Port, "hostKey": c.HostKey, "revision": transferSettingsRevision(d)}
}
func (s *Server) serveImageTransferSettings(w http.ResponseWriter, r *http.Request, id string) {
	w.Header().Set("Cache-Control", "no-store")
	d, ok := s.configSnapshot().Device(id)
	if !ok || d.Archived {
		writeAPIError(w, 404, "device_not_found", "设备不存在")
		return
	}
	if r.Method == http.MethodGet {
		writeJSON(w, 200, transferSettingsStatus(d))
		return
	}
	if r.Method != http.MethodPut {
		methodNotAllowed(w, "GET, PUT")
		return
	}
	var body struct {
		Mode             string `json:"mode"`
		Username         string `json:"username"`
		Password         string `json:"password"`
		UseSavedPassword bool   `json:"useSavedPassword"`
		Port             int    `json:"port"`
		HostKey          string `json:"hostKey"`
		Revision         string `json:"revision"`
		TestOnly         bool   `json:"testOnly"`
		Clear            bool   `json:"clear"`
	}
	if decodeJSONBody(w, r, &body) != nil {
		return
	}
	if body.Revision != transferSettingsRevision(d) {
		writeAPIError(w, 409, "transfer_changed", "设备或传输身份已变化，请重新读取")
		return
	}
	candidate := d
	if body.Clear {
		candidate.ContainerTransfer = nil
		candidate.ContainerSSHHostKey = ""
		candidate.ContainerSSHPort = 0
	} else {
		key := strings.TrimSpace(body.HostKey)
		decoded, e := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(key, "SHA256:"))
		if (key != "" && (e != nil || !strings.HasPrefix(key, "SHA256:") || len(decoded) != 32)) || body.Port < 1 || body.Port > 65535 {
			writeAPIError(w, 400, "invalid_transfer", "请填写有效的 SSH 端口")
			return
		}
		switch body.Mode {
		case "connection":
			candidate.ContainerTransfer = nil
			candidate.ContainerSSHHostKey = key
			candidate.ContainerSSHPort = body.Port
		case "dedicated":
			password := body.Password
			if body.UseSavedPassword {
				if password != "" || d.ContainerTransfer == nil || d.ContainerTransfer.Username != body.Username {
					writeAPIError(w, 400, "transfer_credentials_required", "切换账号时请重新填写完整凭据")
					return
				}
				password = d.ContainerTransfer.Password
			}
			if body.Username == "" || password == "" || strings.ContainsAny(body.Username, "\r\n\x00") {
				writeAPIError(w, 400, "transfer_credentials_required", "请填写专用传输账号和密码")
				return
			}
			candidate.ContainerTransfer = &config.ContainerTransferConfig{Username: body.Username, Password: password, HostKey: key, Port: body.Port}
		default:
			writeAPIError(w, 400, "invalid_transfer", "账号来源无效")
			return
		}
		c := candidate.ContainerTransferCredentials()
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		if c.HostKey == "" {
			candidate, e = verifyAutomaticTransfer(ctx, candidate)
		} else {
			e = routeros.CheckContainerTransfer(ctx, d.RouterOS.BaseURL, c.Username, c.Password, c.HostKey, c.Port)
		}
		cancel()
		if e != nil {
			writeAPIError(w, 502, "transfer_read_failed", "SFTP 只读检查失败；请检查 SSH 服务、端口、来源限制、账号权限；设备密钥变化时请重新配置连接。设备监控不受影响。")
			return
		}
	}
	if body.TestOnly {
		writeJSON(w, 200, map[string]any{"readAccess": !body.Clear, "writeVerified": false})
		return
	}
	s.deviceSaveMu.Lock()
	defer s.deviceSaveMu.Unlock()
	current, ok := s.configSnapshot().Device(id)
	if !ok || transferSettingsRevision(current) != body.Revision {
		writeAPIError(w, 409, "transfer_changed", "设备或传输身份已变化，请重新读取")
		return
	}
	if e := s.saveSettings(func(next *config.Config) {
		for i := range next.Devices {
			if next.Devices[i].ID == id {
				next.Devices[i].ContainerTransfer = candidate.ContainerTransfer
				next.Devices[i].ContainerSSHHostKey = candidate.ContainerSSHHostKey
				next.Devices[i].ContainerSSHPort = candidate.ContainerSSHPort
			}
		}
	}); e != nil {
		writeAPIError(w, 500, "save_failed", "无法保存镜像传输设置")
		return
	}
	writeJSON(w, 200, transferSettingsStatus(candidate))
}
