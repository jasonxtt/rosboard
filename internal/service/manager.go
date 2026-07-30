package service

import (
	"context"
	"errors"
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
	mu     sync.RWMutex
	items  map[string]*managedMonitor
	order  []string
	logger *log.Logger
}

func NewMonitorManager(cfg config.Config, storage *store.Store, logger *log.Logger) *MonitorManager {
	manager := &MonitorManager{items: make(map[string]*managedMonitor), logger: logger}
	for _, device := range cfg.Devices {
		if device.Archived {
			continue
		}
		item := &managedMonitor{device: device}
		if device.Enabled && device.RouterOS.Configured() {
			deviceConfig := cfg
			deviceConfig.RouterOS = device.RouterOS
			client := routeros.NewClient(device.RouterOS.BaseURL, device.RouterOS.Username, device.RouterOS.Password)
			item.monitor = NewMonitor(deviceConfig, client, storage.ForDevice(device.ID), logger)
		}
		manager.items[device.ID] = item
		manager.order = append(manager.order, device.ID)
	}
	return manager
}

func (m *MonitorManager) Start(ctx context.Context) {
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
			if err := m.startMonitor(ctx, item); err != nil {
				go m.retryMonitor(ctx, item)
			}
		}(item)
	}
	wait.Wait()
}

func (m *MonitorManager) startMonitor(ctx context.Context, item *managedMonitor) error {
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

func (m *MonitorManager) retryMonitor(ctx context.Context, item *managedMonitor) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := m.startMonitor(ctx, item); err == nil {
				return
			}
		}
	}
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
