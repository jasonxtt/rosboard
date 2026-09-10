package diagnostics

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"rosboard/internal/policyv2"
	"rosboard/internal/routeros"
)

const (
	deepRunTimeout          = 20 * time.Second
	maxSnapshotObjectReport = 2000
)

// EndpointSnapshot records the one read made for a RouterOS menu during a
// deep run. Objects are the bounded, proplist-filtered evidence returned by
// that read; credentials are never part of a PolicyList response.
type EndpointSnapshot struct {
	Endpoint    string              `json:"endpoint"`
	Purpose     string              `json:"purpose"`
	SharedBy    []string            `json:"sharedBy"`
	Fields      []string            `json:"fields"`
	Required    bool                `json:"required"`
	ReadCount   int                 `json:"readCount"`
	CacheHits   int                 `json:"cacheHits,omitempty"`
	ObjectCount int                 `json:"objectCount"`
	Objects     []map[string]string `json:"objects,omitempty"`
	Truncated   bool                `json:"truncated,omitempty"`
	Error       string              `json:"error,omitempty"`
}

type EvidenceSnapshot struct {
	CapturedAt  time.Time          `json:"capturedAt"`
	Fingerprint string             `json:"fingerprint,omitempty"`
	Endpoints   []EndpointSnapshot `json:"endpoints"`
}

type DeepReport struct {
	Report
	Snapshot     EvidenceSnapshot           `json:"snapshot"`
}

type endpointRead struct {
	menu      routeros.ReadMenu
	fields    []string
	objects   []routeros.RouterOSObject
	err       error
	readCount int
	cacheHits int
}

// snapshotReader is deliberately a read-through cache at the RouterOS menu
// boundary. The scanner remains the only consumer in Slice 2, but the cache
// makes the one-snapshot invariant explicit and prevents a future checker
// from issuing a second request for the same endpoint during one deep run.
type snapshotReader struct {
	source policyv2.PolicyReader
	reads  map[routeros.ReadMenu]*endpointRead
	order  []routeros.ReadMenu
}

func newSnapshotReader(source policyv2.PolicyReader) *snapshotReader {
	return &snapshotReader{source: source, reads: make(map[routeros.ReadMenu]*endpointRead)}
}

func (r *snapshotReader) PolicyList(ctx context.Context, menu routeros.ReadMenu, fields []string) ([]routeros.RouterOSObject, error) {
	if cached, ok := r.reads[menu]; ok {
		cached.cacheHits++
		return cloneRouterOSObjects(cached.objects), cached.err
	}
	read := &endpointRead{menu: menu, fields: append([]string(nil), fields...), readCount: 1}
	r.reads[menu] = read
	r.order = append(r.order, menu)
	if r.source == nil {
		read.err = errors.New("RouterOS reader is unavailable")
		return nil, read.err
	}
	objects, err := r.source.PolicyList(ctx, menu, fields)
	read.objects = cloneRouterOSObjects(objects)
	read.err = err
	return cloneRouterOSObjects(objects), err
}

func (r *snapshotReader) evidence(capturedAt time.Time, fingerprint string) EvidenceSnapshot {
	endpoints := make([]EndpointSnapshot, 0, len(r.order))
	for _, menu := range r.order {
		read := r.reads[menu]
		if read == nil {
			continue
		}
		objects := make([]map[string]string, 0, minInt(len(read.objects), maxSnapshotObjectReport))
		for index, object := range read.objects {
			if index >= maxSnapshotObjectReport {
				break
			}
			copy := make(map[string]string, len(object))
			for key, value := range object {
				copy[key] = value
			}
			objects = append(objects, copy)
		}
		entry := EndpointSnapshot{
			Endpoint:    string(menu),
			Purpose:     endpointPurpose(menu),
			SharedBy:    endpointSharedBy(menu),
			Fields:      append([]string(nil), read.fields...),
			Required:    endpointRequired(menu),
			ReadCount:   read.readCount,
			CacheHits:   read.cacheHits,
			ObjectCount: len(read.objects),
			Objects:     objects,
			Truncated:   len(read.objects) > len(objects),
		}
		if read.err != nil {
			entry.Error = read.err.Error()
		}
		endpoints = append(endpoints, entry)
	}
	return EvidenceSnapshot{CapturedAt: capturedAt, Fingerprint: fingerprint, Endpoints: endpoints}
}

