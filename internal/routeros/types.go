package routeros

type SystemResource struct {
	ArchitectureName string `json:"architecture-name"`
	BoardName        string `json:"board-name"`
	CPU              string `json:"cpu"`
	CPUCount         string `json:"cpu-count"`
	CPUFrequency     string `json:"cpu-frequency"`
	CPULoad          string `json:"cpu-load"`
	FreeMemory       string `json:"free-memory"`
	Platform         string `json:"platform"`
	TotalMemory      string `json:"total-memory"`
	Uptime           string `json:"uptime"`
	Version          string `json:"version"`
}

type SystemHealth struct {
	State            string `json:"state"`
	StateAfterReboot string `json:"state-after-reboot"`
}

type Interface struct {
	ID             string `json:".id"`
	Name           string `json:"name"`
	Type           string `json:"type"`
	Running        string `json:"running"`
	Disabled       string `json:"disabled"`
	MACAddress     string `json:"mac-address"`
	Status         string `json:"status"`
	LastLinkUpTime string `json:"last-link-up-time"`
	LinkDowns      string `json:"link-downs"`
	ActualMTU      string `json:"actual-mtu"`
	RXByte         string `json:"rx-byte"`
	TXByte         string `json:"tx-byte"`
}

type EthernetInterface struct {
	Name       string `json:"name"`
	Running    string `json:"running"`
	MACAddress string `json:"mac-address"`
	RXBytes    string `json:"rx-bytes"`
	TXBytes    string `json:"tx-bytes"`
}

type MonitorTrafficEntry struct {
	Name            string `json:"name"`
	RXBitsPerSecond string `json:"rx-bits-per-second"`
	TXBitsPerSecond string `json:"tx-bits-per-second"`
}

type IPAddress struct {
	Address   string `json:"address"`
	Interface string `json:"interface"`
	Dynamic   string `json:"dynamic"`
	Network   string `json:"network"`
}

type DHCPLease struct {
	Address    string `json:"address"`
	Comment    string `json:"comment"`
	HostName   string `json:"host-name"`
	MACAddress string `json:"mac-address"`
	Status     string `json:"status"`
}

type ARPEntry struct {
	Address    string `json:"address"`
	Interface  string `json:"interface"`
	MACAddress string `json:"mac-address"`
	Status     string `json:"status"`
	Complete   string `json:"complete"`
}

type IPv6Neighbor struct {
	Address    string `json:"address"`
	Interface  string `json:"interface"`
	MACAddress string `json:"mac-address"`
	Status     string `json:"status"`
}

type FirewallConnection struct {
	Protocol        string `json:"protocol"`
	SrcAddress      string `json:"src-address"`
	SrcPort         string `json:"src-port"`
	DstAddress      string `json:"dst-address"`
	DstPort         string `json:"dst-port"`
	ReplySrcAddress string `json:"reply-src-address"`
	ReplySrcPort    string `json:"reply-src-port"`
	ReplyDstAddress string `json:"reply-dst-address"`
	ReplyDstPort    string `json:"reply-dst-port"`
	OrigBytes       string `json:"orig-bytes"`
	ReplBytes       string `json:"repl-bytes"`
	OrigRate        string `json:"orig-rate"`
	ReplRate        string `json:"repl-rate"`
	Assured         string `json:"assured"`
	Fasttrack       string `json:"fasttrack"`
}
