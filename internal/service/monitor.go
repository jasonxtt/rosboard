package service

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"maps"
	"math"
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

	refreshMu         sync.Mutex
	terminalRateMu    sync.Mutex
	mu                sync.RWMutex
	snapshot          model.DashboardSnapshot
	terminalDetails   map[string]model.TerminalDetail
	terminalScope     terminalScope
	routerAddresses   map[string]routerAssignedAddress
	terminalRatesAt   time.Time
	trafficScope      trafficScope
	activityMu        sync.RWMutex
	activeUntil       time.Time
	realtimeWake      chan struct{}
	backgroundWake    chan struct{}
	terminalViewMu    sync.RWMutex
	terminalViewUntil time.Time
	terminalRateWake  chan struct{}
	routeMu           sync.RWMutex
	routeLookup       routeMatcher
}

const (
	viewerHeartbeatTTL = 30 * time.Second
	idlePollInterval   = 60 * time.Second
)

func NewMonitor(cfg config.Config, client *routeros.Client, store *store.Store, logger *log.Logger) *Monitor {
	return &Monitor{
		cfg:              cfg,
		client:           client,
		store:            store,
		logger:           logger,
		realtimeWake:     make(chan struct{}, 1),
		backgroundWake:   make(chan struct{}, 1),
		terminalRateWake: make(chan struct{}, 1),
	}
}

func (m *Monitor) ViewerHeartbeat() time.Time {
	activeUntil, becameActive := m.markViewerActive(time.Now())
	if becameActive {
		notifyWake(m.realtimeWake)
		notifyWake(m.backgroundWake)
	}
	return activeUntil
}

func (m *Monitor) TerminalViewerHeartbeat() time.Time {
	activeUntil, becameActive := m.markTerminalViewerActive(time.Now())
	if becameActive {
		notifyWake(m.terminalRateWake)
	}
	return activeUntil
}

func (m *Monitor) markTerminalViewerActive(now time.Time) (time.Time, bool) {
	m.terminalViewMu.Lock()
	defer m.terminalViewMu.Unlock()
	becameActive := !now.Before(m.terminalViewUntil)
	m.terminalViewUntil = now.Add(viewerHeartbeatTTL)
	return m.terminalViewUntil, becameActive
}

func (m *Monitor) terminalViewerActive(now time.Time) bool {
	m.terminalViewMu.RLock()
	defer m.terminalViewMu.RUnlock()
	return now.Before(m.terminalViewUntil)
}

func (m *Monitor) markViewerActive(now time.Time) (time.Time, bool) {
	m.activityMu.Lock()
	defer m.activityMu.Unlock()
	becameActive := !now.Before(m.activeUntil)
	m.activeUntil = now.Add(viewerHeartbeatTTL)
	return m.activeUntil, becameActive
}

func (m *Monitor) viewerActive(now time.Time) bool {
	m.activityMu.RLock()
	defer m.activityMu.RUnlock()
	return now.Before(m.activeUntil)
}

func notifyWake(channel chan struct{}) {
	select {
	case channel <- struct{}{}:
	default:
	}
}

func (m *Monitor) Start(ctx context.Context) error {
	if err := m.refresh(ctx); err != nil {
		return err
	}

	go func() {
		realtimeInterval := time.Duration(m.cfg.RealtimePollIntervalSeconds) * time.Second
		for {
			interval := idlePollInterval
			if m.viewerActive(time.Now()) {
				interval = realtimeInterval
			}
			timer := time.NewTimer(interval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-m.realtimeWake:
				timer.Stop()
			case <-timer.C:
				if !m.viewerActive(time.Now()) {
					continue
				}
			}
			if err := m.refreshRealtime(ctx); err != nil {
				m.logger.Printf("realtime refresh failed: %v", err)
				m.recordRefreshError(err)
			}
		}
	}()

	go func() {
		terminalInterval := time.Duration(m.cfg.TerminalPollIntervalSeconds) * time.Second
		fullInterval := time.Duration(m.cfg.PollIntervalSeconds) * time.Second
		nextTerminal := time.Now().Add(terminalInterval)
		nextFull := time.Now().Add(fullInterval)
		nextIdleFull := time.Now().Add(idlePollInterval)
		wasActive := false

		for {
			now := time.Now()
			active := m.viewerActive(now)
			if !active && wasActive {
				nextIdleFull = now.Add(idlePollInterval)
			}
			wasActive = active
			due := nextIdleFull
			if active {
				due = nextTerminal
				if nextFull.Before(due) {
					due = nextFull
				}
			}
			timer := time.NewTimer(time.Until(due))
			woken := false
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-m.backgroundWake:
				timer.Stop()
				woken = true
			case <-timer.C:
			}

			var err error
			if woken && m.viewerActive(time.Now()) {
				err = m.refresh(ctx)
				nextFull = time.Now().Add(fullInterval)
				nextTerminal = time.Now().Add(terminalInterval)
				nextIdleFull = time.Now().Add(idlePollInterval)
				wasActive = true
			} else if !m.viewerActive(time.Now()) {
				err = m.refresh(ctx)
				nextIdleFull = time.Now().Add(idlePollInterval)
				wasActive = false
			} else if !nextFull.After(time.Now()) {
				err = m.refresh(ctx)
				nextFull = time.Now().Add(fullInterval)
				nextTerminal = time.Now().Add(terminalInterval)
			} else {
				err = m.refreshTerminals(ctx)
				nextTerminal = time.Now().Add(terminalInterval)
			}
			if err != nil {
				m.logger.Printf("background refresh failed: %v", err)
				m.recordRefreshError(err)
			}
		}
	}()

	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-m.terminalRateWake:
			case <-ticker.C:
			}
			if !m.terminalViewerActive(time.Now()) {
				continue
			}
			if err := m.refreshTerminalRates(ctx); err != nil {
				m.logger.Printf("terminal rate refresh failed: %v", err)
				m.recordRefreshError(err)
			}
		}
	}()

	return nil
}

