package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"sort"
	"sync"
	"time"

	"rosboard/internal/config"
	"rosboard/internal/routeros"
	"rosboard/internal/store"
)

var (
	ErrDeviceNotFound    = errors.New("device not found")
	ErrDeviceUnavailable = errors.New("device is unavailable")
)

type DeviceStatus struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Enabled    bool      `json:"enabled"`
	Archived   bool      `json:"archived"`
	Healthy    bool      `json:"healthy"`
	Error      string    `json:"error,omitempty"`
	RouterName string    `json:"routerName"`
	Version    string    `json:"version"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

const fleetSnapshotStaleAfter = 90 * time.Second

const (
	initialMonitorRetryDelay = 30 * time.Second
	maxMonitorRetryDelay     = 5 * time.Minute
)

type FleetOverview struct {
	TotalDevices   int           `json:"totalDevices"`
	OnlineDevices  int           `json:"onlineDevices"`
	OfflineDevices int           `json:"offlineDevices"`
	AlertDevices   int           `json:"alertDevices"`
	Devices        []FleetDevice `json:"devices"`
}

type FleetDevice struct {
	ID                string    `json:"id"`
	Name              string    `json:"name"`
	State             string    `json:"state"`
	Alerting          bool      `json:"alerting"`
	Error             string    `json:"error,omitempty"`
	RouterName        string    `json:"routerName"`
	Platform          string    `json:"platform"`
	BoardName         string    `json:"boardName"`
	Version           string    `json:"version"`
	Address           string    `json:"address"`
	CPULoadPercent    int64     `json:"cpuLoadPercent"`
	MemoryUsedPercent float64   `json:"memoryUsedPercent"`
	UploadBps         float64   `json:"uploadBps"`
	DownloadBps       float64   `json:"downloadBps"`
	TerminalCount     int       `json:"terminalCount"`
	TerminalOnline    int       `json:"terminalOnline"`
	TerminalInactive  int       `json:"terminalInactive"`
	TerminalOffline   int       `json:"terminalOffline"`
	ConnectionCount   int       `json:"connectionCount"`
	ConnectionTCP     int       `json:"connectionTCP"`
	ConnectionUDP     int       `json:"connectionUDP"`
	ConnectionOther   int       `json:"connectionOther"`
	Uptime            string    `json:"uptime"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

type managedMonitor struct {
	device  config.DeviceConfig
	monitor *Monitor
	started bool
	err     string
}

type MonitorManager struct {
	mu               sync.RWMutex
	items            map[string]*managedMonitor
	order            []string
	logger           *log.Logger
	storage          *store.Store
	mosdns           *MosDNSSynchronizer
	mosdnsConfig     config.MosDNSConfig
	mosdnsInitErr    string
	feature          *FeatureLibrarySynchronizer
	featureConfig    config.FeatureLibraryConfig
	featureInitErr   string
	resolver         *ApplicationResolver
	protocolAnalysis bool
}

func NewMonitorManager(cfg config.Config, storage *store.Store, logger *log.Logger) (*MonitorManager, error) {
	manager := &MonitorManager{items: make(map[string]*managedMonitor), logger: logger, storage: storage, mosdnsConfig: cfg.MosDNS, featureConfig: cfg.FeatureLibrary, protocolAnalysis: cfg.ProtocolAnalysis.Enabled}
	if manager.protocolAnalysis && cfg.MosDNS.Configured() {
		mosdns, err := NewMosDNSSynchronizer(cfg.MosDNS, storage, logger, cfg.SampleRetentionHours)
		if err != nil {
			manager.mosdnsInitErr = err.Error()
			if logger != nil {
				logger.Printf("MosDNS sync disabled: %v", err)
			}
		} else {
			manager.mosdns = mosdns
		}
	}
	if manager.protocolAnalysis && cfg.FeatureLibrary.Configured() {
		feature, err := NewFeatureLibrarySynchronizer(cfg.FeatureLibrary, cfg.DataDir, logger)
		if err != nil {
			manager.featureInitErr = err.Error()
			if logger != nil {
				logger.Printf("feature library disabled: %v", err)
			}
		} else {
			manager.feature = feature
		}
	}
	if manager.protocolAnalysis {
		manager.resolver = NewApplicationResolver(storage, manager.feature, cfg.MosDNS.Configured(), cfg.FeatureLibrary.MatchWindowMinutes)
	}
	for _, device := range cfg.Devices {
		if device.Archived {
			continue
		}
		item := &managedMonitor{device: device}
		if device.Enabled && device.RouterOS.Configured() {
			deviceConfig := cfg
			deviceConfig.RouterOS = device.RouterOS
			client := routeros.NewClient(device.RouterOS.BaseURL, device.RouterOS.Username, device.RouterOS.Password)
			deviceStore, err := storage.OpenDevice(device.ID)
			if err != nil {
				return nil, fmt.Errorf("open store for device %s: %w", device.ID, err)
			}
			item.monitor = NewMonitor(deviceConfig, client, deviceStore, logger)
			item.monitor.SetApplicationResolver(manager.resolver)
		}
		manager.items[device.ID] = item
		manager.order = append(manager.order, device.ID)
	}
	return manager, nil
}

