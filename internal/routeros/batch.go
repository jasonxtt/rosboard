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
	"sync/atomic"
	"time"
	"unicode"
)

const (
	// RouterOS limits a script passed to :execute to 64 KiB. Keep a generous
	// margin for command parsing and future field growth.
	maxBatchScriptBytes    = 48 << 10
	maxBatchScriptItems    = 256
	batchScriptTimeout     = 50 * time.Second
	batchScriptOKPrefix    = "__rosboard_batch_ok_"
	batchScriptErrorPrefix = "__rosboard_batch_error_"
)

var batchScriptSequence uint64

type batchScriptProtocol struct {
	ok          string
	errorPrefix string
}

func newBatchScriptProtocol() batchScriptProtocol {
	token := atomic.AddUint64(&batchScriptSequence, 1)
	return batchScriptProtocol{
		ok:          fmt.Sprintf("%s%x__", batchScriptOKPrefix, token),
		errorPrefix: fmt.Sprintf("%s%x__:", batchScriptErrorPrefix, token),
	}
}

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
	var batchErr error
	for index, script := range splitBatchScript(lines) {
		if _, err := c.executeScript(ctx, script); err != nil {
			batchErr = fmt.Errorf("RouterOS batch %s %s chunk %d: %w", verb, menu, index+1, err)
			break
		}
	}

	// Batch execution is only an optimization. Reconcile the whole requested
	// set once, after all successful chunks or after the first ambiguous/script
	// error. This keeps activation close to O(batch chunks + menu size) instead
	// of scanning the whole RouterOS menu once per chunk.
	pending, readbackErr := c.readBackDisabledBatch(ctx, menu, unique, disabled)
	if readbackErr == nil && len(pending) == 0 {
		return nil
	}
	if readbackErr != nil {
		var invalidReadback *batchReadbackError
		if errors.As(readbackErr, &invalidReadback) {
			if batchErr != nil {
				return errors.Join(batchErr, readbackErr)
			}
			return readbackErr
		}
		pending = append([]string(nil), unique...)
	}
	if fallbackErr := c.patchDisabledIndividually(ctx, menu, pending, disabled); fallbackErr != nil {
		causes := make([]error, 0, 3)
		if batchErr != nil {
			causes = append(causes, batchErr)
		}
		if readbackErr != nil {
			causes = append(causes, fmt.Errorf("batch read-back for IDs %s: %w", strings.Join(unique, ","), readbackErr))
		}
		causes = append(causes, fmt.Errorf("individual REST fallback for IDs %s: %w", strings.Join(pending, ","), fallbackErr))
		return errors.Join(causes...)
	}
	remaining, err := c.readBackDisabledBatch(ctx, menu, unique, disabled)
	if err != nil {
		return fmt.Errorf("read back after individual REST fallback for IDs %s: %w", strings.Join(unique, ","), err)
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
	seen := make(map[string]struct{}, len(ids))
	for _, object := range objects {
		id := object.ID()
		if _, duplicate := seen[id]; duplicate {
			return nil, c.newBatchReadbackError("RouterOS %s read-back returned duplicate ID %s", menu, id)
		}
		if _, ok := wanted[id]; !ok {
			continue
		}
		seen[id] = struct{}{}
		value, ok := object["disabled"]
		if !ok {
			return nil, c.newBatchReadbackError("RouterOS %s object %s has no disabled field", menu, id)
		}
		actual, parseErr := ParseRouterOSBool(value)
		if parseErr != nil {
			return nil, c.newBatchReadbackError("parse RouterOS %s object %s disabled state: %v", menu, id, parseErr)
		}
		if actual != disabled {
			pending = append(pending, id)
		}
		delete(wanted, id)
	}
	if len(wanted) > 0 {
		missing := make([]string, 0, len(wanted))
		for id := range wanted {
			missing = append(missing, id)
		}
		sort.Strings(missing)
		return nil, c.newBatchReadbackError("RouterOS %s read-back is missing IDs %s", menu, strings.Join(missing, ","))
	}
	sort.Strings(pending)
	return pending, nil
}

type batchReadbackError struct {
	detail string
}

func (e *batchReadbackError) Error() string {
	if e == nil || strings.TrimSpace(e.detail) == "" {
		return "RouterOS batch read-back failed"
	}
	return e.detail
}

func (c *MutationClient) newBatchReadbackError(format string, args ...any) error {
	detail := fmt.Sprintf(format, args...)
	detail = sanitizeMutationText(detail, c.username, c.password)
	if len(detail) > maxMutationDetailBytes {
		detail = detail[:maxMutationDetailBytes]
	}
	return &batchReadbackError{detail: detail}
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
	protocol := newBatchScriptProtocol()
	script = batchScriptEnvelope(script, protocol)
	// `as-string` switches :execute from its default detached background job to
	// synchronous execution whose HTTP reply arrives only after every script
	// line has run. Without it, a large CreateBatch can return while later
	// RouterOS reads still see an incomplete batch, failing the strict ID
	// readback and the post-apply verify pass with transient zero matches.
	body, err := c.executeURLWithTimeout(ctx, http.MethodPost, target, RouterOSFields{"script": script, "as-string": ""}, maxMutationJSONBytes, mutationNoRetryMutation, batchScriptTimeout)
	if err != nil {
		return nil, err
	}
	if err := c.validateBatchScriptResponse(body, protocol); err != nil {
		return body, err
	}
	return body, nil
}

func batchScriptEnvelope(script string, protocol batchScriptProtocol) string {
	return ":onerror e in={\n" + script + "\n:put \"" + protocol.ok + "\"\n} do={\n:put \"" + protocol.errorPrefix + " $e\"\n}"
}

func (c *MutationClient) validateBatchScriptResponse(body []byte, protocol batchScriptProtocol) error {
	var response struct {
		Ret json.RawMessage `json:"ret"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return fmt.Errorf("decode RouterOS batch response: %w", err)
	}
	if len(response.Ret) == 0 {
		return errors.New("RouterOS batch response missing ret")
	}
	var ret string
	if err := json.Unmarshal(response.Ret, &ret); err != nil {
		return fmt.Errorf("RouterOS batch response ret is not a string: %w", err)
	}
	lines := strings.Split(strings.ReplaceAll(ret, "\r\n", "\n"), "\n")
	hasOK := false
	hasError := false
	errorDetail := ""
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == protocol.ok {
			hasOK = true
		}
		if strings.HasPrefix(trimmed, protocol.errorPrefix) {
			hasError = true
			if errorDetail == "" {
				errorDetail = strings.TrimSpace(strings.TrimPrefix(trimmed, protocol.errorPrefix))
			}
		}
	}
	if hasOK && hasError {
		return errors.New("RouterOS batch response contains both success and error markers")
	}
	if hasError {
		detail := errorDetail
		detail = sanitizeMutationText(detail, c.username, c.password)
		if len(detail) > maxMutationDetailBytes {
			detail = detail[:maxMutationDetailBytes]
		}
		return &batchScriptErrorResponse{detail: detail}
	}
	if hasOK {
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
