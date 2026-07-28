package routeros

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strings"
)

type VerificationIdentity struct {
	RouterName string `json:"routerName"`
	Version    string `json:"version"`
	Platform   string `json:"platform"`
	BoardName  string `json:"boardName"`
}

type VerificationInterface struct {
	Name      string   `json:"name"`
	Type      string   `json:"type"`
	Running   bool     `json:"running"`
	Disabled  bool     `json:"disabled"`
	Addresses []string `json:"addresses"`
}

type CIDRCandidate struct {
	CIDR      string `json:"cidr"`
	Interface string `json:"interface"`
	Family    string `json:"family"`
}

type VerificationWarning struct {
	Capability string `json:"capability"`
	Message    string `json:"message"`
}

type VerificationResult struct {
	Identity       VerificationIdentity    `json:"identity"`
	Interfaces     []VerificationInterface `json:"interfaces"`
	CIDRCandidates []CIDRCandidate         `json:"cidrCandidates"`
	Warnings       []VerificationWarning   `json:"warnings"`
}

type VerificationError struct {
	Kind    string
	Message string
	Cause   error
}

func (e *VerificationError) Error() string { return e.Message }
func (e *VerificationError) Unwrap() error { return e.Cause }

func (c *Client) Verify(ctx context.Context) (VerificationResult, error) {
	resource, err := c.SystemResource(ctx)
	if err != nil {
		return VerificationResult{}, classifyVerificationError("system identity", err)
	}
	interfaces, err := c.Interfaces(ctx)
	if err != nil {
		return VerificationResult{}, classifyVerificationError("interface list", err)
	}
	ipv4, err := c.IPAddresses(ctx)
	if err != nil {
		return VerificationResult{}, classifyVerificationError("IPv4 addresses", err)
	}
	ipv6, err := c.IPv6Addresses(ctx)
	if err != nil {
		return VerificationResult{}, classifyVerificationError("IPv6 addresses", err)
	}
	if _, err := c.DHCPLeases(ctx); err != nil {
		return VerificationResult{}, classifyVerificationError("DHCP leases", err)
	}
	if _, err := c.ARPEntries(ctx); err != nil {
		return VerificationResult{}, classifyVerificationError("ARP entries", err)
	}
	if _, err := c.FirewallConnectionsV4(ctx); err != nil {
		return VerificationResult{}, classifyVerificationError("IPv4 connection tracking", err)
	}

	result := VerificationResult{
		Identity: VerificationIdentity{
			RouterName: resource.BoardName, Version: resource.Version, Platform: resource.Platform, BoardName: resource.BoardName,
		},
		Interfaces: verificationInterfaces(interfaces, ipv4, ipv6),
	}
	result.CIDRCandidates = verificationCIDRs(ipv4, ipv6)
	optional := []struct {
		capability string
		probe      func() error
	}{
		{"system-health", func() error { _, err := c.SystemHealth(ctx); return err }},
		{"ethernet-details", func() error { _, err := c.EthernetInterfaces(ctx); return err }},
		{"ipv6-neighbors", func() error { _, err := c.IPv6Neighbors(ctx); return err }},
		{"ipv6-connection-tracking", func() error { _, err := c.FirewallConnectionsV6(ctx); return err }},
		{"simple-queues", func() error { _, err := c.SimpleQueues(ctx); return err }},
		{"queue-trees", func() error { _, err := c.QueueTrees(ctx); return err }},
		{"mangle-rules", func() error { _, err := c.MangleRules(ctx); return err }},
		{"routing-rules", func() error { _, err := c.RoutingRules(ctx); return err }},
		{"routing-routes", func() error {
			if _, err := c.RoutingRoutes(ctx); err != nil {
				_, fallbackErr := c.IPRoutes(ctx)
				return fallbackErr
			}
			return nil
		}},
	}
	for _, item := range optional {
		if err := item.probe(); err != nil {
			result.Warnings = append(result.Warnings, VerificationWarning{
				Capability: item.capability,
				Message:    fmt.Sprintf("Optional RouterOS capability %s is unavailable.", item.capability),
			})
		}
	}
	return result, nil
}

func (c *Client) VerifyTrafficInterfaces(ctx context.Context, names []string) error {
	for _, name := range names {
		if _, err := c.MonitorTraffic(ctx, name); err != nil {
			return &VerificationError{Kind: "traffic_permission", Message: fmt.Sprintf("Unable to collect traffic from interface %s.", name), Cause: err}
		}
	}
	return nil
}

func verificationInterfaces(interfaces []Interface, ipv4 []IPAddress, ipv6 []IPv6Address) []VerificationInterface {
	addresses := make(map[string][]string)
	for _, item := range ipv4 {
		if strings.TrimSpace(item.Disabled) != "true" {
			addresses[item.Interface] = append(addresses[item.Interface], strings.TrimSpace(item.Address))
		}
	}
	for _, item := range ipv6 {
		if strings.TrimSpace(item.Disabled) != "true" {
			addresses[item.Interface] = append(addresses[item.Interface], strings.TrimSpace(item.Address))
		}
	}
	result := make([]VerificationInterface, 0, len(interfaces))
	for _, item := range interfaces {
		if strings.TrimSpace(item.Name) == "" {
			continue
		}
		values := uniqueSorted(addresses[item.Name])
		result = append(result, VerificationInterface{
			Name: item.Name, Type: item.Type, Running: strings.EqualFold(item.Running, "true"), Disabled: strings.EqualFold(item.Disabled, "true"), Addresses: values,
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

func verificationCIDRs(ipv4 []IPAddress, ipv6 []IPv6Address) []CIDRCandidate {
	seen := make(map[string]struct{})
	result := make([]CIDRCandidate, 0, len(ipv4)+len(ipv6))
	add := func(address, interfaceName, family string, disabled bool) {
		if disabled {
			return
		}
		_, network, err := net.ParseCIDR(strings.TrimSpace(address))
		if err != nil {
			return
		}
		key := interfaceName + "\x00" + network.String()
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		result = append(result, CIDRCandidate{CIDR: network.String(), Interface: interfaceName, Family: family})
	}
	for _, item := range ipv4 {
		add(item.Address, item.Interface, "ipv4", strings.EqualFold(item.Disabled, "true"))
	}
	for _, item := range ipv6 {
		add(item.Address, item.Interface, "ipv6", strings.EqualFold(item.Disabled, "true"))
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Interface != result[j].Interface {
			return result[i].Interface < result[j].Interface
		}
		return result[i].CIDR < result[j].CIDR
	})
	return result
}

func uniqueSorted(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func classifyVerificationError(capability string, err error) error {
	kind := "connection"
	message := "Unable to connect to RouterOS."
	var httpError *HTTPError
	if errors.As(err, &httpError) {
		switch httpError.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			kind, message = "authentication", "RouterOS username, password, or required permissions are incorrect."
		default:
			kind, message = "routeros_response", fmt.Sprintf("RouterOS could not provide required %s data.", capability)
		}
	} else if errors.Is(err, context.DeadlineExceeded) {
		kind, message = "timeout", "RouterOS connection timed out."
	}
	return &VerificationError{Kind: kind, Message: message, Cause: err}
}
