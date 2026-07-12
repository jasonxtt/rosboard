package service

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"rosboard/internal/config"
	"rosboard/internal/model"
	"rosboard/internal/routeros"
	"rosboard/internal/store"
)

type Monitor struct {
	cfg    config.Config
	client *routeros.Client
	store  *store.Store
	logger *log.Logger

	refreshMu       sync.Mutex
	mu              sync.RWMutex
	snapshot        model.DashboardSnapshot
	terminalDetails map[string]model.TerminalDetail
}

func NewMonitor(cfg config.Config, client *routeros.Client, store *store.Store, logger *log.Logger) *Monitor {
	return &Monitor{
		cfg:    cfg,
		client: client,
		store:  store,
		logger: logger,
	}
}

func (m *Monitor) Start(ctx context.Context) error {
	if err := m.refresh(ctx); err != nil {
		return err
	}

	go func() {
		ticker := time.NewTicker(time.Duration(m.cfg.PollIntervalSeconds) * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := m.refresh(ctx); err != nil {
					m.logger.Printf("refresh failed: %v", err)
					m.recordRefreshError(err)
				}
			}
		}
	}()

	return nil
}

func (m *Monitor) recordRefreshError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	alert := model.AlertEvent{ID: "dashboard-refresh", Level: "error", Source: "核心采集", Message: err.Error(), Timestamp: time.Now().UTC()}
	alerts := make([]model.AlertEvent, 0, len(m.snapshot.Alerts)+1)
	alerts = append(alerts, alert)
	for _, existing := range m.snapshot.Alerts {
		if existing.ID != alert.ID && len(alerts) < 50 {
			alerts = append(alerts, existing)
		}
	}
	m.snapshot.Alerts = alerts
}

func (m *Monitor) Snapshot() model.DashboardSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()

	snapshot := m.snapshot
	snapshot.Interfaces = append([]model.InterfaceStatus(nil), snapshot.Interfaces...)
	snapshot.Terminals = append([]model.Terminal(nil), snapshot.Terminals...)
	snapshot.Capabilities = append([]model.CapabilityNote(nil), snapshot.Capabilities...)
	snapshot.Protocols = append([]model.ProtocolStat(nil), snapshot.Protocols...)
	snapshot.Policies = append([]model.PolicyStat(nil), snapshot.Policies...)
	snapshot.Routes = append([]model.RouteStat(nil), snapshot.Routes...)
	snapshot.Alerts = append([]model.AlertEvent(nil), snapshot.Alerts...)
	snapshot.Warnings = append([]string(nil), snapshot.Warnings...)
	snapshot.Overview.ChartSamples = append([]model.RateSample(nil), snapshot.Overview.ChartSamples...)
	return snapshot
}

func (m *Monitor) TerminalDetail(id string) (model.TerminalDetail, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	detail, ok := m.terminalDetails[id]
	if !ok {
		return model.TerminalDetail{}, false
	}
	detail.Connections = append([]model.TerminalConnection(nil), detail.Connections...)
	detail.FlowCategories = append([]model.TerminalFlowCategory(nil), detail.FlowCategories...)
	detail.History = append([]model.TerminalHistoryEntry(nil), detail.History...)
	detail.Capabilities = append([]model.TerminalCapability(nil), detail.Capabilities...)
	return detail, true
}

func (m *Monitor) UpdateTerminalRemark(ctx context.Context, id, remark string) error {
	detail, ok := m.TerminalDetail(id)
	if !ok {
		return store.ErrTerminalNotFound
	}
	_, err := m.UpdateTerminalMetadata(ctx, id, detail.Terminal.CustomName, remark)
	return err
}

