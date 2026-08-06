package model

import "time"

type DNSObservation struct {
	DedupeKey    string    `json:"-"`
	TraceID      string    `json:"traceId"`
	ClientIP     string    `json:"clientIp"`
	Domain       string    `json:"domain"`
	AnswerIP     string    `json:"answerIp"`
	QueryType    string    `json:"queryType"`
	QueryTime    time.Time `json:"queryTime"`
	TTL          int64     `json:"ttl"`
	EffectiveTag string    `json:"effectiveTag,omitempty"`
	IngestedAt   time.Time `json:"ingestedAt"`
}

type DNSFeature struct {
	ClientIP     string    `json:"clientIp"`
	Domain       string    `json:"domain"`
	AnswerIP     string    `json:"answerIp"`
	QueryType    string    `json:"queryType"`
	FirstSeen    time.Time `json:"firstSeen"`
	LastSeen     time.Time `json:"lastSeen"`
	HitCount     int64     `json:"hitCount"`
	LastTTL      int64     `json:"lastTtl"`
	EffectiveTag string    `json:"effectiveTag,omitempty"`
}

type Overview struct {
	RouterName               string                   `json:"routerName"`
	Platform                 string                   `json:"platform"`
	Version                  string                   `json:"version"`
	BoardName                string                   `json:"boardName"`
	Uptime                   string                   `json:"uptime"`
	SystemResource           SystemResource           `json:"systemResource"`
	CPULoadPercent           int64                    `json:"cpuLoadPercent"`
	MemoryUsedPercent        float64                  `json:"memoryUsedPercent"`
	MemoryUsedBytes          int64                    `json:"memoryUsedBytes"`
	MemoryTotalBytes         int64                    `json:"memoryTotalBytes"`
	StorageUsedPercent       float64                  `json:"storageUsedPercent"`
	StorageUsedBytes         int64                    `json:"storageUsedBytes"`
	StorageTotalBytes        int64                    `json:"storageTotalBytes"`
	ConnectedDeviceCount     int                      `json:"connectedDeviceCount"`
	ConnectionCount          int                      `json:"connectionCount"`
	TerminalStateCounts      TerminalStateCounts      `json:"terminalStateCounts"`
	ConnectionProtocolCounts ConnectionProtocolCounts `json:"connectionProtocolCounts"`
	UploadBps                float64                  `json:"uploadBps"`
	DownloadBps              float64                  `json:"downloadBps"`
	TrafficInterfaces        []string                 `json:"trafficInterfaces"`
	HealthEnabled            bool                     `json:"healthEnabled"`
	UpdatedAt                time.Time                `json:"updatedAt"`
	ChartSamples             []RateSample             `json:"chartSamples"`
}

type SystemResource struct {
	ArchitectureName     string                   `json:"architectureName"`
	BoardName            string                   `json:"boardName"`
	BadBlocks            string                   `json:"badBlocks"`
	BuildTime            string                   `json:"buildTime"`
	CPU                  string                   `json:"cpu"`
	CPUCount             string                   `json:"cpuCount"`
	CPUFrequency         string                   `json:"cpuFrequency"`
	CPULoad              string                   `json:"cpuLoad"`
	FactorySoftware      string                   `json:"factorySoftware"`
	FreeMemory           string                   `json:"freeMemory"`
	FreeHDD              string                   `json:"freeHddSpace"`
	Platform             string                   `json:"platform"`
	TotalMemory          string                   `json:"totalMemory"`
	TotalHDD             string                   `json:"totalHddSpace"`
	Uptime               string                   `json:"uptime"`
	Version              string                   `json:"version"`
	WriteSectSinceReboot string                   `json:"writeSectSinceReboot"`
	WriteSectTotal       string                   `json:"writeSectTotal"`
	CPUCores             []SystemResourceCPU      `json:"cpuCores"`
	IRQs                 []SystemResourceIRQ      `json:"irqs"`
	Hardware             []SystemResourceHardware `json:"hardware"`
}

type SystemResourceCPU struct {
	CPU  string `json:"cpu"`
	Load string `json:"load"`
	IRQ  string `json:"irq"`
	Disk string `json:"disk"`
}

type SystemResourceIRQ struct {
	CPU       string `json:"cpu"`
	ActiveCPU string `json:"activeCpu"`
	Count     string `json:"count"`
	IRQ       string `json:"irq"`
	Users     string `json:"users"`
}

type SystemResourceHardware struct {
	Location     string `json:"location"`
	Parent       string `json:"parent"`
	Type         string `json:"type"`
	Vendor       string `json:"vendor"`
	Name         string `json:"name"`
	SerialNumber string `json:"serialNumber"`
	VendorID     string `json:"vendorId"`
	DeviceID     string `json:"deviceId"`
	Speed        string `json:"speed"`
	Ports        string `json:"ports"`
	USBVersion   string `json:"usbVersion"`
	Owner        string `json:"owner"`
	DevicePath   string `json:"devicePath"`
	Category     string `json:"category"`
	IRQ          string `json:"irq"`
}

