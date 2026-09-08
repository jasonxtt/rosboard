package routeros

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	defaultMutationRequestTimeout = 15 * time.Second
	defaultMutationRetries        = 2
	defaultMutationRetryBaseDelay = 100 * time.Millisecond
	maxMutationRetryDelay         = 2 * time.Second
	maxMutationRetries            = 4
	maxMutationDetailBytes        = 8 << 10
	maxMutationJSONBytes          = 2 << 20
	maxMutationExportBytes        = 32 << 20
)

// writeProbeComment marks the inert capability probe created by WriteProbe so
// operators can identify and safely remove leftovers; it deliberately does not
// use the owned-object comment format so scanner ownership logic never treats a
// failed probe as a managed object.
const writeProbeComment = "rosboard policy write probe (inert; safe to remove)"

type mutationRetryPolicy uint8

const (
	mutationRetryRead mutationRetryPolicy = iota
	mutationNoRetryMutation
)

// MutationOutcomeUnknownError means the client cannot prove whether RouterOS
// applied an ambiguous operation. Callers must reconcile actual state before
// deciding whether to continue; this error is intentionally not retryable.
type MutationOutcomeUnknownError struct {
	Method     string
	Path       string
	StatusCode int
}

func (e *MutationOutcomeUnknownError) Error() string {
	if e.StatusCode != 0 {
		return fmt.Sprintf("RouterOS mutation outcome unknown for %s %s (status %d)", e.Method, e.Path, e.StatusCode)
	}
	return fmt.Sprintf("RouterOS mutation outcome unknown for %s %s", e.Method, e.Path)
}

var authorizationHeaderPattern = regexp.MustCompile(`(?i)authorization\s*:\s*[^\r\n]*`)

type MutationClientOptions struct {
	HTTPClient     *http.Client
	RequestTimeout time.Duration
	MaxRetries     int
	RetryBaseDelay time.Duration
	Sleep          func(context.Context, time.Duration) error
}

type MutationClient struct {
	baseURL        string
	username       string
	password       string
	httpClient     *http.Client
	requestTimeout time.Duration
	maxRetries     int
	retryBaseDelay time.Duration
	sleep          func(context.Context, time.Duration) error
	initErr        error
}

func NewMutationClient(baseURL, username, password string) *MutationClient {
	client, err := NewMutationClientWithOptions(baseURL, username, password, MutationClientOptions{
		MaxRetries:     defaultMutationRetries,
		RetryBaseDelay: defaultMutationRetryBaseDelay,
	})
	if err == nil {
		return client
	}
	return &MutationClient{initErr: err}
}

func NewMutationClientWithOptions(baseURL, username, password string, options MutationClientOptions) (*MutationClient, error) {
	normalizedURL, err := normalizeMutationBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	if options.RequestTimeout <= 0 {
		options.RequestTimeout = defaultMutationRequestTimeout
	}
	if options.MaxRetries < 0 {
		options.MaxRetries = 0
	}
	if options.MaxRetries > maxMutationRetries {
		options.MaxRetries = maxMutationRetries
	}
	if options.RetryBaseDelay <= 0 {
		options.RetryBaseDelay = defaultMutationRetryBaseDelay
	}
	httpClient := options.HTTPClient
	if httpClient == nil {
		// Mutation requests use INDEPENDENT connections: the WriteProbe performs
		// an immediate DELETE right after a PUT create, and a reused keep-alive
		// connection can be stale/reset by flaky link-layer proxies between the
		// two requests (RouterOS WWW/HTTP on some adapters). DisableKeepAlives
		// forces a fresh TCP connection per mutation request. The transport is
		// a clone of the default transport so env-proxy/TLS parity is kept,
		// and the ordinary read-only Client is never affected.
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.DisableKeepAlives = true
		httpClient = &http.Client{Transport: transport}
	} else {
		copy := *httpClient
		httpClient = &copy
	}
	if httpClient.Timeout <= 0 || options.RequestTimeout < httpClient.Timeout {
		httpClient.Timeout = options.RequestTimeout
	}
	httpClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return errors.New("RouterOS mutation redirects are not allowed")
	}
	sleep := options.Sleep
	if sleep == nil {
		sleep = sleepMutationRetry
	}
	return &MutationClient{
		baseURL:        normalizedURL,
		username:       username,
		password:       password,
		httpClient:     httpClient,
		requestTimeout: options.RequestTimeout,
		maxRetries:     options.MaxRetries,
		retryBaseDelay: options.RetryBaseDelay,
		sleep:          sleep,
	}, nil
}

