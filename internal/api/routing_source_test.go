package api

import (
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
