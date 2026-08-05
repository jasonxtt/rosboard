package store

import (
	"context"
	"testing"
	"time"

	"rosboard/internal/model"
)

func TestDNSObservationsPersistDeduplicationWatermarkAndPruning(t *testing.T) {
	ctx := context.Background()
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })

	queryTime := time.Date(2026, 8, 2, 1, 2, 3, 0, time.UTC)
	observations := []model.DNSObservation{
		{DedupeKey: "one", TraceID: "trace-1", ClientIP: "10.0.0.8", Domain: "example.com", AnswerIP: "192.0.2.1", QueryType: "A", QueryTime: queryTime, TTL: 60, IngestedAt: queryTime.Add(time.Minute)},
		{DedupeKey: "two", TraceID: "trace-2", ClientIP: "10.0.0.9", Domain: "video.example", AnswerIP: "2001:db8::1", QueryType: "AAAA", QueryTime: queryTime.Add(time.Second), TTL: 30, IngestedAt: queryTime.Add(time.Minute)},
	}
	watermark := DNSWatermark{QueryTime: observations[1].QueryTime, TraceID: observations[1].TraceID}
	inserted, err := storage.SaveDNSObservations(ctx, observations, watermark)
	if err != nil || inserted != 2 {
		t.Fatalf("initial insert=%d err=%v", inserted, err)
	}
	inserted, err = storage.SaveDNSObservations(ctx, observations, watermark)
	if err != nil || inserted != 0 {
		t.Fatalf("replay insert=%d err=%v", inserted, err)
	}
	repeat := observations[0]
	repeat.DedupeKey = "one-repeat"
	repeat.TraceID = "trace-3"
	repeat.QueryTime = queryTime
	repeat.IngestedAt = queryTime.Add(2 * time.Minute)
	inserted, err = storage.SaveDNSObservations(ctx, []model.DNSObservation{repeat}, watermark)
	if err != nil || inserted != 1 {
		t.Fatalf("repeat insert=%d err=%v", inserted, err)
	}
	features, err := storage.DNSFeaturesForMatch(ctx)
	if err != nil || len(features) != 2 {
		t.Fatalf("unexpected DNS features: %#v err=%v", features, err)
	}
	if features[0].HitCount != 2 {
		t.Fatalf("repeated query did not update long-term feature: %#v", features)
	}

	loaded, err := storage.DNSObservations(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 3 || loaded[0].Domain != "video.example" || loaded[1].AnswerIP != "192.0.2.1" {
		t.Fatalf("unexpected observations: %#v", loaded)
	}
	loadedWatermark, ok, err := storage.LoadDNSWatermark(ctx)
	if err != nil || !ok || !loadedWatermark.QueryTime.Equal(watermark.QueryTime) || loadedWatermark.TraceID != watermark.TraceID {
		t.Fatalf("unexpected watermark: %#v ok=%v err=%v", loadedWatermark, ok, err)
	}

	if err := storage.PruneDNSObservations(ctx, queryTime.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	loaded, err = storage.DNSObservations(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || loaded[0].DedupeKey != "two" {
		t.Fatalf("unexpected pruned observations: %#v", loaded)
	}
	features, err = storage.DNSFeaturesForMatch(ctx)
	if err != nil || len(features) != 2 {
		t.Fatalf("pruning raw observations must retain DNS features: %#v err=%v", features, err)
	}
}