func normalizeMutationBaseURL(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Opaque != "" {
		return "", errors.New("invalid RouterOS mutation base URL")
	}
	if !strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https") {
		return "", errors.New("invalid RouterOS mutation base URL scheme")
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return "", errors.New("RouterOS mutation base URL must not contain a path")
	}
	return strings.TrimRight(parsed.Scheme+"://"+parsed.Host, "/"), nil
}

func (c *MutationClient) List(ctx context.Context, menu MutationMenu, query MutationQuery) ([]RouterOSObject, error) {
	endpoint, err := c.menuEndpoint(menu, "")
	if err != nil {
		return nil, err
	}
	target, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}
	if err := addMutationQuery(target, query); err != nil {
		return nil, err
	}
	body, err := c.executeURL(ctx, http.MethodGet, target, nil, maxMutationJSONBytes, mutationRetryRead)
	if err != nil {
		return nil, err
	}
	return decodeRouterOSObjects(body)
}

func (c *MutationClient) Get(ctx context.Context, menu MutationMenu, id string, query MutationQuery) (RouterOSObject, error) {
	target, err := c.objectEndpoint(menu, id)
	if err != nil {
		return nil, err
	}
	if err := addMutationQuery(target, query); err != nil {
		return nil, err
	}
	body, err := c.executeURL(ctx, http.MethodGet, target, nil, maxMutationJSONBytes, mutationRetryRead)
	if err != nil {
		return nil, err
	}
	return decodeRouterOSObject(body)
}

func (c *MutationClient) Create(ctx context.Context, menu MutationMenu, fields RouterOSFields) (RouterOSObject, error) {
	endpoint, err := c.menuEndpoint(menu, "")
	if err != nil {
		return nil, err
	}
	body, err := c.execute(ctx, http.MethodPut, endpoint, fields, maxMutationJSONBytes, mutationNoRetryMutation)
	if err != nil {
		return nil, err
	}
	object, err := decodeRouterOSObject(body)
	if err != nil {
		return nil, mutationUnknownFor(http.MethodPut, endpoint, 0)
	}
	if object.ID() == "" {
		return nil, mutationUnknownFor(http.MethodPut, endpoint, 0)
	}
	return object, nil
}

func (c *MutationClient) Patch(ctx context.Context, menu MutationMenu, id string, fields RouterOSFields) (RouterOSObject, error) {
	target, err := c.objectEndpoint(menu, id)
	if err != nil {
		return nil, err
	}
	body, err := c.executeURL(ctx, http.MethodPatch, target, fields, maxMutationJSONBytes, mutationNoRetryMutation)
	if err != nil {
		return nil, err
	}
	object, err := decodeRouterOSObject(body)
	if err != nil {
		return nil, mutationUnknownFor(http.MethodPatch, target.Path, 0)
	}
	if object.ID() == "" {
		return nil, mutationUnknownFor(http.MethodPatch, target.Path, 0)
	}
	return object, nil
}

func (c *MutationClient) Delete(ctx context.Context, menu MutationMenu, id string) error {
	target, err := c.objectEndpoint(menu, id)
	if err != nil {
		return err
	}
	_, err = c.executeURL(ctx, http.MethodDelete, target, nil, maxMutationJSONBytes, mutationNoRetryMutation)
	return err
}

func (c *MutationClient) Command(ctx context.Context, menu MutationMenu, command MutationCommand, fields RouterOSFields) (MutationResponse, error) {
	if command == CommandMove {
		return MutationResponse{}, errors.New("RouterOS move requires the typed Move method")
	}
	return c.command(ctx, menu, command, fields)
}

