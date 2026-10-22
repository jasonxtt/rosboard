package containers

import (
	"strings"
	"testing"
)

func TestDockerRunDDNSImport(t *testing.T) {
	r, err := ParseContainerImport(`docker run -d --name ddns-go --restart=always --net=host -v /opt/ddns-go:/root jeessy/ddns-go`)
	if err != nil || r.Format != "docker-run" || len(r.Services) != 1 {
		t.Fatal("Docker source rejected", err)
	}
	s := r.Services[0]
	if s.Fields.Name != "ddns-go" || s.Fields.Image != "jeessy/ddns-go" || s.Fields.RestartPolicy != "always" || len(s.Fields.Mounts) != 1 {
		t.Fatal("DDNS conversion failed")
	}
	m := s.Fields.Mounts[0]
	if m.Target != "/root" || m.Source != "" || m.ReadOnly || s.MountHints[0] != "/opt/ddns-go" || len(s.Skipped) != 2 {
		t.Fatal("incorrect network/volume/detach handling")
	}
}

func TestDockerRunQuotesEscapesAndResources(t *testing.T) {
	source := "sudo docker container run -dit \\\n --name='my-app' --restart on-failure -e 'SPECIAL= a=$HOME; $$value | literal ' -e EMPTY= -e 'EQUALS=a=b' --user=1000 -w /app -m128m --memory-reservation 64mb --cpuset-cpus=0,2-3 -v '/a b:/config:ro' --mount type=bind,src=/data,dst=/persist,readonly --publish=9876:9876 example/app:v1"
	r, err := ParseContainerImport(source)
	if err != nil {
		t.Fatal("quoted command rejected", err)
	}
	s := r.Services[0]
	if s.Fields.Name != "my-app" || s.Fields.User != "1000" || s.Fields.Workdir != "/app" || s.Fields.MemoryMax != "134217728" || s.Fields.MemoryHigh != "67108864" || s.Fields.CPUList != "0,2-3" {
		t.Fatal("supported options lost")
	}
	if len(s.Fields.Env) != 3 || s.Fields.Env[0].Value != " a=$HOME; $$value | literal " || s.Fields.Env[1].Value != "" || s.Fields.Env[2].Value != "a=b" {
		t.Fatal("literal environment values changed")
	}
	if len(s.Fields.Mounts) != 2 || !s.Fields.Mounts[0].ReadOnly || !s.Fields.Mounts[1].ReadOnly || s.MountHints[0] != "/a b" {
		t.Fatal("mount parsing failed")
	}
	for _, m := range s.Fields.Mounts {
		if m.Source != "" {
			t.Fatal("Docker source became ROS path")
		}
	}
}

func TestDockerRunVariablesAndRepeatedFlags(t *testing.T) {
	r, err := ParseContainerImport(`docker run --name old --name "$NAME" --restart always --restart unless-stopped -e SAME=old -e SAME=new -e HOST -e "MISSING=$TOKEN" -e 'LITERAL=${TOKEN}' -e ESCAPED=\$TOKEN -v $HOME/.config:/config -v '/host$$dir:/literal' example/app`)
	if err != nil {
		t.Fatal(err)
	}
	s := r.Services[0]
	if s.Fields.Name != "app" || s.Fields.RestartPolicy != "no" || len(s.Fields.Env) != 3 || s.Fields.Env[0].Value != "new" || s.Fields.Env[1].Value != "${TOKEN}" || s.Fields.Env[2].Value != "$TOKEN" {
		t.Fatal("variable/static or last-value semantics failed")
	}
	if len(s.Fields.Mounts) != 2 || s.MountHints[0] != "$HOME/.config" || s.MountHints[1] != "/host$$dir" || len(s.NeedsInput) < 4 {
		t.Fatal("dynamic host source must stay a reference hint only")
	}
	r, err = ParseContainerImport(`docker run -e A=old -e A --mount 'type=bind,src=$HOME,dst=$TARGET' "$IMAGE"`)
	if err != nil || len(r.Services[0].Fields.Env) != 0 || r.Services[0].Fields.Image != "" {
		t.Fatal("unresolved override did not remove old environment value")
	}
}

func TestDockerRunUnsupportedOptionsDoNotConsumeImage(t *testing.T) {
	r, err := ParseContainerImport(`docker run --privileged=false --cap-add NET_ADMIN --device=/dev/net/tun --sysctl net.ipv4.ip_forward=1 --dns 1.1.1.1 --env-file /never/read --rm --platform linux/amd64 --health-cmd 'test -f /tmp/a' --net host -- jeessy/ddns-go -l ':9876'`)
	if err != nil || r.Services[0].Fields.Image != "jeessy/ddns-go" || r.Services[0].Fields.Command != "" || len(r.Services[0].Skipped) != 11 {
		t.Fatal("option arity or command boundary changed", err)
	}
	r, err = ParseContainerImport("$ docker run -eKEY=value -v=/host:/root -p9876:9876 jeessy/ddns-go")
	if err != nil || r.Services[0].Fields.Env[0].Key != "KEY" || r.Services[0].Fields.Mounts[0].Target != "/root" {
		t.Fatal("short attached options rejected")
	}
}

func TestDockerRunRejectsShellAndMalformedInputWithoutDisclosure(t *testing.T) {
	marker := "fixture-private-value"
	for i, source := range []string{
		"docker run", "docker compose up", "sudo -E docker run app", "docker run --name", "docker run --unknown=" + marker + " app",
		"docker run -Z " + marker + " app", "docker run --privileged=invalid app", "docker run --name '" + marker,
		"docker run app; touch /tmp/" + marker, "docker run app && echo " + marker, "docker run app | cat", "docker run app > /tmp/" + marker,
		"docker run app\ndocker run other", "docker run -e X=$(cat /tmp/" + marker + ") app", "docker run -e X=`cat /tmp/" + marker + "` app",
		"docker run -e 'A=\x00' app", "docker run -e A=\\", "docker run " + strings.Repeat("x", MaxComposeBytes),
	} {
		_, err := ParseContainerImport(source)
		if err == nil || strings.Contains(err.Error(), marker) {
			t.Fatalf("case %d lacked a safe error", i)
		}
	}
	if r, err := ParseContainerImport("services: {app: {image: example/app}}"); err != nil || r.Format != "compose" {
		t.Fatal("Compose compatibility regressed")
	}
}

func TestDockerRunEscapedLiteralsAndCRLF(t *testing.T) {
	r, err := ParseContainerImport("docker run -d=false \\\r\n -e URL=https://example.com/#one -e \"PATH_VALUE=a\\qb\" -e 'QUOTE=a'\\''b' --entrypoint /app --mount \"type=bind,src=$HOME,dst=$TARGET\" example/app")
	if err != nil {
		t.Fatal(err)
	}
	s := r.Services[0]
	if len(s.Fields.Env) != 3 || s.Fields.Env[0].Value != "https://example.com/#one" || s.Fields.Env[1].Value != `a\qb` || s.Fields.Env[2].Value != "a'b" || len(s.Fields.Mounts) != 0 {
		t.Fatal("static quoting or unresolved mount target changed")
	}
	if s.Fields.Entrypoint != "/app" || len(s.NeedsInput) < 2 {
		t.Fatal("entrypoint inheritance needs a manual note")
	}
}