func (m *Monitor) refreshRealtime(ctx context.Context) error {
	pollCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	now := time.Now().UTC()
	resource, err := m.client.SystemResource(pollCtx)
	if err != nil {
		return fmt.Errorf("load realtime system resource: %w", err)
	}

	previous := m.Snapshot()
	m.mu.RLock()
	trafficInterfaces := m.trafficScope.selectedNames()
	m.mu.RUnlock()
	trafficRates := make(map[string]routeros.MonitorTrafficEntry, len(trafficInterfaces))
	for _, name := range trafficInterfaces {
		entry, err := m.client.MonitorTraffic(pollCtx, name)
		if err != nil {
			m.logger.Printf("monitor realtime traffic for %s failed: %v", name, err)
			continue
		}
		trafficRates[name] = entry
		if err := m.store.SaveInterfaceSample(pollCtx, now, name, parseFloat(entry.RXBitsPerSecond), parseFloat(entry.TXBitsPerSecond)); err != nil {
			return err
		}
	}
	chartSamples, err := m.store.LoadInterfaceSamples(pollCtx, trafficInterfaces, now.Add(-5*time.Minute))
	if err != nil {
		return err
	}
	chartSamples = fillRateSampleGaps(chartSamples)

	memoryPercent := memoryUsedPercent(parseInt(resource.TotalMemory), parseInt(resource.FreeMemory))
	uploadBps := totalSelectedTXBps(trafficRates, trafficInterfaces)
	downloadBps := totalSelectedRXBps(trafficRates, trafficInterfaces)
	if err := m.store.SaveLoadSample(pollCtx, model.LoadSample{
		Timestamp: now, CPULoadPercent: float64(parseInt(resource.CPULoad)), MemoryUsedPercent: memoryPercent,
		StorageUsedPercent: previous.Overview.StorageUsedPercent, OnlineTerminalCount: previous.Overview.ConnectedDeviceCount,
		ConnectionCount: previous.Overview.ConnectionCount,
		UploadBps:       uploadBps, DownloadBps: downloadBps,
	}); err != nil {
		return err
	}

	m.mu.Lock()
	m.snapshot.Overview.CPULoadPercent = parseInt(resource.CPULoad)
	m.snapshot.Overview.MemoryUsedPercent = memoryPercent
	m.snapshot.Overview.MemoryUsedBytes = parseInt(resource.TotalMemory) - parseInt(resource.FreeMemory)
	m.snapshot.Overview.MemoryTotalBytes = parseInt(resource.TotalMemory)
	m.snapshot.Overview.UploadBps = uploadBps
	m.snapshot.Overview.DownloadBps = downloadBps
	m.snapshot.Overview.TrafficInterfaces = trafficInterfaces
	m.snapshot.Overview.ChartSamples = chartSamples
	m.snapshot.Overview.UpdatedAt = now
	m.mu.Unlock()
	return nil
}

func (m *Monitor) refreshTerminals(ctx context.Context) error {
	m.refreshMu.Lock()
	defer m.refreshMu.Unlock()

	pollCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	now := time.Now().UTC()

	var addresses []routeros.IPAddress
	var ipv6Addresses []routeros.IPv6Address
	var leases []routeros.DHCPLease
	var arpEntries []routeros.ARPEntry
	var ipv6Neighbors []routeros.IPv6Neighbor
	var connectionsV4 []routeros.FirewallConnection
	var connectionsV6 []routeros.FirewallConnection
	resultErrors := make(chan error, 7)
	var waitGroup sync.WaitGroup
	waitGroup.Go(func() {
		var err error
		addresses, err = m.client.IPAddresses(pollCtx)
		if err != nil {
			resultErrors <- fmt.Errorf("load terminal ip addresses: %w", err)
		}
	})
	waitGroup.Go(func() {
		var err error
		ipv6Addresses, err = m.client.IPv6Addresses(pollCtx)
		if err != nil {
			resultErrors <- fmt.Errorf("load terminal ipv6 addresses: %w", err)
		}
	})
	waitGroup.Go(func() {
		var err error
		leases, err = m.client.DHCPLeases(pollCtx)
		if err != nil {
			resultErrors <- fmt.Errorf("load terminal dhcp leases: %w", err)
		}
	})
	waitGroup.Go(func() {
		var err error
		arpEntries, err = m.client.ARPEntries(pollCtx)
		if err != nil {
			resultErrors <- fmt.Errorf("load terminal arp entries: %w", err)
		}
	})
	waitGroup.Go(func() {
		var err error
		ipv6Neighbors, err = m.client.IPv6Neighbors(pollCtx)
		if err != nil {
			m.logger.Printf("load terminal ipv6 neighbors failed: %v", err)
		}
	})
	waitGroup.Go(func() {
		var err error
		connectionsV4, err = m.client.FirewallConnectionsV4(pollCtx)
		if err != nil {
			resultErrors <- fmt.Errorf("load terminal ipv4 connections: %w", err)
		}
	})
	waitGroup.Go(func() {
		var err error
		connectionsV6, err = m.client.FirewallConnectionsV6(pollCtx)
		if err != nil {
			m.logger.Printf("load terminal ipv6 connections failed: %v", err)
		}
	})
	waitGroup.Wait()
	close(resultErrors)
	if err := <-resultErrors; err != nil {
		return err
	}
	ratesUpdatedAt := time.Now().UTC()

	routerAddresses := deriveRouterAddresses(addresses, ipv6Addresses)
	m.mu.RLock()
	scope := m.terminalScope
	m.mu.RUnlock()
	localCIDRs := scopeNetworks(scope)
	terminals, details, err := m.buildTerminals(pollCtx, now, localCIDRs, routerAddresses, leases, arpEntries, ipv6Neighbors, connectionsV4, connectionsV6, m.currentRouteMatcher())
	if err != nil {
		return err
	}
	protocols := aggregateProtocols(details)
	if err := m.store.SaveProtocolSamples(pollCtx, now, protocols); err != nil {
		return err
	}

	setTerminalRatesUpdatedAt(details, ratesUpdatedAt)
	m.mu.Lock()
	m.snapshot.Terminals = terminals
	m.snapshot.TerminalScopeSummaries = terminalScopeSummaries(terminals, scope)
	m.snapshot.Protocols = protocols
	m.snapshot.Overview.ConnectedDeviceCount = connectedLANDeviceCount(terminals, scope)
	m.snapshot.Overview.ConnectionCount = len(connectionsV4) + len(connectionsV6)
	m.snapshot.Overview.TerminalStateCounts = terminalStateCounts(terminals, scope)
	m.snapshot.Overview.ConnectionProtocolCounts = connectionProtocolCounts(connectionsV4, connectionsV6)
	m.terminalDetails = details
	m.routerAddresses = routerAddresses
	m.terminalRatesAt = ratesUpdatedAt
	m.mu.Unlock()
	return nil
}

