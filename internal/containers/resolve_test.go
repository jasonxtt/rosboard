package containers

import (
	"reflect"
	"strings"
	"testing"
)

func draftFixture() Draft {
	return Draft{DraftID: "abcd12", ImageSource: "registry", Image: "registry.example:5000/team/service", Network: Network{VETH: "new-veth", Bridge: "br-containers", Address: "172.20.0.8/24", Gateway: "172.20.0.1"}, Env: []Environment{}, Mounts: []Mount{}, StartAfterCreate: true, StartOnBoot: true, Logging: true, RestartPolicy: "no", Health: Health{Mode: "inherit"}}
}
func optionsFixture() Snapshot {
	return Snapshot{Items: []Item{}, Options: Options{Bridges: []string{"br-containers"}, Interfaces: []string{"ether1"}, UsedIPs: []string{"172.20.0.1/24"}, Disks: []Disk{{Name: "usb1", FreeBytes: 100, Writable: true}, {Name: "sata1", FreeBytes: 200, Writable: true}, {Name: "unmounted", FreeBytes: 1000, Writable: false}}, MemoryHigh: "256M", MemoryMax: "512M"}}
}
func TestResolveDefaultsAndExplicitOverrides(t *testing.T) {
	s := optionsFixture()
	d := draftFixture()
	r := Resolve(d, s)
	if len(r.Errors) > 0 {
		t.Fatal(r.Errors)
	}
	if r.Effective.Image != "registry.example:5000/team/service:latest" || r.Effective.Name != "service-abcd12" || r.Effective.RootDir != "/sata1/rosboard/containers/service-abcd12/rootfs" {
		t.Fatalf("wrong defaults: %+v", r.Effective)
	}
	for _, field := range []string{"cmd", "entrypoint", "user", "workdir", "cpu-list", "memory-high", "memory-max"} {
		if _, ok := r.ContainerFields[field]; ok {
			t.Fatalf("blank override %s was sent", field)
		}
	}
	if !strings.Contains(strings.Join(r.Defaults, ";"), "256M / 512M") {
		t.Fatal(r.Defaults)
	}
	if !reflect.DeepEqual(d.Network, r.Effective.Network) || !r.Effective.StartOnBoot || !r.Effective.StartAfterCreate || !r.Effective.Logging {
		t.Fatal("defaults changed explicit network/start")
	}
	d.Env = []Environment{{Key: "VALUE", Value: " space ' \" $() ; \\ \n 中文 "}}
	d.Mounts = []Mount{{Source: "/sata1/config", Target: "/etc/config", ReadOnly: true}}
	d.Command = "server --hello 'world'"
	d.MemoryMax = "128M"
	r = Resolve(d, s)
	if len(r.Errors) > 0 {
		t.Fatal(r.Errors)
	}
	if !reflect.DeepEqual(r.Effective.Env, d.Env) || !r.Effective.Mounts[0].ReadOnly || r.ContainerFields["cmd"] != d.Command || r.ContainerFields["memory-max"] != "128M" {
		t.Fatal("explicit configuration not preserved")
	}
}
func TestResolveNetworkAndRows(t *testing.T) {
	cases := []struct {
		name, field string
		change      func(*Draft, *Snapshot)
	}{
		{"missing image", "image", func(d *Draft, s *Snapshot) { d.Image = "" }},
		{"no defaults for network", "network.bridge", func(d *Draft, s *Snapshot) { d.Network = Network{} }},
		{"duplicate interface", "network.veth", func(d *Draft, s *Snapshot) { d.Network.VETH = "ether1" }},
		{"known conflict", "network.address", func(d *Draft, s *Snapshot) { d.Network.Address = "172.20.0.1/24" }},
		{"bad prefix", "network.address", func(d *Draft, s *Snapshot) { d.Network.Address = "172.20.0.8/99" }},
		{"outside gateway", "network.gateway", func(d *Draft, s *Snapshot) { d.Network.Gateway = "172.21.0.1" }},
		{"same gateway", "network.gateway", func(d *Draft, s *Snapshot) { d.Network.Gateway = "172.20.0.8" }},
		{"invalid ipv6", "network.address6", func(d *Draft, s *Snapshot) { d.Network.Address6 = "bad" }},
		{"multicast mac", "network.mac", func(d *Draft, s *Snapshot) { d.Network.MAC = "01:00:00:00:00:01" }},
		{"duplicate environment", "env.1", func(d *Draft, s *Snapshot) { d.Env = []Environment{{Key: "TZ"}, {Key: "TZ"}} }},
		{"invalid mount", "mounts.0", func(d *Draft, s *Snapshot) { d.Mounts = []Mount{{Source: "relative", Target: "/etc"}} }},
		{"bad memory", "memoryMax", func(d *Draft, s *Snapshot) { d.MemoryMax = "-20M" }},
		{"bad CPU", "cpuList", func(d *Draft, s *Snapshot) { d.CPUList = "everything" }},
		{"missing health command", "health.command", func(d *Draft, s *Snapshot) { d.Health.Mode = "override" }},
		{"no writable disk", "rootDir", func(d *Draft, s *Snapshot) { s.Options.Disks = nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, s := draftFixture(), optionsFixture()
			tc.change(&d, &s)
			if Resolve(d, s).Errors[tc.field] == "" {
				t.Fatalf("expected %s", tc.field)
			}
		})
	}
}
func TestEditPreservesExistingAndLocksSharedVETH(t *testing.T) {
	d := draftFixture()
	d.ExistingID = "*7"
	d.Name = "existing"
	d.RootDir = "/sata1/existing"
	d.StartOnBoot = false
	d.Logging = false
	d.RestartPolicy = "on-failure"
	d.MemoryHigh = "unlimited"
	d.Command = "run --exact"
	d.Health = Health{Mode: "override", Command: "test ready", Retries: "3"}
	s := optionsFixture()
	s.Items = []Item{{ID: "*7", Name: d.Name, Network: d.Network, Config: d, SharedVETH: []string{"other"}}}
	s.Options.Interfaces = append(s.Options.Interfaces, d.Network.VETH)
	s.Options.UsedIPs = append(s.Options.UsedIPs, d.Network.Address)
	r := Resolve(d, s)
	if len(r.Errors) > 0 {
		t.Fatal(r.Errors)
	}
	if !reflect.DeepEqual(r.Effective, d) {
		t.Fatal("edit reset existing values")
	}
	d.Network.Gateway = "172.20.0.254"
	if Resolve(d, s).Errors["network.veth"] == "" {
		t.Fatal("shared VETH modification allowed")
	}
}
func TestImageTagsAndDigests(t *testing.T) {
	for input, want := range map[string]string{"nginx": "nginx:latest", "nginx:stable": "nginx:stable", "registry:5000/nginx": "registry:5000/nginx:latest", "nginx@sha256:abc": "nginx@sha256:abc", "": ""} {
		if got := NormalizeImage(input); got != want {
			t.Fatalf("%q => %q", input, got)
		}
	}
}
