package policy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	fetchTimeout       = 15 * time.Second
	maxFetchRedirects  = 5
	maxResponsePreview = MaxSourceBytes + 1
)

type DNSResolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}

type FetcherOptions struct {
	Resolver    DNSResolver
	DialContext func(context.Context, string, string) (net.Conn, error)
	TLSConfig   *tls.Config
}

type FetchOptions struct {
	ETag         string
	LastModified string
}

type FetchResult struct {
	URL          string
	StatusCode   int
	Body         []byte
	ETag         string
	LastModified string
	ContentType  string
	SHA256       string
	NotModified  bool
}

type SourceFetcher struct {
	resolver    DNSResolver
	dialContext func(context.Context, string, string) (net.Conn, error)
	tlsConfig   *tls.Config
}

// URLFetcher is kept as an expressive alias for callers that refer to this
// component by its URL-fetching responsibility.
type URLFetcher = SourceFetcher

func NewSourceFetcher(options FetcherOptions) *SourceFetcher {
	resolver := options.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	dialContext := options.DialContext
	if dialContext == nil {
		dialer := &net.Dialer{}
		dialContext = dialer.DialContext
	}
	var tlsConfig *tls.Config
	if options.TLSConfig != nil {
		tlsConfig = options.TLSConfig.Clone()
	}
	return &SourceFetcher{resolver: resolver, dialContext: dialContext, tlsConfig: tlsConfig}
}

func NewURLFetcher(options FetcherOptions) *SourceFetcher {
	return NewSourceFetcher(options)
}

func (f *SourceFetcher) Fetch(ctx context.Context, rawURL string, options FetchOptions) (FetchResult, error) {
	if f == nil {
		return FetchResult{}, errors.New("source fetcher is nil")
	}
	fetchCtx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	current, err := NormalizeSourceURL(rawURL)
	if err != nil {
		return FetchResult{}, err
	}
	redirects := 0
	for {
		parsed, err := url.Parse(current)
		if err != nil {
			return FetchResult{}, errors.New("source URL is malformed")
		}
		ips, err := f.resolvePublicIPs(fetchCtx, parsed.Hostname())
		if err != nil {
			return FetchResult{}, err
		}

		tlsConfig := &tls.Config{}
		if f.tlsConfig != nil {
			tlsConfig = f.tlsConfig.Clone()
		}
		transport := &http.Transport{
			Proxy:              nil,
			DisableCompression: true,
			ForceAttemptHTTP2:  false,
			TLSClientConfig:    tlsConfig,
			DialContext: func(dialCtx context.Context, network, _ string) (net.Conn, error) {
				return f.dialPinned(dialCtx, network, parsed.Port(), ips)
			},
		}
		transport.TLSClientConfig.ServerName = parsed.Hostname()
		client := &http.Client{Transport: transport, CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		}}

		req, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, current, nil)
		if err != nil {
			transport.CloseIdleConnections()
			return FetchResult{}, errors.New("source request is malformed")
		}
		req.Header.Set("Accept", "text/yaml, application/yaml, text/plain;q=0.9")
		req.Header.Set("Accept-Encoding", "identity")
		if options.ETag != "" {
			req.Header.Set("If-None-Match", options.ETag)
		}
		if options.LastModified != "" {
			req.Header.Set("If-Modified-Since", options.LastModified)
		}

		resp, err := client.Do(req)
		if err != nil {
			transport.CloseIdleConnections()
			return FetchResult{}, safeSourceRequestError(err)
		}
		if resp.StatusCode == http.StatusNotModified {
			resp.Body.Close()
			transport.CloseIdleConnections()
			if options.ETag == "" && options.LastModified == "" {
				return FetchResult{}, errors.New("source returned 304 without validators")
			}
			result := FetchResult{
				URL:          current,
				StatusCode:   resp.StatusCode,
				ETag:         resp.Header.Get("ETag"),
				LastModified: resp.Header.Get("Last-Modified"),
				ContentType:  resp.Header.Get("Content-Type"),
				NotModified:  true,
			}
			if result.ETag == "" {
				result.ETag = options.ETag
			}
			if result.LastModified == "" {
				result.LastModified = options.LastModified
			}
			return result, nil
		}
		if resp.StatusCode >= 300 && resp.StatusCode <= 399 {
			location := resp.Header.Get("Location")
			resp.Body.Close()
			transport.CloseIdleConnections()
			if location == "" {
				return FetchResult{}, fmt.Errorf("source redirect %d has no location", resp.StatusCode)
			}
			if redirects >= maxFetchRedirects {
				return FetchResult{}, fmt.Errorf("source exceeded %d redirects", maxFetchRedirects)
			}
			next, err := parsed.Parse(location)
			if err != nil {
				return FetchResult{}, errors.New("source redirect is malformed")
			}
			current, err = NormalizeSourceURL(next.String())
			if err != nil {
				return FetchResult{}, errors.New("source redirect was rejected")
			}
			redirects++
			continue
		}

		result := FetchResult{
			URL:          current,
			StatusCode:   resp.StatusCode,
			ETag:         resp.Header.Get("ETag"),
			LastModified: resp.Header.Get("Last-Modified"),
			ContentType:  resp.Header.Get("Content-Type"),
		}
		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			resp.Body.Close()
			transport.CloseIdleConnections()
			return FetchResult{}, fmt.Errorf("source returned HTTP status %d", resp.StatusCode)
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponsePreview))
		resp.Body.Close()
		transport.CloseIdleConnections()
		if err != nil {
			return FetchResult{}, safeSourceRequestError(err)
		}
		if len(body) > MaxSourceBytes {
			return FetchResult{}, fmt.Errorf("source exceeds %d bytes", MaxSourceBytes)
		}
		if err := validateSourceContentType(result.ContentType, body); err != nil {
			return FetchResult{}, err
		}
		if !utf8.Valid(body) {
			return FetchResult{}, errors.New("source response is not valid UTF-8")
		}
		hash := sha256.Sum256(body)
		result.Body = body
		result.SHA256 = hex.EncodeToString(hash[:])
		return result, nil
	}
}