func (m *Monitor) refreshTerminalRates(ctx context.Context) error {
	m.terminalRateMu.Lock()
	defer m.terminalRateMu.Unlock()

	pollCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	var connectionsV4 []routeros.FirewallConnection
	var connectionsV6 []routeros.FirewallConnection
	var v4Err error
	var v6Err error
	var waitGroup sync.WaitGroup
	waitGroup.Go(func() { connectionsV4, v4Err = m.client.FirewallConnectionsV4(pollCtx) })
	waitGroup.Go(func() { connectionsV6, v6Err = m.client.FirewallConnectionsV6(pollCtx) })
	waitGroup.Wait()
	if v4Err != nil {
		return fmt.Errorf("load terminal ipv4 connections: %w", v4Err)
	}
	if v6Err != nil {
		m.logger.Printf("load terminal ipv6 connections failed: %v", v6Err)
	}

	m.mu.RLock()
	details := cloneTerminalDetails(m.terminalDetails)
	scope := m.terminalScope
	routerAddresses := cloneRouterAddresses(m.routerAddresses)
	m.mu.RUnlock()
	if len(details) == 0 {
		return nil
	}

	ratesUpdatedAt := time.Now().UTC()
	refreshTerminalRateProjection(details, scopeNetworks(scope), routerAddresses, m.currentRouteMatcher(), connectionsV4, connectionsV6, ratesUpdatedAt)

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.terminalRatesAt.After(ratesUpdatedAt) {
		return nil
	}
	m.terminalDetails = details
	for index := range m.snapshot.Terminals {
		if detail, ok := details[m.snapshot.Terminals[index].ID]; ok {
			m.snapshot.Terminals[index] = detail.Terminal
		}
	}
	m.snapshot.TerminalScopeSummaries = terminalScopeSummaries(m.snapshot.Terminals, m.terminalScope)
	m.snapshot.Protocols = aggregateProtocols(details)
	m.snapshot.Overview.ConnectionCount = len(connectionsV4) + len(connectionsV6)
	m.snapshot.Overview.ConnectionProtocolCounts = connectionProtocolCounts(connectionsV4, connectionsV6)
	m.terminalRatesAt = ratesUpdatedAt
	return nil
}

func cloneRouterAddresses(addresses map[string]routerAssignedAddress) map[string]routerAssignedAddress {
	result := make(map[string]routerAssignedAddress, len(addresses))
	for address, assigned := range addresses {
		result[address] = assigned
	}
	return result
}

func cloneTerminalDetails(details map[string]model.TerminalDetail) map[string]model.TerminalDetail {
	result := make(map[string]model.TerminalDetail, len(details))
	for id, detail := range details {
		detail.Terminal = cloneTerminal(detail.Terminal)
		detail.Connections = append([]model.TerminalConnection(nil), detail.Connections...)
		detail.FlowCategories = append([]model.TerminalFlowCategory(nil), detail.FlowCategories...)
		detail.History = append([]model.TerminalHistoryEntry(nil), detail.History...)
		detail.Capabilities = append([]model.TerminalCapability(nil), detail.Capabilities...)
		detail.FamilySummaries = cloneTerminalSummaries(detail.FamilySummaries)
		detail.FamilyFlows = cloneFamilyFlows(detail.FamilyFlows)
		result[id] = detail
	}
	return result
}

func cloneTerminal(terminal model.Terminal) model.Terminal {
	terminal.IPv4 = append([]string(nil), terminal.IPv4...)
	terminal.IPv6 = append([]string(nil), terminal.IPv6...)
	terminal.FamilyStats = maps.Clone(terminal.FamilyStats)
	return terminal
}

func cloneTerminalSummaries(summaries map[string]model.Terminal) map[string]model.Terminal {
	result := make(map[string]model.Terminal, len(summaries))
	for family, terminal := range summaries {
		result[family] = cloneTerminal(terminal)
	}
	return result
}

func cloneFamilyFlows(flows map[string][]model.TerminalFlowCategory) map[string][]model.TerminalFlowCategory {
	result := make(map[string][]model.TerminalFlowCategory, len(flows))
	for family, items := range flows {
		result[family] = append([]model.TerminalFlowCategory(nil), items...)
	}
	return result
}

func refreshTerminalRateProjection(details map[string]model.TerminalDetail, localCIDRs []*net.IPNet, routerAddresses map[string]routerAssignedAddress, routeLookup routeMatcher, connectionsV4, connectionsV6 []routeros.FirewallConnection, ratesUpdatedAt time.Time) {
	terminalByAddress := map[string]string{}
	for id, detail := range details {
		for _, address := range detail.Terminal.IPv4 {
			terminalByAddress[assignedIP(address)] = id
		}
		for _, address := range detail.Terminal.IPv6 {
			terminalByAddress[assignedIP(address)] = id
		}
	}
	connectionsByTerminal := make(map[string][]model.TerminalConnection, len(details))
	applyConnections := func(family string, connections []routeros.FirewallConnection) {
		for _, connection := range connections {
			view, ok := orientConnection(family, connection, localCIDRs, routerAddresses)
			if !ok {
				continue
			}
			terminalID := terminalByAddress[assignedIP(view.LocalAddress)]
			if view.RouterSelf {
				terminalID = routerTerminalID
			}
			if _, ok := details[terminalID]; !ok {
				continue
			}
			connectionsByTerminal[terminalID] = append(connectionsByTerminal[terminalID], terminalConnectionRow(family, connection, view, routeLookup, details[terminalID].Terminal.PrimaryInterface))
		}
	}
	applyConnections("ipv4", connectionsV4)
	applyConnections("ipv6", connectionsV6)

	for id, detail := range details {
		connections := sortConnections(connectionsByTerminal[id])
		terminal := detail.Terminal
		terminal.ConnectionCount = len(connections)
		terminal.CurrentUploadBps = 0
		terminal.CurrentDownloadBps = 0
		for _, connection := range connections {
			terminal.CurrentUploadBps += connection.UploadBps
			terminal.CurrentDownloadBps += connection.DownloadBps
		}
		terminal.FamilyStats = map[string]model.TerminalFamilyStats{
			"ipv4": terminalFamilyStats(connections, "ipv4"),
			"ipv6": terminalFamilyStats(connections, "ipv6"),
		}
		detail.Terminal = terminal
		detail.Connections = connections
		detail.FlowCategories = terminalFlowCategories(connections, "")
		detail.FamilySummaries = map[string]model.Terminal{
			"ipv4": terminalFamilySummary(terminal, connections, "ipv4"),
			"ipv6": terminalFamilySummary(terminal, connections, "ipv6"),
		}
		detail.FamilyFlows = map[string][]model.TerminalFlowCategory{
			"ipv4": terminalFlowCategories(connections, "ipv4"),
			"ipv6": terminalFlowCategories(connections, "ipv6"),
		}
		detail.RatesUpdatedAt = ratesUpdatedAt
		details[id] = detail
	}
}

