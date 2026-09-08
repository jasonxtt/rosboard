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

	"rosboard/internal/applicationpreset"
	"rosboard/internal/config"
	"rosboard/internal/routeros"
	"rosboard/internal/store"
)

var (
	ErrDeviceNotFound    = errors.New("device not found")
	ErrDeviceUnavailable = errors.New("device is unavailable")
)

type DeviceStatus struct {
	ID         string        `json:"id"`
	Name       string        `json:"name"`
	Enabled    bool          `json:"enabled"`
	Archived   bool          `json:"archived"`
	Healthy    bool          `json:"healthy"`
	Error      string        `json:"error,omitempty"`
	RouterName string        `json:"routerName"`
	Version    string        `json:"version"`
	UpdatedAt  time.Time     `json:"updatedAt"`
	SortOrder  int           `json:"-"`
	MosDNS     *MosDNSStatus `json:"mosdns,omitempty"`
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
	SortOrder         int       `json:"-"`
}

type managedMonitor struct {
	device        config.DeviceConfig
	monitor       *Monitor
	mosdns        *MosDNSSynchronizer
	mosdnsInitErr string
	started       bool
	err           string
}

type MonitorManager struct {
	mu      sync.RWMutex
	items   map[string]*managedMonitor
	order   []string
	logger  *log.Logger
	storage *store.Store
}

func NewMonitorManager(cfg config.Config, storage *store.Store, logger *log.Logger) (*MonitorManager, error) {
	manager := &MonitorManager{items: make(map[string]*managedMonitor), logger: logger, storage: storage}
	for _, device := range cfg.Devices {
		if device.Archived {
			continue
		}
		item := &managedMonitor{device: device}
		if device.Enabled && device.RouterOS.Configured() {
			deviceConfig := cfg
			deviceConfig.RouterOS = device.RouterOS
			deviceConfig.ProtocolAnalysis = config.ProtocolAnalysisConfig{Enabled: device.ProtocolAnalysis}
			client := routeros.NewClient(device.RouterOS.BaseURL, device.RouterOS.Username, device.RouterOS.Password)
			deviceStore, err := storage.OpenDevice(device.ID)
			if err != nil {
				return nil, fmt.Errorf("open store for device %s: %w", device.ID, err)
			}
			item.monitor = NewMonitor(deviceConfig, client, deviceStore, logger)
			if device.ProtocolAnalysis && device.MosDNS.Configured() {
				item.monitor.SetApplicationResolver(NewApplicationResolverWithRegistry(deviceStore, applicationpreset.Default(), true, device.MosDNS.MatchWindowMinutes))
			}
			if device.MosDNS.Configured() {
				mosdns, err := NewMosDNSSynchronizer(device.MosDNS, deviceStore, logger, cfg.SampleRetentionHours)
				if err != nil {
					item.mosdnsInitErr = err.Error()
					if logger != nil {
						logger.Printf("device %s MosDNS sync disabled: %v", device.ID, err)
					}
				} else {
					item.mosdns = mosdns
				}
			}
		}
		manager.items[device.ID] = item
		manager.order = append(manager.order, device.ID)
	}
	return manager, nil
}