func (f *SourceFetcher) Preview(ctx context.Context, rawURL string, options FetchOptions) (SourcePreview, error) {
	result, err := f.Fetch(ctx, rawURL, options)
	if err != nil {
		return SourcePreview{}, err
	}
	preview := SourcePreview{
		URL:          result.URL,
		StatusCode:   result.StatusCode,
		ETag:         result.ETag,
		LastModified: result.LastModified,
		ContentType:  result.ContentType,
		NotModified:  result.NotModified,
	}
	if result.NotModified {
		return preview, nil
	}
	prepared, err := PrepareSourceContent(result.Body)
	if err != nil {
		return SourcePreview{}, fmt.Errorf("source preview rejected: %s", boundedSourceError(err))
	}
	preview.PreparedSourceContent = prepared
	return preview, nil
}

func safeSourceRequestError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return errors.New("source request timed out")
	}
	if errors.Is(err, context.Canceled) {
		return errors.New("source request was canceled")
	}
	return errors.New("source request failed")
}

func boundedSourceError(err error) string {
	if err == nil {
		return "source rejected"
	}
	message := err.Error()
	if len(message) > 160 {
		message = message[:160] + "..."
	}
	return message
}

func (f *SourceFetcher) resolvePublicIPs(ctx context.Context, host string) ([]netip.Addr, error) {
	if ip, err := netip.ParseAddr(host); err == nil {
		if isForbiddenSourceIP(ip) {
			return nil, fmt.Errorf("source host resolves to forbidden address %s", ip)
		}
		return []netip.Addr{ip}, nil
	}
	ips, err := f.resolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, errors.New("source host could not be resolved")
	}
	if len(ips) == 0 {
		return nil, errors.New("source host has no addresses")
	}
	for _, ip := range ips {
		if isForbiddenSourceIP(ip) {
			return nil, fmt.Errorf("source host has forbidden address %s", ip)
		}
	}
	return ips, nil
}

func (f *SourceFetcher) dialPinned(ctx context.Context, network, port string, ips []netip.Addr) (net.Conn, error) {
	if port == "" {
		port = "443"
	}
	var lastErr error
	for _, ip := range ips {
		conn, err := f.dialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errors.New("no validated source addresses")
	}
	return nil, lastErr
}

