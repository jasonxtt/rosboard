package api

import (
	"net/http"
	"os"
	"testing/fstest"

	"rosboard/internal/config"
)

// Optional real device keeps Container writes disabled, permits scoped Files
// operations and receives credentials only from the backend process environment. Never pass them to preview HTML or JSON.
func containerPreviewHandler(mock *simulation) http.Handler {
	devices := []map[string]string{{"id": "demo-router", "name": "演示路由器"}, {"id": "demo-edge", "name": "演示边缘设备"}}
	var real *Server
	if base := os.Getenv("ROSBOARD_CONTAINER_TEST_URL"); base != "" {
		device := config.DeviceConfig{ID: "test-router", Name: "测试 RouterOS", Enabled: true, RouterOS: config.RouterOSConfig{BaseURL: base, Username: os.Getenv("ROSBOARD_CONTAINER_TEST_USER"), Password: os.Getenv("ROSBOARD_CONTAINER_TEST_PASSWORD")}}
		real = NewServer(config.Config{Devices: []config.DeviceConfig{device}}, nil, fstest.MapFS{})
		devices = append(devices, map[string]string{"id": device.ID, "name": "测试 RouterOS（容器只读）"})
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/containers/_preview-devices" && r.Method == http.MethodGet {
			w.Header().Set("Cache-Control", "no-store")
			writeJSON(w, 200, devices)
			return
		}
		if r.URL.Query().Get("device") == "test-router" && real != nil {
			real.ServeHTTP(w, r)
			return
		}
		mock.ServeHTTP(w, r)
	})
}
