// Package downloadnet owns the explicit, immutable outbound download transport.
// RouterOS control and monitoring connections deliberately do not use it.
package downloadnet

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/net/proxy"
)

var ErrInvalidProxy = errors.New("代理 URL 无效；请输入 HTTP(S) 或 SOCKS5 地址，不含路径、查询或片段")

func Normalize(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	if len(raw) > 4096 || strings.TrimSpace(raw) != raw || strings.ContainsFunc(raw, unicode.IsControl) || strings.ContainsAny(raw, " \\#?") {
		return "", ErrInvalidProxy
	}
	u, err := url.Parse(raw)
	if err != nil || u.Opaque != "" || u.Hostname() == "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return "", ErrInvalidProxy
	}
	switch u.Scheme {
	case "socks", "socks5", "socks5h":
		u.Scheme = "socks5h"
	case "http", "https":
	default:
		return "", ErrInvalidProxy
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port < 1 || port > 65535 {
		return "", ErrInvalidProxy
	}
	host := u.Hostname()
	if strings.Contains(host, ":") {
		if net.ParseIP(host) == nil {
			return "", ErrInvalidProxy
		}
	} else {
		for _, label := range strings.Split(host, ".") {
			if label == "" || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
				return "", ErrInvalidProxy
			}
			for _, r := range label {
				if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-') {
					return "", ErrInvalidProxy
				}
			}
		}
	}
	if u.User != nil {
		p, has := u.User.Password()
		user := u.User.Username()
		if user == "" || !has || p == "" || strings.ContainsFunc(user+p, unicode.IsControl) || p == "xxxxx" || p == "********" || strings.Contains(p, "••") {
			return "", ErrInvalidProxy
		}
		// Raw reserved separators must be percent-encoded within userinfo.
		authority := raw[strings.Index(raw, "://")+3:]
		if idx := strings.LastIndex(authority, "@"); idx >= 0 {
			if strings.Count(authority[:idx], "@") > 0 || strings.Count(authority[:idx], ":") != 1 {
				return "", ErrInvalidProxy
			}
		}
	}
	u.Host = net.JoinHostPort(strings.ToLower(host), strconv.Itoa(port))
	return u.String(), nil
}

func Revision(raw string) string {
	sum := sha256.Sum256([]byte("rosboard-download-v1\x00" + raw))
	return hex.EncodeToString(sum[:])
}
func Display(raw string) (string, bool) {
	u, e := url.Parse(raw)
	if e != nil || raw == "" {
		return "", false
	}
	return u.Redacted(), u.User != nil
}

type Snapshot struct {
	Revision  string
	Transport *http.Transport
}

func New(raw string) (Snapshot, error) {
	value, err := Normalize(raw)
	if err != nil {
		return Snapshot{}, err
	}
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.Proxy = nil // Never inherit HTTP_PROXY, HTTPS_PROXY or NO_PROXY.
	t.DialContext = (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	t.ResponseHeaderTimeout = 30 * time.Second
	t.TLSHandshakeTimeout = 15 * time.Second
	t.MaxConnsPerHost = 4
	if value != "" {
		u, _ := url.Parse(value)
		if u.Scheme == "socks5h" {
			var auth *proxy.Auth
			if u.User != nil {
				p, _ := u.User.Password()
				auth = &proxy.Auth{User: u.User.Username(), Password: p}
			}
			dialer, e := proxy.SOCKS5("tcp", u.Host, auth, &net.Dialer{Timeout: 15 * time.Second})
			if e != nil {
				return Snapshot{}, ErrInvalidProxy
			}
			cd, ok := dialer.(proxy.ContextDialer)
			if !ok {
				return Snapshot{}, ErrInvalidProxy
			}
			t.DialContext = cd.DialContext
		} else {
			t.Proxy = http.ProxyURL(u)
		}
	}
	return Snapshot{Revision: Revision(value), Transport: t}, nil
}

type TestResult struct {
	Updates        bool `json:"updates"`
	Registry       bool `json:"registry"`
	Authentication bool `json:"authentication"`
}

func Test(ctx context.Context, s Snapshot) TestResult {
	client := &http.Client{Transport: s.Transport, Timeout: 12 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) > 3 || r.URL.Scheme != "https" || r.URL.User != nil {
			return errors.New("invalid redirect")
		}
		return nil
	}}
	check := func(address string, accept401 bool) bool {
		req, _ := http.NewRequestWithContext(ctx, "GET", address, nil)
		req.Header.Set("User-Agent", "rosboard-download-test")
		resp, e := client.Do(req)
		if e != nil {
			return false
		}
		defer resp.Body.Close()
		return resp.StatusCode >= 200 && resp.StatusCode < 300 || accept401 && resp.StatusCode == 401
	}
	return TestResult{Updates: check("https://api.github.com/repos/jasonxtt/rosboard/releases/latest", false), Registry: check("https://registry-1.docker.io/v2/", true), Authentication: check("https://auth.docker.io/token?service=registry.docker.io&scope=repository:library/alpine:pull", false)}
}
