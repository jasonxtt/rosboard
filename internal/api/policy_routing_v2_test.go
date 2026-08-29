package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"rosboard/internal/config"
	"rosboard/internal/policy"
	"rosboard/internal/policyv2"
	"rosboard/internal/routeros"
	"rosboard/internal/store"
)

type policyV2Router struct{}

type policyGatewayReader map[routeros.ReadMenu][]routeros.RouterOSObject

func (r policyGatewayReader) PolicyList(_ context.Context, menu routeros.ReadMenu, _ []string) ([]routeros.RouterOSObject, error) {
	return r[menu], nil
}

func (policyV2Router) AccountAccess(context.Context, string) (routeros.AccountAccess, error) {
	return routeros.AccountAccess{Username: "device-user", Group: "write", Policies: []string{"read", "write", "rest-api"}, Writable: true}, nil
}

func (policyV2Router) PolicyList(_ context.Context, menu routeros.ReadMenu, _ []string) ([]routeros.RouterOSObject, error) {
	if menu == routeros.ReadMenuInterfaceList {
		return []routeros.RouterOSObject{{"name": "LAN"}}, nil
	}
	if menu == routeros.ReadMenuInterface {
		return []routeros.RouterOSObject{{"name": "lan", "type": "ether", "running": "true"}}, nil
	}
	return []routeros.RouterOSObject{}, nil
}
func (policyV2Router) List(context.Context, routeros.MutationMenu, routeros.MutationQuery) ([]routeros.RouterOSObject, error) {
	return []routeros.RouterOSObject{}, nil
}
func (policyV2Router) Create(context.Context, routeros.MutationMenu, routeros.RouterOSFields) (routeros.RouterOSObject, error) {
	return routeros.RouterOSObject{".id": "*1"}, nil
}
func (policyV2Router) Patch(context.Context, routeros.MutationMenu, string, routeros.RouterOSFields) (routeros.RouterOSObject, error) {
	return routeros.RouterOSObject{".id": "*1"}, nil
}
func (policyV2Router) Delete(context.Context, routeros.MutationMenu, string) error { return nil }
func (policyV2Router) Move(context.Context, routeros.MutationMenu, routeros.MoveRequest) (routeros.MutationResponse, error) {
	return routeros.MutationResponse{}, nil
}
func (policyV2Router) SetDNSSettings(context.Context, routeros.RouterOSFields) error { return nil }
func (policyV2Router) FlushDNSCache(context.Context) error                           { return nil }

func newPolicyV2APIServer(t *testing.T) (*Server, *store.Store) {
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
	router := policyV2Router{}
	if err := manager.RegisterApplier("edge", &policyv2.Applier{Reader: router, Mutation: router, Repo: deviceStore.PolicyRepository()}); err != nil {
		storage.Close()
		t.Fatal(err)
	}
	return &Server{cfg: cfg, store: storage, policy: manager}, storage
}

func policyV2Request(t *testing.T, server *Server, method, path string, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, "/api/policy-routing"+path+"?device=edge", bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.servePolicyRoutingAPI(response, request)
	return response
}

func TestPolicyV2OverviewContractUsesNonNullCollections(t *testing.T) {
	server, storage := newPolicyV2APIServer(t)
	defer storage.Close()
	response := policyV2Request(t, server, http.MethodGet, "/overview", "")
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"egresses", "sources", "activeJobs", "pendingJobs"} {
		if _, ok := payload[field].([]any); !ok {
			t.Fatalf("%s is not an array: %#v", field, payload[field])
		}
	}
	setup := payload["setup"].(map[string]any)
	if setup["state"] != "ready" {
		t.Fatalf("setup state = %#v", setup["state"])
	}
}

func TestPolicyV2EgressCRUDMatchesFrontendShape(t *testing.T) {
	server, storage := newPolicyV2APIServer(t)
	defer storage.Close()
	create := policyV2Request(t, server, http.MethodPost, "/egresses", `{
        "name":"WAN A","priority":10,"listMode":"shared","listName":"proxy-a",
        "dnsUpstream":"1.1.1.1","failureMode":"strict","enabled":true,
        "families":[{"family":"ipv4","enabled":true,"wanInterface":"ether1","gateway":"192.0.2.1","routeTable":"proxy-a","routeMode":"strict","natMode":"masquerade","wanSource":"next-hop"}]
    }`)
	if create.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", create.Code, create.Body.String())
	}
	var created policyv2.Egress
	if err := json.Unmarshal(create.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.Revision != 1 || len(created.Families) != 1 {
		t.Fatalf("unexpected created egress: %#v", created)
	}

	updateBody, _ := json.Marshal(map[string]any{
		"name": "WAN renamed", "priority": 10, "listMode": "shared", "listName": "proxy-a",
		"failureMode": "strict", "enabled": true, "revision": created.Revision, "families": created.Families,
	})
	update := policyV2Request(t, server, http.MethodPut, "/egresses/"+created.ID, string(updateBody))
	if update.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", update.Code, update.Body.String())
	}
	var updated policyv2.Egress
	if err := json.Unmarshal(update.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.Name != "WAN renamed" || updated.Revision != 2 {
		t.Fatalf("unexpected updated egress: %#v", updated)
	}

	get := policyV2Request(t, server, http.MethodGet, "/egresses/"+created.ID, "")
	if get.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", get.Code, get.Body.String())
	}
	var loaded policyv2.Egress
	if err := json.Unmarshal(get.Body.Bytes(), &loaded); err != nil {
		t.Fatal(err)
	}
	if loaded.Name != updated.Name || len(loaded.Families) != 1 {
		t.Fatalf("unexpected loaded egress: %#v", loaded)
	}
}

