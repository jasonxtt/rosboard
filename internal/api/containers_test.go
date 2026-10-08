package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"rosboard/internal/config"
	"rosboard/internal/containers"
	"rosboard/internal/routeros"
)

type containerAPIReader struct {
	calls int
	name  string
}

func (f *containerAPIReader) ContainerRead(_ context.Context, menu routeros.ContainerMenu) ([]routeros.RouterOSObject, error) {
	f.calls++
	if menu == routeros.ContainerList {
		return []routeros.RouterOSObject{{".id": "*7", "name": f.name, "stopped": "true"}}, nil
	}
	return []routeros.RouterOSObject{}, nil
}
func TestContainerAPIDeviceScopeAndWriteDenial(t *testing.T) {
	cfg := config.Config{Devices: []config.DeviceConfig{{ID: "a", Enabled: true}, {ID: "b", Enabled: true}, {ID: "archived", Enabled: true, Archived: true}}}
	s := NewServer(cfg, nil, fstest.MapFS{})
	readers := map[string]*containerAPIReader{"a": {name: "only-a"}, "b": {name: "only-b"}}
	s.containers.ReaderFor = func(d config.DeviceConfig) containers.Reader { return readers[d.ID] }
	for _, tc := range []struct {
		method, path string
		status       int
		want         string
	}{{"GET", "/api/containers", 400, "device_required"}, {"GET", "/api/containers?device=missing", 404, "device_not_found"}, {"GET", "/api/containers?device=archived", 404, "device_not_found"}, {"GET", "/api/containers?device=a", 200, "only-a"}, {"GET", "/api/containers?device=b", 200, "only-b"}, {"POST", "/api/containers/actions?device=a", 403, "container_read_only"}, {"DELETE", "/api/containers/*7?device=a", 403, "container_read_only"}, {"POST", "/api/containers/jobs/x/recover?device=a", 403, "container_read_only"}, {"GET", "/api/containers/not-here?device=a", 404, "container_not_found"}} {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
		if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.want) {
			t.Fatalf("%s => %d %s", tc.path, w.Code, w.Body.String())
		}
		if tc.method == "GET" && tc.status == 200 {
			if strings.Contains(w.Body.String(), `"writes":true`) {
				t.Fatal("production writes enabled")
			}
		}
	}
	before := readers["a"].calls
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("PUT", "/api/containers/*7?device=a", strings.NewReader(`{}`)))
	if readers["a"].calls != before {
		t.Fatal("denied mutation touched router")
	}
}
func simulationCall(t *testing.T, m *simulation, method, path string, body any, status int) []byte {
	t.Helper()
	var input io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		input = bytes.NewReader(raw)
	}
	w := httptest.NewRecorder()
	m.ServeHTTP(w, httptest.NewRequest(method, path, input))
	if w.Code != status {
		t.Fatalf("%s %s => %d: %s", method, path, w.Code, w.Body.String())
	}
	return w.Body.Bytes()
}
func decodeSimulationJob(t *testing.T, raw []byte) containers.Job {
	t.Helper()
	var j containers.Job
	if err := json.Unmarshal(raw, &j); err != nil {
		t.Fatal(err)
	}
	return j
}
func simulateFinished(t *testing.T, m *simulation, req simulatedRequest) containers.Job {
	t.Helper()
	req.RequestID = uuidForTest()
	req.Scenario = "success"
	j := decodeSimulationJob(t, simulationCall(t, m, "POST", "/api/containers/actions?device=demo-router", req, 202))
	m.jobs[j.ID].started = m.now().Add(-10 * time.Second)
	return decodeSimulationJob(t, simulationCall(t, m, "GET", "/api/containers/jobs/"+j.ID+"?device=demo-router", nil, 200))
}

var testRequestCounter int