func (c *MutationClient) command(ctx context.Context, menu MutationMenu, command MutationCommand, fields RouterOSFields) (MutationResponse, error) {
	endpoint, err := c.commandEndpoint(menu, command)
	if err != nil {
		return MutationResponse{}, err
	}
	if err := validateMutationCommandFields(menu, command, fields); err != nil {
		return MutationResponse{}, err
	}
	policy := mutationNoRetryMutation
	responseLimit := int64(maxMutationJSONBytes)
	if command == CommandPrint || command == CommandExport {
		policy = mutationRetryRead
	}
	if command == CommandExport {
		responseLimit = maxMutationExportBytes
	}
	body, err := c.execute(ctx, http.MethodPost, endpoint, fields, responseLimit, policy)
	if err != nil {
		return MutationResponse{}, err
	}
	if command == CommandExport {
		return MutationResponse{Raw: body}, nil
	}
	response, err := decodeMutationResponse(body)
	if err != nil && policy == mutationNoRetryMutation {
		return MutationResponse{}, mutationUnknownFor(http.MethodPost, endpoint, 0)
	}
	return response, err
}

func (c *MutationClient) Move(ctx context.Context, menu MutationMenu, request MoveRequest) (MutationResponse, error) {
	if err := validateMutationID(request.ID); err != nil {
		return MutationResponse{}, err
	}
	if err := validateMutationID(request.BeforeID); err != nil {
		return MutationResponse{}, err
	}
	return c.command(ctx, menu, CommandMove, RouterOSFields{
		".id":         request.ID,
		"destination": request.BeforeID,
	})
}

func (c *MutationClient) FlushDNSCache(ctx context.Context) error {
	_, err := c.command(ctx, MenuIPDNS, CommandDNSCacheFlush, nil)
	return err
}

// WriteProbe proves that the credentials can create and delete an inert,
// disabled RouterOS object owned by the probe. RouterOS REST does not expose
// the write permissions required by FR-2.3, so a create/delete round trip of a
// disabled firewall filter rule carrying an explicit inert chain/action and a
// probe comment is the only behavior-neutral capability proof: the rule is
// chain=output action=accept but disabled, so it can never match or alter
// traffic while existing, and no existing object is touched. Any failure,
// including cleanup failure, fails closed because a read-only account must
// never be treated as suitable for policy mutation.
func (c *MutationClient) WriteProbe(ctx context.Context) error {
	probe, err := c.Create(ctx, MenuIPFirewallFilter, RouterOSFields{
		"comment":  writeProbeComment,
		"chain":    "output",
		"action":   "accept",
		"disabled": "yes",
	})
	if err != nil {
		return fmt.Errorf("RouterOS write capability probe failed: %w", err)
	}
	if probe.ID() == "" {
		return errors.New("RouterOS write capability probe returned no object id")
	}
	if err := c.Delete(ctx, MenuIPFirewallFilter, probe.ID()); err != nil {
		return fmt.Errorf("RouterOS write capability probe cleanup failed: %w", err)
	}
	return nil
}

// VerifyAccessControlCapabilities proves that each firewall family accepts the
// exact disabled rule shapes emitted by the access-control planner. RouterOS
// REST does not expose a capability endpoint, so this uses a create/delete
// round trip for inert rules and fails closed if either side is rejected.
func (c *MutationClient) VerifyAccessControlCapabilities(ctx context.Context, menus []MutationMenu) error {
	for _, menu := range menus {
		if menu != MenuIPFirewallFilter && menu != MenuIPv6FirewallFilter {
			return fmt.Errorf("unsupported access-control capability menu %q", menu)
		}
		suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
		if menu == MenuIPv6FirewallFilter {
			suffix += "6"
		}
		chain := "rosboard_access_probe_" + suffix
		addressList := "__rosboard_access_probe_" + suffix
		comment := "rosboard access capability probe (inert; safe to remove) " + suffix
		probes := []RouterOSFields{
			{"comment": comment + " chain-return", "chain": chain, "action": "return", "disabled": "yes"},
			{"comment": comment + " jump", "chain": "output", "src-address-list": addressList, "dst-address-list": addressList, "action": "jump", "jump-target": chain, "disabled": "yes"},
			{"comment": comment + " address-return", "chain": chain, "src-address-list": addressList, "dst-address-list": addressList, "action": "return", "disabled": "yes"},
			{"comment": comment + " tcp-reject", "chain": chain, "src-address-list": addressList, "dst-address-list": addressList, "protocol": "tcp", "action": "reject", "reject-with": "tcp-reset", "disabled": "yes"},
			{"comment": comment + " other-drop", "chain": chain, "action": "drop", "disabled": "yes"},
		}
		ids := make([]string, 0, len(probes))
		cleanup := func() error {
			var cleanupErr error
			for index := len(ids) - 1; index >= 0; index-- {
				if err := c.Delete(ctx, menu, ids[index]); err != nil {
					cleanupErr = errors.Join(cleanupErr, err)
				}
			}
			return cleanupErr
		}
		for index, fields := range probes {
			probe, err := c.Create(ctx, menu, fields)
			if err != nil {
				return fmt.Errorf("RouterOS %s access-control capability probe %d failed: %w (cleanup: %v)", menu, index+1, err, cleanup())
			}
			if probe.ID() == "" {
				return fmt.Errorf("RouterOS %s access-control capability probe %d returned no object id (cleanup: %v)", menu, index+1, cleanup())
			}
			ids = append(ids, probe.ID())
		}
		if err := cleanup(); err != nil {
			return fmt.Errorf("RouterOS %s access-control capability probe cleanup failed: %w", menu, err)
		}
	}
	return nil
}

