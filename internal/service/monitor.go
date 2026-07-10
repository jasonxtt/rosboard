package service

import (
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
				}
			}
		}
	}()

	return nil
}

func (m *Monitor) Snapshot() model.DashboardSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()

	snapshot := m.snapshot
	snapshot.Interfaces = append([]model.InterfaceStatus(nil), snapshot.Interfaces...)
	snapshot.Terminals = append([]model.Terminal(nil), snapshot.Terminals...)
	snapshot.Capabilities = append([]model.CapabilityNote(nil), snapshot.Capabilities...)
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
	if err := m.store.UpdateTerminalRemark(ctx, id, remark); err != nil {
		return err
	}
	return m.refresh(ctx)
}

func (m *Monitor) refresh(ctx context.Context) error {
	pollCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()

	now := time.Now().UTC()

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

	trafficInterfaces := selectTrafficInterfaces(m.cfg.RouterOS.TrafficInterfaces, interfaces)
	trafficRates := make(map[string]routeros.MonitorTrafficEntry, len(trafficInterfaces))
	for _, name := range trafficInterfaces {
		entry, err := m.client.MonitorTraffic(pollCtx, name)
		if err != nil {
			return fmt.Errorf("monitor traffic for %s: %w", name, err)
		}
		trafficRates[name] = entry
		if err := m.store.SaveInterfaceSample(pollCtx, now, name, parseFloat(entry.RXBitsPerSecond), parseFloat(entry.TXBitsPerSecond)); err != nil {
			return err
		}
	}
	if err := m.store.PruneInterfaceSamples(pollCtx, now.Add(-time.Duration(m.cfg.SampleRetentionHours)*time.Hour)); err != nil {
		return err
	}

	localCIDRs := deriveLocalCIDRs(m.cfg.RouterOS.TerminalCIDRs, trafficInterfaces, addresses)
	interfaceStatuses := buildInterfaces(interfaces, ethernet, trafficRates)
	terminals, details, err := m.buildTerminals(
		pollCtx,
		now,
		localCIDRs,
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

	snapshot := model.DashboardSnapshot{
		Overview: model.Overview{
			RouterName:        resource.BoardName,
			Platform:          resource.Platform,
			Version:           resource.Version,
			BoardName:         resource.BoardName,
			Uptime:            formatRouterOSUptime(resource.Uptime),
			CPULoadPercent:    parseInt(resource.CPULoad),
			MemoryUsedPercent: memoryUsedPercent(parseInt(resource.TotalMemory), parseInt(resource.FreeMemory)),
			MemoryUsedBytes:   parseInt(resource.TotalMemory) - parseInt(resource.FreeMemory),
			MemoryTotalBytes:  parseInt(resource.TotalMemory),
			UploadBps:         totalTXBps(trafficRates),
			DownloadBps:       totalRXBps(trafficRates),
			TrafficInterfaces: trafficInterfaces,
			HealthEnabled:     strings.EqualFold(health.State, "enabled"),
			UpdatedAt:         now,
			ChartSamples:      chartSamples,
		},
		Interfaces:   interfaceStatuses,
		Terminals:    terminals,
		Capabilities: buildCapabilities(strings.EqualFold(health.State, "enabled")),
	}

	m.mu.Lock()
	m.snapshot = snapshot
	m.terminalDetails = details
	m.mu.Unlock()

	return nil
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
}

func (m *Monitor) buildTerminals(
	ctx context.Context,
	now time.Time,
	localCIDRs []*net.IPNet,
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
	ensureBuilder := func(address, family, mac string) *terminalBuilder {
		mac = normalizeMAC(mac)
		id := terminalID(mac, address)
		builder, exists := builders[id]
		if !exists {
			builder = &terminalBuilder{
				ID:               id,
				MACAddress:       mac,
				DisplayName:      preferredName(nameByAddress[address], nameByMAC[mac], mac, address),
				IPv4:             map[string]struct{}{},
				IPv6:             map[string]struct{}{},
				PrimaryInterface: interfaceByAddress[address],
				LastSeen:         now,
			}
			builders[id] = builder
		}
		if builder.DisplayName == "" {
			builder.DisplayName = preferredName(nameByAddress[address], nameByMAC[mac], mac, address)
		}
		if mac != "" {
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
		ensureBuilder(address, "ipv4", macByAddress[address])
	}

	for _, neighbor := range ipv6Neighbors {
		address := strings.TrimSpace(neighbor.Address)
		if address == "" || normalizeMAC(neighbor.MACAddress) == "" {
			continue
		}
		ensureBuilder(address, "ipv6", macByAddress[address])
	}

	applyConnections := func(family string, connections []routeros.FirewallConnection) error {
		for _, connection := range connections {
			view, ok := orientConnection(family, connection, localCIDRs)
			if !ok {
				continue
			}

			mac := normalizeMAC(macByAddress[view.LocalAddress])
			if mac != "" {
				if err := m.store.MergeTerminal(ctx, terminalID("", view.LocalAddress), terminalID(mac, view.LocalAddress)); err != nil {
					return err
				}
			}

			builder := ensureBuilder(view.LocalAddress, family, mac)
			builder.ConnectionCount++
			builder.CurrentUploadBps += view.UploadBps
			builder.CurrentDownloadBps += view.DownloadBps
			builder.LastSeen = now
			builder.DisplayName = preferredName(nameByAddress[view.LocalAddress], nameByMAC[builder.MACAddress], builder.MACAddress, view.LocalAddress)

			if err := m.store.UpsertTerminal(ctx, builder.ID, builder.MACAddress, builder.DisplayName, now); err != nil {
				return err
			}
			if err := m.store.ApplyConnectionSnapshot(ctx, view.ConnectionKey, builder.ID, view.CurrentUploadBytes, view.CurrentDownloadBytes, now); err != nil {
				return err
			}

			application := classifyApplication(connection.Protocol, connection.DstPort, connection.ReplyDstPort, connection.SrcPort)
			connectionMap[builder.ID] = append(connectionMap[builder.ID], model.TerminalConnection{
				Key:                view.ConnectionKey,
				Family:             family,
				Application:        application,
				Protocol:           strings.ToLower(connection.Protocol),
				Line:               preferredName(builder.PrimaryInterface, "-"),
				SourceAddress:      view.LocalAddress,
				SourcePort:         localPort(connection, view.LocalAddress),
				DestinationAddress: remoteAddress(connection, view.LocalAddress),
				DestinationPort:    remotePort(connection, view.LocalAddress),
				UploadBytes:        view.CurrentUploadBytes,
				DownloadBytes:      view.CurrentDownloadBytes,
				UploadBps:          view.UploadBps,
				DownloadBps:        view.DownloadBps,
				Status:             connectionStatus(connection.Assured),
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
		if err := m.store.ReplaceTerminalAddresses(ctx, builder.ID, mapKeys(builder.IPv4), mapKeys(builder.IPv6), builder.LastSeen); err != nil {
			return nil, nil, err
		}
		ids = append(ids, builder.ID)
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
		if !total.TrackingSince.IsZero() {
			onlineSeconds = int64(now.Sub(total.TrackingSince).Seconds())
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
			DisplayName:        builder.DisplayName,
			Remark:             total.Remark,
			MACAddress:         builder.MACAddress,
			PrimaryInterface:   builder.PrimaryInterface,
			IPv4:               mapKeys(builder.IPv4),
			IPv6:               mapKeys(builder.IPv6),
			ConnectionCount:    builder.ConnectionCount,
			CurrentUploadBps:   builder.CurrentUploadBps,
			CurrentDownloadBps: builder.CurrentDownloadBps,
			TotalUploadBytes:   total.UploadBytes,
			TotalDownloadBytes: total.DownloadBytes,
			TrackingSince:      total.TrackingSince,
			LastSeen:           builder.LastSeen,
		}
		terminals = append(terminals, model.Terminal{
			ID:                 terminal.ID,
			DisplayName:        terminal.DisplayName,
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
		})

		flows := flattenFlows(flowMap[builder.ID])
		details[builder.ID] = model.TerminalDetail{
			Terminal:       terminal,
			Connections:    sortConnections(connectionMap[builder.ID]),
			FlowCategories: flows,
			History:        history,
			Capabilities:   terminalCapabilities(flows, history),
		}
	}

	sort.Slice(terminals, func(left, right int) bool {
		leftRate := terminals[left].CurrentUploadBps + terminals[left].CurrentDownloadBps
		rightRate := terminals[right].CurrentUploadBps + terminals[right].CurrentDownloadBps
		if leftRate == rightRate {
			if terminals[left].ConnectionCount == terminals[right].ConnectionCount {
				return terminals[left].DisplayName < terminals[right].DisplayName
			}
			return terminals[left].ConnectionCount > terminals[right].ConnectionCount
		}
		return leftRate > rightRate
	})

	return terminals, details, nil
}

type connectionView struct {
	LocalAddress         string
	ConnectionKey        string
	CurrentUploadBytes   int64
	CurrentDownloadBytes int64
	UploadBps            float64
	DownloadBps          float64
}

func orientConnection(family string, connection routeros.FirewallConnection, localCIDRs []*net.IPNet) (connectionView, bool) {
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
	case srcLocal:
		return connectionView{
			LocalAddress:         connection.SrcAddress,
			ConnectionKey:        key,
			CurrentUploadBytes:   parseInt(connection.OrigBytes),
			CurrentDownloadBytes: parseInt(connection.ReplBytes),
			UploadBps:            parseFloat(connection.OrigRate),
			DownloadBps:          parseFloat(connection.ReplRate),
		}, true
	case replySrcLocal:
		return connectionView{
			LocalAddress:         connection.ReplySrcAddress,
			ConnectionKey:        key,
			CurrentUploadBytes:   parseInt(connection.ReplBytes),
			CurrentDownloadBytes: parseInt(connection.OrigBytes),
			UploadBps:            parseFloat(connection.ReplRate),
			DownloadBps:          parseFloat(connection.OrigRate),
		}, true
	default:
		return connectionView{}, false
	}
}

func buildInterfaces(
	interfaces []routeros.Interface,
	ethernet []routeros.EthernetInterface,
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

func deriveLocalCIDRs(configured []string, trafficInterfaces []string, addresses []routeros.IPAddress) []*net.IPNet {
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
	return parseCIDRs(cidrs)
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

func totalTXBps(trafficRates map[string]routeros.MonitorTrafficEntry) float64 {
	total := 0.0
	for _, entry := range trafficRates {
		total += parseFloat(entry.TXBitsPerSecond)
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

func connectionStatus(assured string) string {
	if parseBool(assured) {
		return "已连接"
	}
	return "等待"
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
