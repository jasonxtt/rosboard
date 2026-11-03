package service

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"rosboard/internal/config"
	"rosboard/internal/routeros"
	"rosboard/internal/store"
)

func retentionTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func waitRetentionHistoryCount(t *testing.T, s *store.Store, want int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		history, err := s.TerminalHistory(context.Background(), "terminal", 30)
		if err != nil {
			t.Fatal(err)
		}
		if len(history) == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("history count=%d, want=%d", len(history), want)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestRetentionMaintenanceRunsWithoutViewersAndStops(t *testing.T) {
	s := retentionTestStore(t)
	m := NewMonitor(config.Config{SampleRetentionHours: 48}, nil, s, log.New(io.Discard, "", 0))
	now := time.Now().UTC()
	expired := now.Add(-36 * 24 * time.Hour)
	retained := now.Add(-23 * time.Hour)
	for _, at := range []time.Time{expired, retained} {
		if err := s.SaveTerminalHistory(context.Background(), "terminal", at, 1, 2, 3); err != nil {
			t.Fatal(err)
		}
	}
	for _, at := range []time.Time{now.Add(-49 * time.Hour), now.Add(-47 * time.Hour)} {
		if err := s.SaveInterfaceSample(context.Background(), at, "ether1", 1, 2); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ticks := make(chan time.Time, 1)
	done := make(chan struct{})
	go func() { defer close(done); m.runRetentionMaintenance(ctx, ticks) }()
	waitRetentionHistoryCount(t, s, 1)
	// A new pass uses the independent maintenance trigger, with no RouterOS
	// client or viewer and no successful collection anywhere in this test.
	if err := s.SaveTerminalHistory(context.Background(), "terminal", expired, 1, 2, 3); err != nil {
		t.Fatal(err)
	}
	ticks <- now
	waitRetentionHistoryCount(t, s, 1)
	samples, err := s.LoadInterfaceSamples(context.Background(), []string{"ether1"}, now.Add(-72*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != 1 || !samples[0].Timestamp.Equal(time.Unix(now.Add(-47*time.Hour).Unix(), 0)) {
		t.Fatalf("interface retention changed: %+v", samples)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("maintenance did not stop")
	}
	if err := s.SaveTerminalHistory(context.Background(), "terminal", expired, 1, 2, 3); err != nil {
		t.Fatal(err)
	}
	ticks <- now
	waitRetentionHistoryCount(t, s, 2)
}

func TestRetentionStartsOnceDespiteInitialCollectionFailure(t *testing.T) {
	s := retentionTestStore(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "synthetic unavailable router", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	m := NewMonitor(config.Config{SampleRetentionHours: 48}, routeros.NewClient(server.URL, "synthetic", "synthetic"), s, log.New(io.Discard, "", 0))
	expired := time.Now().Add(-36 * 24 * time.Hour)
	if err := s.SaveTerminalHistory(context.Background(), "terminal", expired, 1, 2, 3); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := m.Start(ctx); err == nil {
		t.Fatal("expected initial RouterOS failure")
	}
	waitRetentionHistoryCount(t, s, 0)
	// After the initial deletion, reinserting an expired row must not trigger
	// another immediate pass when collection startup is retried.
	if err := s.SaveTerminalHistory(context.Background(), "terminal", expired, 1, 2, 3); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(ctx); err == nil {
		t.Fatal("expected retry RouterOS failure")
	}
	time.Sleep(75 * time.Millisecond)
	waitRetentionHistoryCount(t, s, 1)
}

type retentionLogWriter chan string

func (w retentionLogWriter) Write(p []byte) (int, error) { w <- string(p); return len(p), nil }

func TestRetentionFailureDoesNotBecomeCollectionAlert(t *testing.T) {
	s := retentionTestStore(t)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	logs := make(retentionLogWriter, 1)
	m := NewMonitor(config.Config{SampleRetentionHours: 48}, nil, s, log.New(logs, "", 0))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); m.runRetentionMaintenance(ctx, make(chan time.Time)) }()
	select {
	case message := <-logs:
		if !strings.Contains(message, "retention maintenance failed") {
			t.Fatalf("wrong log: %s", message)
		}
	case <-time.After(time.Second):
		t.Fatal("maintenance error was not reported")
	}
	if len(m.Snapshot().Alerts) != 0 {
		t.Fatal("maintenance failure created a collection alert")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("maintenance did not stop")
	}
}
