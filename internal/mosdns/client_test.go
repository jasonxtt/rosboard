package mosdns

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAuditLogsDecodesV2Response(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v2/audit/logs" || request.URL.Query().Get("page") != "2" || request.URL.Query().Get("limit") != "7" {
			t.Fatalf("unexpected audit request: %s", request.URL.String())
		}
		writer.Header().Set("Content-Type", "application/json")
		fmt.Fprint(writer, `{"pagination":{"total_items":1,"total_pages":1,"current_page":2,"items_per_page":7},"logs":[{"client_ip":"::ffff:10.0.0.8","query_type":"A","query_name":"Example.COM.","query_time":"2026-08-02T01:02:03Z","trace_id":"trace-1","effective_tag":"direct","answers":[{"type":"A","ttl":60,"data":"192.0.2.1"}]}]}`)
	}))
	defer server.Close()

	client, err := NewClient(server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.AuditLogs(context.Background(), 2, 7)
	if err != nil {
		t.Fatal(err)
	}
	if response.Pagination.TotalItems != 1 || len(response.Logs) != 1 || response.Logs[0].TraceID != "trace-1" {
		t.Fatalf("unexpected response: %#v", response)
	}
}

func TestExpandAuditLogNormalizesAndDeduplicates(t *testing.T) {
	queryTime := time.Date(2026, 8, 2, 1, 2, 3, 4, time.FixedZone("CST", 8*60*60))
	record := AuditLog{
		ClientIP:  "::ffff:10.0.0.8",
		QueryType: "a",
		QueryName: "Example.COM.",
		QueryTime: queryTime,
		Answers: []AuditAnswer{
			{Type: "A", TTL: 60, Data: "192.0.2.1"},
			{Type: "A", TTL: 60, Data: "192.0.2.1"},
			{Type: "AAAA", TTL: 60, Data: "2001:db8::1"},
			{Type: "CNAME", TTL: 60, Data: "target.example"},
		},
	}
	observations, skipped := ExpandAuditLog(record, time.Date(2026, 8, 2, 2, 0, 0, 0, time.UTC))
	if len(observations) != 3 || skipped != 1 {
		t.Fatalf("unexpected expansion: observations=%d skipped=%d", len(observations), skipped)
	}
	if observations[0].ClientIP != "10.0.0.8" || observations[0].Domain != "example.com" || observations[0].QueryType != "A" || observations[0].QueryTime != queryTime.UTC() {
		t.Fatalf("normalization failed: %#v", observations[0])
	}
	if observations[0].DedupeKey != observations[1].DedupeKey {
		t.Fatalf("same answer should have the same fallback dedupe key: %q vs %q", observations[0].DedupeKey, observations[1].DedupeKey)
	}
	if observations[0].DedupeKey == observations[2].DedupeKey {
		t.Fatalf("different answer IPs must not share a dedupe key")
	}

	record.TraceID = "trace-1"
	withTrace, _ := ExpandAuditLog(record, time.Now())
	if withTrace[0].DedupeKey != "trace:trace-1|192.0.2.1" {
		t.Fatalf("unexpected trace dedupe key: %q", withTrace[0].DedupeKey)
	}
}

func TestAuditLogsReturnsHTTPErrorBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Error(writer, "audit unavailable", http.StatusBadGateway)
	}))
	defer server.Close()
	client, err := NewClient(server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.AuditLogs(context.Background(), 1, 1); err == nil || !strings.Contains(err.Error(), "audit unavailable") {
		t.Fatalf("expected MosDNS error body, got %v", err)
	}
}