type TerminalStateCounts struct {
	Online   int `json:"online"`
	Inactive int `json:"inactive"`
	Offline  int `json:"offline"`
}

type ConnectionProtocolCounts struct {
	TCP   int `json:"tcp"`
	UDP   int `json:"udp"`
	Other int `json:"other"`
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
	ConnectionCount     int       `json:"connectionCount"`
	UploadBps           float64   `json:"uploadBps"`
	DownloadBps         float64   `json:"downloadBps"`
}

type InterfaceStatus struct {
	Name           string              `json:"name"`
	Type           string              `json:"type"`
	Running        bool                `json:"running"`
	Disabled       bool                `json:"disabled"`
	MACAddress     string              `json:"macAddress"`
	Status         string              `json:"status"`
	LastLinkUpTime string              `json:"lastLinkUpTime"`
	LinkDowns      int64               `json:"linkDowns"`
	ActualMTU      int64               `json:"actualMtu"`
	RXBytes        int64               `json:"rxBytes"`
	TXBytes        int64               `json:"txBytes"`
	CurrentRXBps   float64             `json:"currentRxBps"`
	CurrentTXBps   float64             `json:"currentTxBps"`
	Addresses      []string            `json:"addresses"`
	RXPackets      int64               `json:"rxPackets"`
	TXPackets      int64               `json:"txPackets"`
	RXDrops        int64               `json:"rxDrops"`
	TXDrops        int64               `json:"txDrops"`
	RXErrors       int64               `json:"rxErrors"`
	TXErrors       int64               `json:"txErrors"`
	LinkRate       string              `json:"linkRate"`
	FullDuplex     bool                `json:"fullDuplex"`
	Category       string              `json:"category"`
	Relations      []InterfaceRelation `json:"relations"`
}

type InterfaceRelation struct {
	Kind      string `json:"kind"`
	Interface string `json:"interface"`
}

type InterfaceDetail struct {
	Interface InterfaceStatus `json:"interface"`
	Samples   []RateSample    `json:"samples"`
}