func terminalConnectionRow(family string, connection routeros.FirewallConnection, view connectionView, routeLookup routeMatcher, primaryInterface string) model.TerminalConnection {
	attribution := routeLookup.match(family, view.LocalAddress, remoteAddress(connection, view.LocalAddress), primaryInterface, connection.RoutingMark)
	return model.TerminalConnection{
		Key: firewallConnectionKey(family, connection), Family: family,
		Application: classifyApplication(connection.Protocol, connection.DstPort, connection.ReplyDstPort, connection.SrcPort),
		Protocol:    strings.ToLower(connection.Protocol), Line: "未知",
		SourceAddress: connection.SrcAddress, SourcePort: connection.SrcPort,
		DestinationAddress: connection.DstAddress, DestinationPort: connection.DstPort,
		UploadBytes: view.CurrentUploadBytes, DownloadBytes: view.CurrentDownloadBytes,
		UploadBps: view.UploadBps, DownloadBps: view.DownloadBps,
		Status: connectionStatus(connection.SeenReply, connection.Assured), SeenReply: parseBool(connection.SeenReply), Assured: parseBool(connection.Assured),
		PublicAddress: view.PublicAddress, ConnectionMark: preferredName(connection.ConnectionMark, connection.RoutingMark), RoutingMark: connection.RoutingMark,
		RouteTable: attribution.Table, MatchedRule: attribution.Rule, MatchedRuleID: attribution.RuleID,
		RouteDestination: attribution.Destination, RouteID: attribution.RouteID, RouteIDs: attribution.RouteIDs,
		RouteGateways: attribution.Gateways, RouteMatchBasis: attribution.Basis, RouteAttribution: attribution.State, Estimated: true,
	}
}

