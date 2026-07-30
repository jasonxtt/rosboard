# DHCP 面板、路由页增强与状态监控菜单重组 — 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 新增 DHCP 独立展示页（server 概况 + 地址池利用率 + lease 列表）、增强路由页（补全字段 + 按路由表分组汇总）、重组"状态监控"侧边栏菜单（新增"网络服务"组）。

**Architecture:** 沿用现有快照管线：RouterOS REST → `routeros.Client` → `Monitor.refresh` 组装 `model.DashboardSnapshot`（内存）→ `/api/dashboard` & `/api/dhcp` → 前端轮询渲染。不新增轮询循环、不做持久化。

**Tech Stack:** Go 1.26（net/http、标准库测试）、React 19 + TypeScript + Vite（前端无测试框架，用 `tsc -b` + oxlint 把关）。

**Spec:** `docs/superpowers/specs/2026-07-30-dhcp-routes-monitoring-design.md`

## ⚠️ 提交门禁（覆盖默认"每任务一提交"）

AGENTS.md 规定：**任何改动可运行程序的变更，必须先部署到 10.0.0.6（部署前备份二进制/配置/SQLite），等待用户手动验收批准后才能 commit。** 因此本计划中各任务末尾**不执行 git commit**，只运行测试作为任务完成检查点。全部任务完成 → 部署 → 用户验收 → 一次性提交（Task 12）。

实现前必读：`.trellis/spec/backend/monitoring-contracts.md`、`.trellis/spec/backend/database-guidelines.md`（本次不动库，仅确认无冲突）、`.trellis/spec/frontend/` 对应文件。

---

### Task 1: RouterOS 类型扩充 + IPPools 客户端方法

**Files:**
- Modify: `internal/routeros/types.go`（DHCPLease :83-90、DHCPServer :105-111、DHCPClient :139-146、IPRoute :247-255、RoutingRoute :257-267；新增 IPPool）
- Modify: `internal/routeros/client.go`（新增 IPPools 方法，放在 IPRoutes :235 附近）
- Test: `internal/routeros/client_test.go`

- [ ] **Step 1: 写失败测试**

在 `client_test.go` 末尾追加（模仿现有 `TestInterfaceTopologyEndpoints` 模式）：

```go
func TestIPPoolsAndExpandedDHCPLeaseFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/rest/ip/pool":
			_, _ = writer.Write([]byte(`[{".id":"*1","name":"dhcp_pool0","ranges":"10.0.0.10-10.0.0.254","comment":"lan"}]`))
		case "/rest/ip/dhcp-server/lease":
			_, _ = writer.Write([]byte(`[{".id":"*2","address":"10.0.0.20","server":"dhcp1","host-name":"nas","mac-address":"AA:BB:CC:DD:EE:FF","status":"bound","expires-after":"1d2h3m4s","last-seen":"5m10s","dynamic":"true","blocked":"false","disabled":"false","active-address":"10.0.0.20","active-mac-address":"AA:BB:CC:DD:EE:FF"}]`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "admin", "secret")
	pools, err := client.IPPools(context.Background())
	if err != nil || len(pools) != 1 || pools[0].Name != "dhcp_pool0" || pools[0].Ranges != "10.0.0.10-10.0.0.254" {
		t.Fatalf("unexpected pools: %#v err=%v", pools, err)
	}
	leases, err := client.DHCPLeases(context.Background())
	if err != nil || len(leases) != 1 {
		t.Fatalf("unexpected leases: %#v err=%v", leases, err)
	}
	lease := leases[0]
	if lease.ID != "*2" || lease.ExpiresAfter != "1d2h3m4s" || lease.LastSeen != "5m10s" || lease.Dynamic != "true" || lease.ActiveAddress != "10.0.0.20" || lease.ActiveMACAddress != "AA:BB:CC:DD:EE:FF" || lease.Blocked != "false" || lease.Disabled != "false" {
		t.Fatalf("lease fields not mapped: %#v", lease)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `cd /Users/tom/github/rosboard && go test ./internal/routeros/ -run TestIPPoolsAndExpandedDHCPLeaseFields -v`
Expected: FAIL（`client.IPPools undefined`、`lease.ID undefined` 等编译错误）

- [ ] **Step 3: 实现类型扩充**

`types.go` 中将 `DHCPLease` 整体替换为：

```go
type DHCPLease struct {
	ID               string `json:".id"`
	Address          string `json:"address"`
	Server           string `json:"server"`
	Comment          string `json:"comment"`
	HostName         string `json:"host-name"`
	MACAddress       string `json:"mac-address"`
	Status           string `json:"status"`
	ExpiresAfter     string `json:"expires-after"`
	LastSeen         string `json:"last-seen"`
	Dynamic          string `json:"dynamic"`
	Blocked          string `json:"blocked"`
	Disabled         string `json:"disabled"`
	ActiveAddress    string `json:"active-address"`
	ActiveMACAddress string `json:"active-mac-address"`
}
```

`DHCPServer` 增加两个字段（保留现有字段）：

```go
	AddressPool string `json:"address-pool"`
	LeaseTime   string `json:"lease-time"`
```

`DHCPClient` 增加两个字段：

```go
	Address string `json:"address"`
	Gateway string `json:"gateway"`
```

`IPRoute` 增加四个字段：

```go
	PrefSrc string `json:"pref-src"`
	Static  string `json:"static"`
	Connect string `json:"connect"`
	Comment string `json:"comment"`
