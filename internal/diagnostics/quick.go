package diagnostics

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"

	"rosboard/internal/accesscontrol"
	"rosboard/internal/buildinfo"
	"rosboard/internal/config"
	"rosboard/internal/model"
	"rosboard/internal/policyv2"
	"rosboard/internal/service"
	"rosboard/internal/store"
	"rosboard/internal/update"
)

const (
	storageCheckTimeout = 2 * time.Second
	diskWarningBytes    = 1 << 30
	diskErrorBytes      = 100 << 20
)

// Runner reads existing cached/service/repository state for Quick. Quick never
// calls a RouterOS or MosDNS endpoint and never starts a refresh or update
// check; Deep opts into one separate, read-only RouterOS snapshot.
type Runner struct {
	Config       config.Config
	Device       config.DeviceConfig
	Manager      *service.MonitorManager
	PolicyReader policyv2.PolicyReader
	Store        *store.Store
	StoreError   error
	Updater      *update.Manager
	Now          func() time.Time
	StaleAfter   time.Duration
}

func (r Runner) Quick(ctx context.Context) Report {
	now := time.Now
	if r.Now != nil {
		now = r.Now
	}
	nowUTC := now().UTC()
	report := Report{
		GeneratedAt: nowUTC,
		Mode:        ModeQuick,
		DeviceID:    r.Device.ID,
		Findings:    make([]Finding, 0, 8),
	}

	report.Findings = append(report.Findings, r.runtimeFinding())
	report.Findings = append(report.Findings, r.storageFinding(ctx))

	monitorFinding, snapshot, monitorAvailable := r.monitorFindings(nowUTC)
	report.Findings = append(report.Findings, monitorFinding...)
	report.Findings = append(report.Findings, r.mosDNSFinding(monitorAvailable))

	policyState, policyStateAvailable, policyFinding := r.policyFinding(ctx)
	report.Findings = append(report.Findings, policyFinding)
	report.Findings = append(report.Findings, r.accessFinding(ctx, snapshot, monitorAvailable, policyState, policyStateAvailable))
	report.Findings = append(report.Findings, r.updateFinding())
	report.Overall = OverallFor(report.Findings)
	return report
}

func (r Runner) runtimeFinding() Finding {
	info := buildinfo.Current()
	return Finding{
		ID:             "system.runtime",
		Group:          "system",
		Status:         StatusOK,
		Title:          "rosboard 运行时",
		Summary:        fmt.Sprintf("当前版本 %s，运行平台 %s/%s。", info.Version, info.OS, info.Arch),
		Recommendation: "",
		AffectsOverall: true,
		Evidence: map[string]any{
			"version": info.Version,
			"commit":  info.Commit,
			"builtAt": info.BuiltAt,
			"os":      info.OS,
			"arch":    info.Arch,
		},
	}
}

