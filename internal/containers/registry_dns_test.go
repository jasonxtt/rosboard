package containers

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
	"rosboard/internal/config"
	"rosboard/internal/routeros"
)

func TestRegistrySelectionAndDomains(t *testing.T) {
	for _, tc := range []struct{ image, configured, assumed, want string }{
		{"nginx:alpine", "", "", "registry-1.docker.io"},
		{"owner/service:latest", "https://ghcr.io/v2/", "", "ghcr.io"},
		{"owner/service", "", "https://mirror.example:5443/v2/", "mirror.example"},
		{"docker.io/library/nginx:alpine", "https://ghcr.io", "", "docker.io"},
		{"ghcr.io/owner/service:tag", "https://registry-1.docker.io", "", "ghcr.io"},
		{"registry.example:5443/app:1", "", "", "registry.example"},
		{"localhost:5000/app", "", "", "localhost"},
		{"[2001:db8::1]:5000/app", "", "", "2001:db8::1"},
		{"nginx", "https://user:private@registry.example", "", ""},
	} {
		if got := registryHost(tc.image, tc.configured, tc.assumed); got != tc.want {
			t.Fatalf("%s host=%q want=%q", tc.image, got, tc.want)
		}
	}
	docker := registryDomains("DOCKER.IO.")
	if len(docker) != 4 || !contains(docker, "auth.docker.io") || !contains(docker, "production.cloudfront.docker.com") {
		t.Fatal(docker)
	}
	ghcr := registryDomains("ghcr.io")
	if len(ghcr) != 2 || !contains(ghcr, "pkg-containers.githubusercontent.com") {
		t.Fatal(ghcr)
	}
	for _, h := range []string{"1.2.3.4", "2001:db8::1", "evil/name", "*.example", "bad..example", "-bad.example"} {
		if len(registryDomains(h)) != 0 {
			t.Fatal("invalid DNS name", h)
		}
	}

}