func (m *MonitorManager) Start(ctx context.Context) {
	if m.mosdns != nil {
		go m.mosdns.Start(ctx)
	}
	if m.feature != nil {
		go m.feature.Start(ctx)
	}
	var wait sync.WaitGroup
	m.mu.RLock()
	items := make([]*managedMonitor, 0, len(m.items))
	for _, id := range m.order {
		if item := m.items[id]; item != nil && item.monitor != nil {
			items = append(items, item)
		}
	}
	m.mu.RUnlock()
	for _, item := range items {
		wait.Add(1)
		go func(item *managedMonitor) {
			defer wait.Done()
			if err := m.startMonitor(ctx, item, true); err != nil {
				go m.retryMonitor(ctx, item)
			}
		}(item)
	}
	wait.Wait()
}

func (m *MonitorManager) MosDNSStatus() MosDNSStatus {
	if m == nil || !m.protocolAnalysis {
		return MosDNSStatus{}
	}
	if m.mosdns != nil {
		return m.mosdns.Status()
	}
	status := MosDNSStatus{
		Enabled:             m.mosdnsConfig.Configured(),
		BaseURL:             m.mosdnsConfig.BaseURL,
		SyncIntervalMinutes: m.mosdnsConfig.SyncIntervalMinutes,
		LastError:           m.mosdnsInitErr,
	}
	return status
}

type RecognitionStatus struct {
	MosDNS         MosDNSStatus         `json:"mosdns"`
	FeatureLibrary FeatureLibraryStatus `json:"featureLibrary"`
}

func (m *MonitorManager) RecognitionStatus() RecognitionStatus {
	if m == nil || !m.protocolAnalysis {
		return RecognitionStatus{}
	}
	mosStatus := m.MosDNSStatus()
	if m.feature != nil {
		return RecognitionStatus{MosDNS: mosStatus, FeatureLibrary: m.feature.Status()}
	}
	return RecognitionStatus{
		MosDNS: mosStatus,
		FeatureLibrary: FeatureLibraryStatus{
			Enabled:              m.featureConfig.Configured(),
			SourceURL:            m.featureConfig.SourceURL,
			RefreshIntervalHours: m.featureConfig.RefreshIntervalHours,
			MatchWindowMinutes:   m.featureConfig.MatchWindowMinutes,
			LastError:            m.featureInitErr,
		},
	}
}

func (m *MonitorManager) startMonitor(ctx context.Context, item *managedMonitor, waitPhase bool) error {
	if waitPhase {
		if err := waitForDevicePhase(ctx, item.device.ID); err != nil {
			return err
		}
	}
	err := item.monitor.Start(ctx)
	m.mu.Lock()
	defer m.mu.Unlock()
	if err != nil {
		item.err = err.Error()
		m.logger.Printf("device %s monitor start failed: %v", item.device.ID, err)
		return err
	}
	item.started = true
	item.err = ""
	return nil
}

func waitForDevicePhase(ctx context.Context, deviceID string) error {
	delay := deviceSchedulePhase(deviceID)
	if delay == 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func deviceSchedulePhase(deviceID string) time.Duration {
	var phase uint32
	for _, character := range deviceID {
		phase = phase*33 + uint32(character)
	}
	return time.Duration(phase%20) * time.Second
}

func (m *MonitorManager) retryMonitor(ctx context.Context, item *managedMonitor) {
	delay := initialMonitorRetryDelay
	for {
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			if err := m.startMonitor(ctx, item, false); err == nil {
				return
			}
			delay = nextMonitorRetryDelay(delay)
		}
	}
}

func nextMonitorRetryDelay(delay time.Duration) time.Duration {
	if delay >= maxMonitorRetryDelay {
		return maxMonitorRetryDelay
	}
	next := delay * 2
	if next > maxMonitorRetryDelay {
		return maxMonitorRetryDelay
	}
	return next
}