func (r Runner) storageFinding(ctx context.Context) Finding {
	finding := Finding{
		ID:             "system.storage",
		Group:          "system",
		Title:          "本地存储",
		AffectsOverall: true,
		Evidence:       map[string]any{"dataDir": r.Config.DataDir},
	}
	if errors.Is(r.StoreError, store.ErrDeviceStoreNotOpen) {
		finding.Status = StatusSkipped
		finding.AffectsOverall = false
		finding.Summary = "设备存储尚未由运行时打开，暂不检查 SQLite。"
		finding.Recommendation = "启用设备或等待设备初始化后重新检查。"
		return finding
	}
	if r.StoreError != nil || r.Store == nil {
		finding.Status = StatusError
		finding.Summary = "设备 SQLite 存储不可用。"
		finding.Recommendation = "检查数据目录权限和服务日志。"
		return finding
	}
	info, err := os.Stat(r.Config.DataDir)
	if err != nil || !info.IsDir() {
		finding.Status = StatusError
		finding.Summary = "数据目录不可读或不是目录。"
		finding.Recommendation = "检查配置中的数据目录和服务权限。"
		return finding
	}
	directory, err := os.Open(r.Config.DataDir)
	if err != nil {
		finding.Status = StatusError
		finding.Summary = "数据目录没有读取权限。"
		finding.Recommendation = "检查服务用户对数据目录的读取权限。"
		return finding
	}
	_ = directory.Close()

	storageCtx, cancel := context.WithTimeout(ctx, storageCheckTimeout)
	defer cancel()
	if err := r.Store.ReadOnlyHealthCheck(storageCtx); err != nil {
		finding.Status = StatusError
		finding.Summary = "设备 SQLite 只读检查失败。"
		finding.Recommendation = "检查磁盘、数据目录权限和 SQLite 文件完整性。"
		finding.Evidence["sqlite"] = "failed"
		return finding
	}
	finding.Evidence["sqlite"] = "ok"

	free, total, err := filesystemSpace(r.Config.DataDir)
	if err != nil {
		finding.Status = StatusWarning
		finding.Summary = "SQLite 正常，但无法读取数据目录磁盘空间。"
		finding.Recommendation = "检查数据目录所在文件系统。"
		return finding
	}
	finding.Evidence["freeBytes"] = free
	finding.Evidence["totalBytes"] = total
	switch {
	case free <= diskErrorBytes:
		finding.Status = StatusError
		finding.Summary = "数据目录可读，但剩余磁盘空间严重不足。"
		finding.Recommendation = "释放或扩容数据目录所在磁盘。"
	case free <= diskWarningBytes:
		finding.Status = StatusWarning
		finding.Summary = "数据目录可读，但剩余磁盘空间偏低。"
		finding.Recommendation = "尽快清理或扩容数据目录所在磁盘。"
	default:
		finding.Status = StatusOK
		finding.Summary = "SQLite 和数据目录均可读，磁盘空间充足。"
	}
	return finding
}

func filesystemSpace(path string) (uint64, uint64, error) {
	if strings.TrimSpace(path) == "" {
		return 0, 0, fmt.Errorf("data directory is empty")
	}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, 0, err
	}
	return uint64(stat.Bavail) * uint64(stat.Bsize), uint64(stat.Blocks) * uint64(stat.Bsize), nil
}