func endpointRequired(menu routeros.ReadMenu) bool {
	switch menu {
	case routeros.ReadMenuSystemResource, routeros.ReadMenuInterface, routeros.ReadMenuIPRoute:
		return true
	default:
		return false
	}
}

func cloneRouterOSObjects(objects []routeros.RouterOSObject) []routeros.RouterOSObject {
	if objects == nil {
		return nil
	}
	result := make([]routeros.RouterOSObject, 0, len(objects))
	for _, object := range objects {
		copy := make(routeros.RouterOSObject, len(object))
		for key, value := range object {
			copy[key] = value
		}
		result = append(result, copy)
	}
	return result
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func endpointPurpose(menu routeros.ReadMenu) string {
	switch menu {
	case routeros.ReadMenuSystemResource:
		return "记录 RouterOS 设备身份和版本。"
	case routeros.ReadMenuInterface:
		return "提供接口名称、类型和启用状态，供 WAN 与策略入口识别共用。"
	case routeros.ReadMenuInterfaceList:
		return "解析 LAN/用户接口列表和策略入口推荐所需证据。"
	case routeros.ReadMenuInterfaceListMember:
		return "解析接口列表成员和推荐覆盖关系。"
	case routeros.ReadMenuBridgePort:
		return "识别物理接口是否为 Bridge 从端口。"
	case routeros.ReadMenuIPAddress, routeros.ReadMenuIPv6Address:
		return "提供接口地址证据，用于推荐展示和角色解释。"
	case routeros.ReadMenuIPRoute, routeros.ReadMenuIPv6Route:
		return "提供默认路由和即时出接口证据，用于 WAN 排除。"
	case routeros.ReadMenuIPDHCPClient, routeros.ReadMenuIPv6DHCPClient:
		return "补充已绑定 DHCP 客户端的默认路由证据。"
	case routeros.ReadMenuPPPoEClient:
		return "识别 PPPoE 底层父接口，避免把父接口当作策略入口。"
	default:
		return "策略入口诊断所需的 RouterOS 只读证据。"
	}
}

func endpointSharedBy(menu routeros.ReadMenu) []string {
	switch menu {
	case routeros.ReadMenuSystemResource:
		return []string{"topology.discovery", "deep-report"}
	case routeros.ReadMenuInterface:
		return []string{"wan-discovery", "traffic-ingress-discovery"}
	case routeros.ReadMenuInterfaceList, routeros.ReadMenuInterfaceListMember:
		return []string{"wan-discovery", "traffic-ingress-discovery"}
	case routeros.ReadMenuBridgePort:
		return []string{"traffic-ingress-discovery"}
	case routeros.ReadMenuIPAddress, routeros.ReadMenuIPv6Address:
		return []string{"traffic-ingress-discovery"}
	case routeros.ReadMenuIPRoute, routeros.ReadMenuIPv6Route, routeros.ReadMenuIPDHCPClient, routeros.ReadMenuIPv6DHCPClient, routeros.ReadMenuPPPoEClient:
		return []string{"wan-discovery"}
	default:
		return []string{"topology.discovery"}
	}
}

// Deep performs the quick report plus one read-only RouterOS topology pass.
// All topology evidence and ingress decisions come from the same scanner
// invocation and the same snapshotReader.
func (r Runner) Deep(ctx context.Context) DeepReport {
	deepCtx, cancel := context.WithTimeout(ctx, deepRunTimeout)
	defer cancel()

	quick := r.Quick(deepCtx)
	quick.Mode = ModeDeep
	report := DeepReport{Report: quick}

	generatedAt := quick.GeneratedAt
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}
	if r.Device.Archived || !r.Device.Enabled || !r.Device.RouterOS.Configured() {
		report.Findings = append(report.Findings, Finding{
			ID:             "topology.deep",
			Group:          "topology",
			Status:         StatusSkipped,
			Title:          "网络拓扑与策略入口",
			Summary:        "设备未启用或 RouterOS 尚未配置，暂不执行全面体检。",
			AffectsOverall: false,
		})
		report.Overall = OverallFor(report.Findings)
		report.Snapshot = EvidenceSnapshot{CapturedAt: generatedAt, Endpoints: []EndpointSnapshot{}}
		return report
	}
	if r.PolicyReader == nil {
		report.Findings = append(report.Findings, Finding{
			ID:             "topology.deep",
			Group:          "topology",
			Status:         StatusError,
			Title:          "网络拓扑与策略入口",
			Summary:        "策略 RouterOS 只读运行时不可用，无法执行全面体检。",
			Recommendation: "确认设备策略运行时已初始化后重新执行全面体检。",
			AffectsOverall: false,
		})
		report.Overall = OverallFor(report.Findings)
		report.Snapshot = EvidenceSnapshot{CapturedAt: generatedAt, Endpoints: []EndpointSnapshot{}}
		return report
	}

	reader := newSnapshotReader(r.PolicyReader)
	discovery, scanErr := policyv2.NewScanner(reader).Scan(deepCtx, r.Device.ID)
	report.Snapshot = reader.evidence(generatedAt, discovery.Snapshot.Fingerprint)

	finding := Finding{
		ID:             "topology.deep",
		Group:          "topology",
		Title:          "网络拓扑与策略入口",
		AffectsOverall: false,
		Evidence: map[string]any{
			"available":                discovery.Available,
			"fingerprint":              discovery.Snapshot.Fingerprint,
			"warnings":                 append([]string(nil), discovery.Warnings...),
			"trafficIngressCandidates": discovery.TrafficIngress,
			"wanCandidates":            discovery.WANs,
		},
	}
	if scanErr != nil {
		finding.Status = StatusError
		finding.Summary = fmt.Sprintf("全面体检无法完成：%s。", summarizeDeepError(scanErr))
		finding.Recommendation = "检查 RouterOS 只读权限、连通性和服务日志后重试。"
		if errors.Is(scanErr, context.DeadlineExceeded) || errors.Is(deepCtx.Err(), context.DeadlineExceeded) {
			finding.Summary = "全面体检超过时间限制，已保留已读取的 RouterOS 证据。"
			finding.Recommendation = "检查 RouterOS 响应时间和网络连通性后重试。"
		}
	} else {
		onlyWireGuard := onlyWireGuardCandidates(discovery.TrafficIngress)
		switch {
		case len(discovery.Warnings) > 0:
			finding.Status = StatusWarning
			finding.Summary = "网络拓扑已读取，但部分可选 RouterOS 证据读取失败。"
			finding.Recommendation = "查看快照读取错误，确认是否需要补充 RouterOS 权限。"
		case len(discovery.TrafficIngress) == 0:
			finding.Status = StatusWarning
			finding.Summary = "当前没有可推荐的策略入口；这不等于策略路由不可用。"
			finding.Recommendation = "创建规则时可直接选择任意 RouterOS 接口或接口列表，推荐分析只作提示，应用前会再次执行 fresh preflight。"
		case onlyWireGuard:
			finding.Status = StatusWarning
			finding.Summary = "当前 rosboard 推荐的策略入口只有 WireGuard，可能表示拓扑识别异常；这不等于策略路由不可用。"
			finding.Recommendation = "查看被排除的 Bridge、物理接口和 WAN 证据；需要时直接选择真实 RouterOS 接口或接口列表，应用前会再次执行 fresh preflight。"
		default:
			finding.Status = StatusOK
			finding.Summary = "网络拓扑和策略入口推荐分析已完成全面体检。"
		}
	}
	report.Findings = append(report.Findings, finding)
	report.Overall = OverallFor(report.Findings)
	return report
}

func summarizeDeepError(err error) string {
	if err == nil {
		return "unknown error"
	}
	message := strings.TrimSpace(err.Error())
	if message == "" {
		return "unknown error"
	}
	return message
}

func onlyWireGuardCandidates(candidates []policyv2.TrafficIngressCandidate) bool {
	if len(candidates) == 0 {
		return false
	}
	for _, candidate := range candidates {
		if candidate.Kind != "wireguard" {
			return false
		}
	}
	return true
}
