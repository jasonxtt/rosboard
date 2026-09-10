package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"rosboard/internal/policyv2"
	"rosboard/internal/routeros"
)

type countingPolicyV2Router struct {
	policyV2Router
	creates int
	patches int
	deletes int
	moves   int
}

func (r *countingPolicyV2Router) Create(ctx context.Context, menu routeros.MutationMenu, fields routeros.RouterOSFields) (routeros.RouterOSObject, error) {
	r.creates++
	return r.policyV2Router.Create(ctx, menu, fields)
}

func (r *countingPolicyV2Router) Patch(ctx context.Context, menu routeros.MutationMenu, id string, fields routeros.RouterOSFields) (routeros.RouterOSObject, error) {
	r.patches++
	return r.policyV2Router.Patch(ctx, menu, id, fields)
}

func (r *countingPolicyV2Router) Delete(ctx context.Context, menu routeros.MutationMenu, id string) error {
	r.deletes++
	return r.policyV2Router.Delete(ctx, menu, id)
}

func (r *countingPolicyV2Router) Move(ctx context.Context, menu routeros.MutationMenu, request routeros.MoveRequest) (routeros.MutationResponse, error) {
	r.moves++
	return r.policyV2Router.Move(ctx, menu, request)
}

func (r *countingPolicyV2Router) mutationCount() int {
	return r.creates + r.patches + r.deletes + r.moves
}

func TestRoutingSourceScopeConflictUsesStableAPIError(t *testing.T) {
	response := httptest.NewRecorder()
	writeRoutingRuleSaveError(response, policyv2.ErrRoutingSourceScopeConflict)
	if response.Code != http.StatusConflict {
		t.Fatalf("status=%d, want %d", response.Code, http.StatusConflict)
	}
	var payload map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["code"] != "routing_source_scope_conflict" {
		t.Fatalf("code=%q, want routing_source_scope_conflict", payload["code"])
	}
}

