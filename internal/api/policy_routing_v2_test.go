package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"rosboard/internal/config"
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
	if menu == routeros.ReadMenuIPDNS {
		return []routeros.RouterOSObject{{"servers": "192.0.2.53"}}, nil
	}
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
func (policyV2Router) VerifyAccessControlCapabilities(context.Context, []routeros.MutationMenu) error {
	return nil
}

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
	if err := manager.RegisterApplier("edge", &policyv2.Applier{Reader: router, Mutation: router, Repo: deviceStore.PolicyRepository(), Access: deviceStore.AccessRepository()}); err != nil {
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
	for _, field := range []string{"egresses", "targetLists", "rules", "activeJobs", "pendingJobs"} {
		if _, ok := payload[field].([]any); !ok {
			t.Fatalf("%s is not an array: %#v", field, payload[field])
		}
	}
	setup := payload["setup"].(map[string]any)
	if setup["state"] != "ready" {
		t.Fatalf("setup state = %#v", setup["state"])
	}
}

func TestPolicyV2NewProposalAllocatesEgressIdentity(t *testing.T) {
	server, storage := newPolicyV2APIServer(t)
	defer storage.Close()
	device, ok := server.resolvePolicyDevice(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/?device=edge", nil))
	if !ok {
		t.Fatal("failed to resolve test policy device")
	}

	proposal, err := server.preparePolicyPlanProposal(context.Background(), device, &policyPlanProposalPayload{
		Egress: &policyv2.Egress{
			Name: "New WAN",
			Families: []policyv2.EgressFamily{{
				Family: policyv2.FamilyIPv4, Enabled: true, Gateway: "198.51.100.1",
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if proposal == nil || proposal.Egress == nil {
		t.Fatalf("new egress proposal was not retained: %#v", proposal)
	}
	if strings.TrimSpace(proposal.Egress.ID) == "" {
		t.Fatalf("new egress proposal has no identity: %#v", proposal.Egress)
	}
	if proposal.Egress.FakeAlias != "" {
		t.Fatalf("omitted fake alias became an explicit proposal value: %#v", proposal.Egress)
	}
}

func TestPolicyV2ChangedSharedEgressRebindsRoutingRule(t *testing.T) {
	server, storage := newPolicyV2APIServer(t)
	defer storage.Close()
	deviceStore, err := storage.OpenDevice("edge")
	if err != nil {
		t.Fatal(err)
	}
	repository := deviceStore.PolicyRepository()
	ctx := context.Background()
	if _, err := repository.SaveEgress(ctx, policyv2.Egress{
		ID: "shared-edit-egress", Name: "Shared edit", Enabled: true,
		DNSUpstream: "1.1.1.1", FailureMode: "strict",
		Families: []policyv2.EgressFamily{{Family: policyv2.FamilyIPv4, Enabled: true, Gateway: "192.0.2.1"}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.SaveEgress(ctx, policyv2.Egress{
		ID: "shared-edit-equivalent", Name: "Equivalent edit", Enabled: true,
		DNSUpstream: "1.1.1.1", FailureMode: "strict",
		Families: []policyv2.EgressFamily{{Family: policyv2.FamilyIPv4, Enabled: true, Gateway: "192.0.2.3"}},
	}); err != nil {
		t.Fatal(err)
	}
	target, err := repository.SaveTargetList(ctx, policyv2.TargetList{ID: "shared-edit-target", Name: "Shared edit target", Kind: policyv2.KindIP, SourceType: policyv2.TargetSourceTypeManual, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.SavePendingTargetListVersion(ctx, policyv2.TargetListVersion{ID: "shared-edit-version", TargetListID: target.ID, SHA256: "shared-edit", CompressedYAML: []byte("shared-edit"), State: "pending"}, []policyv2.TargetListRule{{RuleType: "IP-CIDR", Domain: "192.0.2.0/24"}}); err != nil {
		t.Fatal(err)
	}
	for _, ruleID := range []string{"shared-edit-a", "shared-edit-b"} {
		if _, err := repository.SaveRoutingRule(ctx, policyv2.RoutingRule{
			ID: ruleID, Name: ruleID, Subject: policyv2.Subject{Mode: policyv2.SubjectModeSelected, Prefixes: []string{"10.0.0.20/32"}},
			TargetListIDs: []string{target.ID}, EgressID: "shared-edit-egress", Enabled: true,
		}); err != nil {
			t.Fatal(err)
		}
	}
	current, err := repository.GetRoutingRule(ctx, "shared-edit-a")
	if err != nil {
		t.Fatal(err)
	}
	currentEgress, err := repository.GetEgress(ctx, current.EgressID)
	if err != nil {
		t.Fatal(err)
	}
	currentEgress.Families = append([]policyv2.EgressFamily(nil), currentEgress.Families...)
	currentEgress.Families[0].Gateway = "192.0.2.2"
	device, ok := server.resolvePolicyDevice(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/?device=edge", nil))
	if !ok {
		t.Fatal("failed to resolve test policy device")
	}
	proposal, err := server.preparePolicyPlanProposal(ctx, device, &policyPlanProposalPayload{
		Egress: &currentEgress,
		RoutingRule: &policyv2.RoutingRule{
			ID: current.ID, Name: current.Name, Subject: current.Subject, Ingress: current.Ingress,
			TargetListIDs: current.TargetListIDs, EgressID: current.EgressID, Priority: current.Priority, Enabled: current.Enabled, Revision: current.Revision,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if proposal == nil || proposal.Egress == nil || proposal.RoutingRule == nil {
		t.Fatalf("shared egress edit did not produce a complete proposal: %#v", proposal)
	}
	if proposal.Egress.ID == current.EgressID || proposal.RoutingRule.EgressID == current.EgressID {
		t.Fatalf("shared egress edit did not rebind to Copy-on-Write identity: egress=%#v rule=%#v", proposal.Egress, proposal.RoutingRule)
	}

	currentEgress.Families[0].Gateway = "192.0.2.3"
	equivalentProposal, err := server.preparePolicyPlanProposal(ctx, device, &policyPlanProposalPayload{
		Egress: &currentEgress,
		RoutingRule: &policyv2.RoutingRule{
			ID: current.ID, Name: current.Name, Subject: current.Subject, Ingress: current.Ingress,
			TargetListIDs: current.TargetListIDs, EgressID: current.EgressID, Priority: current.Priority, Enabled: current.Enabled, Revision: current.Revision,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if equivalentProposal == nil || equivalentProposal.Egress != nil || equivalentProposal.RoutingRule == nil || equivalentProposal.RoutingRule.EgressID != "shared-edit-equivalent" {
		var proposedEgress any
		if equivalentProposal != nil && equivalentProposal.Egress != nil {
			proposedEgress = *equivalentProposal.Egress
		}
		var proposedRule any
		if equivalentProposal != nil && equivalentProposal.RoutingRule != nil {
			proposedRule = *equivalentProposal.RoutingRule
		}
		t.Fatalf("shared egress edit did not rebind to equivalent existing identity: egress=%+v rule=%+v", proposedEgress, proposedRule)
	}
}

func TestPolicyV2RoutingRuleCRUDUsesCanonicalTargetReferences(t *testing.T) {
	server, storage := newPolicyV2APIServer(t)
	defer storage.Close()
	deviceStore, err := storage.OpenDevice("edge")
	if err != nil {
		t.Fatal(err)
	}
	repository := deviceStore.PolicyRepository()
	ctx := context.Background()
	if _, err := repository.SaveEgress(ctx, policyv2.Egress{
		ID: "wan-rule", Name: "WAN rule", Enabled: true,
		Families: []policyv2.EgressFamily{{Family: policyv2.FamilyIPv4, Enabled: true, Gateway: "192.0.2.1"}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.SaveTrafficIngress(ctx, []byte(`{"interfaceLists":["LAN"],"interfaces":[]}`)); err != nil {
		t.Fatal(err)
	}
	target, err := repository.SaveTargetList(ctx, policyv2.TargetList{
		ID: "target-rule", Name: "Rule target", Kind: policyv2.KindIP, SourceType: policyv2.TargetSourceTypeManual, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.SavePendingTargetListVersion(ctx, policyv2.TargetListVersion{ID: "target-rule-version", TargetListID: target.ID, SHA256: "target-rule", CompressedYAML: []byte("target"), State: "pending"}, []policyv2.TargetListRule{{RuleType: "IP-CIDR", Domain: "192.0.2.0/24"}}); err != nil {
		t.Fatal(err)
	}
	request := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, "/api/policy-routing"+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		server.servePolicyRoutingAPI(response, req)
		return response
	}
	createdResponse := request(http.MethodPost, "/rules?device=edge", `{"name":"Rule A","subject":{"mode":"all"},"targetListIds":["target-rule"],"egressId":"wan-rule","priority":10,"enabled":true,"deferApply":true}`)
	if createdResponse.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", createdResponse.Code, createdResponse.Body.String())
	}
	var created policyv2.RoutingRule
	if err := json.Unmarshal(createdResponse.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.Revision != 1 || created.Subject.Mode != policyv2.SubjectModeAll || len(created.TargetListIDs) != 1 {
		t.Fatalf("created routing rule = %#v", created)
	}
	listResponse := request(http.MethodGet, "/rules?device=edge", "")
	if listResponse.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listResponse.Code, listResponse.Body.String())
	}
	var listed struct {
		Rules []policyv2.RoutingRule `json:"rules"`
	}
	if err := json.Unmarshal(listResponse.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Rules) != 1 || listed.Rules[0].ID != created.ID {
		t.Fatalf("listed routing rules = %#v", listed.Rules)
	}
	getResponse := request(http.MethodGet, "/rules/"+created.ID+"?device=edge", "")
	if getResponse.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", getResponse.Code, getResponse.Body.String())
	}
	var loaded policyv2.RoutingRule
	if err := json.Unmarshal(getResponse.Body.Bytes(), &loaded); err != nil {
		t.Fatal(err)
	}
	updatedBody, err := json.Marshal(map[string]any{
		"name": "Rule renamed", "subject": loaded.Subject, "targetListIds": loaded.TargetListIDs,
		"egressId": loaded.EgressID, "priority": loaded.Priority, "enabled": loaded.Enabled,
		"revision": loaded.Revision, "deferApply": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	updatedResponse := request(http.MethodPut, "/rules/"+created.ID+"?device=edge", string(updatedBody))
	if updatedResponse.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", updatedResponse.Code, updatedResponse.Body.String())
	}
	if err := json.Unmarshal(updatedResponse.Body.Bytes(), &loaded); err != nil {
		t.Fatal(err)
	}
	if loaded.Name != "Rule renamed" || loaded.Revision != 2 {
		t.Fatalf("updated routing rule = %#v", loaded)
	}
	deletedResponse := request(http.MethodDelete, "/rules/"+created.ID+"?device=edge&revision="+strconv.FormatInt(loaded.Revision, 10), "")
	if deletedResponse.Code != http.StatusOK && deletedResponse.Code != http.StatusAccepted {
		t.Fatalf("delete status=%d body=%s", deletedResponse.Code, deletedResponse.Body.String())
	}
	if _, err := repository.GetRoutingRule(ctx, created.ID); !errors.Is(err, policyv2.ErrRoutingRuleNotFound) {
		t.Fatalf("deleted routing rule error = %v", err)
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
	if updated.Name != created.Name || updated.Revision != 2 {
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
	if created.ListMode != policyv2.ListModeShared || created.Name == "" || created.ListName != policyv2.SharedListName(created.Name) {
		t.Fatalf("unexpected shared list normalization: mode=%q name=%q", created.ListMode, created.ListName)
	}
}

func TestPolicyV2EgressGeneratesServerOwnedNamesWithoutUniquenessBlocking(t *testing.T) {
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
	if err := normalizePolicyV2Egress(context.Background(), repository, nil, &duplicateName); err != nil {
		t.Fatalf("duplicate egress name was rejected: %v", err)
	}
	if duplicateName.Name == "" || duplicateName.Name == " wan a " || duplicateName.ListName != policyv2.SharedListName(duplicateName.Name) {
		t.Fatalf("egress name was not generated by the server: %#v", duplicateName)
	}
	second := duplicateName
	second.ID = "duplicate-list"
	if err := normalizePolicyV2Egress(context.Background(), repository, nil, &second); err != nil {
		t.Fatalf("second equivalent egress was rejected: %v", err)
	}
	if second.Name == duplicateName.Name {
		t.Fatalf("server-owned names should remain stable per egress identity: %#v", second)
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

func TestPolicyV2LegacySourceAPIIsRetired(t *testing.T) {
	server, storage := newPolicyV2APIServer(t)
	defer storage.Close()

	for _, path := range []string{"/sources", "/sources/manual/preview", "/sources/legacy/rules"} {
		response := policyV2Request(t, server, http.MethodGet, path, "")
		if response.Code != http.StatusNotFound {
			t.Fatalf("retired source endpoint %s status=%d body=%s", path, response.Code, response.Body.String())
		}
	}
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