func (r Runner) monitorFindings(now time.Time) ([]Finding, model.DashboardSnapshot, bool) {
	if r.Device.Archived || !r.Device.Enabled {
		return []Finding{
			{
				ID:             "routeros.connection",
				Group:          "routeros",
				Status:         StatusDisabled,
				Title:          "RouterOS 连接",
				Summary:        "该设备已停用，未启动 RouterOS 采集。",
				AffectsOverall: false,
			},
			{
				ID:             "monitor.freshness",
				Group:          "monitor",
				Status:         StatusDisabled,
				Title:          "采集快照",
				Summary:        "设备停用期间不检查采集快照。",
				AffectsOverall: false,
			},
		}, model.DashboardSnapshot{}, false
	}
	if !r.Device.RouterOS.Configured() {
		return []Finding{
			{
				ID:             "routeros.connection",
				Group:          "routeros",
				Status:         StatusError,
				Title:          "RouterOS 连接",
				Summary:        "RouterOS 连接配置不完整。",
				Recommendation: "在设备管理中补全地址、用户名和密码。",
				AffectsOverall: true,
			},
			{
				ID:             "monitor.freshness",
				Group:          "monitor",
				Status:         StatusSkipped,
				Title:          "采集快照",
				Summary:        "RouterOS 尚未完成配置，暂不检查快照。",
				AffectsOverall: false,
			},
		}, model.DashboardSnapshot{}, false
	}
	if r.Manager == nil {
		return []Finding{
			{
				ID:             "routeros.connection",
				Group:          "routeros",
				Status:         StatusError,
				Title:          "RouterOS 连接",
				Summary:        "RouterOS 采集管理器不可用。",
				Recommendation: "检查服务日志并确认采集服务已启动。",
				AffectsOverall: true,
			},
			{
				ID:             "monitor.freshness",
				Group:          "monitor",
				Status:         StatusSkipped,
				Title:          "采集快照",
				Summary:        "采集管理器不可用，暂不检查快照。",
				AffectsOverall: false,
			},
		}, model.DashboardSnapshot{}, false
	}

	statuses := r.Manager.Statuses(true, []config.DeviceConfig{r.Device})
	var status service.DeviceStatus
	found := false
	for _, item := range statuses {
		if item.ID == r.Device.ID {
			status = item
			found = true
			break
		}
	}
	connection := Finding{
		ID:             "routeros.connection",
		Group:          "routeros",
		Title:          "RouterOS 连接",
		AffectsOverall: true,
		Evidence:       map[string]any{"started": status.Healthy},
	}
	if !found || !status.Healthy {
		connection.Status = StatusError
		connection.Summary = "RouterOS 连接或采集启动失败。"
		connection.Recommendation = "检查设备凭据、网络连通性和服务日志。"
		if status.Error != "" {
			connection.Evidence["startupError"] = status.Error
		}
	} else {
		connection.Status = StatusOK
		connection.Summary = "RouterOS 采集器已启动。"
	}

	monitor, err := r.Manager.Monitor(r.Device.ID)
	if err != nil || monitor == nil {
		return []Finding{connection, {
			ID:             "monitor.freshness",
			Group:          "monitor",
			Status:         StatusError,
			Title:          "采集快照",
			Summary:        "无法读取设备的缓存采集快照。",
			Recommendation: "检查采集服务状态和服务日志。",
			AffectsOverall: true,
		}}, model.DashboardSnapshot{}, false
	}

	snapshot := monitor.Snapshot()
	freshness := Finding{
		ID:             "monitor.freshness",
		Group:          "monitor",
		Title:          "采集快照",
		AffectsOverall: true,
		Evidence:       map[string]any{"updatedAt": snapshot.Overview.UpdatedAt},
	}
	staleAfter := r.StaleAfter
	if staleAfter <= 0 {
		staleAfter = service.SnapshotStaleAfter
	}
	updatedAt := snapshot.Overview.UpdatedAt
	refreshFailure := latestRefreshFailure(snapshot.Alerts)
	switch {
	case updatedAt.IsZero():
		freshness.Status = StatusError
		freshness.Summary = "采集器尚未生成可用快照。"
		freshness.Recommendation = "等待首次采集；若持续无快照，请检查服务日志。"
	case now.Sub(updatedAt) > staleAfter:
		freshness.Status = StatusError
		freshness.Summary = fmt.Sprintf("采集快照已超过 %s 未更新。", staleAfter.Round(time.Second))
		freshness.Recommendation = "检查 RouterOS 连通性和采集服务日志。"
		freshness.Evidence["ageSeconds"] = int64(now.Sub(updatedAt).Seconds())
	case refreshFailure != nil:
		freshness.Status = StatusWarning
		freshness.Summary = "最近一次采集刷新失败，但当前缓存快照仍在 freshness 范围内。"
		freshness.Recommendation = "关注下一次刷新；若持续失败，请检查 RouterOS 连通性。"
		freshness.Evidence["refreshFailureAt"] = refreshFailure.Timestamp
		freshness.Evidence["refreshFailure"] = refreshFailure.Message
	default:
		freshness.Status = StatusOK
		freshness.Summary = "采集快照新鲜，未发现明确的刷新失败。"
	}
	return []Finding{connection, freshness}, snapshot, true
}

func latestRefreshFailure(alerts []model.AlertEvent) *model.AlertEvent {
	var latest *model.AlertEvent
	for index := range alerts {
		alert := alerts[index]
		if alert.ID != "dashboard-refresh" {
			continue
		}
		if latest == nil || alert.Timestamp.After(latest.Timestamp) {
			copy := alert
			latest = &copy
		}
	}
	return latest
}