func NormalizeSourceURL(raw string) (string, error) {
	if strings.ContainsAny(raw, "\r\n") {
		return "", errors.New("source URL contains control characters")
	}
	if strings.Contains(raw, "#") {
		return "", errors.New("source URL contains a fragment")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", errors.New("source URL is malformed")
	}
	if !strings.EqualFold(u.Scheme, "https") {
		return "", errors.New("source URL must use HTTPS")
	}
	if u.User != nil || u.Fragment != "" || u.Host == "" || u.Opaque != "" {
		return "", errors.New("source URL contains an unsupported authority or fragment")
	}
	if strings.HasSuffix(u.Host, ":") {
		return "", errors.New("source URL has an empty port")
	}
	if port := u.Port(); port != "" {
		value, err := strconv.Atoi(port)
		if err != nil || value < 1 || value > 65535 {
			return "", errors.New("source URL has an invalid port")
		}
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return "", errors.New("source URL has an empty host")
	}
	if strings.Contains(host, "%") {
		return "", errors.New("source URL has an invalid host")
	}
	if host == "github.com" && u.Port() == "" {
		return normalizeGitHubBlobURL(u)
	}
	u.Scheme = "https"
	u.Host = strings.ToLower(u.Host)
	return u.String(), nil
}

func normalizeGitHubBlobURL(u *url.URL) (string, error) {
	segments, err := escapedPathSegments(u.EscapedPath())
	if err != nil {
		return "", err
	}
	if len(segments) < 5 || segments[2] != "blob" || segments[0] == "" || segments[1] == "" {
		return "", errors.New("GitHub URL is not a blob URL")
	}
	for _, segment := range segments {
		if segment == "." || segment == ".." || segment == "" {
			return "", errors.New("GitHub URL contains an unsafe path")
		}
	}
	path := "/" + strings.Join(append(segments[:2], segments[3:]...), "/")
	return (&url.URL{Scheme: "https", Host: "raw.githubusercontent.com", Path: path, RawQuery: u.RawQuery}).String(), nil
}

func escapedPathSegments(escapedPath string) ([]string, error) {
	escapedPath = strings.TrimPrefix(escapedPath, "/")
	if escapedPath == "" {
		return nil, errors.New("source URL has an empty path")
	}
	parts := strings.Split(escapedPath, "/")
	segments := make([]string, 0, len(parts))
	for _, part := range parts {
		decoded, err := url.PathUnescape(part)
		if err != nil || strings.ContainsAny(decoded, "/\\") {
			return nil, errors.New("source URL has an invalid path segment")
		}
		segments = append(segments, decoded)
	}
	return segments, nil
}

func validateSourceContentType(value string, body []byte) error {
	if bytes.IndexByte(body, 0) >= 0 {
		return errors.New("source response contains binary NUL bytes")
	}
	if value == "" {
		if len(body) == 0 {
			return errors.New("source response has no content type or body")
		}
		value = http.DetectContentType(body)
	}
	mediaType, params, err := mime.ParseMediaType(value)
	if err != nil {
		return errors.New("source response has an invalid content type")
	}
	mediaType = strings.ToLower(mediaType)
	if charset := strings.ToLower(params["charset"]); charset != "" && charset != "utf-8" && charset != "utf8" {
		return errors.New("source response is not UTF-8 text")
	}
	switch mediaType {
	case "text/plain", "text/yaml", "text/x-yaml", "application/yaml", "application/x-yaml":
		return nil
	default:
		return fmt.Errorf("source response content type %q is not approved text/YAML", mediaType)
	}
}

var forbiddenSourcePrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.31.196.0/24"),
	netip.MustParsePrefix("192.52.193.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("192.175.48.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("255.255.255.255/32"),
	netip.MustParsePrefix("::/128"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("ff00::/8"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:30::/28"),
	netip.MustParsePrefix("2001:1::/32"),
	netip.MustParsePrefix("2001:20::/28"),
	netip.MustParsePrefix("2001:10::/28"),
	netip.MustParsePrefix("2001:0::/32"),
	netip.MustParsePrefix("2001:3::/32"),
	netip.MustParsePrefix("2001:4:112::/48"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("2001:2::/48"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("5f00::/16"),
	netip.MustParsePrefix("3fff::/20"),
	netip.MustParsePrefix("3ffe::/16"),
	netip.MustParsePrefix("5f00::/8"),
	netip.MustParsePrefix("2620:4f:8000::/48"),
}

var sourceIPv6GlobalUnicastPrefix = netip.MustParsePrefix("2000::/3")

func isForbiddenSourceIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsValid() {
		return true
	}
	if ip.Is4() {
		octets := ip.As4()
		if octets[0] < 1 || octets[0] > 223 {
			return true
		}
	} else if ip.Is6() {
		if !sourceIPv6GlobalUnicastPrefix.Contains(ip) {
			return true
		}
	} else {
		return true
	}
	for _, prefix := range forbiddenSourcePrefixes {
		if prefix.Contains(ip) {
			return true
		}
	}
	return false
}