func (c *MutationClient) SetDNSSettings(ctx context.Context, fields RouterOSFields) error {
	_, err := c.command(ctx, MenuIPDNS, CommandDNSSettingsSet, fields)
	return err
}

func (c *MutationClient) Print(ctx context.Context, menu MutationMenu, fields RouterOSFields) (MutationResponse, error) {
	return c.Command(ctx, menu, CommandPrint, fields)
}

func (c *MutationClient) Export(ctx context.Context, fields RouterOSFields) ([]byte, error) {
	response, err := c.command(ctx, "", CommandExport, fields)
	return response.Raw, err
}

// executeURL is the transport core: it runs the retry loop and builds each
// request from a *url.URL so a RouterOS object id preserved in RawPath stays
// literal in the request-target (see objectEndpoint).
func (c *MutationClient) executeURL(ctx context.Context, method string, target *url.URL, fields RouterOSFields, responseLimit int64, policy mutationRetryPolicy) ([]byte, error) {
	if c == nil {
		return nil, errors.New("RouterOS mutation client is not initialized")
	}
	return c.executeURLWithTimeout(ctx, method, target, fields, responseLimit, policy, c.requestTimeout)
}

func (c *MutationClient) executeURLWithTimeout(ctx context.Context, method string, target *url.URL, fields RouterOSFields, responseLimit int64, policy mutationRetryPolicy, requestTimeout time.Duration) ([]byte, error) {
	if c == nil || c.initErr != nil {
		if c != nil && c.initErr != nil {
			return nil, c.initErr
		}
		return nil, errors.New("RouterOS mutation client is not initialized")
	}
	if requestTimeout <= 0 {
		requestTimeout = defaultMutationRequestTimeout
	}
	if ctx == nil {
		ctx = context.Background()
	}
	httpClient := c.httpClient
	if httpClient == nil {
		return nil, errors.New("RouterOS mutation HTTP client is not initialized")
	}
	if httpClient.Timeout > 0 && httpClient.Timeout < requestTimeout {
		clientCopy := *httpClient
		clientCopy.Timeout = requestTimeout
		httpClient = &clientCopy
	}

	var requestBody []byte
	if fields != nil && method != http.MethodGet && method != http.MethodDelete {
		var err error
		requestBody, err = json.Marshal(fields)
		if err != nil {
			return nil, fmt.Errorf("marshal RouterOS mutation fields: %w", err)
		}
		if len(requestBody) > maxMutationJSONBytes {
			return nil, errors.New("RouterOS mutation request is too large")
		}
	}

	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		// The per-attempt context must honor the caller-supplied requestTimeout
		// (the batch script path deliberately raises it above the default
		// mutation timeout, matching the httpClient.Timeout override above);
		// capping at the field value would defeat the longer window.
		requestContext, cancel := context.WithTimeout(ctx, requestTimeout)
		var body io.Reader
		if requestBody != nil {
			body = bytes.NewReader(requestBody)
		}
		request, err := http.NewRequestWithContext(requestContext, method, target.String(), body)
		if err != nil {
			cancel()
			return nil, errors.New("invalid RouterOS mutation request")
		}
		// Re-apply the literal RawPath: NewRequest parse may drop it from the
		// string form, but the caller-built target carries the exact escaped
		// path segment (e.g. the RouterOS .id "*12" unescaped).
		if target.RawPath != "" {
			request.URL.RawPath = target.RawPath
		}
		request.SetBasicAuth(c.username, c.password)
		if requestBody != nil {
			request.Header.Set("Content-Type", "application/json")
		}
		response, err := httpClient.Do(request)
		if err != nil {
			cancel()
			if policy == mutationRetryRead && shouldRetryMutationNetworkError(ctx, err) && attempt < c.maxRetries {
				if err := c.waitForRetry(ctx, attempt); err != nil {
					return nil, err
				}
				continue
			}
			if policy == mutationNoRetryMutation {
				return nil, mutationUnknownFor(method, request.URL.Path, 0)
			}
			return nil, safeMutationNetworkError(err)
		}
		responseBody, readErr := readMutationBody(response.Body, responseLimit)
		_ = response.Body.Close()
		// Explicit connection-reuse boundary: regardless of the injected
		// http.Client, never leave an idle keep-alive connection pooled between
		// mutation requests. The next mutation (e.g. the WriteProbe DELETE
		// after its CREATE) therefore always starts on a fresh connection.
		httpClient.CloseIdleConnections()
		cancel()
		if readErr != nil {
			if policy == mutationNoRetryMutation {
				return nil, mutationUnknownFor(method, request.URL.Path, response.StatusCode)
			}
			return nil, readErr
		}
		if response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices {
			return responseBody, nil
		}
		if policy == mutationNoRetryMutation && shouldRetryMutationStatus(response.StatusCode) {
			return nil, mutationUnknownFor(method, request.URL.Path, response.StatusCode)
		}

		httpErr := &HTTPError{
			Path:       request.URL.Path,
			StatusCode: response.StatusCode,
			Status:     sanitizeMutationText(response.Status, c.username, c.password),
			Detail:     boundedMutationDetail(responseBody, c.username, c.password),
		}
		if policy == mutationRetryRead && shouldRetryMutationStatus(response.StatusCode) && attempt < c.maxRetries {
			if err := c.waitForRetry(ctx, attempt); err != nil {
				return nil, err
			}
			continue
		}
		return nil, httpErr
	}
	return nil, errors.New("RouterOS mutation request retries exhausted")
}