func TestPolicyV2EgressDefaultsSharedListNameFromName(t *testing.T) {
	server, storage := newPolicyV2APIServer(t)
	defer storage.Close()
	response := policyV2Request(t, server, http.MethodPost, "/egresses", `{
        "name":"Google Exit","priority":10,
        "dnsUpstream":"1.1.1.1","failureMode":"strict","enabled":true,
        "families":[{"family":"ipv4","enabled":true,"wanSource":"next-hop","gateway":"192.0.2.1"}]
    }`)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var created policyv2.Egress
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ListMode != policyv2.ListModeShared || created.ListName != "manual_google_exit_lab" {
		t.Fatalf("unexpected shared list normalization: mode=%q name=%q", created.ListMode, created.ListName)
	}
}

func TestPolicyV2EgressRejectsDuplicateNameAndSharedListName(t *testing.T) {
	storage, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	repository := storage.PolicyRepository()
	if _, err := repository.SaveEgress(context.Background(), policyv2.Egress{
		ID: "existing", Name: "WAN A", ListMode: policyv2.ListModeShared, ListName: "manual_wan_a_lab", DNSUpstream: "1.1.1.1", FakeAlias: "192.0.2.80", FailureMode: "strict", Enabled: true,
		Families: []policyv2.EgressFamily{{Family: policyv2.FamilyIPv4, Enabled: true, WANSource: "next-hop", Gateway: "192.0.2.1"}},
	}); err != nil {
		t.Fatal(err)
	}

	duplicateName := policyv2.Egress{
		ID: "duplicate-name", Name: " wan a ", ListMode: policyv2.ListModeShared, DNSUpstream: "1.1.1.1", FakeAlias: "192.0.2.81", FailureMode: "strict", Enabled: true,
		Families: []policyv2.EgressFamily{{Family: policyv2.FamilyIPv4, Enabled: true, WANSource: "next-hop", Gateway: "192.0.2.1"}},
	}
	if err := normalizePolicyV2Egress(context.Background(), repository, nil, &duplicateName); err == nil || !strings.Contains(err.Error(), "egress name") {
		t.Fatalf("duplicate egress name was accepted or returned the wrong error: %v", err)
	}

	duplicateList := policyv2.Egress{
		ID: "duplicate-list", Name: "WAN-A", ListMode: policyv2.ListModeShared, DNSUpstream: "1.1.1.1", FakeAlias: "192.0.2.82", FailureMode: "strict", Enabled: true,
		Families: []policyv2.EgressFamily{{Family: policyv2.FamilyIPv4, Enabled: true, WANSource: "next-hop", Gateway: "192.0.2.1"}},
	}
	if err := normalizePolicyV2Egress(context.Background(), repository, nil, &duplicateList); err == nil || !strings.Contains(err.Error(), "shared address-list name") {
		t.Fatalf("duplicate shared list name was accepted or returned the wrong error: %v", err)
	}
}

func TestPolicyV2EgressAutoFillsMainGateway(t *testing.T) {
	storage, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	repository := storage.PolicyRepository()
	reader := policyGatewayReader{
		routeros.ReadMenuInterface: {
			{"name": "ether1", "type": "ether"},
		},
		routeros.ReadMenuIPRoute: {
			{"dst-address": "0.0.0.0/0", "gateway": "10.0.2.1", "immediate-gw": "10.0.2.1%ether1", "routing-table": "main", "active": "true"},
		},
	}
	eg := policyv2.Egress{
		ID: "auto", Name: "Auto", ListMode: policyv2.ListModeShared, ListName: "auto", DNSUpstream: "1.1.1.1", FakeAlias: "192.0.2.90", FailureMode: "strict", Enabled: true,
		Families: []policyv2.EgressFamily{{Family: policyv2.FamilyIPv4, Enabled: true, WANInterface: "ether1"}},
	}
	if err := normalizePolicyV2Egress(context.Background(), repository, reader, &eg); err != nil {
		t.Fatal(err)
	}
	if got := eg.Families[0].Gateway; got != "10.0.2.1" {
		t.Fatalf("gateway was not auto-filled: %q", got)
	}
}

