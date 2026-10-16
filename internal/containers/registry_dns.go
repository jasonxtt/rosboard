package containers

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/dns/dnsmessage"
	"rosboard/internal/config"
	"rosboard/internal/routeros"
)

// DNSLookup queries the selected router, never the host's default resolver.
// CNAME TTLs participate in refresh; canonical names are not queried separately.
type DNSLookup func(context.Context, string, string, dnsmessage.Type) (time.Duration, []string, error)
type registryDNSRuntime struct {
	mu       sync.Mutex
	sessions map[string]*registryDNSSession
}
type registryDNSSession struct {
	cancel context.CancelFunc
	done   chan struct{}
}

func dnsHostname(value string) string {
	value = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(value), "."))
	if value == "" || len(value) > 253 || net.ParseIP(value) != nil {
		return ""
	}
	for _, label := range strings.Split(value, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return ""
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return ""
			}
		}
	}
	return value
}
func registryHost(image, configured, assumed string) string {
	head := strings.SplitN(strings.TrimSpace(image), "/", 2)
	if len(head) == 2 && (strings.ContainsAny(head[0], ".:") || head[0] == "localhost") {
		parsed, err := url.Parse("https://" + head[0])
		if err != nil || parsed.User != nil {
			return ""
		}
		return parsed.Hostname()
	}
	value := first(configured, first(assumed, "https://registry-1.docker.io"))
	if !strings.Contains(value, "://") {
		value = "https://" + value
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return ""
	}
	return parsed.Hostname()
}
func registryDomains(host string) []string {
	host = dnsHostname(host)
	switch host {
	case "docker.io", "index.docker.io", "registry-1.docker.io":
		return []string{"registry-1.docker.io", "auth.docker.io", "production.cloudflare.docker.com", "production.cloudfront.docker.com"}
	case "ghcr.io":
		return []string{"ghcr.io", "pkg-containers.githubusercontent.com"}
	case "":
		return nil
	default:
		return []string{host}
	}
}

func queryRouterDNS(ctx context.Context, server, host string, typ dnsmessage.Type) (time.Duration, []string, error) {
	name, err := dnsmessage.NewName(host + ".")
	if err != nil {
		return 0, nil, err
	}
	var idBytes [2]byte
	if _, err = rand.Read(idBytes[:]); err != nil {
		return 0, nil, err
	}
	id := binary.BigEndian.Uint16(idBytes[:])
	q := dnsmessage.Question{Name: name, Type: typ, Class: dnsmessage.ClassINET}
	request, err := (&dnsmessage.Message{Header: dnsmessage.Header{ID: id, RecursionDesired: true}, Questions: []dnsmessage.Question{q}}).Pack()
	if err != nil {
		return 0, nil, err
	}
	exchange := func(network string) ([]byte, error) {
		conn, e := (&net.Dialer{}).DialContext(ctx, network, server)
		if e != nil {
			return nil, e
		}
		defer conn.Close()
		deadline := time.Now().Add(2 * time.Second)
		if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
			deadline = d
		}
		if e = conn.SetDeadline(deadline); e != nil {
			return nil, e
		}
		stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
		defer stop()
		payload := request
		if network == "tcp" {
			payload = make([]byte, 2+len(request))
			binary.BigEndian.PutUint16(payload, uint16(len(request)))
			copy(payload[2:], request)
		}
		if _, e = conn.Write(payload); e != nil {
			return nil, e
		}
		if network == "tcp" {
			var size [2]byte
			if _, e = io.ReadFull(conn, size[:]); e != nil {
				return nil, e
			}
			result := make([]byte, int(binary.BigEndian.Uint16(size[:])))
			_, e = io.ReadFull(conn, result)
			return result, e
		}
		result := make([]byte, 4096)
		n, e := conn.Read(result)
		return result[:n], e
	}
	response, err := exchange("udp")
	if err != nil {
		return 0, nil, err
	}
	if len(response) >= 4 && binary.BigEndian.Uint16(response[:2]) == id && binary.BigEndian.Uint16(response[2:4])&0x200 != 0 {
		response, err = exchange("tcp")
		if err != nil {
			return 0, nil, err
		}
	}
	var message dnsmessage.Message
	if err = message.Unpack(response); err != nil {
		return 0, nil, err
	}
	if !message.Response || message.ID != id || message.Truncated || len(message.Questions) != 1 || message.Questions[0] != q {
		return 0, nil, errors.New("invalid DNS reply")
	}
	if message.RCode != dnsmessage.RCodeSuccess && message.RCode != dnsmessage.RCodeNameError {
		return 0, nil, errors.New("DNS query failed")
	}
	ttl := time.Duration(0)
	seenTTL := false
	aliases := []string{}
	for _, answer := range append(message.Answers, message.Authorities...) {
		if answer.Header.Type != typ && answer.Header.Type != dnsmessage.TypeCNAME && answer.Header.Type != dnsmessage.TypeSOA {
			continue
		}
		value := time.Duration(answer.Header.TTL) * time.Second
		if soa, ok := answer.Body.(*dnsmessage.SOAResource); ok {
			minimum := time.Duration(soa.MinTTL) * time.Second
			if minimum < value {
				value = minimum
			}
		}
		if !seenTTL || value < ttl {
			ttl = value
			seenTTL = true
		}
		if cname, ok := answer.Body.(*dnsmessage.CNAMEResource); ok {
			if h := dnsHostname(cname.CNAME.String()); h != "" {
				aliases = append(aliases, h)
			}
		}
	}
	if !seenTTL {
		ttl = 30 * time.Second
	}
	return ttl, aliases, nil
}

