package api

import (
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"rosboard/internal/config"
	"rosboard/internal/routeros"
	"rosboard/internal/service"
)

func TestAccountTransferBoundReadOnly(t *testing.T) {
	const username = "rosboard_0123456789abcdef"
	const group = "rosboard_g_0123456789abcdef"
	for _, tc := range []struct {
		name, policy             string
		legacy, shared, mismatch bool
		status                   int
	}{
		{name: "old", policy: routeros.ManagedPolicyOld, status: 200},
		{name: "new", policy: routeros.ManagedPolicyNew, status: 200},
		{name: "extra", policy: routeros.ManagedPolicyNew + ",policy", status: 409},
		{name: "partial", policy: routeros.ManagedPolicyOld + ",ssh", status: 409},
		{name: "legacy", policy: routeros.ManagedPolicyOld, legacy: true, status: 409},
		{name: "shared", policy: routeros.ManagedPolicyOld, shared: true, status: 409},
		{name: "mismatch", policy: routeros.ManagedPolicyOld, mismatch: true, status: 409},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reads := 0
			router := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				reads++
				if r.Method != "GET" {
					t.Errorf("mutation %s", r.Method)
					w.WriteHeader(500)
					return
				}
				switch r.URL.Path {
				case "/rest/user":
					users := []map[string]string{{".id": "*1", "name": username, "group": group, "comment": "Managed by rosboard", "disabled": "false"}}
					if tc.shared {
						users = append(users, map[string]string{"name": "foreign", "group": group})
					}
					json.NewEncoder(w).Encode(users)
				case "/rest/user/group":
					json.NewEncoder(w).Encode([]map[string]string{{".id": "*2", "name": group, "policy": tc.policy}})
				case "/rest/system/identity":
					json.NewEncoder(w).Encode(map[string]string{"name": "lab $quote\""})
				default:
					t.Errorf("unexpected read")
					w.WriteHeader(500)
				}
			}))
			defer router.Close()
			d := config.DeviceConfig{ID: "lab", Enabled: true, RouterOS: config.RouterOSConfig{BaseURL: router.URL, Username: username, Password: "private-test-only"}, ManagedAccount: &config.ManagedRouterOSAccount{Username: username, GroupName: group}}
			if tc.legacy {
				d.ManagedAccount = nil
			}
			if tc.mismatch {
				d.ManagedAccount.GroupName = "foreign"
			}
			cfg := config.Config{Path: t.TempDir() + "/config.yaml", Devices: []config.DeviceConfig{d, {ID: "other", Enabled: true}}}
			s := NewServerWithProvisioning(cfg, service.NewMonitor(cfg, nil, nil, log.Default()), nil, nil)
			req := httptest.NewRequest("GET", "/api/devices/lab/account-transfer", nil)
			req.RemoteAddr = "127.0.0.1:1234"
			w := httptest.NewRecorder()
			s.ServeHTTP(w, req)
			if w.Code != tc.status {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("script response may be cached")
			}
			if strings.Contains(w.Body.String(), "private-test-only") {
				t.Fatal("password leaked")
			}
			if tc.legacy || tc.mismatch {
				if reads != 0 {
					t.Fatal("inferred metadata caused remote access")
				}
			}
			if tc.status == 200 {
				var result struct {
					State, Script             string
					ReadAccess, WriteVerified bool
				}
				json.Unmarshal(w.Body.Bytes(), &result)
				if result.ReadAccess || result.WriteVerified {
					t.Fatal("unproven capability")
				}
				if strings.Contains(result.Script, "/user set") || strings.Contains(result.Script, "password=") {
					t.Fatal("upgrade changes credentials")
				}
				if strings.Count(result.Script, "/user group set") != 1 || !strings.Contains(result.Script, `$rbState = "old"`) {
					t.Fatal("unconditional role set")
				}
			}
			w = httptest.NewRecorder()
			req = httptest.NewRequest("POST", "/api/devices/lab/account-transfer", strings.NewReader(`{"writeProbe":true}`))
			req.RemoteAddr = "127.0.0.1:1234"
			s.ServeHTTP(w, req)
			if w.Code != 405 {
				t.Fatalf("write probe accepted: %d", w.Code)
			}
			w = httptest.NewRecorder()
			req = httptest.NewRequest("GET", "/api/devices/other/account-transfer", nil)
			req.RemoteAddr = "127.0.0.1:1234"
			s.ServeHTTP(w, req)
			if w.Code != 409 {
				t.Fatal("cross-device identity accepted")
			}
		})
	}
}

func TestUpgradeScriptEscapingAndInvalidIDs(t *testing.T) {
	i := routeros.ManagedRoleIdentity{UserID: "*1", GroupID: "*2", Username: "user$\";\n", GroupName: "group", RouterName: "lab", Policy: routeros.ManagedPolicyOld}
	script, ok := managedRoleUpgradeScript(i)
	if !ok || strings.Contains(script, i.Username) {
		t.Fatal("unsafe script interpolation")
	}
	i.UserID = "*1; /user remove"
	if _, ok = managedRoleUpgradeScript(i); ok {
		t.Fatal("unsafe ID accepted")
	}
}