func (m *MonitorManager) Monitor(deviceID string) (*Monitor, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if deviceID == "" {
		deviceID = m.defaultIDLocked()
	}
	item, ok := m.items[deviceID]
	if !ok {
		return nil, ErrDeviceNotFound
	}
	if !item.device.Enabled || item.monitor == nil {
		return nil, ErrDeviceUnavailable
	}
	return item.monitor, nil
}

func (m *MonitorManager) DefaultDeviceID() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.defaultIDLocked()
}

func (m *MonitorManager) defaultIDLocked() string {
	for _, id := range m.order {
		if item := m.items[id]; item != nil && item.device.Enabled && item.monitor != nil {
			return id
		}
	}
	return ""
}

func (m *MonitorManager) ViewerHeartbeat() time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var latest time.Time
	for _, item := range m.items {
		if item.monitor == nil {
			continue
		}
		if until := item.monitor.ViewerHeartbeat(); until.After(latest) {
			latest = until
		}
	}
	return latest
}

func (m *MonitorManager) Statuses(includeArchived bool, configured []config.DeviceConfig) []DeviceStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]DeviceStatus, 0, len(configured))
	for _, device := range configured {
		if device.Archived && !includeArchived {
			continue
		}
		status := DeviceStatus{ID: device.ID, Name: device.Name, Enabled: device.Enabled, Archived: device.Archived}
		if item := m.items[device.ID]; item != nil {
			status.Healthy = item.started
			status.Error = item.err
			if item.monitor != nil {
				overview := item.monitor.Snapshot().Overview
				status.RouterName = overview.RouterName
				status.Version = overview.Version
				status.UpdatedAt = overview.UpdatedAt
			}
		}
		result = append(result, status)
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

func (m *MonitorManager) FleetOverview(now time.Time) FleetOverview {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := FleetOverview{Devices: make([]FleetDevice, 0, len(m.order))}
	for _, id := range m.order {
		item := m.items[id]
		if item == nil || !item.device.Enabled || item.device.Archived {
			continue
		}
		device := FleetDevice{ID: item.device.ID, Name: item.device.Name, State: "offline", Error: item.err}
		if endpoint, err := url.Parse(item.device.RouterOS.BaseURL); err == nil {
			device.Address = endpoint.Hostname()
		}
		if item.monitor == nil {
			if device.Error == "" {
				device.Error = "设备尚未完成连接设置"
			}
		} else {
			snapshot := item.monitor.Snapshot()
			overview := snapshot.Overview
			device.RouterName = overview.RouterName
			device.Platform = overview.Platform
			device.BoardName = overview.BoardName
			device.Version = overview.Version
			device.CPULoadPercent = overview.CPULoadPercent
			device.MemoryUsedPercent = overview.MemoryUsedPercent
			device.UploadBps = overview.UploadBps
			device.DownloadBps = overview.DownloadBps
			device.TerminalCount = overview.ConnectedDeviceCount
			device.TerminalOnline = overview.TerminalStateCounts.Online
			device.TerminalInactive = overview.TerminalStateCounts.Inactive
			device.TerminalOffline = overview.TerminalStateCounts.Offline
			device.ConnectionCount = overview.ConnectionCount
			device.ConnectionTCP = overview.ConnectionProtocolCounts.TCP
			device.ConnectionUDP = overview.ConnectionProtocolCounts.UDP
			device.ConnectionOther = overview.ConnectionProtocolCounts.Other
			device.Uptime = overview.Uptime
			device.UpdatedAt = overview.UpdatedAt
			fresh := !overview.UpdatedAt.IsZero() && now.Sub(overview.UpdatedAt) <= fleetSnapshotStaleAfter
			if item.started && fresh {
				device.State = "online"
			} else if device.Error == "" {
				device.Error = "采集数据未更新"
			}
			device.Alerting = len(snapshot.Alerts) > 0 || len(snapshot.Warnings) > 0
		}
		if device.State == "offline" {
			result.OfflineDevices++
			device.Alerting = true
		} else {
			result.OnlineDevices++
		}
		if device.Alerting {
			result.AlertDevices++
		}
		result.Devices = append(result.Devices, device)
	}
	result.TotalDevices = len(result.Devices)
	sort.SliceStable(result.Devices, func(i, j int) bool { return result.Devices[i].Name < result.Devices[j].Name })
	return result
}
