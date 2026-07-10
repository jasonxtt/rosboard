package model

import "time"

type Overview struct {
	RouterName        string       `json:"routerName"`
	Platform          string       `json:"platform"`
	Version           string       `json:"version"`
	BoardName         string       `json:"boardName"`
	Uptime            string       `json:"uptime"`
	CPULoadPercent    int64        `json:"cpuLoadPercent"`
	MemoryUsedPercent float64      `json:"memoryUsedPercent"`
	MemoryUsedBytes   int64        `json:"memoryUsedBytes"`
	MemoryTotalBytes  int64        `json:"memoryTotalBytes"`
	UploadBps         float64      `json:"uploadBps"`
	DownloadBps       float64      `json:"downloadBps"`
	TrafficInterfaces []string     `json:"trafficInterfaces"`
	HealthEnabled     bool         `json:"healthEnabled"`
	UpdatedAt         time.Time    `json:"updatedAt"`
	ChartSamples      []RateSample `json:"chartSamples"`
}

type RateSample struct {
	Timestamp   time.Time `json:"timestamp"`
	UploadBps   float64   `json:"uploadBps"`
	DownloadBps float64   `json:"downloadBps"`
}

type InterfaceStatus struct {
	Name           string  `json:"name"`
	Type           string  `json:"type"`
	Running        bool    `json:"running"`
	Disabled       bool    `json:"disabled"`
	MACAddress     string  `json:"macAddress"`
	Status         string  `json:"status"`
	LastLinkUpTime string  `json:"lastLinkUpTime"`
	LinkDowns      int64   `json:"linkDowns"`
	ActualMTU      int64   `json:"actualMtu"`
	RXBytes        int64   `json:"rxBytes"`
	TXBytes        int64   `json:"txBytes"`
	CurrentRXBps   float64 `json:"currentRxBps"`
	CurrentTXBps   float64 `json:"currentTxBps"`
}

type Terminal struct {
	ID                 string    `json:"id"`
	DisplayName        string    `json:"displayName"`
	Remark             string    `json:"remark"`
	MACAddress         string    `json:"macAddress"`
	PrimaryInterface   string    `json:"primaryInterface"`
	IPv4               []string  `json:"ipv4"`
	IPv6               []string  `json:"ipv6"`
	ConnectionCount    int       `json:"connectionCount"`
	CurrentUploadBps   float64   `json:"currentUploadBps"`
	CurrentDownloadBps float64   `json:"currentDownloadBps"`
	TotalUploadBytes   int64     `json:"totalUploadBytes"`
	TotalDownloadBytes int64     `json:"totalDownloadBytes"`
	TrackingSince      time.Time `json:"trackingSince"`
	LastSeen           time.Time `json:"lastSeen"`
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
	Terminal       Terminal               `json:"terminal"`
	Connections    []TerminalConnection   `json:"connections"`
	FlowCategories []TerminalFlowCategory `json:"flowCategories"`
	History        []TerminalHistoryEntry `json:"history"`
	Capabilities   []TerminalCapability   `json:"capabilities"`
}

type CapabilityNote struct {
	Area    string `json:"area"`
	Item    string `json:"item"`
	Status  string `json:"status"`
	Details string `json:"details"`
}

type DashboardSnapshot struct {
	Overview     Overview          `json:"overview"`
	Interfaces   []InterfaceStatus `json:"interfaces"`
	Terminals    []Terminal        `json:"terminals"`
	Capabilities []CapabilityNote  `json:"capabilities"`
}