```

`RoutingRoute` 增加九个字段：

```go
	ImmediateInterface string `json:"immediate-interface"`
	PrefSrc            string `json:"pref-src"`
	Scope              string `json:"scope"`
	TargetScope        string `json:"target-scope"`
	Static             string `json:"static"`
	Connect            string `json:"connect"`
	Dynamic            string `json:"dynamic"`
	ECMP               string `json:"ecmp"`
	Comment            string `json:"comment"`
```

文件末尾新增：

```go
type IPPool struct {
	ID      string `json:".id"`
	Name    string `json:"name"`
	Ranges  string `json:"ranges"`
	Comment string `json:"comment"`
}
```

`client.go` 在 `IPRoutes` 方法后新增：

```go
func (c *Client) IPPools(ctx context.Context) ([]IPPool, error) {
	var payload []IPPool
	err := c.getJSON(ctx, "/rest/ip/pool", &payload)
	return payload, err
}
```

- [ ] **Step 4: 运行确认通过**

Run: `go test ./internal/routeros/ -v`
Expected: 全部 PASS

---

### Task 2: 投影模型 + Snapshot 深拷贝

**Files:**
- Modify: `internal/model/types.go`（RouteStat :244-257、DashboardSnapshot :259-272）
- Modify: `internal/service/monitor.go`（`Snapshot()` :593-638）
- Test: `internal/service/monitor_test.go`（`TestEmptySnapshotUsesJSONArrays` :16-31）

- [ ] **Step 1: 更新既有测试使其覆盖 DHCP 空切片**

`TestEmptySnapshotUsesJSONArrays` 末尾（`TerminalScopeSummaries` 检查之后）追加：

```go
	if snapshot.DHCP.Servers == nil || snapshot.DHCP.Pools == nil || snapshot.DHCP.Leases == nil {
		t.Fatal("dhcp collections must be empty slices, not nil")
	}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/service/ -run TestEmptySnapshotUsesJSONArrays -v`
Expected: FAIL（`snapshot.DHCP undefined` 编译错误）

- [ ] **Step 3: 实现模型**

`internal/model/types.go` 中 `RouteStat` 增加六个字段（在 `CurrentMatches` 前插入）：

```go
	PrefSrc          string `json:"prefSrc"`
	Scope            string `json:"scope"`
	TargetScope      string `json:"targetScope"`
	ImmediateGateway string `json:"immediateGateway"`
	Protocol         string `json:"protocol"`
	Comment          string `json:"comment"`
```

`RouteStat` 定义之后新增：

```go
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
```

`DashboardSnapshot` 在 `Routes` 字段后增加：

```go
	DHCP                   DHCPStat                        `json:"dhcp"`
```

`monitor.go` 的 `Snapshot()` 中，`snapshot.Routes = append(...)` 行（:603）之后增加：

```go
	snapshot.DHCP.Servers = append([]model.DHCPServerStat{}, snapshot.DHCP.Servers...)
	snapshot.DHCP.Pools = append([]model.DHCPPoolStat{}, snapshot.DHCP.Pools...)
	snapshot.DHCP.Leases = append([]model.DHCPLeaseStat{}, snapshot.DHCP.Leases...)
```

- [ ] **Step 4: 运行确认通过**

Run: `go test ./internal/service/ -run TestEmptySnapshotUsesJSONArrays -v`
Expected: PASS

---

### Task 3: buildDHCP 及辅助函数

**Files:**
- Create: `internal/service/dhcp.go`
- Create: `internal/service/dhcp_test.go`

- [ ] **Step 1: 写失败测试**

新建 `internal/service/dhcp_test.go`：

```go
package service

import (
	"testing"

	"rosboard/internal/routeros"
)

func TestParseRouterOSDurationSeconds(t *testing.T) {
	cases := map[string]int64{
		"1d2h3m4s": 93784,
		"5m10s":    310,
		"1w":       604800,
		"45s":      45,
		"":         0,
		"never":    0,
	}
	for input, expected := range cases {
		if got := parseRouterOSDurationSeconds(input); got != expected {
			t.Fatalf("parseRouterOSDurationSeconds(%q) = %d, want %d", input, got, expected)
		}
	}
}

func TestPoolRangeTotal(t *testing.T) {
	cases := map[string]int{
		"10.0.0.10-10.0.0.254":                    245,
		"10.0.0.10-10.0.0.19,10.0.1.0-10.0.1.9":   20,
		"192.168.1.100":                           1,
		"10.0.0.10-10.0.0.19,bogus,10.0.1.5":      11,
		"":                                        0,
		"10.0.0.20-10.0.0.10":                     0,
	}
	for input, expected := range cases {
		if got := poolRangeTotal(input); got != expected {
			t.Fatalf("poolRangeTotal(%q) = %d, want %d", input, got, expected)
		}
	}
}