func (m *MonitorManager) Start(ctx context.Context) {
	m.mu.RLock()
	items := make([]*managedMonitor, 0, len(m.items))
	for _, id := range m.order {
		if item := m.items[id]; item != nil {
			items = append(items, item)
		}
	}
	m.mu.RUnlock()
	for _, item := range items {
		if item.mosdns != nil {
			go item.mosdns.Start(ctx)
		}
	}
	var wait sync.WaitGroup
	for _, item := range items {
		if item.monitor == nil {
			continue
		}
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

// MosDNSStatus reports the per-device MosDNS recognition status. Devices
// without their own MosDNS configuration report an empty status.
func (m *MonitorManager) MosDNSStatus(deviceID string) MosDNSStatus {
	if m == nil {
		return MosDNSStatus{}
	}
	m.mu.RLock()
	item := m.items[deviceID]
	m.mu.RUnlock()
	if item == nil {
		return MosDNSStatus{}
	}
	if item.mosdns != nil {
		return item.mosdns.Status()
	}
	return MosDNSStatus{
		Enabled:             item.device.MosDNS.Configured(),
		BaseURL:             item.device.MosDNS.BaseURL,
		SyncIntervalMinutes: item.device.MosDNS.SyncIntervalMinutes,
		MatchWindowMinutes:  item.device.MosDNS.MatchWindowMinutes,
		LastError:           item.mosdnsInitErr,
	}
}

type RecognitionStatus struct {
	ProtocolAnalysis bool         `json:"protocolAnalysis"`
	MosDNS           MosDNSStatus `json:"mosdns"`
}

// RecognitionStatus reports the per-device protocol analysis and MosDNS state.
// Devices without configured monitors report the stored configuration only.
func (m *MonitorManager) RecognitionStatus(deviceID string) RecognitionStatus {
	if m == nil {
		return RecognitionStatus{}
	}
	m.mu.RLock()
	item := m.items[deviceID]
	m.mu.RUnlock()
	if item == nil {
		return RecognitionStatus{}
	}
	status := RecognitionStatus{ProtocolAnalysis: item.device.ProtocolAnalysis}
	if item.mosdns != nil {
		status.MosDNS = item.mosdns.Status()
	} else {
		status.MosDNS = MosDNSStatus{
			Enabled:             item.device.MosDNS.Configured(),
			BaseURL:             item.device.MosDNS.BaseURL,
			SyncIntervalMinutes: item.device.MosDNS.SyncIntervalMinutes,
			MatchWindowMinutes:  item.device.MosDNS.MatchWindowMinutes,
			LastError:           item.mosdnsInitErr,
		}
	}
	return status
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

// MonitorForDevices resolves the monitor for deviceID, falling back to the
// first enabled device in the caller's current configuration order when no
// device is requested. This keeps the "first device" default consistent with
// a reordered config without restarting the manager.
func (m *MonitorManager) MonitorForDevices(deviceID string, configured []config.DeviceConfig) (*Monitor, error) {
	if deviceID == "" {
		m.mu.RLock()
		for _, device := range configured {
			if device.Archived {
				continue
			}
			if item := m.items[device.ID]; item != nil && item.device.Enabled && item.monitor != nil {
				deviceID = device.ID
				break
			}
		}
		m.mu.RUnlock()
	}
	return m.Monitor(deviceID)
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
		status := DeviceStatus{ID: device.ID, Name: device.Name, Enabled: device.Enabled, Archived: device.Archived, SortOrder: device.SortOrder}
		if item := m.items[device.ID]; item != nil {
			status.Healthy = item.started
			status.Error = item.err
			if item.monitor != nil {
				overview := item.monitor.Snapshot().Overview
				status.RouterName = overview.RouterName
				status.Version = overview.Version
				status.UpdatedAt = overview.UpdatedAt
			}
			mosStatus := MosDNSStatus{
				Enabled:             device.MosDNS.Configured(),
				BaseURL:             device.MosDNS.BaseURL,
				SyncIntervalMinutes: device.MosDNS.SyncIntervalMinutes,
				MatchWindowMinutes:  device.MosDNS.MatchWindowMinutes,
				LastError:           item.mosdnsInitErr,
			}
			if item.mosdns != nil {
				mosStatus = item.mosdns.Status()
			}
			status.MosDNS = &mosStatus
		}
		result = append(result, status)
	}
	sort.SliceStable(result, func(i, j int) bool {
		return config.DisplayOrderLess(result[i].SortOrder, result[i].Name, result[j].SortOrder, result[j].Name)
	})
	return result
}

// FleetOverview builds the fleet grid from the caller's current device
// configuration so a reordered config is reflected without restarting the
// manager; per-device state still comes from the cached monitors.
func (m *MonitorManager) FleetOverview(now time.Time, configured []config.DeviceConfig) FleetOverview {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := FleetOverview{Devices: make([]FleetDevice, 0, len(configured))}
	for _, deviceConfig := range configured {
		if deviceConfig.Archived {
			continue
		}
		item := m.items[deviceConfig.ID]
		if item == nil || !item.device.Enabled {
			continue
		}
		device := FleetDevice{ID: deviceConfig.ID, Name: deviceConfig.Name, State: "offline", Error: item.err, SortOrder: deviceConfig.SortOrder}
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
	sort.SliceStable(result.Devices, func(i, j int) bool {
		return config.DisplayOrderLess(result.Devices[i].SortOrder, result.Devices[i].Name, result.Devices[j].SortOrder, result.Devices[j].Name)
	})
	return result
}