func uuidForTest() string { testRequestCounter++; return strings.Repeat("id", testRequestCounter) }
func TestSimulationIdempotencyFailureAndUnknownRecovery(t *testing.T) {
	m := newSimulation()
	now := time.Unix(100, 0)
	m.now = func() time.Time { return now }
	d := containers.Draft{DraftID: "test12", Image: "nginx", Network: containers.Network{VETH: "new-veth", Bridge: "br-containers", Address: "172.20.0.8/24", Gateway: "172.20.0.1"}, StartAfterCreate: true, StartOnBoot: true, Logging: true, RestartPolicy: "no", Health: containers.Health{Mode: "inherit"}}
	req := simulatedRequest{Action: "create", RequestID: "same", Draft: d, Scenario: "unknown"}
	j := decodeSimulationJob(t, simulationCall(t, m, "POST", "/api/containers/actions?device=demo-router", req, 202))
	duplicate := decodeSimulationJob(t, simulationCall(t, m, "POST", "/api/containers/actions?device=demo-router", req, 202))
	if j.ID != duplicate.ID {
		t.Fatal("duplicate job")
	}
	changed := req
	changed.Draft.Image = "redis"
	simulationCall(t, m, "POST", "/api/containers/actions?device=demo-router", changed, 409)
	simulationCall(t, m, "GET", "/api/containers/jobs/"+j.ID+"?device=demo-edge", nil, 404)
	now = now.Add(10 * time.Second)
	j = decodeSimulationJob(t, simulationCall(t, m, "GET", "/api/containers/jobs/"+j.ID+"?device=demo-router", nil, 200))
	if j.State != "unknown" || len(m.devices["demo-router"].Items) != 5 {
		t.Fatal("unknown result not tracked")
	}
	req.RequestID = "other"
	simulationCall(t, m, "POST", "/api/containers/actions?device=demo-router", req, 409)
	j = decodeSimulationJob(t, simulationCall(t, m, "POST", "/api/containers/jobs/"+j.ID+"/recover?device=demo-router", nil, 200))
	if j.State != "succeeded" || len(m.devices["demo-router"].Items) != 5 || len(m.devices["demo-edge"].Items) != 1 {
		t.Fatal("recovery duplicated/crossed device")
	}
	req.RequestID = "failure"
	req.Scenario = "failure"
	req.Draft.Network.VETH = "another-veth"
	req.Draft.Network.Address = "172.20.0.9/24"
	req.Draft.DraftID = "second"
	failed := decodeSimulationJob(t, simulationCall(t, m, "POST", "/api/containers/actions?device=demo-router", req, 202))
	now = now.Add(10 * time.Second)
	failed = decodeSimulationJob(t, simulationCall(t, m, "GET", "/api/containers/jobs/"+failed.ID+"?device=demo-router", nil, 200))
	if failed.State != "failed" || len(m.devices["demo-router"].Items) != 5 {
		t.Fatal("failure changed state")
	}
}
func TestSimulationAdoptionLifecycleAndSharedRetention(t *testing.T) {
	m := newSimulation()
	d := m.devices["demo-router"].Items[0].Config
	simulationCall(t, m, "POST", "/api/containers/actions?device=demo-router", simulatedRequest{Action: "edit", TargetID: "*1", RequestID: "before-adoption", Draft: d, Scenario: "success"}, 409)
	for _, action := range []string{"adopt", "start", "restart", "stop", "update"} {
		j := simulateFinished(t, m, simulatedRequest{Action: action, TargetID: "*1"})
		if j.State != "succeeded" {
			t.Fatal(j)
		}
	}
	j := simulateFinished(t, m, simulatedRequest{Action: "edit", TargetID: "*1", Draft: d})
	if j.State != "succeeded" || m.devices["demo-router"].Items[0].Config.Logging {
		t.Fatal("edit changed logging")
	}
	d.Network.Gateway = "172.20.0.254"
	simulationCall(t, m, "POST", "/api/containers/actions?device=demo-router", simulatedRequest{Action: "edit", TargetID: "*1", RequestID: "shared-edit", Draft: d, Scenario: "success"}, 400)
	j = simulateFinished(t, m, simulatedRequest{Action: "delete", TargetID: "*1"})
	if len(m.devices["demo-router"].Items) != 3 || !strings.Contains(strings.Join(j.Retained, ","), "veth-shared") || !strings.Contains(strings.Join(j.Retained, ","), "shared-config") {
		t.Fatal("shared objects not retained", j)
	}
	found := false
	for _, v := range m.devices["demo-router"].Options.Interfaces {
		if v == "veth-shared" {
			found = true
		}
	}
	if !found || len(m.devices["demo-router"].Items[0].SharedVETH) != 0 {
		t.Fatal("shared network removed or stale")
	}
}

