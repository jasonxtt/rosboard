package downloadnet

import (
	"bufio"
	"context"
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestProxyURLContract(t *testing.T) {
	for _, raw := range []string{"http://user:p%40ss@proxy.example:7890", "https://[::1]:443", "socks://u:p%3Aa@proxy.example:1080", "socks5://proxy.example:1080", "socks5h://proxy.example:1080", ""} {
		v, e := Normalize(raw)
		if e != nil {
			t.Fatalf("valid candidate refused: %v", e)
		}
		if strings.HasPrefix(raw, "socks") && !strings.HasPrefix(v, "socks5h://") {
			t.Fatal("alias not remote-DNS SOCKS5")
		}
	}
	for _, raw := range []string{"ftp://proxy:21", "http://proxy:0", "http://proxy:65536", "http://proxy:80/path", "http://proxy:80?x", "http://proxy:80#x", "http://u:p:a@proxy:80", "http://u:p@x@proxy:80", "http://u:xxxxx@proxy:80", "http://u:p%0A@proxy:80", "http://proxy:80\n", "http://bad_host:80"} {
		if _, e := Normalize(raw); e == nil {
			t.Fatal("invalid candidate accepted")
		}
	}
	display, password := Display("http://user:private-fixture@proxy:80")
	if !password || strings.Contains(display, "private-fixture") {
		t.Fatal("proxy secret exposed")
	}
}
func TestHTTPAndHTTPSProxyAuthDoesNotReachOrigin(t *testing.T) {
	for _, secure := range []bool{false, true} {
		t.Run(map[bool]string{false: "http", true: "https"}[secure], func(t *testing.T) {
			var origins, proxied atomic.Int32
			origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				origins.Add(1)
				if r.Header.Get("Proxy-Authorization") != "" {
					t.Error("proxy credentials reached origin")
				}
				w.Write([]byte("ok"))
			}))
			defer origin.Close()
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				proxied.Add(1)
				if r.Method != "CONNECT" || r.Header.Get("Proxy-Authorization") != "Basic "+base64.StdEncoding.EncodeToString([]byte("user:p@ss")) {
					t.Error("proxy auth missing")
				}
				dst, e := net.Dial("tcp", origin.Listener.Addr().String())
				if e != nil {
					t.Error(e)
					return
				}
				conn, _, e := w.(http.Hijacker).Hijack()
				if e != nil {
					dst.Close()
					return
				}
				conn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
				go func() { io.Copy(dst, conn); dst.Close() }()
				go func() { io.Copy(conn, dst); conn.Close() }()
			})
			var proxy *httptest.Server
			if secure {
				proxy = httptest.NewTLSServer(handler)
			} else {
				proxy = httptest.NewServer(handler)
			}
			defer proxy.Close()
			u, _ := url.Parse(proxy.URL)
			u.User = url.UserPassword("user", "p@ss")
			snapshot, e := New(u.String())
			if e != nil {
				t.Fatal(e)
			}
			defer snapshot.Transport.CloseIdleConnections()
			snapshot.Transport.TLSClientConfig = origin.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
			resp, e := (&http.Client{Transport: snapshot.Transport, Timeout: 3 * time.Second}).Get(origin.URL)
			if e != nil {
				t.Fatal(e)
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if origins.Load() != 1 || proxied.Load() != 1 {
				t.Fatal("proxy bypassed")
			}
		})
	}
}
func TestExplicitDirectAndNoProxyFailureFallback(t *testing.T) {
	var hits atomic.Int32
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	defer origin.Close()
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("NO_PROXY", "*")
	direct, e := New("")
	if e != nil {
		t.Fatal(e)
	}
	defer direct.Transport.CloseIdleConnections()
	resp, e := (&http.Client{Transport: direct.Transport}).Get(origin.URL)
	if e != nil {
		t.Fatal("environment changed explicit direct transport")
	}
	resp.Body.Close()
	badProxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(502) }))
	defer badProxy.Close()
	p, _ := New(badProxy.URL)
	defer p.Transport.CloseIdleConnections()
	resp, e = (&http.Client{Transport: p.Transport}).Get(origin.URL)
	if e != nil {
		t.Fatal(e)
	}
	resp.Body.Close()
	if resp.StatusCode != 502 || hits.Load() != 1 {
		t.Fatal("failed explicit proxy bypassed")
	}
}
func TestSOCKS5UsesRemoteDNSAndURLCredentials(t *testing.T) {
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) }))
	defer origin.Close()
	ln, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer ln.Close()
	seen := make(chan string, 1)
	go func() {
		c, e := ln.Accept()
		if e != nil {
			return
		}
		defer c.Close()
		c.SetDeadline(time.Now().Add(5 * time.Second))
		r := bufio.NewReader(c)
		head := make([]byte, 2)
		io.ReadFull(r, head)
		methods := make([]byte, int(head[1]))
		io.ReadFull(r, methods)
		c.Write([]byte{5, 2})
		io.ReadFull(r, head)
		user := make([]byte, int(head[1]))
		io.ReadFull(r, user)
		n, _ := r.ReadByte()
		pass := make([]byte, int(n))
		io.ReadFull(r, pass)
		if string(user) != "u" || string(pass) != "p:a" {
			seen <- "auth mismatch"
			return
		}
		c.Write([]byte{1, 0})
		header := make([]byte, 4)
		io.ReadFull(r, header)
		if header[3] != 3 {
			seen <- "local DNS"
			return
		}
		n, _ = r.ReadByte()
		host := make([]byte, int(n))
		io.ReadFull(r, host)
		port := make([]byte, 2)
		io.ReadFull(r, port)
		seen <- string(host)
		dst, e := net.Dial("tcp", origin.Listener.Addr().String())
		if e != nil {
			return
		}
		defer dst.Close()
		c.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 0})
		go io.Copy(dst, r)
		io.Copy(c, dst)
	}()
	snapshot, e := New("socks://u:p%3Aa@" + ln.Addr().String())
	if e != nil {
		t.Fatal(e)
	}
	defer snapshot.Transport.CloseIdleConnections()
	snapshot.Transport.TLSClientConfig = origin.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	snapshot.Transport.TLSClientConfig.ServerName = origin.Certificate().DNSNames[0]
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://never-resolve.invalid/", nil)
	resp, e := (&http.Client{Transport: snapshot.Transport}).Do(req)
	if e != nil {
		t.Fatal(e)
	}
	resp.Body.Close()
	if <-seen != "never-resolve.invalid" {
		t.Fatal("destination name not forwarded to proxy")
	}
}
