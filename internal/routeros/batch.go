package routeros

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode"
)

const (
	// RouterOS limits a script passed to :execute to 64 KiB. Keep a generous
	// margin for command parsing and future field growth.
	maxBatchScriptBytes = 48 << 10
	maxBatchScriptItems = 256
	batchScriptTimeout  = 50 * time.Second
)

var dnsStaticBatchFields = map[string]struct{}{
	"address":         {},
	"address-list":    {},
	"cname":           {},
	"comment":         {},
	"disabled":        {},
	"forward-to":      {},
	"match-subdomain": {},
	"mx-exchange":     {},
	"mx-preference":   {},
	"name":            {},
	"ns":              {},
	"regexp":          {},
	"srv-port":        {},
	"srv-priority":    {},
	"srv-target":      {},
	"srv-weight":      {},
	"text":            {},
	"ttl":             {},
	"type":            {},
}

// CreateBatch creates DNS static entries by sending a bounded RouterOS
// script. It is intentionally narrower than the ordinary CRUD API: policy
// routing only needs DNS static creation here, and arbitrary script execution
// would turn a safe mutation client into a command injection surface.
func (c *MutationClient) CreateBatch(ctx context.Context, menu MutationMenu, entries []RouterOSFields) error {
	if menu != MenuIPDNSStatic {
		return fmt.Errorf("RouterOS batch create menu %q is not allowlisted", menu)
	}
	if len(entries) == 0 {
		return nil
	}
	lines := make([]string, 0, len(entries))
	for index, fields := range entries {
		line, err := batchCreateLine(menu, fields)
		if err != nil {
			return fmt.Errorf("RouterOS batch create entry %d: %w", index+1, err)
		}
		lines = append(lines, line)
	}
	for index, script := range splitBatchScript(lines) {
		if _, err := c.executeScript(ctx, script); err != nil {
			return fmt.Errorf("RouterOS batch create %s chunk %d: %w", menu, index+1, err)
		}
	}
	return nil
}

// SetDisabledBatch enables or disables known policy menus in bounded
// RouterOS scripts. The caller supplies already-resolved internal IDs from a
// fresh scan; IDs are validated before any request is sent.
func (c *MutationClient) SetDisabledBatch(ctx context.Context, menu MutationMenu, ids []string, disabled bool) error {
	if !batchDisabledMenu(menu) {
		return fmt.Errorf("RouterOS batch enable/disable menu %q is not allowlisted", menu)
	}
	unique := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for index, id := range ids {
		if err := validateMutationID(id); err != nil {
			return fmt.Errorf("RouterOS batch enable/disable ID %d: %w", index+1, err)
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	if len(unique) == 0 {
		return nil
	}
	verb := "enable"
	if disabled {
		verb = "disable"
	}
	lines := make([]string, 0, len(unique))
	for _, id := range unique {
		lines = append(lines, fmt.Sprintf("/%s/%s %s", menu, verb, id))
	}
	for index, script := range splitBatchScript(lines) {
		if _, err := c.executeScript(ctx, script); err != nil {
			return fmt.Errorf("RouterOS batch %s %s chunk %d: %w", verb, menu, index+1, err)
		}
	}
	return nil
}

func (c *MutationClient) executeScript(ctx context.Context, script string) ([]byte, error) {
	if c == nil {
		return nil, errors.New("RouterOS mutation client is not initialized")
	}
	if strings.TrimSpace(script) == "" {
		return nil, errors.New("RouterOS batch script is empty")
	}
	target, err := url.Parse(c.baseURL + "/rest/execute")
	if err != nil {
		return nil, errors.New("invalid RouterOS batch script request")
	}
	return c.executeURLWithTimeout(ctx, http.MethodPost, target, RouterOSFields{"script": script}, maxMutationJSONBytes, mutationNoRetryMutation, batchScriptTimeout)
}

func batchCreateLine(menu MutationMenu, fields RouterOSFields) (string, error) {
	if menu != MenuIPDNSStatic {
		return "", fmt.Errorf("RouterOS batch create menu %q is not allowlisted", menu)
	}
	if len(fields) == 0 {
		return "", errors.New("RouterOS batch create fields are empty")
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		if _, ok := dnsStaticBatchFields[key]; !ok {
			return "", fmt.Errorf("RouterOS DNS static batch field %q is not allowlisted", key)
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := []string{"/ip/dns/static/add"}
	for _, key := range keys {
		value, err := renderBatchValue(fields[key])
		if err != nil {
			return "", fmt.Errorf("field %q: %w", key, err)
		}
		parts = append(parts, key+"="+value)
	}
	line := strings.Join(parts, " ")
	if len(line) > maxBatchScriptBytes {
		return "", errors.New("RouterOS batch create command is too large")
	}
	return line, nil
}

func renderBatchValue(value any) (string, error) {
	switch value := value.(type) {
	case bool:
		if value {
			return "yes", nil
		}
		return "no", nil
	case string:
		var builder strings.Builder
		builder.WriteByte('"')
		for _, character := range value {
			if unicode.IsControl(character) {
				return "", errors.New("RouterOS batch string contains a control character")
			}
			switch character {
			case '\\', '"', '$':
				builder.WriteByte('\\')
			}
			builder.WriteRune(character)
		}
		builder.WriteByte('"')
		return builder.String(), nil
	default:
		return "", fmt.Errorf("unsupported RouterOS batch value type %T", value)
	}
}

func splitBatchScript(lines []string) []string {
	scripts := make([]string, 0, (len(lines)+maxBatchScriptItems-1)/maxBatchScriptItems)
	current := make([]string, 0, maxBatchScriptItems)
	currentBytes := 0
	flush := func() {
		if len(current) == 0 {
			return
		}
		scripts = append(scripts, strings.Join(current, "\n"))
		current = make([]string, 0, maxBatchScriptItems)
		currentBytes = 0
	}
	for _, line := range lines {
		lineBytes := len(line)
		if len(current) > 0 && (len(current) >= maxBatchScriptItems || currentBytes+1+lineBytes > maxBatchScriptBytes) {
			flush()
		}
		current = append(current, line)
		currentBytes += lineBytes
		if len(current) > 1 {
			currentBytes++
		}
	}
	flush()
	return scripts
}

func batchDisabledMenu(menu MutationMenu) bool {
	switch menu {
	case MenuIPDNSStatic,
		MenuIPFirewallAddressList, MenuIPv6FirewallAddressList,
		MenuIPFirewallFilter, MenuIPv6FirewallFilter,
		MenuIPFirewallMangle, MenuIPv6FirewallMangle,
		MenuIPFirewallNAT, MenuIPv6FirewallNAT,
		MenuIPRoute, MenuIPv6Route, MenuRoutingRule:
		return true
	default:
		return false
	}
}