func (r Runner) mosDNSFinding(monitorAvailable bool) Finding {
	finding := Finding{
		ID:             "recognition.mosdns",
		Group:          "recognition",
		Title:          "MosDNS 识别同步",
		AffectsOverall: false,
	}
	if r.Device.Archived || !r.Device.Enabled {
		finding.Status = StatusSkipped
		finding.Summary = "设备停用期间不检查 MosDNS。"
		return finding
	}
	if !r.Device.MosDNS.Configured() {
		finding.Status = StatusDisabled
		finding.Summary = "MosDNS 识别同步未启用。"
		return finding
	}
	if !monitorAvailable || r.Manager == nil {
		finding.Status = StatusSkipped
		finding.Summary = "采集器不可用，暂不检查 MosDNS 同步状态。"
		return finding
	}
	status := r.Manager.MosDNSStatus(r.Device.ID)
	finding.Evidence = map[string]any{
		"enabled":     status.Enabled,
		"lastAttempt": status.LastAttempt,
		"lastSuccess": status.LastSuccess,
	}
	switch {
	case status.LastError != "" && status.LastSuccess.IsZero():
		finding.Status = StatusError
		finding.Summary = "MosDNS 已配置，但尚未成功同步。"
		finding.Recommendation = "检查 MosDNS 地址、权限和服务日志。"
		finding.Evidence["lastError"] = status.LastError
	case status.LastError != "":
		finding.Status = StatusWarning
		finding.Summary = "MosDNS 最近一次同步失败，但仍有历史成功记录。"
		finding.Recommendation = "检查 MosDNS 连通性并观察下一次同步。"
		finding.Evidence["lastError"] = status.LastError
	case status.LastSuccess.IsZero():
		finding.Status = StatusWarning
		finding.Summary = "MosDNS 已配置，尚未完成首次同步。"
		finding.Recommendation = "等待同步周期结束后重新检查。"
	default:
		finding.Status = StatusOK
		finding.Summary = "MosDNS 最近一次同步成功。"
	}
	return finding
}

func (r Runner) policyFinding(ctx context.Context) (policyv2.DeviceState, bool, Finding) {
	finding := Finding{
		ID:             "policy.state",
		Group:          "policy",
		Title:          "策略路由状态",
		AffectsOverall: false,
	}
	if r.Device.Archived || !r.Device.Enabled {
		finding.Status = StatusSkipped
		finding.Summary = "设备停用期间不检查策略路由状态。"
		return policyv2.DeviceState{}, false, finding
	}
	if r.StoreError != nil || r.Store == nil {
		finding.Status = StatusSkipped
		finding.Summary = "设备存储不可用，暂不检查策略路由状态。"
		return policyv2.DeviceState{}, false, finding
	}
	state, err := r.Store.PolicyRepository().GetDeviceState(ctx)
	if err != nil {
		finding.Status = StatusError
		finding.Summary = "无法读取策略路由设备状态。"
		finding.Recommendation = "检查设备 SQLite 数据库和服务日志。"
		return policyv2.DeviceState{}, false, finding
	}
	finding.Evidence = map[string]any{
		"desiredRevision": state.DesiredRevision,
		"appliedRevision": state.AppliedRevision,
		"jobState":        state.Job.State,
		"jobPhase":        state.Job.Phase,
	}
	switch {
	case state.Job.State == "failed":
		finding.Status = StatusError
		finding.Summary = "最近一次策略路由应用失败。"
		finding.Recommendation = "打开策略路由页面检查任务错误并重新应用。"
		if state.Job.Error != "" {
			finding.Evidence["jobError"] = state.Job.Error
		}
	case !state.Applied():
		finding.Status = StatusWarning
		finding.Summary = "策略路由存在尚未应用的变更。"
		finding.Recommendation = "打开策略路由页面检查并应用待处理变更。"
	case state.Job.ID == "" && state.DesiredRevision == 0 && state.AppliedRevision == 0:
		finding.Status = StatusSkipped
		finding.Summary = "尚未建立策略路由状态。"
	default:
		finding.Status = StatusOK
		finding.Summary = "策略路由期望状态与已应用状态一致。"
	}
	return state, true, finding
}