func TestPolicyV2EgressAllowsPPPoEWithoutGateway(t *testing.T) {
	storage, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	eg := policyv2.Egress{
		ID: "pppoe", Name: "PPPoE", ListMode: policyv2.ListModeShared, ListName: "pppoe", DNSUpstream: "1.1.1.1", FakeAlias: "192.0.2.91", FailureMode: "strict", Enabled: true,
		Families: []policyv2.EgressFamily{{Family: policyv2.FamilyIPv4, Enabled: true, WANInterface: "pppoe-out1"}},
	}
	reader := policyGatewayReader{routeros.ReadMenuInterface: {{"name": "pppoe-out1", "type": "pppoe-out"}}}
	if err := normalizePolicyV2Egress(context.Background(), storage.PolicyRepository(), reader, &eg); err != nil {
		t.Fatal(err)
	}
	if got := eg.Families[0].Gateway; got != "" {
		t.Fatalf("PPPoE gateway should remain empty: %q", got)
	}
}

func TestPolicyV2EgressRequiresUnknownEthernetGateway(t *testing.T) {
	storage, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	eg := policyv2.Egress{
		ID: "ether", Name: "Ethernet", ListMode: policyv2.ListModeShared, ListName: "ether", DNSUpstream: "1.1.1.1", FakeAlias: "192.0.2.92", FailureMode: "strict", Enabled: true,
		Families: []policyv2.EgressFamily{{Family: policyv2.FamilyIPv4, Enabled: true, WANInterface: "ether1"}},
	}
	reader := policyGatewayReader{routeros.ReadMenuInterface: {{"name": "ether1", "type": "ether"}}}
	if err := normalizePolicyV2Egress(context.Background(), storage.PolicyRepository(), reader, &eg); err == nil {
		t.Fatal("unknown Ethernet gateway was accepted")
	}
}

