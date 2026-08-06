package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"rosboard/internal/config"
	mosclient "rosboard/internal/mosdns"
	"rosboard/internal/store"
)

func TestMosDNSSynchronizerPaginatesWatermarksAndKeepsFailuresAtomic(t *testing.T) {
	baseTime := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	pageOne := make([]mosclient.AuditLog, 0, 200)
	for index := 199; index >= 0; index-- {
		pageOne = append(pageOne, mosclient.AuditLog{
			ClientIP:  "::ffff:10.0.0.8",
			QueryType: "A",
			QueryName: "example.com.",
			QueryTime: baseTime.Add(time.Duration(index) * time.Second),
			TraceID:   "trace-" + strconv.Itoa(index),
			Answers:   []mosclient.AuditAnswer{{Type: "A", TTL: 60, Data: "192.0.2.1"}},
		})
	}
	var failed atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if failed.Load() {
			http.Error(writer, "temporary failure", http.StatusBadGateway)
			return
		}
		page := request.URL.Query().Get("page")
		response := mosclient.AuditLogsResponse{Pagination: mosclient.Pagination{TotalPages: 2}}
		if page == "1" {
			response.Logs = pageOne
		} else {
			response.Logs = []mosclient.AuditLog{{
				ClientIP: "10.0.0.8", QueryType: "A", QueryName: "old.example.com", QueryTime: baseTime.Add(-time.Minute), TraceID: "trace-old",
				Answers: []mosclient.AuditAnswer{{Type: "A", TTL: 60, Data: "192.0.2.2"}},
			}}
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(response)
	}))
	defer server.Close()

	storage, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	synchronizer, err := NewMosDNSSynchronizer(config.MosDNSConfig{Enabled: true, BaseURL: server.URL, SyncIntervalMinutes: 30}, storage, nil, 48)
	if err != nil {
		t.Fatal(err)
	}
	if err := synchronizer.SyncOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	observations, err := storage.DNSObservations(context.Background(), 500)
	if err != nil {
		t.Fatal(err)
	}
	if len(observations) != 201 {
		t.Fatalf("expected both pages to be imported, got %d", len(observations))
	}
	status := synchronizer.Status()
	if status.LastImported != 201 || status.LastDuplicates != 0 || !status.Watermark.Equal(baseTime.Add(199*time.Second)) {
		t.Fatalf("unexpected initial status: %+v", status)
	}

	if err := synchronizer.SyncOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	status = synchronizer.Status()
	if status.LastImported != 0 || status.LastDuplicates != 1 {
		t.Fatalf("watermark replay was not deduplicated: %+v", status)
	}

	failed.Store(true)
	if err := synchronizer.SyncOnce(context.Background()); err == nil {
		t.Fatal("expected MosDNS failure")
	}
	status = synchronizer.Status()
	if status.LastError == "" || !status.Watermark.Equal(baseTime.Add(199*time.Second)) {
		t.Fatalf("failed sync changed status incorrectly: %+v", status)
	}
}
