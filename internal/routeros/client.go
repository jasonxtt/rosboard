package routeros

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	baseURL    string
	username   string
	password   string
	httpClient *http.Client
}

type HTTPError struct {
	Path       string
	StatusCode int
	Status     string
	Detail     string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("request %s: unexpected status %s", e.Path, e.Status)
}

func routerOSErrorDetail(reader io.Reader) string {
	body, err := io.ReadAll(io.LimitReader(reader, 8<<10))
	if err != nil {
		return ""
	}
	var payload struct {
		Message string `json:"message"`
		Detail  string `json:"detail"`
	}
	if json.Unmarshal(body, &payload) == nil {
		parts := make([]string, 0, 2)
		if value := strings.TrimSpace(payload.Message); value != "" {
			parts = append(parts, value)
		}
		if value := strings.TrimSpace(payload.Detail); value != "" {
			parts = append(parts, value)
		}
		if len(parts) > 0 {
			return strings.Join(parts, ": ")
		}
	}
	return strings.TrimSpace(string(body))
}

func NewClient(baseURL, username, password string) *Client {
	return &Client{
		baseURL:  strings.TrimRight(baseURL, "/"),
		username: username,
		password: password,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 5 {
					return errors.New("too many redirects")
				}
				if len(via) > 0 && !strings.EqualFold(req.URL.Host, via[0].URL.Host) {
					return errors.New("cross-host redirect denied")
				}
				return nil
			},
		},
	}
}

func (c *Client) SystemResource(ctx context.Context) (SystemResource, error) {
	var resource SystemResource
	err := c.getJSON(ctx, "/rest/system/resource", &resource)
	return resource, err
}

func (c *Client) SystemHealth(ctx context.Context) (SystemHealth, error) {
	var payload json.RawMessage
	if err := c.getJSON(ctx, "/rest/system/health", &payload); err != nil {
		return SystemHealth{}, err
	}
	var entries []SystemHealth
	if err := json.Unmarshal(payload, &entries); err == nil {
		if len(entries) == 0 {
			return SystemHealth{}, errors.New("system health: empty response")
		}
		return entries[0], nil
	}
	var health SystemHealth
	if err := json.Unmarshal(payload, &health); err != nil {
		return SystemHealth{}, fmt.Errorf("decode system health response: %w", err)
	}
	return health, nil
}

func (c *Client) Interfaces(ctx context.Context) ([]Interface, error) {
	var payload []Interface
	err := c.getJSON(ctx, "/rest/interface", &payload)
	return payload, err
}

func (c *Client) EthernetInterfaces(ctx context.Context) ([]EthernetInterface, error) {
	var payload []EthernetInterface
	err := c.getJSON(ctx, "/rest/interface/ethernet", &payload)
	return payload, err
}

func (c *Client) MonitorTraffic(ctx context.Context, interfaceName string) (MonitorTrafficEntry, error) {
	body, err := json.Marshal(map[string]string{
		"interface": interfaceName,
		"once":      "",
	})
	if err != nil {
		return MonitorTrafficEntry{}, fmt.Errorf("marshal monitor request: %w", err)
	}
	var payload []MonitorTrafficEntry
	if err := c.postJSON(ctx, "/rest/interface/monitor-traffic", body, &payload); err != nil {
		return MonitorTrafficEntry{}, err
	}
	if len(payload) == 0 {
		return MonitorTrafficEntry{}, fmt.Errorf("monitor traffic: empty response for %s", interfaceName)
	}
	return payload[0], nil
}

func (c *Client) IPAddresses(ctx context.Context) ([]IPAddress, error) {
	var payload []IPAddress
	err := c.getJSON(ctx, "/rest/ip/address", &payload)
	return payload, err
}

func (c *Client) IPv6Addresses(ctx context.Context) ([]IPv6Address, error) {
	var payload []IPv6Address
	err := c.getJSON(ctx, "/rest/ipv6/address", &payload)
	return payload, err
}

