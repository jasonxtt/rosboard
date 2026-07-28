package routeros

type SystemResource struct {
	ArchitectureName string `json:"architecture-name"`
	BoardName        string `json:"board-name"`
	CPU              string `json:"cpu"`
	CPUCount         string `json:"cpu-count"`
	CPUFrequency     string `json:"cpu-frequency"`
	CPULoad          string `json:"cpu-load"`
	FreeMemory       string `json:"free-memory"`
	FreeHDD          string `json:"free-hdd-space"`
	Platform         string `json:"platform"`
	TotalMemory      string `json:"total-memory"`
	TotalHDD         string `json:"total-hdd-space"`
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
	RXPacket       string `json:"rx-packet"`
	TXPacket       string `json:"tx-packet"`
	RXDrop         string `json:"rx-drop"`
	TXDrop         string `json:"tx-drop"`
	RXError        string `json:"rx-error"`
	TXError        string `json:"tx-error"`
}

type EthernetInterface struct {
	Name       string `json:"name"`
	Running    string `json:"running"`
	MACAddress string `json:"mac-address"`
	RXBytes    string `json:"rx-bytes"`
	TXBytes    string `json:"tx-bytes"`
	Rate       string `json:"rate"`
	FullDuplex string `json:"full-duplex"`
}

type MonitorTrafficEntry struct {
	Name            string `json:"name"`
	RXBitsPerSecond string `json:"rx-bits-per-second"`
	TXBitsPerSecond string `json:"tx-bits-per-second"`
}

type IPAddress struct {
	ID              string `json:".id"`
	Address         string `json:"address"`
	Interface       string `json:"interface"`
	ActualInterface string `json:"actual-interface"`
	Dynamic         string `json:"dynamic"`
	Disabled        string `json:"disabled"`
	Invalid         string `json:"invalid"`
	Network         string `json:"network"`
}

type IPv6Address struct {
	ID              string `json:".id"`
	Address         string `json:"address"`
	Interface       string `json:"interface"`
	ActualInterface string `json:"actual-interface"`
	Dynamic         string `json:"dynamic"`
	Disabled        string `json:"disabled"`
	Invalid         string `json:"invalid"`
	Advertise       string `json:"advertise"`
}

type DHCPLease struct {
	Address    string `json:"address"`
	Server     string `json:"server"`
	Comment    string `json:"comment"`
	HostName   string `json:"host-name"`
	MACAddress string `json:"mac-address"`
	Status     string `json:"status"`
}

type InterfaceList struct {
	ID      string `json:".id"`
	Name    string `json:"name"`
	Include string `json:"include"`
	Exclude string `json:"exclude"`
}
type InterfaceListMember struct {
	ID        string `json:".id"`
	List      string `json:"list"`
	Interface string `json:"interface"`
	Disabled  string `json:"disabled"`
	Dynamic   string `json:"dynamic"`
}
type DHCPServer struct {
	ID        string `json:".id"`
	Name      string `json:"name"`
	Interface string `json:"interface"`
	Disabled  string `json:"disabled"`
	Invalid   string `json:"invalid"`
}
type DHCPClient struct {
	ID        string `json:".id"`
	Interface string `json:"interface"`
	Disabled  string `json:"disabled"`
	Status    string `json:"status"`
}
type IPv6ND struct {
	ID        string `json:".id"`
	Interface string `json:"interface"`
	Disabled  string `json:"disabled"`
	Invalid   string `json:"invalid"`
}
type IPv6NDPrefix struct {
	ID         string `json:".id"`
	Interface  string `json:"interface"`
	Prefix     string `json:"prefix"`
	Disabled   string `json:"disabled"`
	Invalid    string `json:"invalid"`
	OnLink     string `json:"on-link"`
	Autonomous string `json:"autonomous"`
	Dynamic    string `json:"dynamic"`
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
	SeenReply       string `json:"seen-reply"`
	Fasttrack       string `json:"fasttrack"`
	ConnectionMark  string `json:"connection-mark"`
	RoutingMark     string `json:"routing-mark"`
	Confirmed       string `json:"confirmed"`
	Dying           string `json:"dying"`
}

type SimpleQueue struct {
	Name     string `json:"name"`
	Target   string `json:"target"`
	Rate     string `json:"rate"`
	Bytes    string `json:"bytes"`
	Packets  string `json:"packets"`
	Disabled string `json:"disabled"`
}

type QueueTree struct {
	Name       string `json:"name"`
	Parent     string `json:"parent"`
	PacketMark string `json:"packet-mark"`
	Rate       string `json:"rate"`
	Bytes      string `json:"bytes"`
	Packets    string `json:"packets"`
	Disabled   string `json:"disabled"`
}

type FirewallRule struct {
	Chain             string `json:"chain"`
	Action            string `json:"action"`
	Comment           string `json:"comment"`
	ConnectionMark    string `json:"connection-mark"`
	NewConnectionMark string `json:"new-connection-mark"`
	NewRoutingMark    string `json:"new-routing-mark"`
	Bytes             string `json:"bytes"`
	Packets           string `json:"packets"`
	Disabled          string `json:"disabled"`
}

type RoutingRule struct {
	ID          string `json:".id"`
	Comment     string `json:"comment"`
	Action      string `json:"action"`
	SrcAddress  string `json:"src-address"`
	DstAddress  string `json:"dst-address"`
	Interface   string `json:"interface"`
	RoutingMark string `json:"routing-mark"`
	MinPrefix   string `json:"min-prefix"`
	Table       string `json:"table"`
	Disabled    string `json:"disabled"`
}

type IPRoute struct {
	ID           string `json:".id"`
	DstAddress   string `json:"dst-address"`
	Gateway      string `json:"gateway"`
	RoutingTable string `json:"routing-table"`
	Distance     string `json:"distance"`
	Active       string `json:"active"`
	Disabled     string `json:"disabled"`
}

type RoutingRoute struct {
	ID               string `json:".id"`
	AFI              string `json:"afi"`
	DstAddress       string `json:"dst-address"`
	Gateway          string `json:"gateway"`
	ImmediateGateway string `json:"immediate-gw"`
	RoutingTable     string `json:"routing-table"`
	Distance         string `json:"distance"`
	Active           string `json:"active"`
	Disabled         string `json:"disabled"`
}
