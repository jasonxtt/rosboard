package api

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"rosboard/internal/config"
	"rosboard/internal/routeros"
)

var managedUserName = regexp.MustCompile(`^rosboard_[0-9a-f]{16}$`)
var routerObjectID = regexp.MustCompile(`^\*[0-9a-fA-F]+$`)

// Encode every byte so quotes, dollar interpolation, controls and Unicode can
// never introduce RouterOS source code. IDs are additionally shape-checked.
func routerScriptLiteral(value string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, c := range []byte(value) {
		fmt.Fprintf(&b, "\\%02X", c)
	}
	b.WriteByte('"')
	return b.String()
}

func recordedTransferAccount(d config.DeviceConfig) bool {
	a := d.ManagedAccount
	return a != nil && managedUserName.MatchString(a.Username) && a.Username == d.RouterOS.Username && a.GroupName == strings.Replace(a.Username, "rosboard_", "rosboard_g_", 1)
}

func managedRoleUpgradeScript(i routeros.ManagedRoleIdentity) (string, bool) {
	if !routerObjectID.MatchString(i.UserID) || !routerObjectID.MatchString(i.GroupID) || i.RouterName == "" || routeros.ManagedPolicyState(i.Policy) == "reject" {
		return "", false
	}
	return "{\n" + routeros.ManagedPolicyScript() + fmt.Sprintf(`:local rbGuard do={
    :if ([/system identity get name] != %s) do={ :error "rosboard router identity changed" }
    :local uid [/user find where name=%s]
    :local gid [/user group find where name=%s]
    :if (([:len $uid] != 1) || ([:len $gid] != 1) || ([:tostr $uid] != %s) || ([:tostr $gid] != %s)) do={ :error "rosboard recorded identity changed" }
    :if (([/user get $uid group] != %s) || ([/user get $uid comment] != "Managed by rosboard") || ([/user get $uid disabled] = true)) do={ :error "rosboard ownership changed" }
    :local members [/user find where group=%s]
    :if (([:len $members] != 1) || ([:tostr $members] != [:tostr $uid])) do={ :error "rosboard group is shared" }
}
$rbGuard
:local rbGroupId [/user group find where name=%s]
:local rbState [$rbRole [/user group get $rbGroupId policy]]
:if ($rbState = "reject") do={ :error "rosboard policy drift; unchanged" }
:if ($rbState = "old") do={
    $rbGuard
    :if ([$rbRole [/user group get $rbGroupId policy]] != "old") do={ :error "rosboard policy drift before write; unchanged" }
    /user group set $rbGroupId policy=%s
}
$rbGuard
:if ([$rbRole [/user group get $rbGroupId policy]] != "new") do={ :error "rosboard upgrade outcome unknown; stop and inspect manually" }
:put "rosboard transfer role ready; password preserved; verify read connectivity in rosboard"
}
`, routerScriptLiteral(i.RouterName), routerScriptLiteral(i.Username), routerScriptLiteral(i.GroupName), routerScriptLiteral(i.UserID), routerScriptLiteral(i.GroupID), routerScriptLiteral(i.GroupName), routerScriptLiteral(i.GroupName), routerScriptLiteral(i.GroupName), routeros.ManagedPolicyNew), true
}

func (s *Server) serveAccountTransfer(w http.ResponseWriter, r *http.Request, deviceID string) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	d, found := s.configSnapshot().Device(deviceID)
	if !found {
		writeAPIError(w, 404, "device_not_found", "device not found")
		return
	}
	if d.Archived || !d.Enabled || !recordedTransferAccount(d) {
		writeAPIError(w, 409, "managed_identity_required", "需要已记录且匹配的专用接入账号；不能根据名称前缀升级")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	i, err := routeros.NewClient(d.RouterOS.BaseURL, d.RouterOS.Username, d.RouterOS.Password).ManagedRoleIdentity(ctx, d.ManagedAccount.Username, d.ManagedAccount.GroupName)
	if err != nil {
		writeAPIError(w, 409, "managed_role_unverified", "无法确认专用账号、独占成员或精确权限；未生成升级脚本，未修改设备")
		return
	}
	script, ok := managedRoleUpgradeScript(i)
	if !ok {
		writeAPIError(w, 409, "managed_role_unverified", "账号身份无效；未生成升级脚本")
		return
	}
	state := routeros.ManagedPolicyState(i.Policy)
	warning := "需管理员执行脚本后重新检查。请确保 SSH 服务、端口和来源限制允许连接；首次传输会自动配置连接校验。"
	readAccess := false
	if state == "new" {
		warning = "已声明传输权限；尚未验证 SFTP 读取连接。读取检查不能证明上传成功。"
		credentials := d.ContainerTransferCredentials()
		if credentials.HostKey != "" {
			if err := routeros.CheckContainerTransfer(ctx, d.RouterOS.BaseURL, credentials.Username, credentials.Password, credentials.HostKey, credentials.Port); err == nil {
				readAccess = true
				warning = "SFTP 认证与目录读取通过；未执行写入，不能据此认定上传成功。"
			} else {
				warning = "SFTP 读取不可用；请检查 SSH 服务、端口、来源限制、凭据或设备连接变化。监控仍可使用。"
			}
		}
	}
	writeJSON(w, 200, map[string]any{"state": state, "script": script, "readAccess": readAccess, "writeVerified": false, "warning": warning})
}