func (s *Service) registryDNSRound(ctx context.Context, server string, domains []string) (time.Duration, []string, bool) {
	type result struct {
		ttl     time.Duration
		aliases []string
		err     error
	}
	results := make(chan result, len(domains)*2)
	limit := make(chan struct{}, 8)
	var tasks sync.WaitGroup
	for _, host := range domains {
		for _, typ := range []dnsmessage.Type{dnsmessage.TypeA, dnsmessage.TypeAAAA} {
			tasks.Add(1)
			go func(host string, typ dnsmessage.Type) {
				defer tasks.Done()
				select {
				case limit <- struct{}{}:
				case <-ctx.Done():
					results <- result{err: ctx.Err()}
					return
				}
				defer func() { <-limit }()
				qctx, cancel := context.WithTimeout(ctx, 2*time.Second)
				defer cancel()
				ttl, aliases, err := s.RegistryDNSLookup(qctx, server, host, typ)
				results <- result{ttl, aliases, err}
			}(host, typ)
		}
	}
	tasks.Wait()
	close(results)
	refresh := 30 * time.Second
	aliases := []string{}
	failed := false
	for r := range results {
		if r.err != nil {
			failed = true
			continue
		}
		interval := r.ttl / 2
		if interval < time.Second {
			interval = time.Second
		}
		if interval < refresh {
			refresh = interval
		}
		aliases = append(aliases, r.aliases...)
	}
	return refresh, aliases, failed
}
func (s *Service) registryDNSWarning(n *nativeDevice, r *nativeRecord) {
	n.mu.Lock()
	defer n.mu.Unlock()
	warning := "部分镜像域名 DNS 查询未完成，将继续下载；现有路由策略保持不变。"
	if !contains(r.Job.Warnings, warning) {
		r.Job.Warnings = append(r.Job.Warnings, warning)
		// The next native journal checkpoint persists this warning.
	}
}

