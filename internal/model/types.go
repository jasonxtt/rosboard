package model

import "time"

type Overview struct {
	RouterName           string       `json:"routerName"`
	Platform             string       `json:"platform"`
	Version              string       `json:"version"`
	BoardName            string       `json:"boardName"`
	Uptime               string       `json:"uptime"`
	CPULoadPercent       int64        `json:"cpuLoadPercent"`
	MemoryUsedPercent    float64      `json:"memoryUsedPercent"`
	MemoryUsedBytes      int64        `json:"memoryUsedBytes"`
	MemoryTotalBytes     int64        `json:"memoryTotalBytes"`
	StorageUsedPercent   float64      `json:"storageUsedPercent"`
	StorageUsedBytes     int64        `json:"storageUsedBytes"`
	StorageTotalBytes    int64        `json:"storageTotalBytes"`
	ConnectedDeviceCount int          `json:"connectedDeviceCount"`
	ConnectionCount      int          `json:"connectionCount"`
	UploadBps            float64      `json:"uploadBps"`
	DownloadBps          float64      `json:"downloadBps"`
	TrafficInterfaces    []string     `json:"trafficInterfaces"`
	HealthEnabled        bool         `json:"healthEnabled"`
	UpdatedAt            time.Time    `json:"updatedAt"`
	ChartSamples         []RateSample `json:"chartSamples"`
}

type RateSample struct {
	Timestamp   time.Time `json:"timestamp"`
	UploadBps   float64   `json:"uploadBps"`
	DownloadBps float64   `json:"downloadBps"`
}

type LoadSample struct {
	Timestamp           time.Time `json:"timestamp"`
	CPULoadPercent      float64   `json:"cpuLoadPercent"`
	MemoryUsedPercent   float64   `json:"memoryUsedPercent"`
	StorageUsedPercent  float64   `json:"storageUsedPercent"`
	OnlineTerminalCount int       `json:"onlineTerminalCount"`
	UploadBps           float64   `json:"uploadBps"`
	DownloadBps         float64   `json:"downloadBps"`
}

type InterfaceStatus struct {
	Name           string   `json:"name"`
	Type           string   `json:"type"`
	Running        bool     `json:"running"`
	Disabled       bool     `json:"disabled"`
	MACAddress     string   `json:"macAddress"`
	Status         string   `json:"status"`
	LastLinkUpTime string   `json:"lastLinkUpTime"`
	LinkDowns      int64    `json:"linkDowns"`
	ActualMTU      int64    `json:"actualMtu"`
	RXBytes        int64    `json:"rxBytes"`
	TXBytes        int64    `json:"txBytes"`
	CurrentRXBps   float64  `json:"currentRxBps"`
	CurrentTXBps   float64  `json:"currentTxBps"`
	Addresses      []string `json:"addresses"`
	RXPackets      int64    `json:"rxPackets"`
	TXPackets      int64    `json:"txPackets"`
	RXDrops        int64    `json:"rxDrops"`
	TXDrops        int64    `json:"txDrops"`
	RXErrors       int64    `json:"rxErrors"`
	TXErrors       int64    `json:"txErrors"`
	LinkRate       string   `json:"linkRate"`
	FullDuplex     bool     `json:"fullDuplex"`
}

type InterfaceDetail struct {
	Interface InterfaceStatus `json:"interface"`
	Samples   []RateSample    `json:"samples"`
}

type Terminal struct {
	ID                 string                         `json:"id"`
	DisplayName        string                         `json:"displayName"`
	Remark             string                         `json:"remark"`
	MACAddress         string                         `json:"macAddress"`
	PrimaryInterface   string                         `json:"primaryInterface"`
	IPv4               []string                       `json:"ipv4"`
	IPv6               []string                       `json:"ipv6"`
	ConnectionCount    int                            `json:"connectionCount"`
	CurrentUploadBps   float64                        `json:"currentUploadBps"`
	CurrentDownloadBps float64                        `json:"currentDownloadBps"`
	TotalUploadBytes   int64                          `json:"totalUploadBytes"`
	TotalDownloadBytes int64                          `json:"totalDownloadBytes"`
	TrackingSince      time.Time                      `json:"trackingSince"`
	LastSeen           time.Time                      `json:"lastSeen"`
	PrimaryIPv4        string                         `json:"primaryIpv4"`
	PrimaryIPv6        string                         `json:"primaryIpv6"`
	State              string                         `json:"state"`
	OnlineSince        time.Time                      `json:"onlineSince"`
	FamilyStats        map[string]TerminalFamilyStats `json:"familyStats"`
}