func (c *MutationClient) execute(ctx context.Context, method, endpoint string, fields RouterOSFields, responseLimit int64, policy mutationRetryPolicy) ([]byte, error) {
	target, err := url.Parse(endpoint)
	if err != nil {
		return nil, errors.New("invalid RouterOS mutation request")
	}
	return c.executeURL(ctx, method, target, fields, responseLimit, policy)
}

func (c *MutationClient) menuEndpoint(menu MutationMenu, suffix string) (string, error) {
	if err := validateMutationMenu(menu); err != nil {
		return "", err
	}
	return c.baseURL + "/rest/" + string(menu) + suffix, nil
}

// objectEndpoint builds the request target for an operation on one RouterOS
// object identified by its .id (e.g. "*12"). RouterOS requires the id VERBATIM
// in the request-target: percent-encoding the asterisk (%2A12) makes RouterOS
// match a different, nonexistent id so create/mutation on the object fails
// with no effect and no server log. The id charset is restricted to
// "*" + hexadecimal by validateMutationID (all RFC 3986 pchar-safe), so a
// literal RawPath is always a valid encoding of Path and Go serializes it
// unchanged on the wire.
func (c *MutationClient) objectEndpoint(menu MutationMenu, id string) (*url.URL, error) {
	if err := validateMutationID(id); err != nil {
		return nil, err
	}
	endpoint, err := c.menuEndpoint(menu, "")
	if err != nil {
		return nil, err
	}
	target, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}
	target.Path += "/" + id
	// RawPath is the PATH-encoded form (not the full URL). Since the id
	// charset is pchar-safe, the literal path is a valid encoding of itself
	// and EscapedPath() returns it verbatim.
	target.RawPath = target.Path
	return target, nil
}