func (s *Service) beginRegistryDNS(d config.DeviceConfig, n *nativeDevice, r *nativeRecord, step nativeStep) {
	if s.RegistryDNSLookup == nil || step.Menu != routeros.ContainerList || step.Command != "add" || step.Fields["remote-image"] == "" || step.Fields["file"] != "" {
		return
	}
	key := nativeKey(d) + ":" + r.Job.ID
	s.registryDNS.mu.Lock()
	if s.registryDNS.sessions == nil {
		s.registryDNS.sessions = map[string]*registryDNSSession{}
	}
	if s.registryDNS.sessions[key] != nil {
		s.registryDNS.mu.Unlock()
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 24*time.Hour)
	session := &registryDNSSession{cancel: cancel, done: make(chan struct{})}
	s.registryDNS.sessions[key] = session
	s.registryDNS.mu.Unlock()
	finish := func() {
		cancel()
		s.registryDNS.mu.Lock()
		if s.registryDNS.sessions[key] == session {
			delete(s.registryDNS.sessions, key)
		}
		s.registryDNS.mu.Unlock()
		close(session.done)
	}
	parsed, err := url.Parse(d.RouterOS.BaseURL)
	if err != nil || parsed.Hostname() == "" {
		s.registryDNSWarning(n, r)
		finish()
		return
	}
	host := registryHost(step.Fields["remote-image"], "", "")
	firstPart := strings.SplitN(step.Fields["remote-image"], "/", 2)
	explicit := len(firstPart) == 2 && (strings.ContainsAny(firstPart[0], ".:") || firstPart[0] == "localhost")
	if !explicit {
		readctx, readCancel := context.WithTimeout(ctx, 4*time.Second)
		configs, readErr := s.ReaderFor(d).ContainerRead(readctx, routeros.ContainerRegistryConfig)
		readCancel()
		if readErr != nil {
			s.registryDNSWarning(n, r)
			finish()
			return
		}
		if len(configs) > 0 {
			host = registryHost(step.Fields["remote-image"], configs[0]["registry-url"], configs[0]["assumed-registry-url"])
		}
	}
	domains := registryDomains(host)
	if len(domains) == 0 {
		finish()
		return
	}
	server := net.JoinHostPort(parsed.Hostname(), "53")
	roundctx, roundCancel := context.WithTimeout(ctx, 5*time.Second)
	refresh, _, failed := s.registryDNSRound(roundctx, server, domains)
	roundCancel()
	if failed && ctx.Err() == nil {
		s.registryDNSWarning(n, r)
	}
	// Refresh the original names using the minimum A/AAAA/CNAME TTL. Querying
	// arbitrary canonical names separately could select another DNS policy.
	go func() {
		defer finish()
		seenImport := false
		for {
			timer := time.NewTimer(refresh)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			n.mu.Lock()
			terminal := r.Job.State == "succeeded" || r.Job.State == "failed"
			n.mu.Unlock()
			if terminal {
				return
			}
			readctx, readCancel := context.WithTimeout(ctx, 4*time.Second)
			rows, readErr := s.ReaderFor(d).ContainerRead(readctx, routeros.ContainerList)
			readCancel()
			if readErr == nil {
				matches := findRows(rows, step.Match)
				if len(matches) == 1 {
					seenImport = true
					status := containerStatus(matches[0])
					if status == "stopped" || status == "running" || status == "error" {
						return
					}
				} else if seenImport {
					return
				}
			}
			roundctx, roundCancel := context.WithTimeout(ctx, 5*time.Second)
			var failed bool
			refresh, _, failed = s.registryDNSRound(roundctx, server, domains)
			roundCancel()
			if failed && ctx.Err() == nil {
				s.registryDNSWarning(n, r)
			}
		}
	}()
}
func (s *Service) stopRegistryDNS(d config.DeviceConfig, jobID string) {
	key := nativeKey(d) + ":" + jobID
	s.registryDNS.mu.Lock()
	session := s.registryDNS.sessions[key]
	s.registryDNS.mu.Unlock()
	if session != nil {
		session.cancel()
		<-session.done
	}
}
