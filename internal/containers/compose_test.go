package containers

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestComposeMSMImport(t *testing.T) {
	r, err := ParseCompose(`services:
  msm:
    image: msmbox/msm:1.3.0
    container_name: custom-msm
    restart: unless-stopped
    cap_add: [NET_ADMIN, NET_RAW]
    devices: [/dev/net/tun:/dev/net/tun]
    sysctls: {net.ipv4.ip_forward: 1}
    ports: ["7777:7777", "53:53/udp"]
    environment:
      JWT_SECRET: fixture-placeholder-not-a-real-secret
      MSM_CONFIG_DIR: /opt/msm
      TZ: Asia/Shanghai
    volumes: [/opt/msm:/opt/msm]
`)
	if err != nil || len(r.Services) != 1 {
		t.Fatal("MSM source rejected", err)
	}
	s := r.Services[0]
	if s.Fields.Name != "custom-msm" || s.Fields.Image != "msmbox/msm:1.3.0" || s.Fields.RestartPolicy != "no" || len(s.Fields.Env) != 3 || len(s.Fields.Mounts) != 1 {
		t.Fatal("incorrect basic conversion")
	}
	m := s.Fields.Mounts[0]
	if m.Source != "" || m.Target != "/opt/msm" || m.ReadOnly || s.MountHints[0] != "/opt/msm" || len(s.Skipped) != 5 || len(s.NeedsInput) != 1 {
		t.Fatal("unsafe mount, dropped notices or invented equivalent options")
	}
}

func TestComposeConversions(t *testing.T) {
	r, err := ParseCompose(`version: "3.9"
services:
  app:
    image: example/app:v2
    user: 1000
    working_dir: /app
    command: /bin/app --listen=:80
    entrypoint: /entrypoint
    cpuset: "0,2-3"
    mem_limit: 128mb
    mem_reservation: 67108864
    restart: on-failure
    environment:
      - EMPTY=
      - EQUALS=a=b
      - LITERAL=$$NAME
      - MISSING
      - UNRESOLVED=${TOKEN:-default}
    volumes:
      - ./config:/config:ro
      - named:/data:rw
      - /cache
      - type: bind
        source: /host
        target: /readonly
        read_only: true
        bind: {propagation: rshared}
  db:
    image: example/db
    environment: {NUMBER: 42, FLAG: false, TEXT: "false", NULL_VALUE: null}
volumes: {named: {}}
`)
	if err != nil || len(r.Services) != 2 {
		t.Fatal("source rejected", err)
	}
	s := r.Services[0]
	if s.Name != "app" || s.Fields.User != "1000" || s.Fields.MemoryMax != "134217728" || s.Fields.MemoryHigh != "67108864" || s.Fields.CPUList != "0,2-3" || s.Fields.RestartPolicy != "on-failure" || s.Fields.Command == "" || s.Fields.Entrypoint == "" {
		t.Fatal("fields were not converted")
	}
	if len(s.Fields.Env) != 3 || s.Fields.Env[0].Value != "" || s.Fields.Env[1].Value != "a=b" || s.Fields.Env[2].Value != "$NAME" {
		t.Fatal("environment values changed")
	}
	if len(s.Fields.Mounts) != 4 || !s.Fields.Mounts[0].ReadOnly || !s.Fields.Mounts[3].ReadOnly || len(r.Skipped) != 2 {
		t.Fatal("mount/selection semantics changed")
	}
	for _, m := range s.Fields.Mounts {
		if m.Source != "" {
			t.Fatal("Docker source became ROS source")
		}
	}
	if len(r.Services[1].Fields.Env) != 3 || r.Services[1].Fields.Env[1].Value != "false" {
		t.Fatal("map scalar conversion failed")
	}
}

func TestComposeUnsupportedStartupAndExternalValues(t *testing.T) {
	r, err := ParseCompose(`services:
  app:
    image: ${REGISTRY}/app
    command: ["/bin/sh", "-c", "echo a b"]
    entrypoint: ""
    env_file: .env
    build: .
    depends_on: [db]
    healthcheck: {test: [CMD, /bin/test]}
    privileged: true
    network_mode: host
`)
	if err != nil {
		t.Fatal(err)
	}
	s := r.Services[0]
	if s.Fields.Image != "" || s.Fields.Command != "" || s.Fields.Entrypoint != "" || len(s.Skipped) != 8 || len(s.NeedsInput) < 4 {
		t.Fatal("unsupported fields were converted or lost their explanations")
	}
	for _, n := range []string{"command: null", "entrypoint: null"} {
		if _, err := ParseCompose("services:\n  app:\n    image: app\n    " + n); err != nil {
			t.Fatal("null inheritance rejected")
		}
	}
}

func TestComposeRejectsUnsafeStructureWithoutSourceDisclosure(t *testing.T) {
	marker := "fixture-sensitive-value"
	cases := []string{
		"", strings.Repeat("x", MaxComposeBytes+1), "services: [", "services: {}", "services: []", "- service",
		"services:\n  app:\n    image: a\n    image: " + marker,
		"services: {app: {image: a}}\n---\nservices: {b: {image: b}}",
		"services: {app: &a {image: a}, other: *a}",
		"services: {app: {<<: {image: a}}}",
		"services: {app: " + marker + "}",
		"services: {app: {nested: " + strings.Repeat("[", 40) + "x" + strings.Repeat("]", 40) + "}}",
	}
	for i, source := range cases {
		_, err := ParseCompose(source)
		if err == nil || strings.Contains(err.Error(), marker) || strings.Contains(err.Error(), source) && len(source) > 20 {
			t.Fatalf("case %d did not return a safe error", i)
		}
	}
}

func TestComposeMalformedValuesAndNoAuthorityInResult(t *testing.T) {
	for _, source := range []string{
		`services: {app: {image: app, mem_limit: bad, cpuset: bad, environment: bad, volumes: bad}}`,
		`services: {app: {image: app, volumes: [ {type: tmpfs, target: /tmp}, "x:${TARGET}", {type: bind, target: /a, read_only: bad} ]}}`,
	} {
		r, err := ParseCompose(source)
		if err != nil || len(r.Services[0].Skipped) == 0 {
			t.Fatal("unsupported value not explained", err)
		}
		b, _ := json.Marshal(r)
		for _, forbidden := range []string{`"network"`, `"rootDir"`, `"existingId"`, `"archiveId"`} {
			if strings.Contains(string(b), forbidden) {
				t.Fatal("parser result carries ROS authority")
			}
		}
	}
}

func TestComposeResourceBoundaries(t *testing.T) {
	for _, tc := range []struct{ value, expected string }{
		{"0", "unlimited"}, {"128m", "134217728"}, {"1GB", "1073741824"},
		{"1024b", "1024"}, {"18446744073709551615g", ""}, {"invalid", ""},
	} {
		got, ok := composeMemory(tc.value)
		if got != tc.expected || ok != (tc.expected != "") {
			t.Fatalf("memory conversion failed for %s", tc.value)
		}
	}
	r, err := ParseCompose("services: {app: {image: app, cpuset: 0}}")
	if err != nil || r.Services[0].Fields.CPUList != "0" {
		t.Fatal("numeric CPU binding rejected")
	}
	r, err = ParseCompose("services: {app: {image: app, cpuset: '2-1'}}")
	if err != nil || r.Services[0].Fields.CPUList != "" || len(r.Services[0].Skipped) != 1 {
		t.Fatal("invalid CPU range imported")
	}
}