type TerminalFamilyStats struct {
	ConnectionCount     int     `json:"connectionCount"`
	CurrentUploadBps    float64 `json:"currentUploadBps"`
	CurrentDownloadBps  float64 `json:"currentDownloadBps"`
	ActiveUploadBytes   int64   `json:"activeUploadBytes"`
	ActiveDownloadBytes int64   `json:"activeDownloadBytes"`
}

type TerminalConnection struct {
	Key                string  `json:"key"`
	Family             string  `json:"family"`
	Application        string  `json:"application"`
	Protocol           string  `json:"protocol"`
	Line               string  `json:"line"`
	SourceAddress      string  `json:"sourceAddress"`
	SourcePort         string  `json:"sourcePort"`
	DestinationAddress string  `json:"destinationAddress"`
	DestinationPort    string  `json:"destinationPort"`
	UploadBytes        int64   `json:"uploadBytes"`
	DownloadBytes      int64   `json:"downloadBytes"`
	UploadBps          float64 `json:"uploadBps"`
	DownloadBps        float64 `json:"downloadBps"`
	Status             string  `json:"status"`
	SeenReply          bool    `json:"seenReply"`
	Assured            bool    `json:"assured"`
	PublicAddress      string  `json:"publicAddress"`
	ConnectionMark     string  `json:"connectionMark"`
	Estimated          bool    `json:"estimated"`
}

type TerminalFlowCategory struct {
	Name               string  `json:"name"`
	CurrentUploadBps   float64 `json:"currentUploadBps"`
	CurrentDownloadBps float64 `json:"currentDownloadBps"`
	TotalUploadBytes   int64   `json:"totalUploadBytes"`
	TotalDownloadBytes int64   `json:"totalDownloadBytes"`
	UploadPercent      float64 `json:"uploadPercent"`
	DownloadPercent    float64 `json:"downloadPercent"`
	Estimated          bool    `json:"estimated"`
}

type TerminalHistoryEntry struct {
	Timestamp          time.Time `json:"timestamp"`
	OnlineSeconds      int64     `json:"onlineSeconds"`
	TotalUploadBytes   int64     `json:"totalUploadBytes"`
	TotalDownloadBytes int64     `json:"totalDownloadBytes"`
}

type TerminalCapability struct {
	Tab     string `json:"tab"`
	Status  string `json:"status"`
	Details string `json:"details"`
}

type TerminalDetail struct {
	Terminal        Terminal                          `json:"terminal"`
	Connections     []TerminalConnection              `json:"connections"`
	FlowCategories  []TerminalFlowCategory            `json:"flowCategories"`
	History         []TerminalHistoryEntry            `json:"history"`
	Capabilities    []TerminalCapability              `json:"capabilities"`
	FamilySummaries map[string]Terminal               `json:"familySummaries"`
	FamilyFlows     map[string][]TerminalFlowCategory `json:"familyFlows"`
}

type CapabilityNote struct {
	Area    string `json:"area"`
	Item    string `json:"item"`
	Status  string `json:"status"`
	Details string `json:"details"`
}

type ProtocolStat struct {
	Name          string  `json:"name"`
	Kind          string  `json:"kind"`
	Connections   int     `json:"connections"`
	UploadBps     float64 `json:"uploadBps"`
	DownloadBps   float64 `json:"downloadBps"`
	UploadBytes   int64   `json:"uploadBytes"`
	DownloadBytes int64   `json:"downloadBytes"`
	Estimated     bool    `json:"estimated"`
}

type ProtocolHistorySample struct {
	Timestamp   time.Time `json:"timestamp"`
	Name        string    `json:"name"`
	Kind        string    `json:"kind"`
	Connections int       `json:"connections"`
	UploadBps   float64   `json:"uploadBps"`
	DownloadBps float64   `json:"downloadBps"`
}

type PolicyStat struct {
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	Target   string `json:"target"`
	Mark     string `json:"mark"`
	Rate     string `json:"rate"`
	Bytes    int64  `json:"bytes"`
	Packets  int64  `json:"packets"`
	Disabled bool   `json:"disabled"`
}

type RouteStat struct {
	Kind        string `json:"kind"`
	Destination string `json:"destination"`
	Gateway     string `json:"gateway"`
	Table       string `json:"table"`
	Action      string `json:"action"`
	Source      string `json:"source"`
	Distance    int64  `json:"distance"`
	Active      bool   `json:"active"`
	Disabled    bool   `json:"disabled"`
}

type DashboardSnapshot struct {
	Overview     Overview          `json:"overview"`
	Interfaces   []InterfaceStatus `json:"interfaces"`
	Terminals    []Terminal        `json:"terminals"`
	Capabilities []CapabilityNote  `json:"capabilities"`
	Protocols    []ProtocolStat    `json:"protocols"`
	Policies     []PolicyStat      `json:"policies"`
	Routes       []RouteStat       `json:"routes"`
	Warnings     []string          `json:"warnings"`
}