func TestDNSWireTCPFallbackTTLAndResponseValidation(t *testing.T) {
	tcp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer tcp.Close()
	udp, err := net.ListenPacket("udp", tcp.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	var tcpCount atomic.Int32
	go func() {
		for {
			buf := make([]byte, 4096)
			n, addr, e := udp.ReadFrom(buf)
			if e != nil {
				return
			}
			var q dnsmessage.Message
			if q.Unpack(buf[:n]) != nil {
				continue
			}
			q.Header = dnsmessage.Header{ID: q.ID, Response: true, Truncated: true}
			data, _ := q.Pack()
			_, _ = udp.WriteTo(data, addr)
		}
	}()
	go func() {
		for {
			conn, e := tcp.Accept()
			if e != nil {
				return
			}
			func() {
				defer conn.Close()
				tcpCount.Add(1)
				var size [2]byte
				if _, e = io.ReadFull(conn, size[:]); e != nil {
					return
				}
				buf := make([]byte, int(binary.BigEndian.Uint16(size[:])))
				if _, e = io.ReadFull(conn, buf); e != nil {
					return
				}
				var q dnsmessage.Message
				if q.Unpack(buf) != nil {
					return
				}
				q.Header = dnsmessage.Header{ID: q.ID, Response: true}
				alias, _ := dnsmessage.NewName("cdn.example.")
				q.Answers = []dnsmessage.Resource{{Header: dnsmessage.ResourceHeader{Name: q.Questions[0].Name, Type: dnsmessage.TypeCNAME, Class: dnsmessage.ClassINET, TTL: 8}, Body: &dnsmessage.CNAMEResource{CNAME: alias}}, {Header: dnsmessage.ResourceHeader{Name: alias, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET, TTL: 20}, Body: &dnsmessage.AResource{A: [4]byte{192, 0, 2, 1}}}}
				data, _ := q.Pack()
				binary.BigEndian.PutUint16(size[:], uint16(len(data)))
				_, _ = conn.Write(append(size[:], data...))
			}()
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	ttl, aliases, err := queryRouterDNS(ctx, tcp.Addr().String(), "registry.example", dnsmessage.TypeA)
	if err != nil || ttl != 8*time.Second || len(aliases) != 1 || aliases[0] != "cdn.example" || tcpCount.Load() != 1 {
		t.Fatal(ttl, aliases, err, tcpCount.Load())
	}
	bad, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer bad.Close()
	go func() {
		buf := make([]byte, 4096)
		n, a, e := bad.ReadFrom(buf)
		if e == nil {
			buf[1] ^= 1
			_, _ = bad.WriteTo(buf[:n], a)
		}
	}()
	if _, _, err = queryRouterDNS(ctx, bad.LocalAddr().String(), "registry.example", dnsmessage.TypeA); err == nil {
		t.Fatal("accepted mismatched DNS response")
	}
}

func TestDNSRoundFailuresAreBoundedAndKeepShortTTL(t *testing.T) {
	service := &Service{RegistryDNSLookup: func(_ context.Context, server, name string, typ dnsmessage.Type) (time.Duration, []string, error) {
		if server != "192.0.2.1:53" {
			t.Error(server)
		}
		if typ == dnsmessage.TypeAAAA {
			return 0, nil, errors.New("fixture unavailable")
		}
		return 2 * time.Second, []string{"cdn.example"}, nil
	}}
	refresh, aliases, failed := service.registryDNSRound(context.Background(), "192.0.2.1:53", []string{"registry.example"})
	if refresh != time.Second || !failed || len(aliases) != 1 {
		t.Fatal(refresh, aliases, failed)
	}
}

type dnsOrderedWriter struct {
	*nativeFake
	calls *atomic.Int32
	t     *testing.T
}

func (w dnsOrderedWriter) ContainerCommand(ctx context.Context, m routeros.ContainerMenu, command string, fields routeros.RouterOSFields) error {
	if m == routeros.ContainerList && command == "add" && w.calls.Load() < 8 {
		w.t.Error("container import preceded A/AAAA warming")
	}
	return w.nativeFake.ContainerCommand(ctx, m, command, fields)
}
func TestNativeRegistryPreparationPrecedesAddAndReplayDoesNotQuery(t *testing.T) {
	s, d, f := nativeFixture(t)
	d.RouterOS.BaseURL = "http://192.0.2.20:8080"
	var calls atomic.Int32
	s.RegistryDNSLookup = func(_ context.Context, server, name string, typ dnsmessage.Type) (time.Duration, []string, error) {
		if server != "192.0.2.20:53" {
			t.Error(server)
		}
		calls.Add(1)
		return time.Minute, nil, nil
	}
	s.NativeWriterFor = func(config.DeviceConfig) NativeWriter { return dnsOrderedWriter{f, &calls, t} }
	draft := draftFixture()
	draft.Image = "docker.io/library/nginx:alpine"
	req := ActionRequest{Action: "create", RequestID: "dns-create-test", Draft: draft}
	job := nativeSubmit(t, s, d, req)
	if calls.Load() != 8 {
		t.Fatal(calls.Load())
	}
	nativeSubmit(t, s, d, req)
	if calls.Load() != 8 {
		t.Fatal("replay started DNS worker")
	}
	s.registryDNS.mu.Lock()
	active := len(s.registryDNS.sessions)
	s.registryDNS.mu.Unlock()
	if active != 0 {
		t.Fatal("finished job retained DNS worker", job)
	}
}

func TestRegistryRefreshRestoresPendingImportAndStopsOnCompletion(t *testing.T) {
	s, d, f := nativeFixture(t)
	d.RouterOS.BaseURL = "http://192.0.2.21"
	n, err := s.nativeDevice(d)
	if err != nil {
		t.Fatal(err)
	}
	step := addStep(routeros.ContainerList, map[string]string{"remote-image": "ghcr.io/test/image:1", "name": "dns-test", "comment": "dns-fixture"})
	r := &nativeRecord{Job: Job{ID: "dns-restored", DeviceID: d.ID, State: "running"}, Pending: true, Steps: []nativeStep{step}}
	n.mu.Lock()
	n.state.Jobs[r.Job.ID] = r
	n.state.Latest = r.Job.ID
	err = n.save()
	n.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	f.rows[routeros.ContainerList] = []routeros.RouterOSObject{{".id": "*A", "name": "dns-test", "comment": "dns-fixture", "downloading": "true"}}
	restored := NewServiceWithState(s.NativeStateDir)
	restored.ReaderFor = s.ReaderFor
	var calls atomic.Int32
	restored.RegistryDNSLookup = func(_ context.Context, server, name string, typ dnsmessage.Type) (time.Duration, []string, error) {
		calls.Add(1)
		return 2 * time.Second, nil, nil
	}
	if err = restored.RestoreNative(d); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for calls.Load() < 8 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if calls.Load() < 8 {
		t.Fatal("no initial/TTL refresh", calls.Load())
	}
	restored.registryDNS.mu.Lock()
	session := restored.registryDNS.sessions[nativeKey(d)+":"+r.Job.ID]
	restored.registryDNS.mu.Unlock()
	if session == nil {
		t.Fatal("pending download lost worker")
	}
	loaded, _ := restored.nativeDevice(d)
	loaded.mu.Lock()
	job := cloneJob(loaded.state.Jobs[r.Job.ID].Job)
	loaded.mu.Unlock()
	if job.State != "unknown" || f.writes != 0 {
		t.Fatal("restore executed native write", job, f.writes)
	}
	restored.beginRegistryDNS(d, loaded, loaded.state.Jobs[r.Job.ID], step)
	f.mu.Lock()
	delete(f.rows[routeros.ContainerList][0], "downloading")
	f.rows[routeros.ContainerList][0]["stopped"] = "true"
	f.mu.Unlock()
	select {
	case <-session.done:
	case <-time.After(3 * time.Second):
		restored.stopRegistryDNS(d, r.Job.ID)
		t.Fatal("completed import did not stop DNS refresh")
	}
	count := calls.Load()
	time.Sleep(30 * time.Millisecond)
	if calls.Load() != count {
		t.Fatal("queries after completion")
	}
}

func TestArchiveAndLifecycleSkipDNSAndFailuresDoNotAbortImport(t *testing.T) {
	s, d, f := nativeFixture(t)
	d.RouterOS.BaseURL = "http://192.0.2.22"
	n, _ := s.nativeDevice(d)
	var calls atomic.Int32
	s.RegistryDNSLookup = func(context.Context, string, string, dnsmessage.Type) (time.Duration, []string, error) {
		calls.Add(1)
		return 0, nil, errors.New("fixture secret must not be exposed")
	}
	r := &nativeRecord{Job: Job{ID: "skip", State: "running"}}
	for _, step := range []nativeStep{addStep(routeros.ContainerList, map[string]string{"file": "sata1/image.tar"}), actionStep("start", nil), {Menu: routeros.ContainerList, Command: "set", Fields: map[string]string{"remote-image": "nginx"}}} {
		s.beginRegistryDNS(d, n, r, step)
	}
	if calls.Load() != 0 {
		t.Fatal("non-registry operation queried DNS")
	}
	s.NativeWriterFor = func(config.DeviceConfig) NativeWriter { return f }
	job := nativeSubmit(t, s, d, ActionRequest{Action: "create", RequestID: "dns-unavailable-test", Draft: draftFixture()})
	if job.State != "succeeded" || len(job.Warnings) != 1 {
		t.Fatal("best-effort DNS failed native operation", job)
	}
}
