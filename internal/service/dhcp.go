package service

import (
	"bytes"
	"encoding/binary"
	"net"
	"sort"
	"strconv"
	"strings"

	"rosboard/internal/model"
	"rosboard/internal/routeros"
)

// parseRouterOSDurationSeconds converts RouterOS duration strings like "1d2h3m4s" to seconds.
func parseRouterOSDurationSeconds(raw string) int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	total := int64(0)
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
			total += value * 7 * 24 * 3600
		case 'd':
			total += value * 24 * 3600
		case 'h':
			total += value * 3600
		case 'm':
			total += value * 60
		case 's':
			total += value
		}
		current = ""
	}
	return total
}

// poolRangeTotal counts IPv4 addresses covered by a RouterOS pool ranges string
// such as "10.0.0.10-10.0.0.254" or "10.0.0.10-10.0.0.19,10.0.1.5".
// Unparseable segments are skipped so one bad segment does not zero the pool.
func poolRangeTotal(ranges string) int {
	total := 0
	for _, segment := range strings.Split(ranges, ",") {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			continue
		}
		startRaw, endRaw, found := strings.Cut(segment, "-")
		if !found {
			endRaw = startRaw
		}
		start, startOK := ipv4ToUint32(strings.TrimSpace(startRaw))
		end, endOK := ipv4ToUint32(strings.TrimSpace(endRaw))
		if !startOK || !endOK || end < start {
			continue
		}
		total += int(int64(end) - int64(start) + 1)
	}
	return total
}

func ipv4ToUint32(value string) (uint32, bool) {
	ip := net.ParseIP(value)
	if ip == nil {
		return 0, false
	}
	ipv4 := ip.To4()
	if ipv4 == nil {
		return 0, false
	}
	return binary.BigEndian.Uint32(ipv4), true
}

func buildDHCP(servers []routeros.DHCPServer, leases []routeros.DHCPLease, pools []routeros.IPPool) model.DHCPStat {
	serverStats := make([]model.DHCPServerStat, 0, len(servers))
	poolByServer := make(map[string]string, len(servers))
	serversByPool := make(map[string][]string, len(pools))
	for _, server := range servers {
		serverStats = append(serverStats, model.DHCPServerStat{
			Name:        server.Name,
			Interface:   server.Interface,
			AddressPool: server.AddressPool,
			LeaseTime:   server.LeaseTime,
			Disabled:    parseBool(server.Disabled),
			Invalid:     parseBool(server.Invalid),
		})
		pool := strings.TrimSpace(server.AddressPool)
		if pool != "" {
			poolByServer[server.Name] = pool
			serversByPool[pool] = append(serversByPool[pool], server.Name)
		}
	}

	boundByPool := make(map[string]int)
	leaseStats := make([]model.DHCPLeaseStat, 0, len(leases))
	for _, lease := range leases {
		address := strings.TrimSpace(lease.ActiveAddress)
		if address == "" {
			address = strings.TrimSpace(lease.Address)
		}
		status := strings.TrimSpace(lease.Status)
		if strings.EqualFold(status, "bound") {
			if pool, ok := poolByServer[lease.Server]; ok {
				boundByPool[pool]++
			}
		}
		leaseStats = append(leaseStats, model.DHCPLeaseStat{
			ID:           lease.ID,
			Address:      address,
			MACAddress:   preferredName(lease.ActiveMACAddress, lease.MACAddress),
			HostName:     lease.HostName,
			Comment:      lease.Comment,
			Server:       lease.Server,
			Status:       status,
			ExpiresAfter: parseRouterOSDurationSeconds(lease.ExpiresAfter),
			LastSeen:     parseRouterOSDurationSeconds(lease.LastSeen),
			Dynamic:      parseBool(lease.Dynamic),
			Blocked:      parseBool(lease.Blocked),
			Disabled:     parseBool(lease.Disabled),
		})
	}
	sort.Slice(leaseStats, func(left, right int) bool {
		return lessIPAddress(leaseStats[left].Address, leaseStats[right].Address)
	})

	poolStats := make([]model.DHCPPoolStat, 0, len(pools))
	for _, pool := range pools {
		total := poolRangeTotal(pool.Ranges)
		used := boundByPool[pool.Name]
		free := total - used
		if free < 0 {
			free = 0
		}
		usedPercent := 0.0
		if total > 0 {
			usedPercent = float64(used) / float64(total) * 100
		}
		poolStats = append(poolStats, model.DHCPPoolStat{
			Name:        pool.Name,
			Ranges:      pool.Ranges,
			Total:       total,
			Used:        used,
			Free:        free,
			UsedPercent: usedPercent,
			Servers:     append([]string{}, serversByPool[pool.Name]...),
		})
	}

	return model.DHCPStat{Servers: serverStats, Pools: poolStats, Leases: leaseStats}
}

func lessIPAddress(left, right string) bool {
	leftIP := net.ParseIP(left)
	rightIP := net.ParseIP(right)
	if leftIP != nil && rightIP != nil {
		return bytes.Compare(leftIP.To16(), rightIP.To16()) < 0
	}
	if leftIP != nil {
		return true
	}
	if rightIP != nil {
		return false
	}
	return left < right
}