func setTerminalRatesUpdatedAt(details map[string]model.TerminalDetail, ratesUpdatedAt time.Time) {
	for id, detail := range details {
		detail.RatesUpdatedAt = ratesUpdatedAt
		details[id] = detail
	}
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
	snapshot.Interfaces = append([]model.InterfaceStatus{}, snapshot.Interfaces...)
	snapshot.Terminals = append([]model.Terminal{}, snapshot.Terminals...)
	snapshot.Capabilities = append([]model.CapabilityNote{}, snapshot.Capabilities...)
	snapshot.Protocols = append([]model.ProtocolStat{}, snapshot.Protocols...)
	snapshot.Policies = append([]model.PolicyStat{}, snapshot.Policies...)
	snapshot.Routes = append([]model.RouteStat{}, snapshot.Routes...)
	snapshot.Alerts = append([]model.AlertEvent{}, snapshot.Alerts...)
	snapshot.Warnings = append([]string{}, snapshot.Warnings...)
	snapshot.Overview.TrafficInterfaces = append([]string{}, snapshot.Overview.TrafficInterfaces...)
	snapshot.Overview.ChartSamples = append([]model.RateSample{}, snapshot.Overview.ChartSamples...)
	snapshot.TerminalScope.Interfaces = append([]model.TerminalScopeInterface(nil), snapshot.TerminalScope.Interfaces...)
	for index := range snapshot.TerminalScope.Interfaces {
		snapshot.TerminalScope.Interfaces[index].Reasons = append([]string(nil), snapshot.TerminalScope.Interfaces[index].Reasons...)
	}
	snapshot.TerminalScope.Prefixes = append([]model.TerminalScopePrefix(nil), snapshot.TerminalScope.Prefixes...)
	snapshot.TerminalScope.Warnings = append([]string(nil), snapshot.TerminalScope.Warnings...)
	snapshot.TrafficScope.Interfaces = append([]model.TrafficScopeInterface(nil), snapshot.TrafficScope.Interfaces...)
	for index := range snapshot.TrafficScope.Interfaces {
		snapshot.TrafficScope.Interfaces[index].Reasons = append([]string(nil), snapshot.TrafficScope.Interfaces[index].Reasons...)
	}
	snapshot.TrafficScope.Warnings = append([]string(nil), snapshot.TrafficScope.Warnings...)
	if snapshot.TerminalScope.Interfaces == nil {
		snapshot.TerminalScope.Interfaces = []model.TerminalScopeInterface{}
	}
	if snapshot.TerminalScope.Prefixes == nil {
		snapshot.TerminalScope.Prefixes = []model.TerminalScopePrefix{}
	}
	if snapshot.TerminalScope.Warnings == nil {
		snapshot.TerminalScope.Warnings = []string{}
	}
	if snapshot.TrafficScope.Interfaces == nil {
		snapshot.TrafficScope.Interfaces = []model.TrafficScopeInterface{}
	}
	if snapshot.TrafficScope.Warnings == nil {
		snapshot.TrafficScope.Warnings = []string{}
	}
	if snapshot.TerminalScopeSummaries == nil {
		snapshot.TerminalScopeSummaries = make(map[string]model.TerminalScopeSummary)
	}
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

func (m *Monitor) LoadHistory(ctx context.Context, since time.Time, bucket time.Duration) ([]model.LoadSample, error) {
	samples, err := m.store.LoadSamples(ctx, since)
	if err != nil {
		return nil, err
	}
	return downsampleLoadSamples(samples, bucket), nil
}

func downsampleLoadSamples(samples []model.LoadSample, bucket time.Duration) []model.LoadSample {
	if len(samples) == 0 {
		return []model.LoadSample{}
	}
	if bucket < time.Minute {
		bucket = time.Minute
	}
	type aggregate struct {
		sample            model.LoadSample
		count             int
		connectionTotal   int
		connectionSamples int
	}
	buckets := make([]aggregate, 0, len(samples))
	for _, sample := range samples {
		at := sample.Timestamp.Truncate(bucket)
		if len(buckets) == 0 || !buckets[len(buckets)-1].sample.Timestamp.Equal(at) {
			buckets = append(buckets, aggregate{sample: model.LoadSample{Timestamp: at}})
		}
		current := &buckets[len(buckets)-1]
		current.sample.CPULoadPercent += sample.CPULoadPercent
		current.sample.MemoryUsedPercent += sample.MemoryUsedPercent
		current.sample.StorageUsedPercent += sample.StorageUsedPercent
		current.sample.OnlineTerminalCount += sample.OnlineTerminalCount
		current.sample.UploadBps += sample.UploadBps
		current.sample.DownloadBps += sample.DownloadBps
		current.count++
		if sample.ConnectionCount >= 0 {
			current.connectionTotal += sample.ConnectionCount
			current.connectionSamples++
		}
	}
	result := make([]model.LoadSample, 0, len(buckets))
	for _, bucket := range buckets {
		count := float64(bucket.count)
		bucket.sample.CPULoadPercent /= count
		bucket.sample.MemoryUsedPercent /= count
		bucket.sample.StorageUsedPercent /= count
		bucket.sample.OnlineTerminalCount = int(math.Round(float64(bucket.sample.OnlineTerminalCount) / count))
		bucket.sample.UploadBps /= count
		bucket.sample.DownloadBps /= count
		bucket.sample.ConnectionCount = -1
		if bucket.connectionSamples > 0 {
			bucket.sample.ConnectionCount = int(math.Round(float64(bucket.connectionTotal) / float64(bucket.connectionSamples)))
		}
		result = append(result, bucket.sample)
	}
	if len(result) > 360 {
		result = result[len(result)-360:]
	}
	return result
}

func (m *Monitor) TrafficHistory(ctx context.Context, since time.Time, bucket time.Duration) ([]model.RateSample, error) {
	m.mu.RLock()
	interfaces := m.trafficScope.selectedNames()
	m.mu.RUnlock()
	samples, err := m.store.LoadInterfaceSamples(ctx, interfaces, since)
	if err != nil {
		return nil, err
	}
	return downsampleRateSamples(samples, bucket), nil
}

func downsampleRateSamples(samples []model.RateSample, bucket time.Duration) []model.RateSample {
	if len(samples) == 0 || bucket <= time.Second {
		return latestRateSamples(samples, 360)
	}
	type aggregate struct {
		at       time.Time
		upload   float64
		download float64
		count    int
	}
	ordered := make([]aggregate, 0, len(samples))
	for _, sample := range samples {
		at := sample.Timestamp.Truncate(bucket)
		if len(ordered) == 0 || !ordered[len(ordered)-1].at.Equal(at) {
			ordered = append(ordered, aggregate{at: at})
		}
		current := &ordered[len(ordered)-1]
		current.upload += sample.UploadBps
		current.download += sample.DownloadBps
		current.count++
	}
	result := make([]model.RateSample, 0, len(ordered))
	for _, item := range ordered {
		result = append(result, model.RateSample{
			Timestamp: item.at, UploadBps: item.upload / float64(item.count), DownloadBps: item.download / float64(item.count),
		})
	}
	return latestRateSamples(result, 360)
}

func latestRateSamples(samples []model.RateSample, limit int) []model.RateSample {
	if limit <= 0 || len(samples) <= limit {
		return samples
	}
	return samples[len(samples)-limit:]
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
		m.logger.Printf("load system health failed: %v", err)
		addWarning("system-health", "系统健康", "RouterOS 系统健康数据暂时不可用。")
	}
	interfaces, err := m.client.Interfaces(pollCtx)
	if err != nil {
		return fmt.Errorf("load interfaces: %w", err)
	}
	ethernet, err := m.client.EthernetInterfaces(pollCtx)
	if err != nil {
		m.logger.Printf("load ethernet interfaces failed: %v", err)
		addWarning("ethernet-details", "接口采集", "RouterOS 以太网接口详情暂时不可用。")
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
		m.logger.Printf("load ipv6 neighbors failed: %v", err)
		addWarning("ipv6-neighbors", "IPv6 采集", "RouterOS IPv6 邻居数据暂时不可用。")
	}
	connectionsV4, err := m.client.FirewallConnectionsV4(pollCtx)
	if err != nil {
		return fmt.Errorf("load ipv4 connections: %w", err)
	}
	connectionsV6, err := m.client.FirewallConnectionsV6(pollCtx)
	if err != nil {
		m.logger.Printf("load ipv6 connections failed: %v", err)
		addWarning("ipv6-connections", "IPv6 采集", "RouterOS IPv6 连接跟踪暂时不可用。")
	}
	ratesUpdatedAt := time.Now().UTC()
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
	routingRoutes, err := m.client.RoutingRoutes(pollCtx)
	if err != nil {
		m.logger.Printf("load routing routes failed, trying ipv4 compatibility endpoint: %v", err)
		ipRoutes, fallbackErr := m.client.IPRoutes(pollCtx)
		if fallbackErr != nil {
			addWarning("ip-routes", "路由采集", "路由表暂时不可用，保留上次有效路由数据。")
			routesComplete = false
		} else {
			routingRoutes = make([]routeros.RoutingRoute, 0, len(ipRoutes))
			for _, route := range ipRoutes {
				routingRoutes = append(routingRoutes, routeros.RoutingRoute{ID: route.ID, AFI: "ip4", DstAddress: route.DstAddress, Gateway: route.Gateway, RoutingTable: route.RoutingTable, Distance: route.Distance, Active: route.Active, Disabled: route.Disabled})
			}
		}
	}
	interfaceLists, listErr := m.client.InterfaceLists(pollCtx)
	if listErr != nil {
		addWarning("topology-interface-lists", "终端范围", "RouterOS 接口列表不可用，已使用其余拓扑证据。")
	}
	listMembers, memberErr := m.client.InterfaceListMembers(pollCtx)
	if memberErr != nil {
		addWarning("topology-interface-members", "终端范围", "RouterOS 接口列表成员不可用，已使用其余拓扑证据。")
	}
	dhcpServers, dhcpServerErr := m.client.DHCPServers(pollCtx)
	if dhcpServerErr != nil {
		addWarning("topology-dhcp-servers", "终端范围", "DHCP Server 拓扑数据不可用。")
	}
	dhcpClients, dhcpClientErr := m.client.DHCPClients(pollCtx)
	if dhcpClientErr != nil {
		addWarning("topology-dhcp-clients", "终端范围", "DHCP Client 拓扑数据不可用。")
	}
	nds, ndErr := m.client.IPv6NDs(pollCtx)
	if ndErr != nil {
		addWarning("topology-ipv6-nd", "终端范围", "IPv6 ND 拓扑数据不可用。")
	}
	ndPrefixes, ndPrefixErr := m.client.IPv6NDPrefixes(pollCtx)
	if ndPrefixErr != nil {
		addWarning("topology-ipv6-nd-prefix", "终端范围", "IPv6 ND 前缀拓扑数据不可用。")
	}
	pppoeClients, pppoeErr := m.client.PPPoEClients(pollCtx)
	if pppoeErr != nil {
		addWarning("topology-pppoe-clients", "流量采集", "PPPoE Client 拓扑数据不可用，已使用接口类型降级识别。")
	}
	routeLookup := newRouteMatcher(routingRules, routingRoutes)
	m.routeMu.Lock()
	m.routeLookup = routeLookup
	m.routeMu.Unlock()

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
	terminalScope := deriveTerminalScope(m.cfg.RouterOS, interfaces, addresses, ipv6Addresses, interfaceLists, listMembers, dhcpServers, dhcpClients, nds, ndPrefixes, routingRoutes)
	for _, warning := range terminalScope.Warnings {
		addWarning("terminal-scope-"+warning, "终端范围", warning)
	}
	trafficScope := deriveTrafficScope(m.cfg.RouterOS, terminalScope, interfaces, pppoeClients, dhcpClients, interfaceLists, listMembers, routingRoutes)
	for _, warning := range trafficScope.Warnings {
		addWarning("traffic-scope-"+warning, "流量采集", warning)
	}
	trafficInterfaces := trafficScope.selectedNames()
	localCIDRs := scopeNetworks(terminalScope)
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
		routeLookup,
	)
	if err != nil {
		return err
	}
	setTerminalRatesUpdatedAt(details, ratesUpdatedAt)

	chartSamples, err := m.store.LoadInterfaceSamples(pollCtx, trafficInterfaces, now.Add(-5*time.Minute))
	if err != nil {
		return err
	}
	chartSamples = fillRateSampleGaps(chartSamples)

	memoryPercent := memoryUsedPercent(parseInt(resource.TotalMemory), parseInt(resource.FreeMemory))
	storagePercent := memoryUsedPercent(parseInt(resource.TotalHDD), parseInt(resource.FreeHDD))
	uploadBps := totalSelectedTXBps(trafficRates, trafficInterfaces)
	downloadBps := totalSelectedRXBps(trafficRates, trafficInterfaces)
	connectedDevices := connectedLANDeviceCount(terminals, terminalScope)
	connectionCount := len(connectionsV4) + len(connectionsV6)
	terminalStates := terminalStateCounts(terminals, terminalScope)
	connectionProtocols := connectionProtocolCounts(connectionsV4, connectionsV6)
	if err := m.store.SaveLoadSample(pollCtx, model.LoadSample{Timestamp: now, CPULoadPercent: float64(parseInt(resource.CPULoad)), MemoryUsedPercent: memoryPercent, StorageUsedPercent: storagePercent, OnlineTerminalCount: connectedDevices, ConnectionCount: connectionCount, UploadBps: uploadBps, DownloadBps: downloadBps}); err != nil {
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
	routes := buildRoutes(routingRules, routingRoutes, details)
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
			RouterName:               resource.BoardName,
			Platform:                 resource.Platform,
			Version:                  resource.Version,
			BoardName:                resource.BoardName,
			Uptime:                   formatRouterOSUptime(resource.Uptime),
			CPULoadPercent:           parseInt(resource.CPULoad),
			MemoryUsedPercent:        memoryPercent,
			MemoryUsedBytes:          parseInt(resource.TotalMemory) - parseInt(resource.FreeMemory),
			MemoryTotalBytes:         parseInt(resource.TotalMemory),
			StorageUsedPercent:       storagePercent,
			StorageUsedBytes:         parseInt(resource.TotalHDD) - parseInt(resource.FreeHDD),
			StorageTotalBytes:        parseInt(resource.TotalHDD),
			ConnectedDeviceCount:     connectedDevices,
			ConnectionCount:          connectionCount,
			TerminalStateCounts:      terminalStates,
			ConnectionProtocolCounts: connectionProtocols,
			UploadBps:                uploadBps,
			DownloadBps:              downloadBps,
			TrafficInterfaces:        trafficInterfaces,
			HealthEnabled:            strings.EqualFold(health.State, "enabled"),
			UpdatedAt:                now,
			ChartSamples:             chartSamples,
		},
		Interfaces:             interfaceStatuses,
		Terminals:              terminals,
		TerminalScopeSummaries: terminalScopeSummaries(terminals, terminalScope),
		TerminalScope:          terminalScope.projection(),
		TrafficScope:           trafficScope.projection(),
		Capabilities:           buildCapabilities(strings.EqualFold(health.State, "enabled")),
		Protocols:              protocols,
		Policies:               policies,
		Routes:                 routes,
		Alerts:                 alerts,
		Warnings:               warnings,
	}

	m.mu.Lock()
	if m.snapshot.Overview.UpdatedAt.After(snapshot.Overview.UpdatedAt) {
		copyRealtimeOverview(&snapshot.Overview, m.snapshot.Overview)
	}
	if m.terminalRatesAt.After(ratesUpdatedAt) {
		preserveTerminalRateProjection(&snapshot, details, terminalScope, m.snapshot, m.terminalDetails)
		details = m.terminalDetails
	} else {
		m.terminalRatesAt = ratesUpdatedAt
	}
	m.snapshot = snapshot
	m.terminalDetails = details
	m.terminalScope = terminalScope
	m.routerAddresses = routerAddresses
	m.trafficScope = trafficScope
	m.mu.Unlock()

	return nil
}

