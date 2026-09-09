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
