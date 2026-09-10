package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"rosboard/internal/policyv2"
)

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
