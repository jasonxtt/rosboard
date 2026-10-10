package routeros

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestContainerReadsAreClosedGETOnlyAndExcludeCredentials(t *testing.T) {
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "GET" {
			t.Errorf("mutation: %s", r.Method)
		}
		props := r.URL.Query().Get(".proplist")
		for _, forbidden := range []string{"password", "registry-url", "config-json", "sensitive", "contents"} {
			if strings.Contains(props, forbidden) {
				t.Errorf("secret property requested: %s", props)
			}
		}
		if r.URL.Path == "/rest/container" && (!strings.Contains(props, "download/extract failed") || !strings.Contains(props, "memory-current")) {
			t.Error("missing real runtime properties")
		}
		if r.URL.Path == "/rest/container/config" {
			fmt.Fprint(w, `{"memory-high":"256M"}`)
		} else {
			fmt.Fprint(w, `[{".id":"*1","name":"dns","stopped":"true"}]`)
		}
	}))
	defer s.Close()
	client := NewClient(s.URL, "test", "fake-only")
	if _, err := client.ContainerRead(context.Background(), ContainerMenu("system/reboot")); err == nil {
		t.Fatal("arbitrary menu allowed")
	}
	if _, err := client.ContainerRead(context.Background(), ContainerMenu("ip/firewall/nat")); err == nil {
		t.Fatal("container feature allowed firewall NAT reads")
	}
	if calls != 0 {
		t.Fatal("unvalidated menu sent")
	}
	rows, err := client.ContainerRead(context.Background(), ContainerList)
	if err != nil || len(rows) != 1 || rows[0]["stopped"] != "true" {
		t.Fatal(rows, err)
	}
	if _, err := client.ContainerRead(context.Background(), ContainerFiles); err != nil {
		t.Fatal(err)
	}
	rows, err = client.ContainerRead(context.Background(), ContainerConfig)
	if err != nil || rows[0]["memory-high"] != "256M" {
		t.Fatal(rows, err)
	}
}