func (c *MutationClient) commandEndpoint(menu MutationMenu, command MutationCommand) (string, error) {
	switch command {
	case CommandMove:
		if err := validateMoveMenu(menu); err != nil {
			return "", err
		}
		return c.baseURL + "/rest/" + string(menu) + "/move", nil
	case CommandDNSCacheFlush:
		if menu != MenuIPDNS {
			return "", errors.New("DNS cache flush is only allowed under ip/dns")
		}
		return c.baseURL + "/rest/ip/dns/cache/flush", nil
	case CommandDNSSettingsSet:
		if menu != MenuIPDNS {
			return "", errors.New("DNS settings are only allowed under ip/dns")
		}
		return c.baseURL + "/rest/ip/dns/set", nil
	case CommandPrint:
		if err := validateMutationMenu(menu); err != nil {
			return "", err
		}
		return c.baseURL + "/rest/" + string(menu) + "/print", nil
	case CommandExport:
		if menu == "" {
			return c.baseURL + "/rest/export", nil
		}
		if err := validateMutationMenu(menu); err != nil {
			return "", err
		}
		return c.baseURL + "/rest/" + string(menu) + "/export", nil
	default:
		return "", errors.New("RouterOS mutation command is not allowlisted")
	}
}

func validateMutationCommandFields(menu MutationMenu, command MutationCommand, fields RouterOSFields) error {
	switch command {
	case CommandMove:
		return nil
	case CommandDNSCacheFlush:
		if fields != nil {
			return errors.New("DNS cache flush does not accept fields")
		}
		return nil
	case CommandDNSSettingsSet:
		if fields == nil || len(fields) == 0 {
			return errors.New("DNS settings require fields")
		}
		return validateAllowedMutationFields(fields, map[string]struct{}{
			"allow-remote-requests": {},
			"cache-size":            {},
		})
	case CommandPrint:
		return validateAllowedMutationFields(fields, map[string]struct{}{
			".proplist":      {},
			"count-only":     {},
			"detail":         {},
			"follow-only":    {},
			"from":           {},
			"interval":       {},
			"to":             {},
			"terse":          {},
			"where":          {},
			"without-paging": {},
		})
	case CommandExport:
		return validateAllowedMutationFields(fields, map[string]struct{}{
			"compact": {},
			"terse":   {},
			"verbose": {},
		})
	default:
		return errors.New("RouterOS mutation command is not allowlisted")
	}
}

func validateAllowedMutationFields(fields RouterOSFields, allowed map[string]struct{}) error {
	for key := range fields {
		if _, ok := allowed[key]; !ok {
			return fmt.Errorf("RouterOS command field %q is not allowlisted", key)
		}
	}
	return nil
}

func mutationUnknownFor(method, endpoint string, statusCode int) *MutationOutcomeUnknownError {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return &MutationOutcomeUnknownError{Method: method, Path: "/", StatusCode: statusCode}
	}
	return &MutationOutcomeUnknownError{Method: method, Path: parsed.Path, StatusCode: statusCode}
}

func validateMutationMenu(menu MutationMenu) error {
	switch menu {
	case MenuIPDNS, MenuIPDNSForwarders, MenuIPDNSStatic,
		MenuInterfaceList, MenuInterfaceListMember,
		MenuIPFirewallAddressList, MenuIPv6FirewallAddressList,
		MenuIPFirewallFilter, MenuIPv6FirewallFilter,
		MenuIPFirewallMangle, MenuIPv6FirewallMangle,
		MenuIPFirewallNAT, MenuIPv6FirewallNAT,
		MenuIPRoute, MenuIPv6Route, MenuRoutingTable, MenuRoutingRule,
		MenuIPDHCPClient, MenuIPDHCPServerNetwork, MenuIPv6DHCPClient:
		return nil
	default:
		return errors.New("RouterOS mutation menu is not allowlisted")
	}
}

func validateMoveMenu(menu MutationMenu) error {
	switch menu {
	case MenuIPFirewallFilter, MenuIPv6FirewallFilter,
		MenuIPFirewallMangle, MenuIPv6FirewallMangle,
		MenuIPFirewallNAT, MenuIPv6FirewallNAT,
		MenuIPDNSStatic:
		return nil
	default:
		return errors.New("RouterOS move menu is not allowlisted")
	}
}

