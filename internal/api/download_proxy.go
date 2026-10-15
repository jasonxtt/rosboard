package api

import (
	"context"
	"net/http"
	"time"

	"rosboard/internal/config"
	"rosboard/internal/downloadnet"
)

func (s *Server) downloadSnapshot() (downloadnet.Snapshot, error) {
	return downloadnet.New(s.configSnapshot().DownloadProxyURL)
}
func proxyStatus(raw string) any {
	display, password := downloadnet.Display(raw)
	return map[string]any{"displayURL": display, "passwordSet": password, "revision": downloadnet.Revision(raw)}
}
func (s *Server) serveDownloadProxy(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodGet && r.URL.Path == "/api/settings/download-proxy" {
		writeJSON(w, 200, proxyStatus(s.configSnapshot().DownloadProxyURL))
		return
	}
	if (r.Method != http.MethodPut || r.URL.Path != "/api/settings/download-proxy") && (r.Method != http.MethodPost || r.URL.Path != "/api/settings/download-proxy/test") {
		methodNotAllowed(w, "GET, PUT, POST")
		return
	}
	var body struct {
		URL      string `json:"url"`
		UseSaved bool   `json:"useSaved"`
		Revision string `json:"revision"`
	}
	if decodeJSONBody(w, r, &body) != nil {
		return
	}
	raw := body.URL
	if body.UseSaved {
		saved := s.configSnapshot().DownloadProxyURL
		if body.URL != "" || body.Revision != downloadnet.Revision(saved) {
			writeAPIError(w, 409, "proxy_changed", "代理配置已变化，请重新读取")
			return
		}
		raw = saved
	}
	normalized, e := downloadnet.Normalize(raw)
	if e != nil {
		writeAPIError(w, 400, "invalid_proxy", downloadnet.ErrInvalidProxy.Error())
		return
	}
	if r.Method == http.MethodPut {
		// A saved mask never becomes a secret or moves to another endpoint.
		if e = s.saveSettings(func(next *config.Config) { next.DownloadProxyURL = normalized }); e != nil {
			writeAPIError(w, 500, "save_failed", "无法保存代理配置")
			return
		}
		writeJSON(w, 200, proxyStatus(normalized))
		return
	}
	snap, e := downloadnet.New(normalized)
	if e != nil {
		writeAPIError(w, 400, "invalid_proxy", downloadnet.ErrInvalidProxy.Error())
		return
	}
	defer snap.Transport.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
	defer cancel()
	writeJSON(w, 200, downloadnet.Test(ctx, snap))
}

// StartContainerWorkers starts only after activation and updater commit. GET
// endpoints never trigger cleanup, even when observing first successful runtime.
func (s *Server) StartContainerWorkers(ctx context.Context, allowed func(context.Context) error) {
	go func() {
		if allowed != nil {
			if allowed(ctx) != nil {
				return
			}
		}
		s.containers.StartImageWorkers(ctx, s.configSnapshot().Devices)
	}()
}
