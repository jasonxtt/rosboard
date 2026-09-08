package routeros

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// MutationMenu names one of the RouterOS menus that the policy mutation
// client is allowed to address. Keeping this as a closed set prevents callers
// from turning a user-controlled value into an arbitrary REST path.
type MutationMenu string

const (
	MenuIPDNS                   MutationMenu = "ip/dns"
	MenuIPDNSForwarders         MutationMenu = "ip/dns/forwarders"
	MenuIPDNSStatic             MutationMenu = "ip/dns/static"
	MenuInterfaceList           MutationMenu = "interface/list"
	MenuInterfaceListMember     MutationMenu = "interface/list/member"
	MenuIPFirewallAddressList   MutationMenu = "ip/firewall/address-list"
	MenuIPv6FirewallAddressList MutationMenu = "ipv6/firewall/address-list"
	MenuIPFirewallFilter        MutationMenu = "ip/firewall/filter"
	MenuIPv6FirewallFilter      MutationMenu = "ipv6/firewall/filter"
	MenuIPFirewallMangle        MutationMenu = "ip/firewall/mangle"
	MenuIPv6FirewallMangle      MutationMenu = "ipv6/firewall/mangle"
	MenuIPFirewallNAT           MutationMenu = "ip/firewall/nat"
	MenuIPv6FirewallNAT         MutationMenu = "ipv6/firewall/nat"
	MenuIPRoute                 MutationMenu = "ip/route"
	MenuIPv6Route               MutationMenu = "ipv6/route"
	MenuRoutingTable            MutationMenu = "routing/table"
	MenuRoutingRule             MutationMenu = "routing/rule"
	MenuIPDHCPClient            MutationMenu = "ip/dhcp-client"
	MenuIPDHCPServerNetwork     MutationMenu = "ip/dhcp-server/network"
	MenuIPv6DHCPClient          MutationMenu = "ipv6/dhcp-client"
)

// ReadMenu is the closed set of RouterOS menus that the policy scanner may
// read. Keeping this separate from MutationMenu makes it impossible for the
// scanner to accidentally inherit a write-only command path.
type ReadMenu string

const (
	ReadMenuSystemResource          ReadMenu = "system/resource"
	ReadMenuInterface               ReadMenu = "interface"
	ReadMenuInterfaceList           ReadMenu = "interface/list"
	ReadMenuInterfaceListMember     ReadMenu = "interface/list/member"
	ReadMenuBridgePort              ReadMenu = "interface/bridge/port"
	ReadMenuIPAddress               ReadMenu = "ip/address"
	ReadMenuIPv6Address             ReadMenu = "ipv6/address"
	ReadMenuRoutingTable            ReadMenu = "routing/table"
	ReadMenuRoutingRoute            ReadMenu = "routing/route"
	ReadMenuIPRoute                 ReadMenu = "ip/route"
	ReadMenuIPv6Route               ReadMenu = "ipv6/route"
	ReadMenuIPDHCPClient            ReadMenu = "ip/dhcp-client"
	ReadMenuIPv6DHCPClient          ReadMenu = "ipv6/dhcp-client"
	ReadMenuPPPoEClient             ReadMenu = "interface/pppoe-client"
	ReadMenuWireGuard               ReadMenu = "interface/wireguard"
	ReadMenuWireGuardPeer           ReadMenu = "interface/wireguard/peers"
	ReadMenuIPDNS                   ReadMenu = "ip/dns"
	ReadMenuIPDNSForwarders         ReadMenu = "ip/dns/forwarders"
	ReadMenuIPDNSStatic             ReadMenu = "ip/dns/static"
	ReadMenuIPFirewallMangle        ReadMenu = "ip/firewall/mangle"
	ReadMenuIPFirewallNAT           ReadMenu = "ip/firewall/nat"
	ReadMenuIPFirewallFilter        ReadMenu = "ip/firewall/filter"
	ReadMenuIPFirewallAddressList   ReadMenu = "ip/firewall/address-list"
	ReadMenuIPv6FirewallMangle      ReadMenu = "ipv6/firewall/mangle"
	ReadMenuIPv6FirewallNAT         ReadMenu = "ipv6/firewall/nat"
	ReadMenuIPv6FirewallFilter      ReadMenu = "ipv6/firewall/filter"
	ReadMenuIPv6FirewallAddressList ReadMenu = "ipv6/firewall/address-list"
	ReadMenuRoutingRule             ReadMenu = "routing/rule"
	ReadMenuRoutingSettings         ReadMenu = "routing/settings"
	ReadMenuDHCPServerNetwork       ReadMenu = "ip/dhcp-server/network"
	ReadMenuIPVRF                   ReadMenu = "ip/vrf"
)