func TestSimulationRestoresPendingTaskAfterNavigation(t *testing.T) {
	m := newSimulation()
	req := simulatedRequest{Action: "adopt", TargetID: "*1", RequestID: "restore", Scenario: "unknown"}
	job := decodeSimulationJob(t, simulationCall(t, m, "POST", "/api/containers/actions?device=demo-router", req, 202))
	m.jobs[job.ID].started = m.now().Add(-10 * time.Second)
	simulationCall(t, m, "GET", "/api/containers/jobs/"+job.ID+"?device=demo-router", nil, 200)
	var s containers.Snapshot
	raw := simulationCall(t, m, "GET", "/api/containers?device=demo-router", nil, 200)
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	if s.ActiveJob == nil || s.ActiveJob.ID != job.ID || s.ActiveJob.State != "unknown" {
		t.Fatal("pending job lost after remount")
	}
	simulationCall(t, m, "POST", "/api/containers/jobs/"+job.ID+"/recover?device=demo-router", nil, 200)
	simulationCall(t, m, "POST", "/api/containers/_reset?device=demo-router", nil, 200)
	raw = simulationCall(t, m, "GET", "/api/containers?device=demo-router", nil, 200)
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	if s.ActiveJob != nil {
		t.Fatal("reset retained old task")
	}
}

func TestAdoptionDoesNotClaimVETHWhenNewOwnedEnvIsAdded(t *testing.T) {
	m := newSimulation()
	simulateFinished(t, m, simulatedRequest{Action: "adopt", TargetID: "*3"})
	d := m.devices["demo-router"].Items[2].Config
	d.Env = append(d.Env, containers.Environment{Key: "NEW", Value: "owned"})
	edit := simulateFinished(t, m, simulatedRequest{Action: "edit", TargetID: "*3", Draft: d})
	if len(m.resources["demo-router"]["*3"]) != 1 || !strings.Contains(strings.Join(edit.Retained, ","), "shared-env") {
		t.Fatal("owned env/external env tracking failed")
	}
	deletion := simulateFinished(t, m, simulatedRequest{Action: "delete", TargetID: "*3"})
	if !strings.Contains(strings.Join(deletion.Retained, ","), "veth-nginx") || strings.Contains(strings.Join(deletion.Retained, ","), "rosboard-env-") {
		t.Fatal("VETH claimed or owned env retained", deletion)
	}
}

func TestContainerReadsAndResolutionRequireSessionAndSameOrigin(t *testing.T) {
	s, _ := newAuthServer(t, nil)
	unauthenticated := authRequest(t, s, "GET", "/api/containers?device=a", "", nil)
	if unauthenticated.Code != 401 {
		t.Fatal("container read bypassed auth")
	}
	created := authRequest(t, s, "POST", "/api/setup/admin", `{"username":"admin","password":"1234","passwordConfirmation":"1234"}`, nil)
	cookie := responseCookie(t, created)
	authRequest(t, s, "POST", "/api/setup/complete", `{"skipRouterOS":true}`, cookie)
	s.cfg.Devices = []config.DeviceConfig{{ID: "a", Enabled: true}}
	reader := &containerAPIReader{name: "visible"}
	s.containers.ReaderFor = func(config.DeviceConfig) containers.Reader { return reader }
	visible := authRequest(t, s, "GET", "/api/containers?device=a", "", cookie)
	if visible.Code != 200 || visible.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("authenticated read failed or cached", visible.Code)
	}
	request := httptest.NewRequest("POST", "http://example.com/api/containers/resolve?device=a", strings.NewReader(`{}`))
	request.AddCookie(cookie)
	request.Header.Set("Origin", "http://attacker.test")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, request)
	if w.Code != 403 {
		t.Fatal("cross-origin resolve allowed")
	}
}