func TestBuildDHCPComputesPoolUsageAndSortsLeases(t *testing.T) {
	servers := []routeros.DHCPServer{
		{Name: "dhcp1", Interface: "bridge-lan", AddressPool: "pool0", LeaseTime: "30m"},
		{Name: "dhcp2", Interface: "guest", AddressPool: "pool0", Disabled: "true"},
	}
	leases := []routeros.DHCPLease{
		{ID: "*2", Address: "10.0.0.30", Server: "dhcp1", Status: "bound", ExpiresAfter: "10m", Dynamic: "true", MACAddress: "aa:aa:aa:aa:aa:02"},
		{ID: "*1", Address: "10.0.0.9", ActiveAddress: "10.0.0.20", Server: "dhcp1", Status: "bound", HostName: "nas", MACAddress: "aa:aa:aa:aa:aa:01"},
		{ID: "*3", Address: "10.0.0.40", Server: "dhcp1", Status: "waiting"},
		{ID: "*4", Address: "10.0.0.50", Server: "unknown-server", Status: "bound"},
	}
	pools := []routeros.IPPool{
		{Name: "pool0", Ranges: "10.0.0.10-10.0.0.254"},
		{Name: "orphan", Ranges: "10.0.9.1-10.0.9.10"},
	}

	stat := buildDHCP(servers, leases, pools)

	if len(stat.Servers) != 2 || stat.Servers[0].Name != "dhcp1" || stat.Servers[0].AddressPool != "pool0" || !stat.Servers[1].Disabled {
		t.Fatalf("unexpected servers: %#v", stat.Servers)
	}
	if len(stat.Pools) != 2 {
		t.Fatalf("unexpected pools: %#v", stat.Pools)
	}
	pool0 := stat.Pools[0]
	// bound leases on dhcp1/dhcp2 (pool0): *1 and *2; waiting and unknown-server leases excluded.
	if pool0.Total != 245 || pool0.Used != 2 || pool0.Free != 243 || len(pool0.Servers) != 2 {
		t.Fatalf("unexpected pool0: %#v", pool0)
	}
	if pool0.UsedPercent < 0.8 || pool0.UsedPercent > 0.9 {
		t.Fatalf("unexpected pool0 percent: %f", pool0.UsedPercent)
	}
	orphan := stat.Pools[1]
	if orphan.Used != 0 || orphan.Total != 10 || len(orphan.Servers) != 0 {
		t.Fatalf("unexpected orphan pool: %#v", orphan)
	}
	// Sorted numerically by display address: 10.0.0.20(*1) < 10.0.0.30(*2) < 10.0.0.40(*3) < 10.0.0.50(*4).
	if stat.Leases[0].ID != "*1" || stat.Leases[1].ID != "*2" || stat.Leases[2].ID != "*3" || stat.Leases[3].ID != "*4" {
		t.Fatalf("unexpected lease order: %#v", stat.Leases)
	}
	if stat.Leases[0].Address != "10.0.0.20" {
		t.Fatalf("active-address should win: %#v", stat.Leases[0])
	}
	if stat.Leases[1].ExpiresAfter != 600 || !stat.Leases[1].Dynamic {
		t.Fatalf("lease fields not projected: %#v", stat.Leases[1])
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/service/ -run 'TestParseRouterOSDurationSeconds|TestPoolRangeTotal|TestBuildDHCP' -v`
Expected: FAIL（函数未定义）

- [ ] **Step 3: 实现 dhcp.go**

新建 `internal/service/dhcp.go`：

```go
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
		total += int(end - start + 1)
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
		if status == "bound" {
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
```

- [ ] **Step 4: 运行确认通过**

Run: `go test ./internal/service/ -run 'TestParseRouterOSDurationSeconds|TestPoolRangeTotal|TestBuildDHCP' -v`
Expected: PASS

---

### Task 4: buildRoutes 字段透传与 protocol 归一化

**Files:**
- Modify: `internal/service/monitor.go`（`buildRoutes` :1258-1280）
- Test: `internal/service/monitor_test.go`

- [ ] **Step 1: 写失败测试**

`monitor_test.go` 末尾追加：

```go
func TestBuildRoutesProjectsExtendedFieldsAndProtocol(t *testing.T) {
	routes := []routeros.RoutingRoute{
		{ID: "*A", DstAddress: "0.0.0.0/0", Gateway: "10.9.9.1", RoutingTable: "main", Distance: "1", Active: "true", Static: "true", PrefSrc: "10.0.0.1", Scope: "30", TargetScope: "10", ImmediateGateway: "10.9.9.1%ether1", Comment: "wan default"},
		{ID: "*B", DstAddress: "10.0.0.0/24", Gateway: "bridge-lan", RoutingTable: "main", Active: "true", Connect: "true"},
		{ID: "*C", DstAddress: "10.8.0.0/24", Gateway: "10.9.9.2", RoutingTable: "vpn", Active: "true", Dynamic: "true"},
	}
	result := buildRoutes(nil, routes, nil)
	if len(result) != 3 {
		t.Fatalf("unexpected result: %#v", result)
	}
	if result[0].Protocol != "static" || result[0].PrefSrc != "10.0.0.1" || result[0].Scope != "30" || result[0].TargetScope != "10" || result[0].ImmediateGateway != "10.9.9.1%ether1" || result[0].Comment != "wan default" {
		t.Fatalf("static route not projected: %#v", result[0])
	}
	if result[1].Protocol != "connected" {
		t.Fatalf("connected route not normalized: %#v", result[1])
	}
	if result[2].Protocol != "dynamic" {
		t.Fatalf("dynamic route not normalized: %#v", result[2])
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/service/ -run TestBuildRoutesProjectsExtendedFields -v`
Expected: FAIL（Protocol 为空字符串）

- [ ] **Step 3: 实现**

`buildRoutes` 中 rules 循环（:1271-1274）的 `model.RouteStat` 字面量增加一项 `Comment: item.Comment,`（`RoutingRule` 已有 `Comment` 字段）。

routes 循环（:1275-1278）整体替换为：

```go
	for index, item := range routes {
		id := stableRouteID(item, index)
		protocol := "dynamic"
		if parseBool(item.Static) {
			protocol = "static"
		} else if parseBool(item.Connect) {
			protocol = "connected"
		}
		result = append(result, model.RouteStat{
			ID:               id,
			Kind:             "route",
			Family:           routeFamily(item.AFI, item.DstAddress),
			Destination:      item.DstAddress,
			Gateway:          preferredName(item.ImmediateGateway, item.Gateway),
			Table:            item.RoutingTable,
			Distance:         parseInt(item.Distance),
			Active:           parseBool(item.Active),
			Disabled:         parseBool(item.Disabled),
			PrefSrc:          item.PrefSrc,
			Scope:            item.Scope,
			TargetScope:      item.TargetScope,
			ImmediateGateway: item.ImmediateGateway,
			Protocol:         protocol,
			Comment:          item.Comment,
			CurrentMatches:   matches[id],
		})
	}
```

同时更新 refresh 中 IPRoute 兼容回退的转换（monitor.go :947-950），把单行 struct 字面量替换为：

```go
			routingRoutes = make([]routeros.RoutingRoute, 0, len(ipRoutes))
			for _, route := range ipRoutes {
				routingRoutes = append(routingRoutes, routeros.RoutingRoute{
					ID: route.ID, AFI: "ip4", DstAddress: route.DstAddress, Gateway: route.Gateway,
					RoutingTable: route.RoutingTable, Distance: route.Distance, Active: route.Active,
					Disabled: route.Disabled, PrefSrc: route.PrefSrc, Static: route.Static,
					Connect: route.Connect, Comment: route.Comment,
				})
			}
```

- [ ] **Step 4: 运行确认通过**

Run: `go test ./internal/service/ -v`
Expected: 全部 PASS（含既有路由归因测试不回归）

---

### Task 5: refresh 接入 buildDHCP + capabilities 注记

**Files:**
- Modify: `internal/service/monitor.go`（refresh :848-1139、buildCapabilities :1856）

- [ ] **Step 1: refresh 中新增 pool 采集与 DHCP 组装**

在 `dhcpClients` 采集块（:965-968）之后插入：

```go
	ipPools, poolErr := m.client.IPPools(pollCtx)
	dhcpComplete := dhcpServerErr == nil
	if poolErr != nil {
		m.logger.Printf("load ip pools failed: %v", poolErr)
		addWarning("ip-pools", "DHCP 采集", "IP Pool 数据暂时不可用，保留上次有效 DHCP 数据。")
		dhcpComplete = false
	}
```

在 `routes := buildRoutes(...)` 块（:1071-1074）之后插入：

```go
	dhcp := buildDHCP(dhcpServers, leases, ipPools)
	if !dhcpComplete && (len(previous.DHCP.Servers) > 0 || len(previous.DHCP.Leases) > 0) {
		dhcp = previous.DHCP
	}
```

`snapshot := model.DashboardSnapshot{...}` 字面量中 `Routes: routes,` 之后加：

```go
		DHCP:                   dhcp,
```

- [ ] **Step 2: buildCapabilities 增加 DHCP/pool 注记**

签名改为 `func buildCapabilities(healthEnabled bool, dhcpPoolsAvailable bool) []model.CapabilityNote`，返回列表中追加一项（"Terminal monitoring" 条目之后）：

```go
		{
			Area:    "Network services",
			Item:    "DHCP servers, pools, and leases",
			Status:  dhcpCapabilityStatus(dhcpPoolsAvailable),
			Details: dhcpCapabilityDetails(dhcpPoolsAvailable),
		},
```

并在文件中新增两个辅助函数：

```go
func dhcpCapabilityStatus(poolsAvailable bool) string {
	if poolsAvailable {
		return "supported_now"
	}
	return "limited"
}

func dhcpCapabilityDetails(poolsAvailable bool) string {
	if poolsAvailable {
		return "DHCP server status, address-pool utilization, and lease details are read directly from RouterOS REST."
	}
	return "DHCP lease and server data is available, but `/rest/ip/pool` could not be read, so pool utilization is unavailable."
}
```

调用点（:1113）改为：

```go
			Capabilities:           buildCapabilities(strings.EqualFold(health.State, "enabled"), poolErr == nil),
```

- [ ] **Step 3: 编译与全量测试**

Run: `go build ./... && go test ./internal/service/`
Expected: 编译通过、全部 PASS

---

### Task 6: /api/dhcp 端点

**Files:**
- Modify: `internal/api/server.go`（switch :249-252）
- Test: `internal/api/server_test.go`

- [ ] **Step 1: 写失败测试**

`server_test.go` 末尾追加：

```go
func TestDHCPEndpointReturnsEmptyCollections(t *testing.T) {
	monitor := service.NewMonitor(config.Config{}, nil, nil, log.Default())
	server := NewServer(config.Config{}, monitor, nil)

	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/dhcp", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		Servers []map[string]any `json:"servers"`
		Pools   []map[string]any `json:"pools"`
		Leases  []map[string]any `json:"leases"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Servers == nil || payload.Pools == nil || payload.Leases == nil {
		t.Fatalf("dhcp collections must be arrays, got %s", response.Body.String())
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/api/ -run TestDHCPEndpoint -v`
Expected: FAIL（404 not found）

- [ ] **Step 3: 实现**

`server.go` switch 中 `case "/api/routes":` 之后新增：

```go
	case "/api/dhcp":
		writeJSON(writer, http.StatusOK, monitor.Snapshot().DHCP)
```

说明：`?device=<id>` 多设备作用域由已有的 `monitorFor(request)`（:175）统一处理，本端点无需额外代码。

- [ ] **Step 4: 运行确认通过**

Run: `go test ./internal/api/ -v && go test ./...`
Expected: 全部 PASS

---

### Task 7: 前端类型与标题

**Files:**
- Modify: `web/src/lib/types.ts`（RouteStat :113、DashboardResponse :145-158、ActiveView :284）
- Modify: `web/src/lib/format.ts`（viewTitle :3-15）

- [ ] **Step 1: types.ts**

`RouteStat`（:113）替换为：

```ts
export type RouteStat = { id: string; kind: string; family: string; destination: string; gateway: string; table: string; action: string; source: string; distance: number; active: boolean; disabled: boolean; prefSrc: string; scope: string; targetScope: string; immediateGateway: string; protocol: string; comment: string; currentMatches: number }
```

其后新增：

```ts
export type DHCPServerStat = { name: string; interface: string; addressPool: string; leaseTime: string; disabled: boolean; invalid: boolean }
export type DHCPPoolStat = { name: string; ranges: string; total: number; used: number; free: number; usedPercent: number; servers: string[] }
export type DHCPLeaseStat = { id: string; address: string; macAddress: string; hostName: string; comment: string; server: string; status: string; expiresAfter: number; lastSeen: number; dynamic: boolean; blocked: boolean; disabled: boolean }
export type DHCPStat = { servers: DHCPServerStat[]; pools: DHCPPoolStat[]; leases: DHCPLeaseStat[] }
```

`DashboardResponse` 在 `routes: RouteStat[]` 后加 `dhcp: DHCPStat`。

`ActiveView`（:284）替换为：

```ts
export type ActiveView = 'overview' | 'interfaces' | 'terminals' | 'load' | 'protocols' | 'policies' | 'dhcp' | 'routes' | 'settings'
```

- [ ] **Step 2: format.ts viewTitle**

titles 映射中 `policies` 行后加：

```ts
    dhcp: 'DHCP',
```

- [ ] **Step 3: 确认编译失败点已知**

Run: `cd web && npx tsc -b --pretty false 2>&1 | head -20`
Expected: 此时 App.tsx 报错 `dashboard.dhcp` 尚未使用属正常；若报 `Record<ActiveView, string>` 缺 key 以外的意外错误则修复。（App.tsx 的 dhcp 视图在 Task 8 补齐后整体通过。）

---

### Task 8: DHCPPage 组件 + 渲染接线 + CSS

**Files:**
- Modify: `web/src/App.tsx`（imports、landingViews :193、渲染块 :1051-1054、新组件放在 RoutesPage 之前）
- Modify: `web/src/index.css`（末尾追加）

- [ ] **Step 1: App.tsx 接线**

- import 类型：在现有 `from './lib/types'` 导入列表中加入 `DHCPStat`。
- `landingViews`（:193）替换为：

```ts
const landingViews: ActiveView[] = ['overview', 'interfaces', 'terminals', 'load', 'protocols', 'policies', 'dhcp', 'routes', 'settings']
```

- 渲染块 `{activeView === 'routes' ? ... : null}`（:1054）之前加：

```tsx
        {activeView === 'dhcp' ? <DHCPPage dhcp={dashboard.dhcp ?? { servers: [], pools: [], leases: [] }} /> : null}
```

- [ ] **Step 2: DHCPPage 组件**

在 `RoutesPage` 定义之前插入：

```tsx
function DHCPPage(props: { dhcp: DHCPStat }) {
  const [query, setQuery] = useState('')
  const trimmed = query.trim().toLowerCase()
  const leases = trimmed ? props.dhcp.leases.filter((item) => [item.address, item.macAddress, item.hostName, item.comment].some((value) => value.toLowerCase().includes(trimmed))) : props.dhcp.leases
  if (!props.dhcp.servers.length && !props.dhcp.leases.length) {
    return <section className="panel compact-panel"><div className="empty-row dhcp-empty">该设备未启用 DHCP Server 或接口无权限</div></section>
  }
  const leaseStatusText = (item: DHCPStat['leases'][number]) => item.blocked ? '已阻止' : item.disabled ? '已禁用' : item.status === 'bound' ? '已绑定' : item.status === 'waiting' ? '等待中' : item.status || '-'
  const leaseStatusClass = (item: DHCPStat['leases'][number]) => item.blocked || item.disabled ? 'lease-status blocked' : item.status === 'bound' ? 'lease-status bound' : 'lease-status waiting'
  return (
    <div className="page-grid">
      <section className="dhcp-server-grid">
        {props.dhcp.servers.length ? props.dhcp.servers.map((item) => (
          <div key={item.name} className="panel dhcp-server-card">
            <h4>{item.name}</h4>
            <span>接口：{item.interface || '-'}</span>
            <span>地址池：{item.addressPool || '-'}</span>
            <span>Lease 时长：{item.leaseTime || '-'}</span>
            <span className={item.disabled || item.invalid ? 'server-bad' : 'server-ok'}>{item.disabled ? '已禁用' : item.invalid ? '配置无效' : '运行中'}</span>
          </div>
        )) : <div className="panel dhcp-server-card"><span>没有 DHCP Server 配置</span></div>}
      </section>
      <section className="panel compact-panel">
        <div className="data-toolbar"><strong>地址池利用率</strong><span className="result-count">按 bound 租约计数</span><span className="toolbar-spacer" /><span>共 {props.dhcp.pools.length} 个</span></div>
        <div className="pool-list">
          {props.dhcp.pools.length ? props.dhcp.pools.map((pool) => {
            const level = pool.usedPercent > 95 ? 'critical' : pool.usedPercent > 85 ? 'warning' : 'normal'
            return (
              <div key={pool.name} className="pool-row">
                <div className="pool-meta"><strong>{pool.name}</strong><span>{pool.ranges}</span><span>{pool.servers.length ? `server：${pool.servers.join('、')}` : '未被 DHCP Server 引用'}</span></div>
                <div className="pool-bar"><i className={`pool-bar-fill ${level}`} style={{ width: `${Math.min(100, pool.usedPercent)}%` }} /></div>
                <span className="pool-count">{pool.used} / {pool.total || '-'}{pool.total ? `（${pool.usedPercent.toFixed(1)}%）` : ''}</span>
              </div>
            )
          }) : <div className="empty-row dhcp-empty">没有可解析的地址池（/ip/pool 无数据或不可用）</div>}
        </div>
      </section>
      <section className="panel compact-panel">
        <div className="data-toolbar">
          <strong>DHCP 租约</strong>
          <input className="search-input" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="地址 / MAC / 主机名 / 备注" />
          <span className="toolbar-spacer" />
          <span>显示 {leases.length} / {props.dhcp.leases.length} 条</span>
        </div>
        <div className="table-scroll"><table className="data-table"><thead><tr><th>地址</th><th>MAC</th><th>主机名</th><th>备注</th><th>Server</th><th>状态</th><th>剩余到期</th><th>类型</th></tr></thead><tbody>
          {leases.length ? leases.map((item, index) => (
            <tr key={item.id || `${item.address}-${index}`}>
              <td>{item.address || '-'}</td>
              <td>{item.macAddress || '-'}</td>
              <td>{item.hostName || '-'}</td>
              <td>{item.comment || '-'}</td>
              <td>{item.server || '-'}</td>
              <td><span className={leaseStatusClass(item)}>{leaseStatusText(item)}</span></td>
              <td>{item.expiresAfter > 0 ? formatSeconds(item.expiresAfter) : '-'}</td>
              <td>{item.dynamic ? '动态' : '静态'}</td>
            </tr>
          )) : <tr><td colSpan={8} className="empty-row">{props.dhcp.leases.length ? '没有匹配搜索条件的租约' : '当前没有租约'}</td></tr>}
        </tbody></table></div>
      </section>
    </div>
  )
}
```

（`formatSeconds` 已从 `./lib/format` 导入，确认 import 列表包含它，无则加入。）

- [ ] **Step 3: index.css 追加样式**

文件末尾（暗色主题覆盖区之前的合适位置）追加：

```css
.dhcp-server-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(220px, 1fr)); gap: 12px; }
.dhcp-server-card { display: grid; gap: 6px; padding: 14px; font-size: 12px; }
.dhcp-server-card h4 { margin: 0; font-size: 13px; }
.dhcp-server-card .server-ok { color: #166534; }
.dhcp-server-card .server-bad { color: #991b1b; }
.dhcp-empty { padding: 20px; text-align: center; }
.pool-list { display: grid; gap: 10px; padding: 14px; }
.pool-row { display: grid; grid-template-columns: minmax(180px, 1fr) 2fr max-content; align-items: center; gap: 12px; font-size: 12px; }
.pool-meta { display: grid; gap: 2px; min-width: 0; }
.pool-meta span { color: #64748b; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.pool-bar { height: 10px; background: #e2e8f0; border-radius: 999px; overflow: hidden; }
.pool-bar-fill { display: block; height: 100%; background: #16a34a; border-radius: 999px; }
.pool-bar-fill.warning { background: #d97706; }
.pool-bar-fill.critical { background: #dc2626; }
.pool-count { white-space: nowrap; }
.lease-status { display: inline-flex; align-items: center; padding: 2px 8px; border-radius: 999px; font-size: 11px; color: #334155; background: #e2e8f0; }
.lease-status.bound { color: #166534; background: #dcfce7; }
.lease-status.blocked { color: #991b1b; background: #fee2e2; }
:root[data-theme="dark"] .pool-bar { background: #1e293b; }
:root[data-theme="dark"] .pool-meta span { color: #94a3b8; }
:root[data-theme="dark"] .lease-status { color: #cbd5e1; background: #334155; }
:root[data-theme="dark"] .lease-status.bound { color: #86efac; background: #14532d; }
:root[data-theme="dark"] .lease-status.blocked { color: #fca5a5; background: #7f1d1d; }
```

- [ ] **Step 4: 编译检查**

Run: `cd web && npx tsc -b --pretty false && npm run lint`
Expected: 无错误

---

### Task 9: 菜单重组（网络服务组 + 系统运行改名）

**Files:**
- Modify: `web/src/App.tsx`（expandedMonitorGroup :480、statusActive :831、useEffect :583-589、菜单 :929-938、面包屑 :1007 附近无需动）

- [ ] **Step 1: 状态与联动**

- `:480` 替换为：

```ts
  const [expandedMonitorGroup, setExpandedMonitorGroup] = useState<'terminals' | 'traffic' | 'services' | 'runtime' | null>(null)
```

- `:831` `statusActive` 追加 `|| activeView === 'dhcp'`。
- useEffect（:583-589）中的两行分组联动替换为：

```ts
    if (activeView === 'protocols' || activeView === 'policies') setExpandedMonitorGroup('traffic')
    if (activeView === 'dhcp' || activeView === 'routes') setExpandedMonitorGroup('services')
    if (activeView === 'load') setExpandedMonitorGroup('runtime')
```

- [ ] **Step 2: 菜单结构**

把现有 runtime 组（:934-938）整体替换为"网络服务"+"系统运行"两组：

```tsx
              <button type="button" className="submenu-section submenu-toggle" aria-expanded={expandedMonitorGroup === 'services'} onClick={() => setExpandedMonitorGroup((value) => value === 'services' ? null : 'services')}><NavLabel icon="network" label="网络服务" /></button>
              {expandedMonitorGroup === 'services' ? <>
                <button type="button" className={activeView === 'dhcp' ? 'submenu-item nested active' : 'submenu-item nested'} onClick={() => { setActiveView('dhcp'); setSelectedTerminalID(null); setSidebarOpen(false) }}><NavLabel icon="router" label="DHCP" /></button>
                <button type="button" className={activeView === 'routes' ? 'submenu-item nested active' : 'submenu-item nested'} onClick={() => { setActiveView('routes'); setSelectedTerminalID(null); setSidebarOpen(false) }}><NavLabel icon="route" label="路由 / 分流" /></button>
              </> : null}
              <button type="button" className="submenu-section submenu-toggle" aria-expanded={expandedMonitorGroup === 'runtime'} onClick={() => setExpandedMonitorGroup((value) => value === 'runtime' ? null : 'runtime')}><NavLabel icon="runtime" label="系统运行" /></button>
              {expandedMonitorGroup === 'runtime' ? <>
                <button type="button" className={activeView === 'load' ? 'submenu-item nested active' : 'submenu-item nested'} onClick={() => { setActiveView('load'); setSelectedTerminalID(null); setSidebarOpen(false) }}><NavLabel icon="runtime" label="负载历史" /></button>
              </> : null}
```

（图标复用现有 `IconName`：组头 `network`、DHCP 项 `router`，不新增图标。）

- [ ] **Step 3: 编译检查**

Run: `cd web && npx tsc -b --pretty false && npm run lint`
Expected: 无错误

---

### Task 10: RoutesPage 按表分组增强

**Files:**
- Modify: `web/src/App.tsx`（RoutesPage :2134-2139 整体重写）

- [ ] **Step 1: 重写 RoutesPage**

整体替换为：

```tsx
function RoutesPage(props: { routes: RouteStat[] }) {
  const [hideDisabled, setHideDisabled] = useState(true)
  const disabledCount = props.routes.filter((item) => item.disabled).length
  const visibleRoutes = hideDisabled ? props.routes.filter((item) => !item.disabled) : props.routes
  const rules = visibleRoutes.filter((item) => item.kind === 'rule')
  const routeItems = visibleRoutes.filter((item) => item.kind !== 'rule')
  const tables = Array.from(new Set(routeItems.map((item) => item.table || 'main'))).sort((left, right) => left === 'main' ? -1 : right === 'main' ? 1 : left.localeCompare(right))
  const isDefaultRoute = (item: RouteStat) => item.destination === '0.0.0.0/0' || item.destination === '::/0'
  const protocolText = (value: string) => value === 'static' ? '静态' : value === 'connected' ? '直连' : value === 'dynamic' ? '动态' : value || '-'
  const routeRow = (item: RouteStat, index: number) => (
    <tr key={item.id || `${item.kind}-${item.destination}-${item.table}-${index}`} title={item.scope || item.targetScope ? `scope: ${item.scope || '-'} / target-scope: ${item.targetScope || '-'}` : undefined}>
      <td>{item.family === 'ipv6' ? 'IPv6' : 'IPv4'}</td>
      <td>{item.destination || '-'}</td>
      <td>{item.gateway || '-'}</td>
      <td>{item.prefSrc || '-'}</td>
      <td>{protocolText(item.protocol)}</td>
      <td>{item.distance || '-'}</td>
      <td>{item.currentMatches}</td>
      <td>{item.comment || '-'}</td>
      <td>{item.disabled ? '已禁用' : item.active ? '活动' : '非活动'}</td>
    </tr>
  )
  return (
    <div className="page-grid">
      <div className="data-toolbar panel">
        <strong>现有路由与分流状态</strong><span className="result-count">匹配数为当前 conntrack 快照推算</span><span className="toolbar-spacer" />
        <label className="toolbar-toggle"><input type="checkbox" checked={hideDisabled} onChange={(event) => setHideDisabled(event.target.checked)} /><span>隐藏已禁用</span></label>
        <span>显示 {visibleRoutes.length} / {props.routes.length} 条{disabledCount ? `，已禁用 ${disabledCount}` : ''}</span>
      </div>
      {rules.length ? (
        <section className="panel compact-panel">
          <div className="data-toolbar"><strong>Routing Rules（分流入口）</strong><span className="toolbar-spacer" /><span>{rules.length} 条 · 命中连接 {rules.reduce((sum, item) => sum + item.currentMatches, 0)}</span></div>
          <div className="table-scroll"><table className="data-table"><thead><tr><th>IP</th><th>源地址 / 接口</th><th>目标网段</th><th>路由表</th><th>动作</th><th>命中连接</th><th>备注</th><th>状态</th></tr></thead><tbody>
            {rules.map((item, index) => (
              <tr key={item.id || `rule-${index}`}>
                <td>{item.family === 'ipv6' ? 'IPv6' : 'IPv4'}</td>
                <td>{item.source || '-'}</td>
                <td>{item.destination || '-'}</td>
                <td>{item.table || 'main'}</td>
                <td>{item.action || '-'}</td>
                <td>{item.currentMatches}</td>
                <td>{item.comment || '-'}</td>
                <td>{item.disabled ? '已禁用' : '生效中'}</td>
              </tr>
            ))}
          </tbody></table></div>
        </section>
      ) : null}
      {tables.map((table) => {
        const group = routeItems.filter((item) => (item.table || 'main') === table)
        const defaultRoute = group.find((item) => isDefaultRoute(item) && !item.disabled)
        const matchTotal = group.reduce((sum, item) => sum + item.currentMatches, 0)
        return (
          <section className="panel compact-panel" key={table}>
            <div className="data-toolbar"><strong>路由表 {table}</strong><span className="result-count">{group.length} 条 · 命中连接 {matchTotal} · {defaultRoute ? `默认路由 ${defaultRoute.active ? '活动' : '非活动'}（${defaultRoute.gateway || '-'}）` : '无默认路由'}</span></div>
            <div className="table-scroll"><table className="data-table"><thead><tr><th>IP</th><th>目标网段</th><th>网关</th><th>pref-src</th><th>来源</th><th>距离</th><th>命中连接</th><th>备注</th><th>状态</th></tr></thead><tbody>{group.map(routeRow)}</tbody></table></div>
          </section>
        )
      })}
      {!rules.length && !routeItems.length ? <section className="panel compact-panel"><div className="empty-row dhcp-empty">{props.routes.length ? '已隐藏全部禁用路由与分流规则' : '当前没有可读取的路由或分流状态'}</div></section> : null}
    </div>
  )
}
```

（routing rule 的 comment 字段：`RoutingRule` 已有 `Comment`，但 `buildRoutes` 的 rules 分支此前未透传——Task 4 的 rules 循环需同步在 `model.RouteStat` 字面量中加 `Comment: item.Comment,`。若 Task 4 已完成而漏了此项，在本任务补上并重跑 `go test ./internal/service/`。）

- [ ] **Step 2: 编译检查**

Run: `cd web && npx tsc -b --pretty false && npm run lint`
Expected: 无错误

---

### Task 11: 全量验证与本地运行

- [ ] **Step 1: 后端全量测试**

Run: `cd /Users/tom/github/rosboard && go test ./...`
Expected: 全部 PASS

- [ ] **Step 2: 前端构建**

Run: `cd web && npm run build`
Expected: 构建成功（产物嵌入路径由 `internal/ui/assets.go` 处理，确认其引用的构建产物目录已更新）

- [ ] **Step 3: 接口冒烟（无浏览器）**

Run: 启动后端（参照 `scripts/run-local.sh`，后台运行），然后用 curl 验证：

```bash
curl -s http://127.0.0.1:<port>/api/dhcp | head -c 400
curl -s http://127.0.0.1:<port>/api/dashboard | python3 -c "import json,sys; d=json.load(sys.stdin); print('dhcp keys:', sorted(d['dhcp'].keys())); print('route sample keys:', sorted(d['routes'][0].keys()) if d['routes'] else 'no routes')"
```

Expected: `/api/dhcp` 返回含 `servers`/`pools`/`leases` 的 JSON；dashboard 的 `dhcp` 字段存在，routes 条目含 `prefSrc`/`protocol`/`comment` 等新键。验证后停止后端进程。浏览器视觉验收留给 Task 12 的用户验收环节，本任务不做。

---

### Task 12: 部署验收与提交（门禁）

- [ ] **Step 1: 备份并部署到 10.0.0.6**

按 AGENTS.md：对 10.0.0.6 上现有二进制/配置/SQLite 做时间戳备份，再部署新二进制。

- [ ] **Step 2: 等待用户手动验收**

请用户在 10.0.0.6 面板上确认 DHCP 页、路由页与新菜单。**未获批准不得进入 Step 3。**

- [ ] **Step 3: 提交**

```bash
git add internal/ web/src/ 
git commit -m "feat: add DHCP panel, enrich routes page, and reorganize status-monitor menu"
```

---

## 自查记录

- Spec 覆盖：§2→Task 1；§3→Task 2；§4→Task 3/4/5；§5→Task 6；§6.1→Task 7；§6.2→Task 8；§6.3→Task 10；§6.4→Task 9；§7→各任务测试步骤；§8→Task 12。
- 类型一致性：`buildDHCP`/`DHCPStat`/`dhcp` JSON 字段在 Go 与 TS 两侧一一对应；`parseRouterOSDurationSeconds`、`poolRangeTotal`、`lessIPAddress` 命名在 Task 3 定义、无他处引用偏差。
- 已知联动点：Task 4 rules 循环需透传 `Comment`（已在 Task 10 备注兜底）。