func (m *Monitor) UpdateTerminalMetadata(ctx context.Context, id, customName, remark string) (model.TerminalDetail, error) {
	m.refreshMu.Lock()
	defer m.refreshMu.Unlock()

	if _, ok := m.TerminalDetail(id); !ok {
		return model.TerminalDetail{}, store.ErrTerminalNotFound
	}
	if err := m.store.UpdateTerminalMetadata(ctx, id, customName, remark); err != nil {
		return model.TerminalDetail{}, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	for index := range m.snapshot.Terminals {
		if m.snapshot.Terminals[index].ID == id {
			applyTerminalMetadata(&m.snapshot.Terminals[index], customName, remark)
		}
	}
	detail, ok := m.terminalDetails[id]
	if !ok {
		return model.TerminalDetail{}, store.ErrTerminalNotFound
	}
	applyTerminalMetadata(&detail.Terminal, customName, remark)
	for family, summary := range detail.FamilySummaries {
		applyTerminalMetadata(&summary, customName, remark)
		detail.FamilySummaries[family] = summary
	}
	m.terminalDetails[id] = detail
	return detail, nil
}

func applyTerminalMetadata(terminal *model.Terminal, customName, remark string) {
	terminal.CustomName = customName
	terminal.Remark = remark
	terminal.DisplayName = effectiveTerminalName(*terminal)
}

func effectiveTerminalName(terminal model.Terminal) string {
	return preferredName(terminal.CustomName, terminal.AutoName, terminal.PrimaryIPv4, terminal.PrimaryIPv6, terminal.MACAddress, "未命名设备")
}

func recognizedAutoName(value, mac string, addressGroups ...[]string) string {
	value = strings.TrimSpace(value)
	if value == "" || strings.EqualFold(value, strings.TrimSpace(mac)) {
		return ""
	}
	for _, addresses := range addressGroups {
		for _, address := range addresses {
			if value == strings.TrimSpace(address) {
				return ""
			}
		}
	}
	return value
}

func (m *Monitor) LoadHistory(ctx context.Context, since time.Time) ([]model.LoadSample, error) {
	return m.store.LoadSamples(ctx, since)
}

func (m *Monitor) ProtocolHistory(ctx context.Context, since time.Time) ([]model.ProtocolHistorySample, error) {
	return m.store.ProtocolSamples(ctx, since)
}

func (m *Monitor) InterfaceDetail(ctx context.Context, name string, since time.Time) (model.InterfaceDetail, bool, error) {
	snapshot := m.Snapshot()
	for _, item := range snapshot.Interfaces {
		if item.Name != name {
			continue
		}
		samples, err := m.store.LoadInterfaceSamples(ctx, []string{name}, since)
		if err != nil {
			return model.InterfaceDetail{}, false, err
		}
		return model.InterfaceDetail{Interface: item, Samples: samples}, true, nil
	}
	return model.InterfaceDetail{}, false, nil
}

func (m *Monitor) refresh(ctx context.Context) error {
	m.refreshMu.Lock()
	defer m.refreshMu.Unlock()

	pollCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()

	now := time.Now().UTC()
	previous := m.Snapshot()
	warnings := make([]string, 0)
	alertsByID := make(map[string]model.AlertEvent)
	addWarning := func(id, source, message string) {
		warnings = append(warnings, message)
		alertsByID[id] = model.AlertEvent{ID: id, Level: "warning", Source: source, Message: message, Timestamp: now}
	}

	resource, err := m.client.SystemResource(pollCtx)
	if err != nil {
		return fmt.Errorf("load system resource: %w", err)
	}
	health, err := m.client.SystemHealth(pollCtx)
	if err != nil {
		return fmt.Errorf("load system health: %w", err)
	}
	interfaces, err := m.client.Interfaces(pollCtx)
	if err != nil {
		return fmt.Errorf("load interfaces: %w", err)
	}
	ethernet, err := m.client.EthernetInterfaces(pollCtx)
	if err != nil {
		return fmt.Errorf("load ethernet interfaces: %w", err)
	}
	addresses, err := m.client.IPAddresses(pollCtx)
	if err != nil {
		return fmt.Errorf("load ip addresses: %w", err)
	}
	ipv6Addresses, err := m.client.IPv6Addresses(pollCtx)
	if err != nil {
		return fmt.Errorf("load ipv6 addresses: %w", err)
	}
	leases, err := m.client.DHCPLeases(pollCtx)
	if err != nil {
		return fmt.Errorf("load dhcp leases: %w", err)
	}
	arpEntries, err := m.client.ARPEntries(pollCtx)
	if err != nil {
		return fmt.Errorf("load arp entries: %w", err)
	}
	ipv6Neighbors, err := m.client.IPv6Neighbors(pollCtx)
	if err != nil {
		return fmt.Errorf("load ipv6 neighbors: %w", err)
	}
	connectionsV4, err := m.client.FirewallConnectionsV4(pollCtx)
	if err != nil {
		return fmt.Errorf("load ipv4 connections: %w", err)
	}
	connectionsV6, err := m.client.FirewallConnectionsV6(pollCtx)
	if err != nil {
		return fmt.Errorf("load ipv6 connections: %w", err)
	}
	simpleQueues, err := m.client.SimpleQueues(pollCtx)
	policyComplete := true
	if err != nil {
		m.logger.Printf("load simple queues failed: %v", err)
		addWarning("simple-queues", "策略采集", "Simple Queue 数据暂时不可用，保留上次有效策略数据。")
		policyComplete = false
	}
	queueTrees, err := m.client.QueueTrees(pollCtx)
	if err != nil {
		m.logger.Printf("load queue trees failed: %v", err)
		addWarning("queue-trees", "策略采集", "Queue Tree 数据暂时不可用，保留上次有效策略数据。")
		policyComplete = false
	}
	mangleRules, err := m.client.MangleRules(pollCtx)
	if err != nil {
		m.logger.Printf("load mangle rules failed: %v", err)
		addWarning("mangle-rules", "策略采集", "Mangle 计数器暂时不可用，保留上次有效策略数据。")
		policyComplete = false
	}
	routingRules, err := m.client.RoutingRules(pollCtx)
	routesComplete := true
	if err != nil {
		m.logger.Printf("load routing rules failed: %v", err)
		addWarning("routing-rules", "路由采集", "Routing Rule 数据暂时不可用，保留上次有效路由数据。")
		routesComplete = false
	}
	ipRoutes, err := m.client.IPRoutes(pollCtx)
	if err != nil {
		m.logger.Printf("load routes failed: %v", err)
		addWarning("ip-routes", "路由采集", "路由表暂时不可用，保留上次有效路由数据。")
		routesComplete = false
	}

	trafficInterfaces := selectTrafficInterfaces(m.cfg.RouterOS.TrafficInterfaces, interfaces)
	monitorInterfaces := selectMonitorableInterfaces(interfaces)
	trafficRates := make(map[string]routeros.MonitorTrafficEntry, len(monitorInterfaces))
	for _, name := range monitorInterfaces {
		entry, err := m.client.MonitorTraffic(pollCtx, name)
		if err != nil {
			m.logger.Printf("monitor traffic for %s failed: %v", name, err)
			addWarning("traffic-"+name, "接口采集", fmt.Sprintf("接口 %s 实时速率暂时不可用。", name))
			continue
		}
		trafficRates[name] = entry
		if err := m.store.SaveInterfaceSample(pollCtx, now, name, parseFloat(entry.RXBitsPerSecond), parseFloat(entry.TXBitsPerSecond)); err != nil {
			return err
		}
	}
	if err := m.store.PruneInterfaceSamples(pollCtx, now.Add(-time.Duration(m.cfg.SampleRetentionHours)*time.Hour)); err != nil {
		return err
	}
	if err := m.store.PruneRuntimeState(pollCtx, now.Add(-2*time.Hour), now.Add(-35*24*time.Hour)); err != nil {
		return err
	}

	routerAddresses := deriveRouterAddresses(addresses, ipv6Addresses)
	localCIDRs := deriveLocalCIDRs(m.cfg.RouterOS.TerminalCIDRs, trafficInterfaces, addresses, ipv6Addresses, ipv6Neighbors)
	interfaceStatuses := buildInterfaces(interfaces, ethernet, addresses, trafficRates)
	terminals, details, err := m.buildTerminals(
		pollCtx,
		now,
		localCIDRs,
		routerAddresses,
		leases,
		arpEntries,
		ipv6Neighbors,
		connectionsV4,
		connectionsV6,
	)
	if err != nil {
		return err
	}

	chartSamples, err := m.store.LoadInterfaceSamples(pollCtx, trafficInterfaces, now.Add(-5*time.Minute))
	if err != nil {
		return err
	}

	memoryPercent := memoryUsedPercent(parseInt(resource.TotalMemory), parseInt(resource.FreeMemory))
	storagePercent := memoryUsedPercent(parseInt(resource.TotalHDD), parseInt(resource.FreeHDD))
	uploadBps := totalSelectedTXBps(trafficRates, trafficInterfaces)
	downloadBps := totalSelectedRXBps(trafficRates, trafficInterfaces)
	connectedDevices := connectedLANDeviceCount(terminals, trafficInterfaces)
	connectionCount := len(connectionsV4) + len(connectionsV6)
	if err := m.store.SaveLoadSample(pollCtx, model.LoadSample{Timestamp: now, CPULoadPercent: float64(parseInt(resource.CPULoad)), MemoryUsedPercent: memoryPercent, StorageUsedPercent: storagePercent, OnlineTerminalCount: connectedDevices, UploadBps: uploadBps, DownloadBps: downloadBps}); err != nil {
		return err
	}

	protocols := aggregateProtocols(details)
	if err := m.store.SaveProtocolSamples(pollCtx, now, protocols); err != nil {
		return err
	}
	policies := buildPolicies(simpleQueues, queueTrees, mangleRules)
	if !policyComplete && len(previous.Policies) > 0 {
		policies = previous.Policies
	}
	routes := buildRoutes(routingRules, ipRoutes)
	if !routesComplete && len(previous.Routes) > 0 {
		routes = previous.Routes
	}
	alerts := make([]model.AlertEvent, 0, len(alertsByID))
	for _, alert := range alertsByID {
		alerts = append(alerts, alert)
	}
	sort.Slice(alerts, func(i, j int) bool { return alerts[i].Timestamp.After(alerts[j].Timestamp) })
	if len(alerts) > 50 {
		alerts = alerts[:50]
	}
	snapshot := model.DashboardSnapshot{
		Overview: model.Overview{
			RouterName:           resource.BoardName,
			Platform:             resource.Platform,
			Version:              resource.Version,
			BoardName:            resource.BoardName,
			Uptime:               formatRouterOSUptime(resource.Uptime),
			CPULoadPercent:       parseInt(resource.CPULoad),
			MemoryUsedPercent:    memoryPercent,
			MemoryUsedBytes:      parseInt(resource.TotalMemory) - parseInt(resource.FreeMemory),
			MemoryTotalBytes:     parseInt(resource.TotalMemory),
			StorageUsedPercent:   storagePercent,
			StorageUsedBytes:     parseInt(resource.TotalHDD) - parseInt(resource.FreeHDD),
			StorageTotalBytes:    parseInt(resource.TotalHDD),
			ConnectedDeviceCount: connectedDevices,
			ConnectionCount:      connectionCount,
			UploadBps:            uploadBps,
			DownloadBps:          downloadBps,
			TrafficInterfaces:    trafficInterfaces,
			HealthEnabled:        strings.EqualFold(health.State, "enabled"),
			UpdatedAt:            now,
			ChartSamples:         chartSamples,
		},
		Interfaces:   interfaceStatuses,
		Terminals:    terminals,
		Capabilities: buildCapabilities(strings.EqualFold(health.State, "enabled")),
		Protocols:    protocols,
		Policies:     policies,
		Routes:       routes,
		Alerts:       alerts,
		Warnings:     warnings,
	}

	m.mu.Lock()
	m.snapshot = snapshot
	m.terminalDetails = details
	m.mu.Unlock()

	return nil
}

func aggregateProtocols(details map[string]model.TerminalDetail) []model.ProtocolStat {
	byName := map[string]*model.ProtocolStat{}
	for _, detail := range details {
		for _, connection := range detail.Connections {
			name := connection.Application
			stat := byName[name]
			if stat == nil {
				stat = &model.ProtocolStat{Name: name, Kind: connection.Protocol, Estimated: true}
				byName[name] = stat
			}
			stat.Connections++
			stat.UploadBps += connection.UploadBps
			stat.DownloadBps += connection.DownloadBps
			stat.UploadBytes += connection.UploadBytes
			stat.DownloadBytes += connection.DownloadBytes
		}
	}
	result := make([]model.ProtocolStat, 0, len(byName))
	for _, stat := range byName {
		result = append(result, *stat)
	}
	sort.Slice(result, func(left, right int) bool {
		return result[left].UploadBps+result[left].DownloadBps > result[right].UploadBps+result[right].DownloadBps
	})
	return result
}

func buildPolicies(simple []routeros.SimpleQueue, trees []routeros.QueueTree, mangle []routeros.FirewallRule) []model.PolicyStat {
	result := make([]model.PolicyStat, 0, len(simple)+len(trees)+len(mangle))
	for _, item := range simple {
		result = append(result, model.PolicyStat{Kind: "simple queue", Name: item.Name, Target: item.Target, Rate: item.Rate, Bytes: parseCounterPair(item.Bytes), Packets: parseCounterPair(item.Packets), Disabled: parseBool(item.Disabled)})
	}
	for _, item := range trees {
		result = append(result, model.PolicyStat{Kind: "queue tree", Name: item.Name, Target: item.Parent, Mark: item.PacketMark, Rate: item.Rate, Bytes: parseCounterPair(item.Bytes), Packets: parseCounterPair(item.Packets), Disabled: parseBool(item.Disabled)})
	}
	for index, item := range mangle {
		mark := preferredName(item.NewRoutingMark, item.NewConnectionMark, item.ConnectionMark)
		if mark == "" && parseInt(item.Bytes) == 0 && parseInt(item.Packets) == 0 {
			continue
		}
		result = append(result, model.PolicyStat{Kind: "mangle", Name: preferredName(item.Comment, fmt.Sprintf("%s #%d", item.Chain, index+1)), Target: item.Action, Mark: mark, Bytes: parseInt(item.Bytes), Packets: parseInt(item.Packets), Disabled: parseBool(item.Disabled)})
	}
	return result
}

func buildRoutes(rules []routeros.RoutingRule, routes []routeros.IPRoute) []model.RouteStat {
	result := make([]model.RouteStat, 0, len(rules)+len(routes))
	for _, item := range rules {
		result = append(result, model.RouteStat{Kind: "rule", Destination: item.DstAddress, Table: item.Table, Action: item.Action, Source: preferredName(item.SrcAddress, item.Interface), Disabled: parseBool(item.Disabled)})
	}
	for _, item := range routes {
		result = append(result, model.RouteStat{Kind: "route", Destination: item.DstAddress, Gateway: item.Gateway, Table: item.RoutingTable, Distance: parseInt(item.Distance), Active: parseBool(item.Active), Disabled: parseBool(item.Disabled)})
	}
	return result
}

func parseCounterPair(value string) int64 {
	total := int64(0)
	for _, part := range strings.Split(value, "/") {
		total += parseInt(part)
	}
	return total
}

type terminalBuilder struct {
	ID                 string
	DisplayName        string
	MACAddress         string
	PrimaryInterface   string
	IPv4               map[string]struct{}
	IPv6               map[string]struct{}
	ConnectionCount    int
	CurrentUploadBps   float64
	CurrentDownloadBps float64
	LastSeen           time.Time
	State              string
	StrongEvidence     bool
}

const routerTerminalID = "routeros:self"
const routerTerminalDisplayName = "RouterOS 本机连接跟踪"

type routerAssignedAddress struct {
	Family    string
	Interface string
}

func (m *Monitor) buildTerminals(
	ctx context.Context,
	now time.Time,
	localCIDRs []*net.IPNet,
	routerAddresses map[string]routerAssignedAddress,
	leases []routeros.DHCPLease,
	arpEntries []routeros.ARPEntry,
	ipv6Neighbors []routeros.IPv6Neighbor,
	connectionsV4 []routeros.FirewallConnection,
	connectionsV6 []routeros.FirewallConnection,
) ([]model.Terminal, map[string]model.TerminalDetail, error) {
	macByAddress := map[string]string{}
	nameByAddress := map[string]string{}
	nameByMAC := map[string]string{}
	interfaceByAddress := map[string]string{}

	for _, lease := range leases {
		address := strings.TrimSpace(lease.Address)
		mac := normalizeMAC(lease.MACAddress)
		name := preferredName(lease.Comment, lease.HostName)
		if address != "" && mac != "" {
			macByAddress[address] = mac
		}
		if address != "" && name != "" {
			nameByAddress[address] = name
		}
		if mac != "" && name != "" {
			nameByMAC[mac] = name
		}
	}

	for _, entry := range arpEntries {
		address := strings.TrimSpace(entry.Address)
		mac := normalizeMAC(entry.MACAddress)
		if address != "" && mac != "" {
			macByAddress[address] = mac
		}
		if address != "" && strings.TrimSpace(entry.Interface) != "" {
			interfaceByAddress[address] = strings.TrimSpace(entry.Interface)
		}
	}

	for _, neighbor := range ipv6Neighbors {
		address := strings.TrimSpace(neighbor.Address)
		mac := normalizeMAC(neighbor.MACAddress)
		if address != "" && mac != "" {
			macByAddress[address] = mac
		}
		if address != "" && strings.TrimSpace(neighbor.Interface) != "" {
			interfaceByAddress[address] = strings.TrimSpace(neighbor.Interface)
		}
	}

	builders := map[string]*terminalBuilder{}
	connectionMap := map[string][]model.TerminalConnection{}
	flowMap := map[string]map[string]*model.TerminalFlowCategory{}
	connectionSnapshots := make([]store.ConnectionSnapshot, 0, len(connectionsV4)+len(connectionsV6))
	ensureBuilder := func(address, family, mac string) *terminalBuilder {
		mac = normalizeMAC(mac)
		id := terminalIdentity(mac, address, routerAddresses)
		builder, exists := builders[id]
		if !exists {
			displayName := preferredName(nameByAddress[address], nameByMAC[mac])
			if id == routerTerminalID {
				displayName = routerTerminalDisplayName
				mac = ""
			}
			builder = &terminalBuilder{
				ID:               id,
				MACAddress:       mac,
				DisplayName:      displayName,
				IPv4:             map[string]struct{}{},
				IPv6:             map[string]struct{}{},
				PrimaryInterface: interfaceByAddress[address],
				State:            "offline",
			}
			builders[id] = builder
		}
		if builder.DisplayName == "" {
			builder.DisplayName = preferredName(nameByAddress[address], nameByMAC[mac])
		}
		if mac != "" && id != routerTerminalID {
			builder.MACAddress = mac
		}
		if builder.PrimaryInterface == "" {
			builder.PrimaryInterface = interfaceByAddress[address]
		}
		switch family {
		case "ipv4":
			builder.IPv4[address] = struct{}{}
		case "ipv6":
			builder.IPv6[address] = struct{}{}
		}
		return builder
	}
	markOnline := func(builder *terminalBuilder) {
		builder.StrongEvidence = true
		builder.State = "online"
		builder.LastSeen = now
	}

	for address, assigned := range routerAddresses {
		if err := m.store.MergeTerminal(ctx, terminalID("", address), routerTerminalID); err != nil {
			return nil, nil, err
		}
		builder := ensureBuilder(address, assigned.Family, "")
		if builder.PrimaryInterface == "" || assigned.Interface == "lan" {
			builder.PrimaryInterface = assigned.Interface
		}
		markOnline(builder)
	}

	for _, lease := range leases {
		if !strings.EqualFold(lease.Status, "bound") {
			continue
		}
		address := strings.TrimSpace(lease.Address)
		if address == "" {
			continue
		}
		ensureBuilder(address, "ipv4", macByAddress[address])
	}

	for _, entry := range arpEntries {
		address := strings.TrimSpace(entry.Address)
		if address == "" || normalizeMAC(entry.MACAddress) == "" {
			continue
		}
		builder := ensureBuilder(address, "ipv4", macByAddress[address])
		if strings.EqualFold(entry.Status, "reachable") || strings.EqualFold(entry.Status, "permanent") {
			markOnline(builder)
		}
	}

	for _, neighbor := range ipv6Neighbors {
		address := strings.TrimSpace(neighbor.Address)
		if address == "" || normalizeMAC(neighbor.MACAddress) == "" {
			continue
		}
		builder := ensureBuilder(address, "ipv6", macByAddress[address])
		if strings.EqualFold(neighbor.Status, "reachable") || strings.EqualFold(neighbor.Status, "permanent") {
			markOnline(builder)
		}
	}

	applyConnections := func(family string, connections []routeros.FirewallConnection) error {
		for _, connection := range connections {
			view, ok := orientConnection(family, connection, localCIDRs, routerAddresses)
			if !ok {
				continue
			}

			mac := normalizeMAC(macByAddress[view.LocalAddress])
			if view.RouterSelf {
				mac = ""
			}
			if mac != "" {
				if err := m.store.MergeTerminal(ctx, terminalID("", view.LocalAddress), terminalID(mac, view.LocalAddress)); err != nil {
					return err
				}
			}

			builder := ensureBuilder(view.LocalAddress, family, mac)
			builder.ConnectionCount++
			builder.CurrentUploadBps += view.UploadBps
			builder.CurrentDownloadBps += view.DownloadBps
			markOnline(builder)
			if !view.RouterSelf {
				builder.DisplayName = preferredName(nameByAddress[view.LocalAddress], nameByMAC[builder.MACAddress])
			}

			connectionSnapshots = append(connectionSnapshots, store.ConnectionSnapshot{Key: view.ConnectionKey, TerminalID: builder.ID, UploadBytes: view.CurrentUploadBytes, DownloadBytes: view.CurrentDownloadBytes, SeenAt: now})

			application := classifyApplication(connection.Protocol, connection.DstPort, connection.ReplyDstPort, connection.SrcPort)
			connectionMap[builder.ID] = append(connectionMap[builder.ID], model.TerminalConnection{
				Key:                view.ConnectionKey,
				Family:             family,
				Application:        application,
				Protocol:           strings.ToLower(connection.Protocol),
				Line:               "未知",
				SourceAddress:      view.LocalAddress,
				SourcePort:         localPort(connection, view.LocalAddress),
				DestinationAddress: remoteAddress(connection, view.LocalAddress),
				DestinationPort:    remotePort(connection, view.LocalAddress),
				UploadBytes:        view.CurrentUploadBytes,
				DownloadBytes:      view.CurrentDownloadBytes,
				UploadBps:          view.UploadBps,
				DownloadBps:        view.DownloadBps,
				Status:             connectionStatus(connection.SeenReply, connection.Assured),
				SeenReply:          parseBool(connection.SeenReply),
				Assured:            parseBool(connection.Assured),
				PublicAddress:      view.PublicAddress,
				ConnectionMark:     preferredName(connection.ConnectionMark, connection.RoutingMark),
				Estimated:          true,
			})

			if flowMap[builder.ID] == nil {
				flowMap[builder.ID] = map[string]*model.TerminalFlowCategory{}
			}
			flow := flowMap[builder.ID][application]
			if flow == nil {
				flow = &model.TerminalFlowCategory{
					Name:      application,
					Estimated: true,
				}
				flowMap[builder.ID][application] = flow
			}
			flow.CurrentUploadBps += view.UploadBps
			flow.CurrentDownloadBps += view.DownloadBps
			flow.TotalUploadBytes += view.CurrentUploadBytes
			flow.TotalDownloadBytes += view.CurrentDownloadBytes
		}
		return nil
	}

	if err := applyConnections("ipv4", connectionsV4); err != nil {
		return nil, nil, err
	}
	if err := applyConnections("ipv6", connectionsV6); err != nil {
		return nil, nil, err
	}

	ids := make([]string, 0, len(builders))
	for _, builder := range builders {
		if err := m.store.UpsertTerminal(ctx, builder.ID, builder.MACAddress, builder.DisplayName, builder.LastSeen); err != nil {
			return nil, nil, err
		}
		if err := m.store.ReplaceTerminalAddresses(ctx, builder.ID, mapKeys(builder.IPv4), mapKeys(builder.IPv6), now); err != nil {
			return nil, nil, err
		}
		ids = append(ids, builder.ID)
	}
	previousTotals, err := m.store.TerminalTotals(ctx, ids)
	if err != nil {
		return nil, nil, err
	}
	for _, builder := range builders {
		if !builder.StrongEvidence {
			lastSeen := previousTotals[builder.ID].LastSeen
			if !lastSeen.IsZero() && now.Sub(lastSeen) <= 5*time.Minute {
				builder.State = "inactive"
			} else {
				builder.State = "offline"
			}
			builder.LastSeen = time.Time{}
		}
		if _, _, err := m.store.UpdateTerminalPresence(ctx, builder.ID, builder.State, builder.LastSeen); err != nil {
			return nil, nil, err
		}
	}
	if err := m.store.ApplyConnectionSnapshots(ctx, connectionSnapshots); err != nil {
		return nil, nil, err
	}

	totalsByID, err := m.store.TerminalTotals(ctx, ids)
	if err != nil {
		return nil, nil, err
	}

	terminals := make([]model.Terminal, 0, len(builders))
	details := make(map[string]model.TerminalDetail, len(builders))
	for _, builder := range builders {
		total := totalsByID[builder.ID]
		onlineSeconds := int64(0)
		if !total.OnlineSince.IsZero() {
			onlineSeconds = int64(now.Sub(total.OnlineSince).Seconds())
		}
		if err := m.store.SaveTerminalHistory(ctx, builder.ID, now, onlineSeconds, total.UploadBytes, total.DownloadBytes); err != nil {
			return nil, nil, err
		}
		history, err := m.store.TerminalHistory(ctx, builder.ID, 30)
		if err != nil {
			return nil, nil, err
		}

		terminal := model.Terminal{
			ID:                 builder.ID,
			AutoName:           recognizedAutoName(total.AutoName, builder.MACAddress, mapKeys(builder.IPv4), mapKeys(builder.IPv6)),
			CustomName:         total.CustomName,
			Remark:             total.Remark,
			MACAddress:         builder.MACAddress,
			PrimaryInterface:   builder.PrimaryInterface,
			IPv4:               sortedAddresses(builder.IPv4),
			IPv6:               sortedAddresses(builder.IPv6),
			ConnectionCount:    builder.ConnectionCount,
			CurrentUploadBps:   builder.CurrentUploadBps,
			CurrentDownloadBps: builder.CurrentDownloadBps,
			TotalUploadBytes:   total.UploadBytes,
			TotalDownloadBytes: total.DownloadBytes,
			TrackingSince:      total.TrackingSince,
			LastSeen:           total.LastSeen,
			State:              total.State,
			OnlineSince:        total.OnlineSince,
			FamilyStats: map[string]model.TerminalFamilyStats{
				"ipv4": terminalFamilyStats(connectionMap[builder.ID], "ipv4"),
				"ipv6": terminalFamilyStats(connectionMap[builder.ID], "ipv6"),
			},
		}
		if len(terminal.IPv4) > 0 {
			terminal.PrimaryIPv4 = terminal.IPv4[0]
		}
		if len(terminal.IPv6) > 0 {
			terminal.PrimaryIPv6 = terminal.IPv6[0]
		}
		if terminal.ID == routerTerminalID {
			terminal.PrimaryIPv4 = preferredRouterAddress(routerAddresses, "ipv4")
			terminal.PrimaryIPv6 = preferredRouterAddress(routerAddresses, "ipv6")
			if assigned, exists := routerAddresses[preferredName(terminal.PrimaryIPv4, terminal.PrimaryIPv6)]; exists {
				terminal.PrimaryInterface = assigned.Interface
			}
		}
		terminal.DisplayName = effectiveTerminalName(terminal)
		terminals = append(terminals, model.Terminal{
			ID:                 terminal.ID,
			DisplayName:        terminal.DisplayName,
			AutoName:           terminal.AutoName,
			CustomName:         terminal.CustomName,
			Remark:             terminal.Remark,
			MACAddress:         terminal.MACAddress,
			PrimaryInterface:   terminal.PrimaryInterface,
			IPv4:               terminal.IPv4,
			IPv6:               terminal.IPv6,
			ConnectionCount:    terminal.ConnectionCount,
			CurrentUploadBps:   terminal.CurrentUploadBps,
			CurrentDownloadBps: terminal.CurrentDownloadBps,
			TotalUploadBytes:   terminal.TotalUploadBytes,
			TotalDownloadBytes: terminal.TotalDownloadBytes,
			TrackingSince:      terminal.TrackingSince,
			LastSeen:           terminal.LastSeen,
			PrimaryIPv4:        terminal.PrimaryIPv4,
			PrimaryIPv6:        terminal.PrimaryIPv6,
			State:              terminal.State,
			OnlineSince:        terminal.OnlineSince,
			FamilyStats:        terminal.FamilyStats,
		})

		flows := flattenFlows(flowMap[builder.ID])
		details[builder.ID] = model.TerminalDetail{
			Terminal:       terminal,
			Connections:    sortConnections(connectionMap[builder.ID]),
			FlowCategories: flows,
			History:        history,
			Capabilities:   terminalCapabilities(flows, history),
			FamilySummaries: map[string]model.Terminal{
				"ipv4": terminalFamilySummary(terminal, connectionMap[builder.ID], "ipv4"),
				"ipv6": terminalFamilySummary(terminal, connectionMap[builder.ID], "ipv6"),
			},
			FamilyFlows: map[string][]model.TerminalFlowCategory{
				"ipv4": terminalFamilyFlows(connectionMap[builder.ID], "ipv4"),
				"ipv6": terminalFamilyFlows(connectionMap[builder.ID], "ipv6"),
			},
		}
	}

	sort.Slice(terminals, func(left, right int) bool {
		return compareTerminalAddress(terminals[left], terminals[right]) < 0
	})

	return terminals, details, nil
}

func terminalFamilySummary(terminal model.Terminal, connections []model.TerminalConnection, family string) model.Terminal {
	stats := terminalFamilyStats(connections, family)
	summary := terminal
	summary.ConnectionCount = stats.ConnectionCount
	summary.CurrentUploadBps = stats.CurrentUploadBps
	summary.CurrentDownloadBps = stats.CurrentDownloadBps
	summary.TotalUploadBytes = stats.ActiveUploadBytes
	summary.TotalDownloadBytes = stats.ActiveDownloadBytes
	if family == "ipv4" {
		summary.IPv6 = nil
		summary.PrimaryIPv6 = ""
	} else {
		summary.IPv4 = nil
		summary.PrimaryIPv4 = ""
	}
	return summary
}

func terminalFamilyStats(connections []model.TerminalConnection, family string) model.TerminalFamilyStats {
	var stats model.TerminalFamilyStats
	for _, connection := range connections {
		if connection.Family != family {
			continue
		}
		stats.ConnectionCount++
		stats.CurrentUploadBps += connection.UploadBps
		stats.CurrentDownloadBps += connection.DownloadBps
		stats.ActiveUploadBytes += connection.UploadBytes
		stats.ActiveDownloadBytes += connection.DownloadBytes
	}
	return stats
}

func terminalFamilyFlows(connections []model.TerminalConnection, family string) []model.TerminalFlowCategory {
	flows := map[string]*model.TerminalFlowCategory{}
	for _, connection := range connections {
		if connection.Family != family {
			continue
		}
		flow := flows[connection.Application]
		if flow == nil {
			flow = &model.TerminalFlowCategory{Name: connection.Application, Estimated: true}
			flows[connection.Application] = flow
		}
		flow.CurrentUploadBps += connection.UploadBps
		flow.CurrentDownloadBps += connection.DownloadBps
		flow.TotalUploadBytes += connection.UploadBytes
		flow.TotalDownloadBytes += connection.DownloadBytes
	}
	return flattenFlows(flows)
}

type connectionView struct {
	LocalAddress         string
	ConnectionKey        string
	CurrentUploadBytes   int64
	CurrentDownloadBytes int64
	UploadBps            float64
	DownloadBps          float64
	PublicAddress        string
	RouterSelf           bool
}

func orientConnection(family string, connection routeros.FirewallConnection, localCIDRs []*net.IPNet, routerAddresses map[string]routerAssignedAddress) (connectionView, bool) {
	_, srcRouter := routerAddresses[assignedIP(connection.SrcAddress)]
	_, replySrcRouter := routerAddresses[assignedIP(connection.ReplySrcAddress)]
	srcLocal := containsIP(localCIDRs, connection.SrcAddress)
	replySrcLocal := containsIP(localCIDRs, connection.ReplySrcAddress)

	key := fmt.Sprintf(
		"%s|%s|%s|%s|%s|%s|%s|%s|%s|%s",
		family,
		connection.Protocol,
		connection.SrcAddress,
		connection.SrcPort,
		connection.DstAddress,
		connection.DstPort,
		connection.ReplySrcAddress,
		connection.ReplySrcPort,
		connection.ReplyDstAddress,
		connection.ReplyDstPort,
	)

	switch {
	case srcRouter || srcLocal:
		return connectionView{
			LocalAddress:         connection.SrcAddress,
			ConnectionKey:        key,
			CurrentUploadBytes:   parseInt(connection.OrigBytes),
			CurrentDownloadBytes: parseInt(connection.ReplBytes),
			UploadBps:            parseFloat(connection.OrigRate),
			DownloadBps:          parseFloat(connection.ReplRate),
			PublicAddress:        externalAddress(connection.ReplyDstAddress, connection.SrcAddress),
			RouterSelf:           srcRouter,
		}, true
	case replySrcRouter || replySrcLocal:
		return connectionView{
			LocalAddress:         connection.ReplySrcAddress,
			ConnectionKey:        key,
			CurrentUploadBytes:   parseInt(connection.ReplBytes),
			CurrentDownloadBytes: parseInt(connection.OrigBytes),
			UploadBps:            parseFloat(connection.ReplRate),
			DownloadBps:          parseFloat(connection.OrigRate),
			PublicAddress:        externalAddress(connection.DstAddress, connection.ReplySrcAddress),
			RouterSelf:           replySrcRouter,
		}, true
	default:
		return connectionView{}, false
	}
}

func buildInterfaces(
	interfaces []routeros.Interface,
	ethernet []routeros.EthernetInterface,
	addresses []routeros.IPAddress,
	trafficRates map[string]routeros.MonitorTrafficEntry,
) []model.InterfaceStatus {
	ethernetByName := map[string]routeros.EthernetInterface{}
	for _, iface := range ethernet {
		ethernetByName[iface.Name] = iface
	}

	result := make([]model.InterfaceStatus, 0, len(interfaces))
	for _, iface := range interfaces {
		rate := trafficRates[iface.Name]
		rxBytes := parseInt(iface.RXByte)
		txBytes := parseInt(iface.TXByte)
		if eth, exists := ethernetByName[iface.Name]; exists {
			if parsed := parseInt(eth.RXBytes); parsed > 0 {
				rxBytes = parsed
			}
			if parsed := parseInt(eth.TXBytes); parsed > 0 {
				txBytes = parsed
			}
		}

		result = append(result, model.InterfaceStatus{
			Name:           iface.Name,
			Type:           iface.Type,
			Running:        parseBool(iface.Running),
			Disabled:       parseBool(iface.Disabled),
			MACAddress:     iface.MACAddress,
			Status:         iface.Status,
			LastLinkUpTime: iface.LastLinkUpTime,
			LinkDowns:      parseInt(iface.LinkDowns),
			ActualMTU:      parseInt(iface.ActualMTU),
			RXBytes:        rxBytes,
			TXBytes:        txBytes,
			CurrentRXBps:   parseFloat(rate.RXBitsPerSecond),
			CurrentTXBps:   parseFloat(rate.TXBitsPerSecond),
			Addresses:      interfaceAddresses(addresses, iface.Name),
			RXPackets:      parseInt(iface.RXPacket),
			TXPackets:      parseInt(iface.TXPacket),
			RXDrops:        parseInt(iface.RXDrop),
			TXDrops:        parseInt(iface.TXDrop),
			RXErrors:       parseInt(iface.RXError),
			TXErrors:       parseInt(iface.TXError),
			LinkRate:       ethernetByName[iface.Name].Rate,
			FullDuplex:     parseBool(ethernetByName[iface.Name].FullDuplex),
		})
	}

	sort.Slice(result, func(left, right int) bool {
		return result[left].Name < result[right].Name
	})
	return result
}

func buildCapabilities(healthEnabled bool) []model.CapabilityNote {
	healthStatus := "supported_now"
	healthDetails := "CPU, memory, interface status, live rates, unified terminals, and locally persisted terminal totals are available now."
	if !healthEnabled {
		healthDetails = "CPU, memory, interface status, live rates, unified terminals, and locally persisted terminal totals are available now. Hardware health details like temperature are unavailable on this current CHR deployment because `/rest/system/health` is disabled."
	}

	return []model.CapabilityNote{
		{
			Area:    "System overview",
			Item:    "Core live metrics",
			Status:  healthStatus,
			Details: healthDetails,
		},
		{
			Area:    "System overview",
			Item:    "30-minute protocol distribution",
			Status:  "not_natively_feasible",
			Details: "Current RouterOS REST data does not expose iKuai-style protocol/category accounting, so v1 intentionally omits this widget.",
		},
		{
			Area:    "Terminal monitoring",
			Item:    "Unified IPv4/IPv6 table",
			Status:  "supported_now",
			Details: "v1 correlates DHCP, ARP, IPv6 neighbor, and firewall connection data into a single terminal view.",
		},
		{
			Area:    "Terminal monitoring",
			Item:    "Cumulative upload/download",
			Status:  "supported_with_panel_persistence",
			Details: "Totals are computed and persisted by the panel itself because the live RouterOS REST surface does not present them directly.",
		},
		{
			Area:    "Future expansion",
			Item:    "Protocol / policy / load / split-flow pages",
			Status:  "deferred",
			Details: "These are reserved for later only if future RouterOS data and panel logic can make them genuinely complete enough to be useful.",
		},
	}
}

func selectTrafficInterfaces(configured []string, interfaces []routeros.Interface) []string {
	if len(configured) > 0 {
		return configured
	}

	for _, iface := range interfaces {
		if iface.Type == "pppoe-out" && parseBool(iface.Running) {
			return []string{iface.Name}
		}
	}
	for _, iface := range interfaces {
		if strings.EqualFold(iface.Name, "wan") && parseBool(iface.Running) {
			return []string{iface.Name}
		}
	}
	for _, iface := range interfaces {
		if iface.Type != "loopback" && parseBool(iface.Running) {
			return []string{iface.Name}
		}
	}
	return nil
}

func selectMonitorableInterfaces(interfaces []routeros.Interface) []string {
	result := make([]string, 0, len(interfaces))
	for _, iface := range interfaces {
		if parseBool(iface.Disabled) || iface.Type == "loopback" {
			continue
		}
		result = append(result, iface.Name)
	}
	return result
}

func interfaceAddresses(addresses []routeros.IPAddress, interfaceName string) []string {
	result := make([]string, 0)
	for _, address := range addresses {
		if address.Interface == interfaceName && strings.TrimSpace(address.Address) != "" {
			result = append(result, address.Address)
		}
	}
	sort.Strings(result)
	return result
}

func deriveLocalCIDRs(configured []string, trafficInterfaces []string, addresses []routeros.IPAddress, ipv6Addresses []routeros.IPv6Address, neighbors []routeros.IPv6Neighbor) []*net.IPNet {
	if len(configured) > 0 {
		return parseCIDRs(configured)
	}

	trafficSet := map[string]struct{}{}
	for _, name := range trafficInterfaces {
		trafficSet[name] = struct{}{}
	}

	cidrs := make([]string, 0)
	for _, address := range addresses {
		if parseBool(address.Dynamic) {
			continue
		}
		if _, exists := trafficSet[address.Interface]; exists {
			continue
		}
		if strings.Contains(strings.ToLower(address.Interface), "wan") {
			continue
		}
		cidrs = append(cidrs, address.Address)
	}
	for _, address := range ipv6Addresses {
		if parseBool(address.Disabled) || excludedTerminalInterface(address.Interface, trafficSet) {
			continue
		}
		cidrs = append(cidrs, address.Address)
	}
	for _, neighbor := range neighbors {
		address := strings.TrimSpace(neighbor.Address)
		if address == "" || excludedTerminalInterface(neighbor.Interface, trafficSet) {
			continue
		}
		if ip := net.ParseIP(address); ip != nil && ip.To4() == nil {
			cidrs = append(cidrs, address+"/128")
		}
	}
	return parseCIDRs(cidrs)
}

func deriveRouterAddresses(addresses []routeros.IPAddress, ipv6Addresses []routeros.IPv6Address) map[string]routerAssignedAddress {
	result := make(map[string]routerAssignedAddress, len(addresses)+len(ipv6Addresses))
	for _, address := range addresses {
		if parseBool(address.Disabled) {
			continue
		}
		if ip := assignedIP(address.Address); ip != "" {
			result[ip] = routerAssignedAddress{Family: "ipv4", Interface: strings.TrimSpace(address.Interface)}
		}
	}
	for _, address := range ipv6Addresses {
		if parseBool(address.Disabled) {
			continue
		}
		if ip := assignedIP(address.Address); ip != "" {
			result[ip] = routerAssignedAddress{Family: "ipv6", Interface: strings.TrimSpace(address.Interface)}
		}
	}
	return result
}

func assignedIP(value string) string {
	value = strings.TrimSpace(value)
	if ip, _, err := net.ParseCIDR(value); err == nil {
		return ip.String()
	}
	if ip := net.ParseIP(value); ip != nil {
		return ip.String()
	}
	return ""
}

func preferredRouterAddress(addresses map[string]routerAssignedAddress, family string) string {
	candidates := make([]string, 0)
	for address, assigned := range addresses {
		if assigned.Family == family {
			candidates = append(candidates, address)
		}
	}
	sort.Slice(candidates, func(left, right int) bool {
		leftAssigned := addresses[candidates[left]]
		rightAssigned := addresses[candidates[right]]
		leftLAN := leftAssigned.Interface == "lan"
		rightLAN := rightAssigned.Interface == "lan"
		if leftLAN != rightLAN {
			return leftLAN
		}
		return bytes.Compare(net.ParseIP(candidates[left]).To16(), net.ParseIP(candidates[right]).To16()) < 0
	})
	if len(candidates) == 0 {
		return ""
	}
	return candidates[0]
}

func excludedTerminalInterface(name string, trafficSet map[string]struct{}) bool {
	name = strings.TrimSpace(name)
	if _, exists := trafficSet[name]; exists {
		return true
	}
	lower := strings.ToLower(name)
	return name == "" || lower == "lo" || strings.Contains(lower, "loopback") || strings.Contains(lower, "wan")
}

func connectedLANDeviceCount(terminals []model.Terminal, trafficInterfaces []string) int {
	trafficSet := make(map[string]struct{}, len(trafficInterfaces))
	for _, name := range trafficInterfaces {
		trafficSet[strings.TrimSpace(name)] = struct{}{}
	}

	count := 0
	for _, terminal := range terminals {
		if terminal.ID == routerTerminalID || terminal.State != "online" {
			continue
		}
		if terminal.PrimaryInterface != "" && excludedTerminalInterface(terminal.PrimaryInterface, trafficSet) {
			continue
		}
		count++
	}
	return count
}

func parseCIDRs(values []string) []*net.IPNet {
	result := make([]*net.IPNet, 0, len(values))
	for _, value := range values {
		_, network, err := net.ParseCIDR(strings.TrimSpace(value))
		if err == nil {
			result = append(result, network)
		}
	}
	return result
}

func containsIP(networks []*net.IPNet, address string) bool {
	ip := net.ParseIP(strings.TrimSpace(address))
	if ip == nil {
		return false
	}
	for _, network := range networks {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func preferredName(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func terminalID(mac, address string) string {
	mac = normalizeMAC(mac)
	if mac != "" {
		return "mac:" + mac
	}
	return "addr:" + strings.ToLower(strings.TrimSpace(address))
}

func terminalIdentity(mac, address string, routerAddresses map[string]routerAssignedAddress) string {
	if _, exists := routerAddresses[assignedIP(address)]; exists {
		return routerTerminalID
	}
	return terminalID(mac, address)
}

func normalizeMAC(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}

func mapKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedAddresses(values map[string]struct{}) []string {
	addresses := mapKeys(values)
	sort.Slice(addresses, func(left, right int) bool {
		leftIP := net.ParseIP(addresses[left])
		rightIP := net.ParseIP(addresses[right])
		if leftIP == nil || rightIP == nil {
			return addresses[left] < addresses[right]
		}
		return bytes.Compare(leftIP.To16(), rightIP.To16()) < 0
	})
	return addresses
}

func compareTerminalAddress(left, right model.Terminal) int {
	if left.PrimaryIPv4 != "" && right.PrimaryIPv4 == "" {
		return -1
	}
	if left.PrimaryIPv4 == "" && right.PrimaryIPv4 != "" {
		return 1
	}
	for _, pair := range [][2]string{{left.PrimaryIPv4, right.PrimaryIPv4}, {left.PrimaryIPv6, right.PrimaryIPv6}, {left.MACAddress, right.MACAddress}} {
		leftIP, rightIP := net.ParseIP(pair[0]), net.ParseIP(pair[1])
		comparison := 0
		if leftIP != nil && rightIP != nil {
			comparison = bytes.Compare(leftIP.To16(), rightIP.To16())
		} else {
			comparison = strings.Compare(pair[0], pair[1])
		}
		if comparison != 0 {
			return comparison
		}
	}
	return strings.Compare(left.ID, right.ID)
}

func externalAddress(candidate, localAddress string) string {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" || candidate == strings.TrimSpace(localAddress) {
		return ""
	}
	return candidate
}

func parseInt(value string) int64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0
	}
	return parsed
}

func parseFloat(value string) float64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0
	}
	return parsed
}