func TestPolicyV2TrafficIngressRejectsBuiltInLists(t *testing.T) {
	server, storage := newPolicyV2APIServer(t)
	defer storage.Close()
	response := policyV2Request(t, server, http.MethodPut, "/traffic-ingress", `{"trafficIngress":{"interfaceLists":["all"],"interfaces":[]}}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestPolicyV2TrafficIngressReclassifiesLegacyInterfaceName(t *testing.T) {
	server, storage := newPolicyV2APIServer(t)
	defer storage.Close()
	response := policyV2Request(t, server, http.MethodPut, "/traffic-ingress", `{"trafficIngress":{"interfaceLists":["lan"],"interfaces":["lan"]}}`)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	deviceStore, err := storage.OpenDevice("edge")
	if err != nil {
		t.Fatal(err)
	}
	state, err := deviceStore.PolicyRepository().GetDeviceState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	scope, err := policyv2.ParseTrafficIngressScope(state.TrafficIngress)
	if err != nil {
		t.Fatal(err)
	}
	if len(scope.InterfaceLists) != 0 || len(scope.Interfaces) != 1 || scope.Interfaces[0] != "lan" {
		t.Fatalf("legacy selection was not normalized: %#v", scope)
	}
}

func TestPolicyV2SourceSaveRequiresAndConsumesPreview(t *testing.T) {
	server, storage := newPolicyV2APIServer(t)
	defer storage.Close()

	missing := policyV2Request(t, server, http.MethodPost, "/sources", `{"name":"list","type":"upload","enabled":true}`)
	if missing.Code != http.StatusUnprocessableEntity {
		t.Fatalf("missing preview status=%d body=%s", missing.Code, missing.Body.String())
	}

	prepared, err := policy.PrepareSourceContent([]byte("payload:\n  - DOMAIN,example.com\n"))
	if err != nil {
		t.Fatal(err)
	}
	previewID := server.savePolicyPreview(policyPreviewEntry{DeviceID: "edge", SourceType: "upload", Content: prepared})
	created := policyV2Request(t, server, http.MethodPost, "/sources", `{"name":"list","type":"upload","enabled":true,"previewId":"`+previewID+`"}`)
	if created.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	var source policyv2.Source
	if err := json.Unmarshal(created.Body.Bytes(), &source); err != nil {
		t.Fatal(err)
	}
	if source.ID == "" || source.Revision != 2 || len(source.Versions) != 1 || source.Versions[0].State != "pending" {
		t.Fatalf("unexpected source: %#v", source)
	}
	if _, ok := server.policyPreview(previewID, "edge"); ok {
		t.Fatal("consumed preview remains available")
	}
}

func TestPolicyV2AssignedSourceSaveStartsAutomaticApply(t *testing.T) {
	server, storage := newPolicyV2APIServer(t)
	defer storage.Close()
	deviceStore, err := storage.OpenDevice("edge")
	if err != nil {
		t.Fatal(err)
	}
	repository := deviceStore.PolicyRepository()
	if _, err := repository.SaveEgress(context.Background(), policyv2.Egress{
		ID: "wan", Name: "WAN", ListMode: policyv2.ListModeShared, ListName: "proxy", DNSUpstream: "1.1.1.1", FakeAlias: "192.0.2.70", FailureMode: "strict", Enabled: true,
		Families: []policyv2.EgressFamily{{Family: policyv2.FamilyIPv4, Enabled: true, Gateway: "198.51.100.1"}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.SaveTrafficIngress(context.Background(), []byte(`{"interfaceLists":["LAN"],"interfaces":[]}`)); err != nil {
		t.Fatal(err)
	}
	prepared, err := policy.PrepareSourceContent([]byte("payload:\n  - DOMAIN,example.com\n"))
	if err != nil {
		t.Fatal(err)
	}
	previewID := server.savePolicyPreview(policyPreviewEntry{DeviceID: "edge", SourceType: "upload", Content: prepared})
	payload, err := json.Marshal(map[string]any{
		"name": "assigned-list", "type": "upload", "enabled": true, "egressId": "wan", "previewId": previewID,
	})
	if err != nil {
		t.Fatal(err)
	}
	response := policyV2Request(t, server, http.MethodPost, "/sources", string(payload))
	if response.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var accepted struct {
		Source policyv2.Source `json:"source"`
		JobID  string          `json:"jobId"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &accepted); err != nil {
		t.Fatal(err)
	}
	if accepted.Source.ID == "" || accepted.JobID == "" {
		t.Fatalf("automatic apply response is incomplete: %#v", accepted)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		job, err := repository.GetApplyJob(context.Background(), accepted.JobID)
		if err == nil && job.Terminal() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	job, err := repository.GetApplyJob(context.Background(), accepted.JobID)
	if err != nil {
		t.Fatal(err)
	}
	t.Fatalf("automatic apply did not reach a terminal state: %#v", job)
}

func TestPolicyV2PlanAndApplyEndpointsMatchFrontendContract(t *testing.T) {
	server, storage := newPolicyV2APIServer(t)
	defer storage.Close()
	deviceStore, err := storage.OpenDevice("edge")
	if err != nil {
		t.Fatal(err)
	}
	repository := deviceStore.PolicyRepository()
	if _, err := repository.SaveEgress(context.Background(), policyv2.Egress{
		ID: "wan", Name: "WAN", ListMode: policyv2.ListModeShared, ListName: "proxy", DNSUpstream: "1.1.1.1", FakeAlias: "192.0.2.70", FailureMode: "strict", Enabled: true,
		Families: []policyv2.EgressFamily{{Family: policyv2.FamilyIPv4, Enabled: true, WANInterface: "ether1", Gateway: "198.51.100.1"}},
	}); err != nil {
		t.Fatal(err)
	}
	ingress := policyV2Request(t, server, http.MethodPut, "/traffic-ingress", `{"trafficIngress":{"interfaceLists":["LAN"],"interfaces":[]}}`)
	if ingress.Code != http.StatusOK {
		t.Fatalf("traffic ingress status=%d body=%s", ingress.Code, ingress.Body.String())
	}
	preview := policyV2Request(t, server, http.MethodPost, "/plans", `{"kind":"initial"}`)
	if preview.Code != http.StatusCreated {
		t.Fatalf("plan status=%d body=%s", preview.Code, preview.Body.String())
	}
	var envelope struct {
		PlanID   string `json:"planId"`
		PlanHash string `json:"planHash"`
		Plan     struct {
			State      string                   `json:"state"`
			Operations []policyv2.PlanOperation `json:"operations"`
		} `json:"plan"`
	}
	if err := json.Unmarshal(preview.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.PlanID == "" || envelope.PlanHash == "" || envelope.Plan.State != "ready" || len(envelope.Plan.Operations) == 0 {
		t.Fatalf("unexpected plan envelope: %#v", envelope)
	}
	apply := policyV2Request(t, server, http.MethodPost, "/plans/"+envelope.PlanID+"/apply", `{}`)
	if apply.Code != http.StatusAccepted {
		t.Fatalf("apply status=%d body=%s", apply.Code, apply.Body.String())
	}
	var accepted map[string]any
	if err := json.Unmarshal(apply.Body.Bytes(), &accepted); err != nil {
		t.Fatal(err)
	}
	if accepted["jobId"] == "" || accepted["job"] == nil {
		t.Fatalf("unexpected apply response: %#v", accepted)
	}
}
