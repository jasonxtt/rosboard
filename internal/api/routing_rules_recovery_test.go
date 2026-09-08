package api

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"

	"rosboard/internal/config"
	"rosboard/internal/policyv2"
	"rosboard/internal/routeros"
	"rosboard/internal/store"
)

// failingPolicyRouter reads nothing: the save path (repository + account
// check) still succeeds, but every plan/apply fails at the read stage —
// the deterministic "desired saved, apply failed" scenario from P1-2.
type failingPolicyRouter struct{ policyV2Router }

func (failingPolicyRouter) PolicyList(context.Context, routeros.ReadMenu, []string) ([]routeros.RouterOSObject, error) {
	return nil, errors.New("router read failed")
}

func newFailingReaderPolicyServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	storage, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{Devices: []config.DeviceConfig{{
		ID: "edge", Name: "Edge", Enabled: true,
		RouterOS: config.RouterOSConfig{BaseURL: "http://router.invalid", Username: "device-user", Password: "secret"},
	}}}
	manager := policyv2.NewManager(log.New(io.Discard, "", 0))
	deviceStore, err := storage.OpenDevice("edge")
	if err != nil {
		storage.Close()
		t.Fatal(err)
	}
	router := failingPolicyRouter{}
	if err := manager.RegisterApplier("edge", &policyv2.Applier{Reader: router, Mutation: router, Repo: deviceStore.PolicyRepository(), Access: deviceStore.AccessRepository()}); err != nil {
		storage.Close()
		t.Fatal(err)
	}
	return &Server{cfg: cfg, store: storage, policy: manager}, storage
}

// P1-2: 快捷保存已写 desired 但 GenerateAndApply 失败——响应必须带
// desiredSaved 标记，且规则确实已保存在 desired state。
func TestRoutingRuleSaveApplyFailureMarksDesiredSaved(t *testing.T) {
	server, storage := newFailingReaderPolicyServer(t)
	defer storage.Close()
	deviceStore, err := storage.OpenDevice("edge")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := deviceStore.PolicyRepository().SaveEgress(context.Background(), policyv2.Egress{
		ID: "wan", Name: "WAN", Enabled: true,
		Families: []policyv2.EgressFamily{{Family: policyv2.FamilyIPv4, Enabled: true, WANInterface: "wan1", Gateway: "192.0.2.1"}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := deviceStore.PolicyRepository().SaveSource(context.Background(), policyv2.Source{
		ID: "games", Type: "manual", Kind: policyv2.KindDomain, Name: "Games", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}

	response := policyV2Request(t, server, http.MethodPost, "/rules", `{
		"id":"quick-toggle-target","name":"全部走 WAN","subject":{"mode":"all","members":[],"prefixes":[]},
		"ingress":{"interfaceLists":["LAN"],"interfaces":[]},"targetListIds":["games"],"egressId":"wan","priority":10,"enabled":true
	}`)
	if response.Code == http.StatusOK || response.Code == http.StatusAccepted {
		t.Fatalf("apply must fail with an unreadable router: status=%d body=%s", response.Code, response.Body.String())
	}
	if !bytes.Contains(response.Body.Bytes(), []byte(`"desiredSaved":true`)) {
		t.Fatalf("apply-failure payload must carry desiredSaved marker: %s", response.Body.String())
	}
	rules, err := deviceStore.PolicyRepository().ListRoutingRules(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 || rules[0].ID != "quick-toggle-target" {
		t.Fatalf("desired state must contain the saved rule, got %#v", rules)
	}
}

// P1-2: 快捷删除已写 desired 但 GenerateAndApply 失败——响应必须带
// deleted/desiredSaved 标记，且规则确实已从 desired state 删除。
func TestRoutingRuleDeleteApplyFailureMarksPartialSuccess(t *testing.T) {
	server, storage := newFailingReaderPolicyServer(t)
	defer storage.Close()
	deviceStore, err := storage.OpenDevice("edge")
	if err != nil {
		t.Fatal(err)
	}
	repository := deviceStore.PolicyRepository()
	if _, err := repository.SaveEgress(context.Background(), policyv2.Egress{
		ID: "wan", Name: "WAN", Enabled: true,
		Families: []policyv2.EgressFamily{{Family: policyv2.FamilyIPv4, Enabled: true, WANInterface: "wan1", Gateway: "192.0.2.1"}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.SaveSource(context.Background(), policyv2.Source{
		ID: "games", Type: "manual", Kind: policyv2.KindDomain, Name: "Games", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.SaveRoutingRule(context.Background(), policyv2.RoutingRule{
		ID: "doomed-rule", Name: "待删除", Enabled: true, Priority: 10, EgressID: "wan",
		Subject:       policyv2.Subject{Mode: policyv2.SubjectModeAll},
		Ingress:       policyv2.TrafficIngressScope{InterfaceLists: []string{"LAN"}},
		TargetListIDs: []string{"games"},
	}); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodDelete, "/api/policy-routing/rules/doomed-rule?device=edge&revision=1", nil)
	response := httptest.NewRecorder()
	server.servePolicyRoutingAPI(response, request)
	if response.Code == http.StatusOK || response.Code == http.StatusAccepted {
		t.Fatalf("apply must fail with an unreadable router: status=%d body=%s", response.Code, response.Body.String())
	}
	if !bytes.Contains(response.Body.Bytes(), []byte(`"deleted":true`)) || !bytes.Contains(response.Body.Bytes(), []byte(`"desiredSaved":true`)) {
		t.Fatalf("delete apply-failure payload must carry deleted/desiredSaved markers: %s", response.Body.String())
	}
	rules, err := repository.ListRoutingRules(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 0 {
		t.Fatalf("desired state must not contain the deleted rule, got %#v", rules)
	}
}