func (r Runner) accessFinding(ctx context.Context, snapshot model.DashboardSnapshot, monitorAvailable bool, policyState policyv2.DeviceState, policyStateAvailable bool) Finding {
	finding := Finding{
		ID:             "access.state",
		Group:          "access",
		Title:          "访问控制状态",
		AffectsOverall: false,
	}
	if r.Device.Archived || !r.Device.Enabled {
		finding.Status = StatusSkipped
		finding.Summary = "设备停用期间不检查访问控制状态。"
		return finding
	}
	if r.StoreError != nil || r.Store == nil {
		finding.Status = StatusSkipped
		finding.Summary = "设备存储不可用，暂不检查访问控制状态。"
		return finding
	}
	repository := r.Store.AccessRepository()
	state, err := repository.GetState(ctx)
	if err != nil {
		finding.Status = StatusError
		finding.Summary = "无法读取访问控制 revision 状态。"
		finding.Recommendation = "检查设备 SQLite 数据库和服务日志。"
		return finding
	}
	rules, err := repository.ListRules(ctx)
	if err != nil {
		finding.Status = StatusError
		finding.Summary = "无法读取访问控制规则。"
		finding.Recommendation = "检查设备 SQLite 数据库和服务日志。"
		return finding
	}
	members, err := repository.ListMembers(ctx)
	if err != nil {
		finding.Status = StatusError
		finding.Summary = "无法读取访问控制成员。"
		finding.Recommendation = "检查设备 SQLite 数据库和服务日志。"
		return finding
	}
	finding.Evidence = map[string]any{
		"desiredRevision": state.DesiredRevision,
		"appliedRevision": state.AppliedRevision,
		"rules":           len(rules),
		"members":         len(members),
	}
	if policyStateAvailable {
		finding.Evidence["jobState"] = policyState.Job.State
	}
	if len(rules) == 0 && len(members) == 0 && state.DesiredRevision == 0 && state.AppliedRevision == 0 {
		finding.Status = StatusSkipped
		finding.Summary = "尚未建立访问控制规则。"
		return finding
	}

	switch {
	case policyStateAvailable && policyState.Job.State == "failed":
		finding.Status = StatusError
		finding.Summary = "访问控制关联的设备级应用任务失败。"
		finding.Recommendation = "检查访问控制或策略路由任务错误后重新应用。"
		return finding
	case !state.Applied():
		finding.Status = StatusWarning
		finding.Summary = "访问控制 revision 尚未应用到设备。"
		finding.Recommendation = "打开访问控制页面检查并重新同步。"
		return finding
	}

	if monitorAvailable {
		terminals := make([]accesscontrol.Terminal, 0, len(snapshot.Terminals))
		for _, terminal := range snapshot.Terminals {
			terminals = append(terminals, accesscontrol.Terminal{
				ID: terminal.ID, DisplayName: terminal.DisplayName, MACAddress: terminal.MACAddress,
				IPv4: append([]string{}, terminal.IPv4...), IPv6: append([]string{}, terminal.IPv6...),
			})
		}
		unresolved, conflicted := 0, 0
		for _, rule := range rules {
			ruleMembers := make([]accesscontrol.RuleMember, 0)
			for _, member := range members {
				if member.RuleID == rule.ID {
					ruleMembers = append(ruleMembers, member)
				}
			}
			for _, evaluation := range accesscontrol.EvaluateMembers(ruleMembers, terminals) {
				switch evaluation.State {
				case accesscontrol.MemberUnresolved:
					unresolved++
				case accesscontrol.MemberConflicted:
					conflicted++
				}
			}
		}
		finding.Evidence["unresolvedMembers"] = unresolved
		finding.Evidence["conflictedMembers"] = conflicted
		if unresolved > 0 || conflicted > 0 {
			finding.Status = StatusWarning
			finding.Summary = fmt.Sprintf("访问控制有 %d 个暂时未解析、%d 个冲突成员。", unresolved, conflicted)
			finding.Recommendation = "检查终端身份锚点和当前地址，确认后再应用访问控制。"
			return finding
		}
	} else {
		finding.Evidence["memberResolution"] = "skipped_monitor_unavailable"
		finding.Status = StatusOK
		finding.Summary = "访问控制 revision 与已应用状态一致，但采集快照不可用，未检查自动成员解析。"
		return finding
	}

	finding.Status = StatusOK
	finding.Summary = "访问控制 revision 与已应用状态一致。"
	return finding
}