func validateMutationID(id string) error {
	if len(id) < 2 || id[0] != '*' {
		return errors.New("invalid RouterOS object ID")
	}
	for _, character := range id[1:] {
		if !((character >= '0' && character <= '9') || (character >= 'a' && character <= 'f') || (character >= 'A' && character <= 'F')) {
			return errors.New("invalid RouterOS object ID")
		}
	}
	return nil
}

func addMutationQuery(target *url.URL, query MutationQuery) error {
	if target == nil {
		return errors.New("mutation query target is nil")
	}
	values := url.Values{}
	for key, value := range query.Filters {
		values.Add(key, value)
	}
	if len(query.Proplist) > 0 {
		values.Add(".proplist", strings.Join(query.Proplist, ","))
	}
	encoded := values.Encode()
	if encoded == "" {
		return nil
	}
	if target.RawQuery != "" {
		target.RawQuery += "&" + encoded
	} else {
		target.RawQuery = encoded
	}
	return nil
}

// mutationQueryKey is the single query field MutationQuery carries.
type mutationQueryKey struct{}

func decodeMutationResponse(body []byte) (MutationResponse, error) {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return MutationResponse{}, nil
	}
	if body[0] == '{' {
		object, err := decodeRouterOSObject(body)
		if err != nil {
			return MutationResponse{}, err
		}
		return MutationResponse{Object: object, Objects: []RouterOSObject{object}}, nil
	}
	objects, err := decodeRouterOSObjects(body)
	if err != nil {
		return MutationResponse{}, err
	}
	return MutationResponse{Objects: objects}, nil
}

func readMutationBody(reader io.Reader, limit int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, errors.New("read RouterOS mutation response")
	}
	if int64(len(body)) > limit {
		return nil, errors.New("RouterOS mutation response is too large")
	}
	return body, nil
}

func boundedMutationDetail(body []byte, username, password string) string {
	var payload struct {
		Message string `json:"message"`
		Detail  string `json:"detail"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return ""
	}
	parts := make([]string, 0, 2)
	for _, value := range []string{payload.Message, payload.Detail} {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		value = sanitizeMutationText(value, username, password)
		parts = append(parts, value)
	}
	if len(parts) == 0 {
		return ""
	}
	detail := strings.Join(parts, ": ")
	if len(detail) > maxMutationDetailBytes {
		return detail[:maxMutationDetailBytes]
	}
	return detail
}

func sanitizeMutationText(value, username, password string) string {
	value = authorizationHeaderPattern.ReplaceAllString(value, "Authorization: [redacted]")
	if username != "" && password != "" {
		value = strings.ReplaceAll(value, username+":"+password, "[redacted]")
		basicToken := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
		value = strings.ReplaceAll(value, "Basic "+basicToken, "Basic [redacted]")
		value = strings.ReplaceAll(value, basicToken, "[redacted]")
	}
	if password != "" {
		value = strings.ReplaceAll(value, password, "[redacted]")
	}
	if username != "" {
		value = strings.ReplaceAll(value, username, "[redacted]")
	}
	return value
}

func shouldRetryMutationStatus(statusCode int) bool {
	return statusCode == http.StatusTooManyRequests || (statusCode >= 500 && statusCode <= 599)
}

func shouldRetryMutationNetworkError(ctx context.Context, err error) bool {
	if errors.Is(err, context.Canceled) {
		return false
	}
	if ctx.Err() != nil {
		return false
	}
	return true
}

func safeMutationNetworkError(err error) error {
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return errors.New("RouterOS mutation network request failed")
}

func (c *MutationClient) waitForRetry(ctx context.Context, attempt int) error {
	delay := c.retryBaseDelay
	for i := 0; i < attempt; i++ {
		if delay >= maxMutationRetryDelay/2 {
			delay = maxMutationRetryDelay
			break
		}
		delay *= 2
	}
	if delay > maxMutationRetryDelay {
		delay = maxMutationRetryDelay
	}
	return c.sleep(ctx, delay)
}

func sleepMutationRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
