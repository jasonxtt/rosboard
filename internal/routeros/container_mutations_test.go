package routeros

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestContainerUnsetUsesClosedNativeCommand(t *testing.T) {
	fields := make(chan string, 8)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/rest/container/unset" {
			t.Errorf("unexpected command: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body) != 2 || body["numbers"] != "*1" {
			t.Errorf("unexpected unset body: %v, %v", body, err)
		}
		fields <- body["value-name"]
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("[{}]"))
	}))
	defer server.Close()
	c := NewMutationClient(server.URL, "test", "test")
	for _, field := range []string{"cmd", "entrypoint", "user", "workdir"} {
		if err := c.ContainerCommand(context.Background(), ContainerList, "unset", RouterOSFields{"numbers": "*1", "value-name": field}); err != nil {
			t.Fatal(err)
		}
		if got := <-fields; got != field {
			t.Fatalf("wrong unset property: %s", got)
		}
	}
	if len(fields) != 0 {
		t.Fatalf("unset was retried: %d extra requests", len(fields))
	}
}

func TestContainerMutationsFailClosed(t *testing.T) {
	c := NewMutationClient("http://127.0.0.1:1", "test", "test")
	for _, tc := range []struct {
		m ContainerMenu
		c string
		f RouterOSFields
	}{
		{ContainerBridge, "set", RouterOSFields{"numbers": "*1", "vlan-filtering": "no"}},
		{ContainerList, "restart", RouterOSFields{"numbers": "*1"}},
		{ContainerList, "add", RouterOSFields{"check-certificate": "no"}},
		{ContainerList, "remove", RouterOSFields{"numbers": "*1", "name": "test"}},
		{ContainerList, "start", RouterOSFields{"numbers": "all"}},
		{ContainerVETH, "start", RouterOSFields{"numbers": "*1"}},
		{ContainerVETH, "unset", RouterOSFields{"numbers": "*1", "value-name": "cmd"}},
		{ContainerList, "unset", RouterOSFields{"numbers": "*1", "value-name": "remote-image"}},
		{ContainerList, "unset", RouterOSFields{"numbers": "*1", "value-name": true}},
		{ContainerList, "unset", RouterOSFields{"numbers": "*1", "value-name": "cmd", "cmd": ""}},
	} {
		if err := c.ContainerCommand(context.Background(), tc.m, tc.c, tc.f); err == nil {
			t.Fatal("unsafe mutation allowed", tc)
		}
	}
}

func TestContainerCPUListProjection(t *testing.T) {
	for input, want := range map[string]string{"": "", "0": "cpu0", "0,1-2": "cpu0,cpu1,cpu2", "cpu0,0": "cpu0"} {
		got, err := ContainerCPUList(input)
		if err != nil || got != want {
			t.Fatalf("%q => %q, %v", input, got, err)
		}
	}
	for _, input := range []string{"-1", "256", "0-999999999", "2-1", "0,", "cpu0;evil"} {
		if _, err := ContainerCPUList(input); err == nil {
			t.Fatalf("invalid CPU accepted: %s", input)
		}
	}
}