func (c *Client) InterfaceLists(ctx context.Context) ([]InterfaceList, error) {
	var payload []InterfaceList
	return payload, c.getJSON(ctx, "/rest/interface/list", &payload)
}
func (c *Client) InterfaceListMembers(ctx context.Context) ([]InterfaceListMember, error) {
	var payload []InterfaceListMember
	return payload, c.getJSON(ctx, "/rest/interface/list/member", &payload)
}
func (c *Client) DHCPServers(ctx context.Context) ([]DHCPServer, error) {
	var payload []DHCPServer
	return payload, c.getJSON(ctx, "/rest/ip/dhcp-server", &payload)
}
func (c *Client) PPPoEClients(ctx context.Context) ([]PPPoEClient, error) {
	var payload []PPPoEClient
	return payload, c.getJSON(ctx, "/rest/interface/pppoe-client", &payload)
}
func (c *Client) VLANInterfaces(ctx context.Context) ([]VLANInterface, error) {
	var payload []VLANInterface
	return payload, c.getJSON(ctx, "/rest/interface/vlan", &payload)
}
func (c *Client) BridgePorts(ctx context.Context) ([]BridgePort, error) {
	var payload []BridgePort
	return payload, c.getJSON(ctx, "/rest/interface/bridge/port", &payload)
}
func (c *Client) DHCPClients(ctx context.Context) ([]DHCPClient, error) {
	var payload []DHCPClient
	return payload, c.getJSON(ctx, "/rest/ip/dhcp-client", &payload)
}
func (c *Client) IPv6NDs(ctx context.Context) ([]IPv6ND, error) {
	var payload []IPv6ND
	return payload, c.getJSON(ctx, "/rest/ipv6/nd", &payload)
}
func (c *Client) IPv6NDPrefixes(ctx context.Context) ([]IPv6NDPrefix, error) {
	var payload []IPv6NDPrefix
	return payload, c.getJSON(ctx, "/rest/ipv6/nd/prefix", &payload)
}

func (c *Client) DHCPLeases(ctx context.Context) ([]DHCPLease, error) {
	var payload []DHCPLease
	err := c.getJSON(ctx, "/rest/ip/dhcp-server/lease", &payload)
	return payload, err
}

func (c *Client) ARPEntries(ctx context.Context) ([]ARPEntry, error) {
	var payload []ARPEntry
	err := c.getJSON(ctx, "/rest/ip/arp", &payload)
	return payload, err
}

func (c *Client) IPv6Neighbors(ctx context.Context) ([]IPv6Neighbor, error) {
	var payload []IPv6Neighbor
	err := c.getJSON(ctx, "/rest/ipv6/neighbor", &payload)
	return payload, err
}

func (c *Client) FirewallConnectionsV4(ctx context.Context) ([]FirewallConnection, error) {
	var payload []FirewallConnection
	err := c.getJSON(ctx, "/rest/ip/firewall/connection", &payload)
	return payload, err
}

func (c *Client) FirewallConnectionsV6(ctx context.Context) ([]FirewallConnection, error) {
	var payload []FirewallConnection
	err := c.getJSON(ctx, "/rest/ipv6/firewall/connection", &payload)
	return payload, err
}

func (c *Client) SimpleQueues(ctx context.Context) ([]SimpleQueue, error) {
	var payload []SimpleQueue
	err := c.getJSON(ctx, "/rest/queue/simple", &payload)
	return payload, err
}

func (c *Client) QueueTrees(ctx context.Context) ([]QueueTree, error) {
	var payload []QueueTree
	err := c.getJSON(ctx, "/rest/queue/tree", &payload)
	return payload, err
}

func (c *Client) MangleRules(ctx context.Context) ([]FirewallRule, error) {
	var payload []FirewallRule
	err := c.getJSON(ctx, "/rest/ip/firewall/mangle", &payload)
	return payload, err
}

func (c *Client) RoutingRules(ctx context.Context) ([]RoutingRule, error) {
	var payload []RoutingRule
	err := c.getJSON(ctx, "/rest/routing/rule", &payload)
	return payload, err
}

func (c *Client) IPRoutes(ctx context.Context) ([]IPRoute, error) {
	var payload []IPRoute
	err := c.getJSON(ctx, "/rest/ip/route", &payload)
	return payload, err
}

func (c *Client) RoutingRoutes(ctx context.Context) ([]RoutingRoute, error) {
	var payload []RoutingRoute
	err := c.getJSON(ctx, "/rest/routing/route", &payload)
	return payload, err
}

func (c *Client) getJSON(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("build request %s: %w", path, err)
	}
	return c.do(req, out)
}

func (c *Client) postJSON(ctx context.Context, path string, body []byte, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request %s: %w", path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req, out)
}

func (c *Client) do(req *http.Request, out any) error {
	req.SetBasicAuth(c.username, c.password)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request %s: %w", req.URL.Path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &HTTPError{
			Path:       req.URL.Path,
			StatusCode: resp.StatusCode,
			Status:     resp.Status,
			Detail:     routerOSErrorDetail(resp.Body),
		}
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode %s: %w", req.URL.Path, err)
	}
	return nil
}