func parseBool(value string) bool {
	return strings.EqualFold(strings.TrimSpace(value), "true")
}

func totalRXBps(trafficRates map[string]routeros.MonitorTrafficEntry) float64 {
	total := 0.0
	for _, entry := range trafficRates {
		total += parseFloat(entry.RXBitsPerSecond)
	}
	return total
}

func totalSelectedRXBps(trafficRates map[string]routeros.MonitorTrafficEntry, selected []string) float64 {
	total := 0.0
	for _, name := range selected {
		total += parseFloat(trafficRates[name].RXBitsPerSecond)
	}
	return total
}

func totalTXBps(trafficRates map[string]routeros.MonitorTrafficEntry) float64 {
	total := 0.0
	for _, entry := range trafficRates {
		total += parseFloat(entry.TXBitsPerSecond)
	}
	return total
}

func totalSelectedTXBps(trafficRates map[string]routeros.MonitorTrafficEntry, selected []string) float64 {
	total := 0.0
	for _, name := range selected {
		total += parseFloat(trafficRates[name].TXBitsPerSecond)
	}
	return total
}

func memoryUsedPercent(total, free int64) float64 {
	if total <= 0 {
		return 0
	}
	return float64(total-free) * 100 / float64(total)
}

func classifyApplication(protocol string, ports ...string) string {
	protocol = strings.ToLower(strings.TrimSpace(protocol))
	port := ""
	for _, candidate := range ports {
		candidate = strings.TrimSpace(candidate)
		if candidate != "" && candidate != "0" {
			port = candidate
			break
		}
	}

	switch port {
	case "80", "443", "8080", "8443":
		return "HTTP协议"
	case "53", "123":
		return "网络通讯"
	case "20", "21", "22", "445", "989", "990":
		return "文件传输"
	case "554", "8554", "1935":
		return "网络视频"
	case "6881", "6882", "6883", "6884", "6885":
		return "网络下载"
	}

	switch protocol {
	case "icmp", "icmpv6":
		return "网络通讯"
	case "udp":
		return "常用协议"
	case "tcp":
		return "未知应用"
	default:
		return "其它应用"
	}
}