func TestCanonicalDeviceRoutingRuleAPILegacyJSONRoundTripPreservesIdentity(t *testing.T) {
	server, storage := newPolicyV2APIServer(t)
	defer storage.Close()
	deviceStore, err := storage.OpenDevice("edge")
	if err != nil {
		t.Fatal(err)
	}
	repository := deviceStore.PolicyRepository()
	ctx := context.Background()
	if _, err := repository.SaveEgress(ctx, policyv2.Egress{ID: "api-device-egress", Name: "API device egress"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.SaveTargetList(ctx, policyv2.TargetList{ID: "api-device-target", Name: "API device target", Kind: policyv2.KindIP, SourceType: policyv2.TargetSourceTypeManual, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	saved, err := repository.SaveRoutingRule(ctx, policyv2.RoutingRule{
		ID: "api-device-rule", Name: "API device", EgressID: "api-device-egress", TargetListIDs: []string{"api-device-target"}, Enabled: true,
		SourceScope: &policyv2.RoutingSourceScope{Kind: policyv2.RoutingSourceDevice},
		Subject: policyv2.Subject{Mode: policyv2.SubjectModeSelected, Members: []policyv2.SubjectMember{{
			TerminalID: "terminal-a", Binding: "auto", AnchorMAC: "AA:BB:CC:DD:EE:FF", LastIPv4: []string{"10.0.0.10"},
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}

	payload, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	var legacy policyv2.RoutingRule
	if err := json.Unmarshal(payload, &legacy); err != nil {
		t.Fatal(err)
	}
	legacy.SourceScope = nil
	legacy.Name = "API device renamed"
	saveBody := func(rule policyv2.RoutingRule) string {
		t.Helper()
		body, marshalErr := json.Marshal(map[string]any{
			"name": rule.Name, "subject": rule.Subject, "ingress": rule.Ingress,
			"targetListIds": rule.TargetListIDs, "egressId": rule.EgressID,
			"priority": rule.Priority, "enabled": rule.Enabled, "revision": rule.Revision, "deferApply": true,
		})
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		return string(body)
	}

	updatedResponse := policyV2Request(t, server, http.MethodPut, "/rules/"+saved.ID, saveBody(legacy))
	if updatedResponse.Code != http.StatusOK {
		t.Fatalf("legacy device API edit status=%d body=%s", updatedResponse.Code, updatedResponse.Body.String())
	}
	updated, err := repository.GetRoutingRule(ctx, saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "API device renamed" || updated.Revision != saved.Revision+1 || updated.SourceScope == nil {
		t.Fatalf("legacy device API edit did not preserve canonical source: %#v", updated)
	}
	if member := updated.Subject.Members[0]; member.AnchorMAC != "AA:BB:CC:DD:EE:FF" || len(member.LastIPv4) != 1 || member.LastIPv4[0] != "10.0.0.10" {
		t.Fatalf("legacy device API edit lost hidden identity: %#v", member)
	}

	changed := updated
	changed.SourceScope = nil
	changed.Subject.Members = append([]policyv2.SubjectMember(nil), updated.Subject.Members...)
	changed.Subject.Members[0].TerminalID = "terminal-b"
	conflictResponse := policyV2Request(t, server, http.MethodPut, "/rules/"+saved.ID, saveBody(changed))
	if conflictResponse.Code != http.StatusConflict {
		t.Fatalf("changed device source status=%d body=%s, want %d", conflictResponse.Code, conflictResponse.Body.String(), http.StatusConflict)
	}
	var conflict map[string]any
	if err := json.Unmarshal(conflictResponse.Body.Bytes(), &conflict); err != nil {
		t.Fatal(err)
	}
	if conflict["code"] != "routing_source_scope_conflict" {
		t.Fatalf("changed device source code=%v, want routing_source_scope_conflict", conflict["code"])
	}
}

func TestCanonicalDevicePlanAPIUsesLegacyAuthorityGuard(t *testing.T) {
	server, storage := newPolicyV2APIServer(t)
	defer storage.Close()
	deviceStore, err := storage.OpenDevice("edge")
	if err != nil {
		t.Fatal(err)
	}
	repository := deviceStore.PolicyRepository()
	ctx := context.Background()
	if _, err := repository.SaveEgress(ctx, policyv2.Egress{
		ID: "plan-device-egress", Name: "Plan device egress", ListMode: policyv2.ListModeShared, ListName: "plan-device", DNSUpstream: "1.1.1.1", FakeAlias: "192.0.2.71", FailureMode: "strict", Enabled: true,
		Families: []policyv2.EgressFamily{{Family: policyv2.FamilyIPv4, Enabled: true, WANInterface: "lan", Gateway: "198.51.100.1"}},
	}); err != nil {
		t.Fatal(err)
	}
	target := seedAccessSource(t, server, "plan-device-target")
	saved, err := repository.SaveRoutingRule(ctx, policyv2.RoutingRule{
		ID: "plan-device-rule", Name: "Plan device", EgressID: "plan-device-egress", TargetListIDs: []string{target.ID}, Enabled: true,
		SourceScope: &policyv2.RoutingSourceScope{Kind: policyv2.RoutingSourceDevice},
		Subject: policyv2.Subject{Mode: policyv2.SubjectModeSelected, Members: []policyv2.SubjectMember{{
			TerminalID: "terminal-a", Binding: "auto", AnchorMAC: "AA:BB:CC:DD:EE:FF", LastIPv4: []string{"10.0.0.10"},
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}

	payload, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	var legacy policyv2.RoutingRule
	if err := json.Unmarshal(payload, &legacy); err != nil {
		t.Fatal(err)
	}
	legacy.SourceScope = nil
	legacy.Name = "Plan device renamed"
	planBody := func(rule policyv2.RoutingRule) string {
		t.Helper()
		body, marshalErr := json.Marshal(map[string]any{
			"kind":     "routing-rule-save",
			"proposal": map[string]any{"routingRule": rule},
		})
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		return string(body)
	}

	preview := policyV2Request(t, server, http.MethodPost, "/plans", planBody(legacy))
	if preview.Code != http.StatusCreated {
		t.Fatalf("equivalent legacy plan status=%d body=%s", preview.Code, preview.Body.String())
	}

	changed := legacy
	changed.Subject.Members = append([]policyv2.SubjectMember(nil), legacy.Subject.Members...)
	changed.Subject.Members[0].TerminalID = "terminal-b"
	conflict := policyV2Request(t, server, http.MethodPost, "/plans", planBody(changed))
	if conflict.Code != http.StatusConflict {
		t.Fatalf("changed legacy plan status=%d body=%s, want %d", conflict.Code, conflict.Body.String(), http.StatusConflict)
	}
	var payloadError map[string]any
	if err := json.Unmarshal(conflict.Body.Bytes(), &payloadError); err != nil {
		t.Fatal(err)
	}
	if payloadError["code"] != "routing_source_scope_conflict" {
		t.Fatalf("changed legacy plan code=%v, want routing_source_scope_conflict", payloadError["code"])
	}
}

func TestCanonicalInterfaceListAllPlanBlocksAndDirectApplyDoesNotMutate(t *testing.T) {
	server, storage := newPolicyV2APIServer(t)
	defer storage.Close()
	deviceStore, err := storage.OpenDevice("edge")
	if err != nil {
		t.Fatal(err)
	}
	repository := deviceStore.PolicyRepository()
	ctx := context.Background()
	if _, err := repository.SaveEgress(ctx, policyv2.Egress{
		ID: "all-list-egress", Name: "All list egress", ListMode: policyv2.ListModeShared, ListName: "all-list", DNSUpstream: "1.1.1.1", FakeAlias: "192.0.2.72", FailureMode: "strict", Enabled: true,
		Families: []policyv2.EgressFamily{{Family: policyv2.FamilyIPv4, Enabled: true, WANInterface: "lan", Gateway: "198.51.100.1"}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.SaveTargetList(ctx, policyv2.TargetList{ID: "all-list-target", Name: "All list target", Kind: policyv2.KindIP, SourceType: policyv2.TargetSourceTypeManual, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.SaveRoutingRule(ctx, policyv2.RoutingRule{
		ID: "all-list-rule", Name: "All list rule", EgressID: "all-list-egress", TargetListIDs: []string{"all-list-target"}, Enabled: true,
		SourceScope: &policyv2.RoutingSourceScope{Kind: policyv2.RoutingSourceInterfaceList, Name: "all"},
	}); err != nil {
		t.Fatal(err)
	}

	preview := policyV2Request(t, server, http.MethodPost, "/plans", `{"kind":"initial"}`)
	if preview.Code != http.StatusCreated {
		t.Fatalf("all-list plan status=%d body=%s", preview.Code, preview.Body.String())
	}
	var envelope policyv2.PlanEnvelope
	if err := json.Unmarshal(preview.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Plan.State != "blocked" {
		t.Fatalf("all-list plan state=%q, want blocked: %#v", envelope.Plan.State, envelope.Plan.Blockers)
	}
	foundBlocker := false
	for _, blocker := range envelope.Plan.Blockers {
		if blocker.Code == policyv2.RoutingSourceInterfaceListAllDeferredCode && blocker.LogicalID == "all-list-rule" {
			foundBlocker = true
		}
	}
	if !foundBlocker {
		t.Fatalf("all-list plan lost stable safety blocker: %#v", envelope.Plan.Blockers)
	}
	loaded, err := repository.GetRoutingRule(ctx, "all-list-rule")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.SourceScope == nil || loaded.SourceScope.Kind != policyv2.RoutingSourceInterfaceList || loaded.SourceScope.Name != "all" {
		t.Fatalf("canonical all-list source was not persisted: %#v", loaded)
	}

	countingRouter := &countingPolicyV2Router{}
	if err := server.policy.RegisterApplier("edge", &policyv2.Applier{Reader: countingRouter, Mutation: countingRouter, Repo: deviceStore.PolicyRepository(), Access: deviceStore.AccessRepository()}); err != nil {
		t.Fatal(err)
	}
	deferApply := policyV2Request(t, server, http.MethodPut, "/rules/all-list-rule", `{"name":"All list direct edit","sourceScope":{"kind":"interface-list","name":"all"},"targetListIds":["all-list-target"],"egressId":"all-list-egress","enabled":true,"revision":1}`)
	if deferApply.Code != http.StatusUnprocessableEntity {
		t.Fatalf("all-list direct auto-apply status=%d body=%s", deferApply.Code, deferApply.Body.String())
	}
	var applyError map[string]any
	if err := json.Unmarshal(deferApply.Body.Bytes(), &applyError); err != nil {
		t.Fatal(err)
	}
	if applyError["code"] != "plan_blocked" {
		t.Fatalf("all-list direct auto-apply code=%v, want plan_blocked", applyError["code"])
	}
	if countingRouter.mutationCount() != 0 {
		t.Fatalf("blocked all-list direct save mutated RouterOS: creates=%d patches=%d deletes=%d moves=%d", countingRouter.creates, countingRouter.patches, countingRouter.deletes, countingRouter.moves)
	}
}
