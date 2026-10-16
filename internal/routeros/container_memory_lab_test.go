package routeros

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"os"
	"strings"
	"testing"
	"time"
)

func TestContainerMemoryIndependentLab(t *testing.T) {
	p := os.Getenv("ROSBOARD_HOST_IMAGE_LAB_CONFIG")
	if p == "" {
		t.Skip("independent lab opt-in")
	}
	raw, e := os.ReadFile(p)
	var c struct{ BaseURL, Username, Password, HostKey string }
	if e != nil || json.Unmarshal(raw, &c) != nil || c.BaseURL != "http://10.0.2.5" {
		t.Fatal("independent lab config required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	files, close, e := containerTransferSession(ctx, c.BaseURL, c.Username, c.Password, c.HostKey, 22)
	if e != nil {
		t.Fatal(e)
	}
	defer close()
	token := uuid.NewString()
	file := "sata1/rosboard-memory-probe-" + token + ".tar"
	name := "rb-memory-" + token[:8]
	owner := "rosboard-memory-lab:" + token
	f, e := files.OpenFile(file, os.O_CREATE|os.O_EXCL|os.O_WRONLY)
	if e != nil {
		t.Fatal(e)
	}
	f.Write(make([]byte, 1024))
	f.Close()
	defer files.Remove(file)
	writer := NewMutationClient(c.BaseURL, c.Username, c.Password)
	reader := NewClient(c.BaseURL, c.Username, c.Password)
	if e = writer.ContainerCommand(ctx, ContainerVETH, "add", RouterOSFields{"name": name, "address": "10.0.2.249/24", "gateway": "10.0.2.1", "comment": owner}); e != nil {
		t.Fatal(e)
	}
	defer func() {
		rows, _ := reader.ContainerRead(context.Background(), ContainerVETH)
		for _, r := range rows {
			if r["comment"] == owner {
				writer.ContainerCommand(context.Background(), ContainerVETH, "remove", RouterOSFields{"numbers": r[".id"]})
			}
		}
	}()
	for _, value := range []string{"128", "128M"} {
		e = writer.ContainerCommand(ctx, ContainerList, "add", RouterOSFields{"name": name, "file": file, "interface": name, "root-dir": "sata1/rosboard-memory-probe-" + token + "/rootfs", "memory-high": value, "memory-max": value, "logging": "yes", "restart-policy": "no", "start-on-boot": "no", "envlists": "", "comment": owner})
		t.Logf("memory=%s add response=%v", value, e)
		if value == "128" && (e == nil || !strings.Contains(e.Error(), "memory-max < 1MB")) {
			t.Fatal("byte-limit rejection changed")
		}
		if value == "128M" && e != nil {
			t.Fatal("128MiB limit rejected")
		}
		rows, _ := reader.ContainerRead(ctx, ContainerList)
		for _, r := range rows {
			if r["comment"] == owner {
				writer.ContainerCommand(ctx, ContainerList, "remove", RouterOSFields{"numbers": r[".id"]})
			}
		}
	}
}
