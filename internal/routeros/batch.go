package routeros

import (
	"context"
	"encoding/json"
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
	batchScriptOK       = "__rosboard_batch_ok__"
	batchScriptError    = "__rosboard_batch_error__:"
)

type batchScriptErrorResponse struct {
	detail string
}

func (e *batchScriptErrorResponse) Error() string {
	if e == nil || strings.TrimSpace(e.detail) == "" {
		return "RouterOS batch script failed"
	}
	return "RouterOS batch script failed: " + e.detail
}

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
	offset := 0
	for index, script := range splitBatchScript(lines) {
		lineCount := len(strings.Split(script, "\n"))
		chunkIDs := unique[offset : offset+lineCount]
		if err := c.applyDisabledBatchChunk(ctx, menu, chunkIDs, disabled, script); err != nil {
			return fmt.Errorf("RouterOS batch %s %s chunk %d: %w", verb, menu, index+1, err)
		}
		offset += lineCount
	}
	return nil
}

// applyDisabledBatchChunk treats the batch as an optimization, not as the
// source of truth. RouterOS may report a script error in a successful HTTP
// response, or a command may fail to change an item while the script itself
// returns successfully. Read the known IDs back immediately and repair only
// the items that did not reach the requested state through ordinary REST
// PATCH calls. Both enable and disable are idempotent, so this is safe even
// when the batch partially completed before returning an error.
func (c *MutationClient) applyDisabledBatchChunk(ctx context.Context, menu MutationMenu, ids []string, disabled bool, script string) error {
	_, batchErr := c.executeScript(ctx, script)
	pending, readbackErr := c.readBackDisabledBatch(ctx, menu, ids, disabled)
	if readbackErr == nil && len(pending) == 0 {
		return nil
	}
	if readbackErr != nil {
		pending = append([]string(nil), ids...)
	}
	if fallbackErr := c.patchDisabledIndividually(ctx, menu, pending, disabled); fallbackErr != nil {
		causes := make([]error, 0, 3)
		if batchErr != nil {
			causes = append(causes, batchErr)
		}
		if readbackErr != nil {
			causes = append(causes, fmt.Errorf("batch read-back: %w", readbackErr))
		}
		causes = append(causes, fmt.Errorf("individual REST fallback: %w", fallbackErr))
		return errors.Join(causes...)
	}
	remaining, err := c.readBackDisabledBatch(ctx, menu, ids, disabled)
	if err != nil {
		return fmt.Errorf("read back after individual REST fallback: %w", err)
	}
	if len(remaining) > 0 {
		return fmt.Errorf("RouterOS batch did not converge for IDs %s", strings.Join(remaining, ","))
	}
	return nil
}

func (c *MutationClient) readBackDisabledBatch(ctx context.Context, menu MutationMenu, ids []string, disabled bool) ([]string, error) {
	objects, err := c.List(ctx, menu, MutationQuery{Proplist: []string{".id", "disabled"}})
	if err != nil {
		return nil, fmt.Errorf("read back %s disabled state: %w", menu, err)
	}
	wanted := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		wanted[id] = struct{}{}
	}
	pending := make([]string, 0)
	for _, object := range objects {
		id := object.ID()
		if _, ok := wanted[id]; !ok {
			continue
		}
		value, ok := object["disabled"]
		if !ok {
			return nil, fmt.Errorf("RouterOS %s object %s has no disabled field", menu, id)
		}
		actual, parseErr := ParseRouterOSBool(value)
		if parseErr != nil {
			return nil, fmt.Errorf("parse RouterOS %s object %s disabled state: %w", menu, id, parseErr)
		}
		if actual != disabled {
			pending = append(pending, id)
		}
		delete(wanted, id)
	}
	for id := range wanted {
		pending = append(pending, id)
	}
	sort.Strings(pending)
	return pending, nil
}

func (c *MutationClient) patchDisabledIndividually(ctx context.Context, menu MutationMenu, ids []string, disabled bool) error {
	for _, id := range ids {
		if _, err := c.Patch(ctx, menu, id, RouterOSFields{"disabled": disabled}); err != nil {
			return fmt.Errorf("patch %s %s disabled=%t: %w", menu, id, disabled, err)
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
	script = batchScriptEnvelope(script)
	// `as-string` switches :execute from its default detached background job to
	// synchronous execution whose HTTP reply arrives only after every script
	// line has run. Without it, a large CreateBatch can return while later
	// RouterOS reads still see an incomplete batch, failing the strict ID
	// readback and the post-apply verify pass with transient zero matches.
	body, err := c.executeURLWithTimeout(ctx, http.MethodPost, target, RouterOSFields{"script": script, "as-string": ""}, maxMutationJSONBytes, mutationNoRetryMutation, batchScriptTimeout)
	if err != nil {
		return nil, err
	}
	if err := c.validateBatchScriptResponse(body); err != nil {
		return body, err
	}
	return body, nil
}

func batchScriptEnvelope(script string) string {
	return ":onerror e in={\n" + script + "\n:put \"" + batchScriptOK + "\"\n} do={\n:put \"" + batchScriptError + " $e\"\n}"
}

func (c *MutationClient) validateBatchScriptResponse(body []byte) error {
	var response struct {
		Ret string `json:"ret"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return fmt.Errorf("decode RouterOS batch response: %w", err)
	}
	lines := strings.Split(strings.ReplaceAll(string(response.Ret), "\r\n", "\n"), "\n")
	last := ""
	for _, line := range lines {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			last = trimmed
		}
	}
	if strings.HasPrefix(last, batchScriptError) {
		detail := strings.TrimSpace(strings.TrimPrefix(last, batchScriptError))
		detail = sanitizeMutationText(detail, c.username, c.password)
		if len(detail) > maxMutationDetailBytes {
			detail = detail[:maxMutationDetailBytes]
		}
		return &batchScriptErrorResponse{detail: detail}
	}
	if last == batchScriptOK {
		return nil
	}
	return errors.New("RouterOS batch response missing success marker")
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
	case MenuIPDNSForwarders, MenuIPDNSStatic,
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
