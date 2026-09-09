package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"rosboard/internal/buildinfo"
)

func TestActivatedCandidateCrashRollsBack(t *testing.T) {
	p := fixturePaths(t)
	marker := filepath.Join(filepath.Dir(p.Binary), "activated")
	t.Setenv("UPDATE_TEST_MARKER", marker)
	t.Setenv("UPDATE_TEST_DATA", filepath.Join(p.Data, "rosboard.db"))
	if err := os.WriteFile(p.Config, []byte(fmt.Sprintf("listen_address: '127.0.0.1:1'\ndata_dir: %q\n", p.Data)), 0600); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
printf '%s\n' '{"version":"0.3.0","os":"linux"}' >&3
read command <&4
[ "$command" = activate ] || exit 2
printf activated > "$UPDATE_TEST_MARKER"
printf changed > "$UPDATE_TEST_DATA"
exit 42
`
	if err := os.WriteFile(filepath.Join(p.State, "candidate"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	j := &Job{ID: "crash", From: "0.2.0", To: "0.3.0", Stage: "pending"}
	if c, err := apply(context.Background(), p, j, log.New(io.Discard, "", 0)); err != nil || c != nil {
		t.Fatalf("apply: %v %v", c, err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("candidate never activated", err)
	}
	for path, want := range map[string]string{p.Binary: "old binary", filepath.Join(p.Data, "rosboard.db"): "db bytes"} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Fatalf("restore %s: %q %v", path, got, err)
		}
	}
	saved, err := readJob(p)
	if err != nil || saved.Stage != "rolled_back" {
		t.Fatalf("journal: %+v %v", saved, err)
	}
}

func TestPostActivationHealthObservation(t *testing.T) {
	for _, mode := range []string{"healthy", "wrong-version", "wrong-pid", "unhealthy", "flapping", "exit", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			c := &child{cmd: &exec.Cmd{Process: &os.Process{Pid: 123}}, done: make(chan error, 1)}
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				version, pid, ok := "0.3.0", 123, true
				if mode == "flapping" {
					ok = requests%2 == 1
				}
				if mode == "wrong-version" {
					version = "0.2.0"
				}
				if mode == "wrong-pid" {
					pid = 456
				}
				if mode == "unhealthy" {
					ok = false
				}
				json.NewEncoder(w).Encode(map[string]any{"ok": ok, "version": version, "pid": pid})
			}))
			defer server.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "exit" {
				c.done <- errors.New("crash")
			}
			if mode == "cancel" {
				cancel()
			}
			start := time.Now()
			err := c.observe(ctx, server.URL, "0.3.0", 100*time.Millisecond, 500*time.Millisecond)
			if mode == "healthy" {
				if err != nil || time.Since(start) < 100*time.Millisecond {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("accepted unhealthy child")
			}
			if mode == "exit" {
				select {
				case <-c.done:
				default:
					t.Fatal("exit lost before stop")
				}
			}
		})
	}
}

func TestWaitForCommit(t *testing.T) {
	p := fixturePaths(t)
	m := NewManager(context.Background(), p, buildinfo.Info{Version: "0.3.0"}, nil, log.New(io.Discard, "", 0))
	j := &Job{ID: "observation", From: "0.2.0", To: "0.3.0", Stage: "verifying_startup"}
	if err := saveJob(p, j); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- m.WaitForCommit(ctx) }()
	select {
	case err := <-result:
		t.Fatalf("released before commit: %v", err)
	case <-time.After(120 * time.Millisecond):
	}
	if err := finishJob(p, j, "succeeded", "ok"); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("commit did not release")
	}
	j.Stage = "verifying_startup"
	saveJob(p, j)
	cancel()
	if !errors.Is(m.WaitForCommit(ctx), context.Canceled) {
		t.Fatal("ignored cancellation")
	}
	j.Stage = "downloading"
	j.To = "0.4.0"
	saveJob(p, j)
	if err := m.WaitForCommit(context.Background()); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(p.State, "job.json"), []byte("broken"), 0600)
	if err := m.WaitForCommit(context.Background()); err == nil || !strings.Contains(err.Error(), "read update commit") {
		t.Fatal("corrupt journal accepted", err)
	}
}