func localPort(connection routeros.FirewallConnection, localAddress string) string {
	if strings.TrimSpace(localAddress) == strings.TrimSpace(connection.SrcAddress) {
		return connection.SrcPort
	}
	return connection.ReplySrcPort
}

func remoteAddress(connection routeros.FirewallConnection, localAddress string) string {
	if strings.TrimSpace(localAddress) == strings.TrimSpace(connection.SrcAddress) {
		return connection.DstAddress
	}
	return connection.ReplyDstAddress
}

func remotePort(connection routeros.FirewallConnection, localAddress string) string {
	if strings.TrimSpace(localAddress) == strings.TrimSpace(connection.SrcAddress) {
		return connection.DstPort
	}
	return connection.ReplyDstPort
}

func connectionStatus(seenReply, assured string) string {
	if !parseBool(seenReply) {
		return "未见回包"
	}
	if parseBool(assured) {
		return "已见回包 · Assured"
	}
	return "已见回包"
}

func sortConnections(connections []model.TerminalConnection) []model.TerminalConnection {
	sort.Slice(connections, func(left, right int) bool {
		leftBytes := connections[left].UploadBytes + connections[left].DownloadBytes
		rightBytes := connections[right].UploadBytes + connections[right].DownloadBytes
		if leftBytes == rightBytes {
			return connections[left].DestinationPort < connections[right].DestinationPort
		}
		return leftBytes > rightBytes
	})
	return connections
}

