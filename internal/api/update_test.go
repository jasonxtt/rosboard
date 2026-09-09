package api

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"rosboard/internal/buildinfo"
	"rosboard/internal/update"
)

func TestUpdateAuthenticationAndMutationExclusion(t *testing.T) {
	server, storage := newAuthServer(t, []string{"127.0.0.0/8"})
	dir := t.TempDir()
	paths, _ := update.NewPaths(filepath.Join(dir, "rosboard"), filepath.Join(dir, "config.yaml"), filepath.Join(dir, "data"))
	server.SetUpdater(update.NewManager(context.Background(), paths, buildinfo.Info{Version: "0.2.0", OS: "linux", Arch: "amd64"}, func() {}, log.New(io.Discard, "", 0)))
	for _, route := range []struct{ method, path string }{{"GET", "/api/settings/update"}, {"POST", "/api/settings/update/check"}, {"POST", "/api/settings/update/install"}} {
		r := authRequest(t, server, route.method, route.path, `{"version":"0.3.0"}`, nil)
		if r.Code != 401 {
			t.Fatalf("unauthenticated %s: %d", route.path, r.Code)
		}
	}
	created := authRequest(t, server, "POST", "/api/setup/admin", `{"username":"admin","password":"1234","passwordConfirmation":"1234"}`, nil)
	cookie := responseCookie(t, created)
	if err := storage.SetOnboardingComplete(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
	r := authRequest(t, server, "GET", "/api/settings/update", "", cookie)
	if r.Code != 200 || !strings.Contains(r.Body.String(), `"version":"0.2.0"`) {
		t.Fatal(r.Code, r.Body.String())
	}
	req := httptest.NewRequest("POST", "/api/settings/update/install", strings.NewReader(`{"version":"0.3.0"}`))
	req.RemoteAddr = "127.0.0.1:42"
	req.AddCookie(cookie)
	req.Header.Set("Origin", "https://evil.example")
	out := httptest.NewRecorder()
	server.ServeHTTP(out, req)
	if out.Code != 403 {
		t.Fatal("cross origin accepted", out.Code)
	}
	os.MkdirAll(paths.State, 0700)
	os.WriteFile(filepath.Join(paths.State, "job.json"), []byte(`{"id":"active","stage":"pending"}`), 0600)
	for _, path := range []string{"/api/settings/restart", "/api/settings/full-reset", "/api/settings/collection", "/api/settings/update/install"} {
		r = authRequest(t, server, "POST", path, `{"version":"0.3.0","confirmed":true}`, cookie)
		if r.Code != http.StatusConflict {
			t.Fatal(path, r.Code, r.Body.String())
		}
	}
	r = authRequest(t, server, "GET", "/api/settings/update", "", cookie)
	if r.Code != 200 {
		t.Fatal("status blocked during update")
	}
}

func TestPendingRestartRejectsInstall(t *testing.T) {
	restarted := make(chan struct{}, 1)
	server, storage := newAuthServerWithRestart(t, []string{"127.0.0.0/8"}, func() { restarted <- struct{}{} })
	dir := t.TempDir()
	paths, _ := update.NewPaths(filepath.Join(dir, "rosboard"), filepath.Join(dir, "config.yaml"), filepath.Join(dir, "data"))
	server.SetUpdater(update.NewManager(context.Background(), paths, buildinfo.Info{Version: "0.2.0", OS: "linux", Arch: "amd64"}, nil, log.New(io.Discard, "", 0)))
	created := authRequest(t, server, "POST", "/api/setup/admin", `{"username":"admin","password":"1234","passwordConfirmation":"1234"}`, nil)
	cookie := responseCookie(t, created)
	if err := storage.SetOnboardingComplete(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
	r := authRequest(t, server, "POST", "/api/settings/restart", `{}`, cookie)
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	// The delayed callback has not run, but admission is already closed.
	select {
	case <-restarted:
		t.Fatal("restart delay skipped")
	default:
	}
	r = authRequest(t, server, "POST", "/api/settings/update/install", `{"version":"0.3.0"}`, cookie)
	if r.Code != 409 || !strings.Contains(r.Body.String(), "restart_pending") {
		t.Fatal(r.Code, r.Body.String())
	}
	r = authRequest(t, server, "GET", "/api/settings/update", "", cookie)
	if !strings.Contains(r.Body.String(), `"canInstall":false`) || !strings.Contains(r.Body.String(), "面板正在重启") {
		t.Fatal(r.Body.String())
	}
	if _, err := os.Stat(filepath.Join(paths.State, "job.json")); !os.IsNotExist(err) {
		t.Fatal("created update journal", err)
	}
	select {
	case <-restarted:
	case <-time.After(time.Second):
		t.Fatal("restart not scheduled")
	}
}

func TestFullResetPublishesRestartGateBeforeReturning(t *testing.T) {
	restarted := make(chan struct{}, 1)
	server, storage := newAuthServerWithRestart(t, []string{"127.0.0.0/8"}, func() { restarted <- struct{}{} })
	dir := t.TempDir()
	paths, _ := update.NewPaths(filepath.Join(dir, "rosboard"), filepath.Join(dir, "config.yaml"), filepath.Join(dir, "data"))
	server.SetUpdater(update.NewManager(context.Background(), paths, buildinfo.Info{Version: "0.2.0", OS: "linux", Arch: "amd64"}, nil, log.New(io.Discard, "", 0)))
	created := authRequest(t, server, "POST", "/api/setup/admin", `{"username":"admin","password":"1234","passwordConfirmation":"1234"}`, nil)
	cookie := responseCookie(t, created)
	if err := storage.SetOnboardingComplete(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
	r := authRequest(t, server, "POST", "/api/settings/full-reset", `{"confirmed":true}`, cookie)
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	if !server.restartPending.Load() {
		t.Fatal("full reset returned before publishing restart gate")
	}
	// Simulate an install whose authentication finished before reset invalidated
	// sessions, but which acquires mutationMu only after reset returns.
	request := httptest.NewRequest("POST", "/api/settings/update/install", strings.NewReader(`{"version":"0.3.0"}`))
	out := httptest.NewRecorder()
	server.mutationMu.Lock()
	server.serveUpdate(out, request)
	server.mutationMu.Unlock()
	if out.Code != 409 || !strings.Contains(out.Body.String(), "restart_pending") {
		t.Fatal(out.Code, out.Body.String())
	}
	select {
	case <-restarted:
	case <-time.After(time.Second):
		t.Fatal("restart not scheduled")
	}
}