func (r Runner) updateFinding() Finding {
	finding := Finding{
		ID:             "update.state",
		Group:          "update",
		Title:          "更新状态",
		AffectsOverall: false,
	}
	if r.Updater == nil {
		finding.Status = StatusSkipped
		finding.Summary = "当前服务未提供在线更新状态。"
		return finding
	}
	status := r.Updater.Status()
	finding.Evidence = map[string]any{
		"currentVersion": status.Current.Version,
		"reason":         status.Reason,
		"canInstall":     status.CanInstall,
	}
	if status.Job != nil {
		finding.Evidence["jobStage"] = status.Job.Stage
		finding.Evidence["jobId"] = status.Job.ID
		if status.Job.Message != "" {
			finding.Evidence["jobMessage"] = status.Job.Message
		}
	}
	if status.Job != nil {
		switch status.Job.Stage {
		case "recovery_required":
			finding.Status = StatusError
			finding.Summary = "更新状态需要恢复处理。"
			finding.Recommendation = "检查更新目录和服务日志，完成恢复后再重试。"
			return finding
		case "failed":
			finding.Status = StatusError
			finding.Summary = "最近一次更新任务失败。"
			finding.Recommendation = "检查更新任务详情和服务日志，确认后再重试。"
			return finding
		case "rolled_back":
			finding.Status = StatusWarning
			finding.Summary = "更新任务失败后已回滚到原版本。"
			finding.Recommendation = "检查更新任务详情和服务日志，确认原因后再重试。"
			return finding
		case "succeeded":
			// Continue with the normal update status checks below.
		default:
			if status.Job.Active() {
				finding.Status = StatusWarning
				finding.Summary = "更新任务正在进行。"
				finding.Recommendation = "等待更新任务完成，不要重复操作。"
				return finding
			}
		}
	}
	switch {
	case status.CheckError != "":
		finding.Status = StatusWarning
		finding.Summary = "最近一次更新检查失败。"
		finding.Recommendation = "在维护设置中重新检查更新。"
		finding.Evidence["checkError"] = status.CheckError
	case strings.Contains(status.Reason, "禁用") || strings.Contains(status.Reason, "需要 Linux") || strings.Contains(status.Reason, "开发构建"):
		finding.Status = StatusDisabled
		finding.Summary = "在线更新在当前安装中不可用。"
	case status.Latest != nil && status.Latest.Version != status.Current.Version:
		finding.Status = StatusWarning
		finding.Summary = fmt.Sprintf("发现可用更新 %s。", status.Latest.Version)
		finding.Recommendation = "按维护流程确认版本后再安装。"
		finding.Evidence["latestVersion"] = status.Latest.Version
	case status.Reason == "请先检查更新":
		finding.Status = StatusSkipped
		finding.Summary = "尚未检查在线更新。"
	default:
		finding.Status = StatusOK
		finding.Summary = "当前更新状态正常。"
	}
	return finding
}