func flattenFlows(input map[string]*model.TerminalFlowCategory) []model.TerminalFlowCategory {
	if len(input) == 0 {
		return nil
	}
	result := make([]model.TerminalFlowCategory, 0, len(input))
	totalUpload := int64(0)
	totalDownload := int64(0)
	for _, item := range input {
		totalUpload += item.TotalUploadBytes
		totalDownload += item.TotalDownloadBytes
		result = append(result, *item)
	}
	for index := range result {
		if totalUpload > 0 {
			result[index].UploadPercent = float64(result[index].TotalUploadBytes) * 100 / float64(totalUpload)
		}
		if totalDownload > 0 {
			result[index].DownloadPercent = float64(result[index].TotalDownloadBytes) * 100 / float64(totalDownload)
		}
	}
	sort.Slice(result, func(left, right int) bool {
		leftBytes := result[left].TotalUploadBytes + result[left].TotalDownloadBytes
		rightBytes := result[right].TotalUploadBytes + result[right].TotalDownloadBytes
		return leftBytes > rightBytes
	})
	return result
}

func terminalCapabilities(flows []model.TerminalFlowCategory, history []model.TerminalHistoryEntry) []model.TerminalCapability {
	flowStatus := "supported_now"
	flowDetails := "基于当前活动连接的协议与端口进行面板侧估算，不是 RouterOS 原生应用识别。"
	if len(flows) == 0 {
		flowStatus = "limited"
		flowDetails = "当前没有足够的活动连接来估算流量分布。"
	}

	historyStatus := "supported_with_panel_persistence"
	historyDetails := "历史记录来自面板本地累计快照，从面板开始运行后持续记录。"
	if len(history) == 0 {
		historyStatus = "limited"
		historyDetails = "历史记录尚未积累出可展示数据。"
	}

	return []model.TerminalCapability{
		{Tab: "连接详情", Status: "supported_now", Details: "来自当前 RouterOS 连接跟踪表。"},
		{Tab: "流量分布", Status: flowStatus, Details: flowDetails},
		{Tab: "历史记录", Status: historyStatus, Details: historyDetails},
	}
}

func formatRouterOSUptime(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "-"
	}
	totalMinutes := int64(0)
	current := ""
	for _, char := range raw {
		if char >= '0' && char <= '9' {
			current += string(char)
			continue
		}
		if current == "" {
			continue
		}
		value, err := strconv.ParseInt(current, 10, 64)
		if err != nil {
			current = ""
			continue
		}
		switch char {
		case 'w':
			totalMinutes += value * 7 * 24 * 60
		case 'd':
			totalMinutes += value * 24 * 60
		case 'h':
			totalMinutes += value * 60
		case 'm':
			totalMinutes += value
		}
		current = ""
	}
	days := totalMinutes / (24 * 60)
	hours := (totalMinutes % (24 * 60)) / 60
	minutes := totalMinutes % 60
	return fmt.Sprintf("%d天%d小时%d分", days, hours, minutes)
}
