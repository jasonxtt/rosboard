package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"rosboard/internal/config"
	"rosboard/internal/model"
	"rosboard/internal/store"
)

func TestApplicationResolverMatchesRecentDNSObservation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/yaml")
		_, _ = writer.Write([]byte("lists:\n  - name: youtube\n    rules:\n      - domain:youtube.com\n"))
	}))
	defer server.Close()

	storage, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	feature, err := NewFeatureLibrarySynchronizer(config.FeatureLibraryConfig{Enabled: true, SourceURL: server.URL, RefreshIntervalHours: 1, MatchWindowMinutes: 30}, t.TempDir(), nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := feature.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	queryTime := time.Date(2026, 8, 2, 1, 2, 3, 0, time.UTC)
	_, err = storage.SaveDNSObservations(context.Background(), []model.DNSObservation{{
		DedupeKey: "dns-1", TraceID: "trace-1", ClientIP: "10.0.0.8", Domain: "r3.youtube.com", AnswerIP: "142.250.1.1", QueryType: "A", QueryTime: queryTime, IngestedAt: queryTime,
	}}, store.DNSWatermark{QueryTime: queryTime, TraceID: "trace-1"})
	if err != nil {
		t.Fatal(err)
	}

	resolver := NewApplicationResolver(storage, feature, true, 30)
	application, domain, ok := resolver.Resolve(context.Background(), "10.0.0.8", "142.250.1.1", queryTime.Add(time.Second))
	if !ok || application != "YouTube" || domain != "r3.youtube.com" {
		t.Fatalf("unexpected resolver result: application=%q domain=%q ok=%v", application, domain, ok)
	}
	if _, _, ok := resolver.Resolve(context.Background(), "10.0.0.9", "142.250.1.1", queryTime.Add(time.Second)); ok {
		t.Fatal("resolver matched a different client")
	}
	if err := storage.PruneDNSObservations(context.Background(), queryTime.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	historicalResolver := NewApplicationResolver(storage, feature, true, 30)
	application, domain, ok = historicalResolver.Resolve(context.Background(), "10.0.0.8", "142.250.1.1", queryTime.Add(time.Hour))
	if !ok || application != "YouTube" || domain != "r3.youtube.com" {
		t.Fatalf("long-term DNS feature was not used: application=%q domain=%q ok=%v", application, domain, ok)
	}
}

func TestAggregateProtocolsPreservesRecognitionSource(t *testing.T) {
	protocols := aggregateProtocols(map[string]model.TerminalDetail{
		"terminal": {Connections: []model.TerminalConnection{
			{Application: "YouTube", Protocol: "tcp", ApplicationSource: "dns", UploadBps: 100, Estimated: false},
			{Application: "YouTube", Protocol: "tcp", ApplicationSource: "port", UploadBps: 50, Estimated: true},
		}},
	})
	if len(protocols) != 1 || protocols[0].Source != "mixed" || protocols[0].Name != "YouTube" {
		t.Fatalf("unexpected aggregate protocol: %#v", protocols)
	}
}