func (menu ReadMenu) valid() bool {
	switch menu {
	case ReadMenuSystemResource,
		ReadMenuInterface,
		ReadMenuInterfaceList,
		ReadMenuInterfaceListMember,
		ReadMenuBridgePort,
		ReadMenuIPAddress,
		ReadMenuIPv6Address,
		ReadMenuRoutingTable,
		ReadMenuRoutingRoute,
		ReadMenuIPRoute,
		ReadMenuIPv6Route,
		ReadMenuIPDHCPClient,
		ReadMenuIPv6DHCPClient,
		ReadMenuPPPoEClient,
		ReadMenuWireGuard,
		ReadMenuWireGuardPeer,
		ReadMenuIPDNS,
		ReadMenuIPDNSForwarders,
		ReadMenuIPDNSStatic,
		ReadMenuIPFirewallMangle,
		ReadMenuIPFirewallNAT,
		ReadMenuIPFirewallFilter,
		ReadMenuIPFirewallAddressList,
		ReadMenuIPv6FirewallMangle,
		ReadMenuIPv6FirewallNAT,
		ReadMenuIPv6FirewallFilter,
		ReadMenuIPv6FirewallAddressList,
		ReadMenuRoutingRule,
		ReadMenuRoutingSettings,
		ReadMenuDHCPServerNetwork,
		ReadMenuIPVRF:
		return true
	default:
		return false
	}
}

type MutationCommand string

const (
	CommandMove           MutationCommand = "move"
	CommandDNSCacheFlush  MutationCommand = "dns-cache-flush"
	CommandDNSSettingsSet MutationCommand = "dns-settings-set"
	CommandPrint          MutationCommand = "print"
	CommandExport         MutationCommand = "export"
)

type RouterOSFields map[string]any

type MutationQuery struct {
	Filters  map[string]string
	Proplist []string
}

type RouterOSObject map[string]string

// PolicyList performs a read-only, allow-listed RouterOS REST list request.
// It is intentionally not a generic REST escape hatch: callers cannot supply
// an arbitrary path and the scanner has no access to mutation commands.
func (c *Client) PolicyList(ctx context.Context, menu ReadMenu, proplist []string) ([]RouterOSObject, error) {
	if !menu.valid() {
		return nil, fmt.Errorf("unsupported policy read menu %q", menu)
	}
	path := "/rest/" + string(menu)
	if len(proplist) > 0 {
		path += "?" + url.Values{".proplist": {strings.Join(proplist, ",")}}.Encode()
	}
	var payload json.RawMessage
	if err := c.getJSON(ctx, path, &payload); err != nil {
		return nil, err
	}
	return decodeRouterOSObjects(payload)
}

func (o RouterOSObject) ID() string {
	return o[".id"]
}

func (o RouterOSObject) Bool(key string) (bool, error) {
	value, ok := o[key]
	if !ok {
		return false, fmt.Errorf("RouterOS field %q is missing", key)
	}
	return ParseRouterOSBool(value)
}

func (o RouterOSObject) Int(key string) (int64, error) {
	value, ok := o[key]
	if !ok {
		return 0, fmt.Errorf("RouterOS field %q is missing", key)
	}
	return ParseRouterOSInt(value)
}

func ParseRouterOSBool(value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "yes", "1", "on":
		return true, nil
	case "false", "no", "0", "off":
		return false, nil
	default:
		return false, fmt.Errorf("invalid RouterOS boolean %q", value)
	}
}

func ParseRouterOSInt(value string) (int64, error) {
	trimmed := strings.TrimSpace(value)
	parsed, err := strconv.ParseInt(trimmed, 10, 64)
	if err != nil && strings.HasPrefix(strings.ToLower(trimmed), "0x") {
		parsed, err = strconv.ParseInt(trimmed[2:], 16, 64)
	}
	if err != nil {
		return 0, fmt.Errorf("invalid RouterOS integer %q: %w", value, err)
	}
	return parsed, nil
}

func ParseRouterOSFloat(value string) (float64, error) {
	parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil {
		return 0, fmt.Errorf("invalid RouterOS number %q: %w", value, err)
	}
	return parsed, nil
}

func decodeRouterOSObject(data []byte) (RouterOSObject, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("decode RouterOS object: %w", err)
	}
	if raw == nil {
		return nil, fmt.Errorf("decode RouterOS object: expected object")
	}
	object := make(RouterOSObject, len(raw))
	for key, value := range raw {
		normalized, err := normalizeRouterOSJSONValue(value)
		if err != nil {
			return nil, fmt.Errorf("decode RouterOS field %q: %w", key, err)
		}
		object[key] = normalized
	}
	return object, nil
}

func decodeRouterOSObjects(data []byte) ([]RouterOSObject, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, nil
	}
	if data[0] == '{' {
		object, err := decodeRouterOSObject(data)
		if err != nil {
			return nil, err
		}
		return []RouterOSObject{object}, nil
	}

	var raw []json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("decode RouterOS objects: %w", err)
	}
	objects := make([]RouterOSObject, 0, len(raw))
	for _, value := range raw {
		object, err := decodeRouterOSObject(value)
		if err != nil {
			return nil, err
		}
		objects = append(objects, object)
	}
	return objects, nil
}

func normalizeRouterOSJSONValue(raw json.RawMessage) (string, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return "", err
	}
	switch value := value.(type) {
	case nil:
		return "", nil
	case string:
		return value, nil
	case bool:
		return strconv.FormatBool(value), nil
	case json.Number:
		return value.String(), nil
	default:
		return "", fmt.Errorf("unsupported JSON value type %T", value)
	}
}

type MoveRequest struct {
	ID       string
	BeforeID string
}

type MutationResponse struct {
	Object  RouterOSObject
	Objects []RouterOSObject
	Raw     []byte
}

type MoveAdapter interface {
	Move(context.Context, MutationMenu, MoveRequest) (MutationResponse, error)
}

type ExportReader interface {
	Export(context.Context, RouterOSFields) ([]byte, error)
}