type Terminal struct {
	ID                 string                         `json:"id"`
	DisplayName        string                         `json:"displayName"`
	AutoName           string                         `json:"autoName"`
	CustomName         string                         `json:"customName"`
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

type TerminalScopeSummary struct {
	DeviceCount         int     `json:"deviceCount"`
	ConnectionCount     int     `json:"connectionCount"`
	CurrentUploadBps    float64 `json:"currentUploadBps"`
	CurrentDownloadBps  float64 `json:"currentDownloadBps"`
	ActiveUploadBytes   int64   `json:"activeUploadBytes"`
	ActiveDownloadBytes int64   `json:"activeDownloadBytes"`
}

type TerminalConnection struct {
	Key                string   `json:"key"`
	Family             string   `json:"family"`
	Application        string   `json:"application"`
	MatchedDomain      string   `json:"matchedDomain,omitempty"`
	ApplicationSource  string   `json:"applicationSource,omitempty"`
	Protocol           string   `json:"protocol"`
	Line               string   `json:"line"`
	SourceAddress      string   `json:"sourceAddress"`
	SourcePort         string   `json:"sourcePort"`
	DestinationAddress string   `json:"destinationAddress"`
	DestinationPort    string   `json:"destinationPort"`
	UploadBytes        int64    `json:"uploadBytes"`
	DownloadBytes      int64    `json:"downloadBytes"`
	UploadBps          float64  `json:"uploadBps"`
	DownloadBps        float64  `json:"downloadBps"`
	Status             string   `json:"status"`
	SeenReply          bool     `json:"seenReply"`
	Assured            bool     `json:"assured"`
	PublicAddress      string   `json:"publicAddress"`
	ConnectionMark     string   `json:"connectionMark"`
	RoutingMark        string   `json:"routingMark"`
	RouteTable         string   `json:"routeTable"`
	MatchedRule        string   `json:"matchedRule"`
	MatchedRuleID      string   `json:"matchedRuleId"`
	RouteDestination   string   `json:"routeDestination"`
	RouteID            string   `json:"routeId"`
	RouteIDs           []string `json:"routeIds"`
	RouteGateways      []string `json:"routeGateways"`
	RouteInterfaces    []string `json:"routeInterfaces"`
	EgressInterfaces   []string `json:"egressInterfaces"`
	RouteMatchBasis    string   `json:"routeMatchBasis"`
	RouteAttribution   string   `json:"routeAttribution"`
	Estimated          bool     `json:"estimated"`
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
	RatesUpdatedAt  time.Time                         `json:"ratesUpdatedAt"`
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
	Source        string  `json:"source,omitempty"`
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
	ID               string `json:"id"`
	Kind             string `json:"kind"`
	Family           string `json:"family"`
	Destination      string `json:"destination"`
	Gateway          string `json:"gateway"`
	Table            string `json:"table"`
	Action           string `json:"action"`
	Source           string `json:"source"`
	Distance         int64  `json:"distance"`
	Active           bool   `json:"active"`
	Disabled         bool   `json:"disabled"`
	PrefSrc          string `json:"prefSrc"`
	Scope            string `json:"scope"`
	TargetScope      string `json:"targetScope"`
	ImmediateGateway string `json:"immediateGateway"`
	Protocol         string `json:"protocol"`
	Comment          string `json:"comment"`
	CurrentMatches   int    `json:"currentMatches"`
}

type DHCPServerStat struct {
	Name        string `json:"name"`
	Interface   string `json:"interface"`
	AddressPool string `json:"addressPool"`
	LeaseTime   string `json:"leaseTime"`
	Disabled    bool   `json:"disabled"`
	Invalid     bool   `json:"invalid"`
}

type DHCPPoolStat struct {
	Name        string   `json:"name"`
	Ranges      string   `json:"ranges"`
	Total       int      `json:"total"`
	Used        int      `json:"used"`
	Free        int      `json:"free"`
	UsedPercent float64  `json:"usedPercent"`
	Servers     []string `json:"servers"`
}

type DHCPLeaseStat struct {
	ID           string `json:"id"`
	Address      string `json:"address"`
	MACAddress   string `json:"macAddress"`
	HostName     string `json:"hostName"`
	Comment      string `json:"comment"`
	Server       string `json:"server"`
	Status       string `json:"status"`
	ExpiresAfter int64  `json:"expiresAfter"`
	LastSeen     int64  `json:"lastSeen"`
	Dynamic      bool   `json:"dynamic"`
	Blocked      bool   `json:"blocked"`
	Disabled     bool   `json:"disabled"`
}

type DHCPStat struct {
	Servers []DHCPServerStat `json:"servers"`
	Pools   []DHCPPoolStat   `json:"pools"`
	Leases  []DHCPLeaseStat  `json:"leases"`
}

type DashboardSnapshot struct {
	Overview               Overview                        `json:"overview"`
	Interfaces             []InterfaceStatus               `json:"interfaces"`
	Terminals              []Terminal                      `json:"terminals"`
	TerminalScopeSummaries map[string]TerminalScopeSummary `json:"terminalScopeSummaries"`
	TerminalScope          TerminalScope                   `json:"terminalScope"`
	TrafficScope           TrafficScope                    `json:"trafficScope"`
	Capabilities           []CapabilityNote                `json:"capabilities"`
	Protocols              []ProtocolStat                  `json:"protocols"`
	Policies               []PolicyStat                    `json:"policies"`
	Routes                 []RouteStat                     `json:"routes"`
	DHCP                   DHCPStat                        `json:"dhcp"`
	Alerts                 []AlertEvent                    `json:"alerts"`
	Warnings               []string                        `json:"warnings"`
}

type TrafficScope struct {
	Mode             string                  `json:"mode"`
	Legacy           bool                    `json:"legacy"`
	Interfaces       []TrafficScopeInterface `json:"interfaces"`
	Warnings         []string                `json:"warnings"`
	OverridesApplied bool                    `json:"overridesApplied"`
}
type TrafficScopeInterface struct {
	Name      string   `json:"name"`
	Kind      string   `json:"kind"`
	Reasons   []string `json:"reasons"`
	Automatic bool     `json:"automatic"`
	Running   bool     `json:"running"`
	Disabled  bool     `json:"disabled"`
}

type TerminalScope struct {
	Mode             string                   `json:"mode"`
	Legacy           bool                     `json:"legacy"`
	Interfaces       []TerminalScopeInterface `json:"interfaces"`
	Prefixes         []TerminalScopePrefix    `json:"prefixes"`
	Warnings         []string                 `json:"warnings"`
	OverridesApplied bool                     `json:"overridesApplied"`
}
type TerminalScopeInterface struct {
	Name       string   `json:"name"`
	Role       string   `json:"role"`
	Confidence string   `json:"confidence"`
	Reasons    []string `json:"reasons"`
}
type TerminalScopePrefix struct {
	CIDR      string `json:"cidr"`
	Family    string `json:"family"`
	Interface string `json:"interface"`
	Source    string `json:"source"`
	Automatic bool   `json:"automatic"`
}

type AlertEvent struct {
	ID        string    `json:"id"`
	Level     string    `json:"level"`
	Source    string    `json:"source"`
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
}