func copyRealtimeOverview(destination *model.Overview, source model.Overview) {
	destination.CPULoadPercent = source.CPULoadPercent
	destination.MemoryUsedPercent = source.MemoryUsedPercent
	destination.MemoryUsedBytes = source.MemoryUsedBytes
	destination.MemoryTotalBytes = source.MemoryTotalBytes
	destination.UploadBps = source.UploadBps
	destination.DownloadBps = source.DownloadBps
	destination.TrafficInterfaces = append([]string(nil), source.TrafficInterfaces...)
	destination.ChartSamples = append([]model.RateSample(nil), source.ChartSamples...)
	destination.UpdatedAt = source.UpdatedAt
}

func preserveTerminalRateProjection(snapshot *model.DashboardSnapshot, details map[string]model.TerminalDetail, scope terminalScope, source model.DashboardSnapshot, sourceDetails map[string]model.TerminalDetail) {
	latestTerminals := make(map[string]model.Terminal, len(source.Terminals))
	for _, terminal := range source.Terminals {
		latestTerminals[terminal.ID] = terminal
	}
	for index := range snapshot.Terminals {
		if latest, ok := latestTerminals[snapshot.Terminals[index].ID]; ok {
			copyTerminalCurrentRates(&snapshot.Terminals[index], latest)
		}
	}
	for id, detail := range details {
		latest, ok := sourceDetails[id]
		if !ok {
			continue
		}
		copyTerminalCurrentRates(&detail.Terminal, latest.Terminal)
		detail.Connections = append([]model.TerminalConnection(nil), latest.Connections...)
		detail.FlowCategories = append([]model.TerminalFlowCategory(nil), latest.FlowCategories...)
		detail.FamilySummaries = cloneTerminalSummaries(latest.FamilySummaries)
		detail.FamilyFlows = cloneFamilyFlows(latest.FamilyFlows)
		detail.RatesUpdatedAt = latest.RatesUpdatedAt
		details[id] = detail
	}
	snapshot.TerminalScopeSummaries = terminalScopeSummaries(snapshot.Terminals, scope)
	snapshot.Protocols = append([]model.ProtocolStat(nil), source.Protocols...)
	snapshot.Overview.ConnectionCount = source.Overview.ConnectionCount
	snapshot.Overview.ConnectionProtocolCounts = source.Overview.ConnectionProtocolCounts
}

func copyTerminalCurrentRates(destination *model.Terminal, source model.Terminal) {
	destination.ConnectionCount = source.ConnectionCount
	destination.CurrentUploadBps = source.CurrentUploadBps
	destination.CurrentDownloadBps = source.CurrentDownloadBps
	destination.FamilyStats = maps.Clone(source.FamilyStats)
}

func (m *Monitor) currentRouteMatcher() routeMatcher {
	m.routeMu.RLock()
	defer m.routeMu.RUnlock()
	return m.routeLookup
}

func fillRateSampleGaps(samples []model.RateSample) []model.RateSample {
	if len(samples) < 2 {
		return samples
	}
	result := make([]model.RateSample, 0, len(samples))
	result = append(result, samples[0])
	for _, sample := range samples[1:] {
		previous := result[len(result)-1]
		for timestamp := previous.Timestamp.Add(time.Second); timestamp.Before(sample.Timestamp); timestamp = timestamp.Add(time.Second) {
			result = append(result, model.RateSample{
				Timestamp: timestamp, UploadBps: previous.UploadBps, DownloadBps: previous.DownloadBps,
			})
		}
		result = append(result, sample)
	}
	return result
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

func buildRoutes(rules []routeros.RoutingRule, routes []routeros.RoutingRoute, details map[string]model.TerminalDetail) []model.RouteStat {
	matches := make(map[string]int)
	for _, detail := range details {
		for _, connection := range detail.Connections {
			if connection.MatchedRuleID != "" {
				matches[connection.MatchedRuleID]++
			}
			for _, routeID := range connection.RouteIDs {
				matches[routeID]++
			}
		}
	}
	result := make([]model.RouteStat, 0, len(rules)+len(routes))
	for index, item := range rules {
		id := stableRuleID(item, index)
		result = append(result, model.RouteStat{ID: id, Kind: "rule", Family: routeFamily(item.DstAddress, item.SrcAddress), Destination: item.DstAddress, Table: item.Table, Action: item.Action, Source: preferredName(item.SrcAddress, item.Interface), Disabled: parseBool(item.Disabled), CurrentMatches: matches[id]})
	}
	for index, item := range routes {
		id := stableRouteID(item, index)
		result = append(result, model.RouteStat{ID: id, Kind: "route", Family: routeFamily(item.AFI, item.DstAddress), Destination: item.DstAddress, Gateway: preferredName(item.ImmediateGateway, item.Gateway), Table: item.RoutingTable, Distance: parseInt(item.Distance), Active: parseBool(item.Active), Disabled: parseBool(item.Disabled), CurrentMatches: matches[id]})
	}
	return result
}

func routeFamily(values ...string) string {
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), "ip6") || strings.Contains(value, ":") {
			return "ipv6"
		}
	}
	return "ipv4"
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
	routeLookup routeMatcher,
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
		if address == "" || !terminalDiscoveryInScope(localCIDRs, address) {
			continue
		}
		ensureBuilder(address, "ipv4", macByAddress[address])
	}

	for _, entry := range arpEntries {
		address := strings.TrimSpace(entry.Address)
		if address == "" || normalizeMAC(entry.MACAddress) == "" || !terminalDiscoveryInScope(localCIDRs, address) {
			continue
		}
		builder := ensureBuilder(address, "ipv4", macByAddress[address])
		if strings.EqualFold(entry.Status, "reachable") || strings.EqualFold(entry.Status, "permanent") {
			markOnline(builder)
		}
	}

	for _, neighbor := range ipv6Neighbors {
		address := strings.TrimSpace(neighbor.Address)
		if address == "" || normalizeMAC(neighbor.MACAddress) == "" || !terminalDiscoveryInScope(localCIDRs, address) {
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

			row := terminalConnectionRow(family, connection, view, routeLookup, builder.PrimaryInterface)
			connectionMap[builder.ID] = append(connectionMap[builder.ID], row)

			if flowMap[builder.ID] == nil {
				flowMap[builder.ID] = map[string]*model.TerminalFlowCategory{}
			}
			flow := flowMap[builder.ID][row.Application]
			if flow == nil {
				flow = &model.TerminalFlowCategory{
					Name:      row.Application,
					Estimated: true,
				}
				flowMap[builder.ID][row.Application] = flow
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
	return terminalFlowCategories(connections, family)
}

func terminalFlowCategories(connections []model.TerminalConnection, family string) []model.TerminalFlowCategory {
	flows := map[string]*model.TerminalFlowCategory{}
	for _, connection := range connections {
		if family != "" && connection.Family != family {
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

	key := firewallConnectionKey(family, connection)

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

func firewallConnectionKey(family string, connection routeros.FirewallConnection) string {
	if id := strings.TrimSpace(connection.ID); id != "" {
		return family + "|" + id
	}
	return fmt.Sprintf(
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

func connectedLANDeviceCount(terminals []model.Terminal, scope terminalScope) int {
	count := 0
	for _, terminal := range terminals {
		if !connectedLANTerminal(terminal, scope) {
			continue
		}
		count++
	}
	return count
}

func connectedLANTerminal(terminal model.Terminal, scope terminalScope) bool {
	return terminal.State == "online" && scopedLANTerminal(terminal, scope)
}

func scopedLANTerminal(terminal model.Terminal, scope terminalScope) bool {
	if terminal.ID == routerTerminalID {
		return false
	}
	name := strings.TrimSpace(terminal.PrimaryInterface)
	if name == "" {
		return true
	}
	if strings.EqualFold(name, "lo") || strings.Contains(strings.ToLower(name), "loopback") {
		return false
	}
	evidence, known := scope.Interfaces[name]
	return !known || evidence.Role == InterfaceRoleLAN
}

func terminalStateCounts(terminals []model.Terminal, scope terminalScope) model.TerminalStateCounts {
	counts := model.TerminalStateCounts{}
	for _, terminal := range terminals {
		if !scopedLANTerminal(terminal, scope) {
			continue
		}
		switch terminal.State {
		case "online":
			counts.Online++
		case "inactive":
			counts.Inactive++
		case "offline":
			counts.Offline++
		}
	}
	return counts
}

func connectionProtocolCounts(groups ...[]routeros.FirewallConnection) model.ConnectionProtocolCounts {
	counts := model.ConnectionProtocolCounts{}
	for _, connections := range groups {
		for _, connection := range connections {
			switch strings.ToLower(strings.TrimSpace(connection.Protocol)) {
			case "tcp":
				counts.TCP++
			case "udp":
				counts.UDP++
			default:
				counts.Other++
			}
		}
	}
	return counts
}

func terminalScopeSummaries(terminals []model.Terminal, scope terminalScope) map[string]model.TerminalScopeSummary {
	summaries := map[string]model.TerminalScopeSummary{
		"all": {}, "ipv4": {}, "ipv6": {},
	}
	for _, terminal := range terminals {
		if !connectedLANTerminal(terminal, scope) {
			continue
		}
		all := summaries["all"]
		all.DeviceCount++
		for _, family := range []string{"ipv4", "ipv6"} {
			familySummary := summaries[family]
			if (family == "ipv4" && len(terminal.IPv4) > 0) || (family == "ipv6" && len(terminal.IPv6) > 0) {
				familySummary.DeviceCount++
			}
			stats := terminal.FamilyStats[family]
			familySummary.ConnectionCount += stats.ConnectionCount
			familySummary.CurrentUploadBps += stats.CurrentUploadBps
			familySummary.CurrentDownloadBps += stats.CurrentDownloadBps
			familySummary.ActiveUploadBytes += stats.ActiveUploadBytes
			familySummary.ActiveDownloadBytes += stats.ActiveDownloadBytes
			summaries[family] = familySummary
			all.ConnectionCount += stats.ConnectionCount
			all.CurrentUploadBps += stats.CurrentUploadBps
			all.CurrentDownloadBps += stats.CurrentDownloadBps
			all.ActiveUploadBytes += stats.ActiveUploadBytes
			all.ActiveDownloadBytes += stats.ActiveDownloadBytes
		}
		summaries["all"] = all
	}
	return summaries
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

func terminalDiscoveryInScope(networks []*net.IPNet, address string) bool {
	return len(networks) > 0 && containsIP(networks, address)
}

func scopeNetworks(scope terminalScope) []*net.IPNet {
	result := make([]*net.IPNet, 0, len(scope.Prefixes))
	for _, prefix := range scope.Prefixes {
		bits := 128
		if prefix.Prefix.Addr().Is4() {
			bits = 32
		}
		result = append(result, &net.IPNet{IP: net.IP(prefix.Prefix.Addr().AsSlice()), Mask: net.CIDRMask(prefix.Prefix.Bits(), bits)})
	}
	return result
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

func remoteAddress(connection routeros.FirewallConnection, localAddress string) string {
	if strings.TrimSpace(localAddress) == strings.TrimSpace(connection.SrcAddress) {
		return connection.DstAddress
	}
	return connection.ReplyDstAddress
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
