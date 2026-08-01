package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"rosboard/internal/model"
)

func TestOpenMigratesLegacyRowsToDefaultDevice(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "rosboard.db"))
	if err != nil {
		t.Fatal(err)
	}
	statements := []string{
		`CREATE TABLE interface_samples (ts INTEGER NOT NULL, interface_name TEXT NOT NULL, rx_bps REAL NOT NULL, tx_bps REAL NOT NULL, PRIMARY KEY (ts, interface_name))`,
		`CREATE TABLE terminals (id TEXT PRIMARY KEY, mac TEXT, display_name TEXT NOT NULL, remark TEXT NOT NULL DEFAULT '', tracking_since INTEGER NOT NULL, last_seen INTEGER NOT NULL, state TEXT NOT NULL DEFAULT 'offline', online_since INTEGER NOT NULL DEFAULT 0, custom_name TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE terminal_addresses (terminal_id TEXT NOT NULL, family TEXT NOT NULL, address TEXT NOT NULL, last_seen INTEGER NOT NULL, PRIMARY KEY (terminal_id, family, address))`,
		`CREATE TABLE terminal_totals (terminal_id TEXT PRIMARY KEY, upload_bytes INTEGER NOT NULL, download_bytes INTEGER NOT NULL, tracking_since INTEGER NOT NULL, updated_at INTEGER NOT NULL)`,
		`CREATE TABLE connection_state (conn_key TEXT PRIMARY KEY, terminal_id TEXT NOT NULL, upload_bytes INTEGER NOT NULL, download_bytes INTEGER NOT NULL, last_seen INTEGER NOT NULL)`,
		`CREATE TABLE terminal_history (terminal_id TEXT NOT NULL, ts INTEGER NOT NULL, online_seconds INTEGER NOT NULL, upload_bytes INTEGER NOT NULL, download_bytes INTEGER NOT NULL, PRIMARY KEY (terminal_id, ts))`,
		`CREATE TABLE load_samples (ts INTEGER PRIMARY KEY, cpu_percent REAL NOT NULL, memory_percent REAL NOT NULL, online_terminals INTEGER NOT NULL, upload_bps REAL NOT NULL, download_bps REAL NOT NULL, storage_percent REAL NOT NULL DEFAULT 0)`,
		`CREATE TABLE protocol_samples (ts INTEGER NOT NULL, name TEXT NOT NULL, kind TEXT NOT NULL, connections INTEGER NOT NULL, upload_bps REAL NOT NULL, download_bps REAL NOT NULL, PRIMARY KEY (ts, name, kind))`,
		`INSERT INTO terminals (id, mac, display_name, remark, tracking_since, last_seen, custom_name) VALUES ('mac:legacy', 'AA:BB:CC:DD:EE:FF', 'legacy', 'kept', 100, 200, 'Legacy device')`,
		`INSERT INTO terminal_totals VALUES ('mac:legacy', 123, 456, 100, 200)`,
		`INSERT INTO load_samples (ts, cpu_percent, memory_percent, online_terminals, upload_bps, download_bps, storage_percent) VALUES (120, 10, 20, 3, 40, 50, 60)`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	storage, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	totals, err := storage.ForDevice(defaultDeviceID).TerminalTotals(context.Background(), []string{"mac:legacy"})
	if err != nil {
		t.Fatal(err)
	}
	if totals["mac:legacy"].UploadBytes != 123 || totals["mac:legacy"].Remark != "kept" || totals["mac:legacy"].CustomName != "Legacy device" {
		t.Fatalf("legacy row was not preserved: %#v", totals)
	}
	other, err := storage.ForDevice("other").TerminalTotals(context.Background(), []string{"mac:legacy"})
	if err != nil {
		t.Fatal(err)
	}
	if len(other) != 0 {
		t.Fatalf("legacy row leaked to another device: %#v", other)
	}
	samples, err := storage.ForDevice(defaultDeviceID).LoadSamples(context.Background(), time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != 1 || samples[0].ConnectionCount != -1 {
		t.Fatalf("legacy load sample was not migrated with an unknown connection count: %#v", samples)
	}
}

func TestLoadSamplesPersistConnectionCountPerDevice(t *testing.T) {
	ctx := context.Background()
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })

	at := time.Unix(1_700_000_000, 0).UTC()
	if err := storage.ForDevice("router-a").SaveLoadSample(ctx, model.LoadSample{Timestamp: at, ConnectionCount: 321}); err != nil {
		t.Fatal(err)
	}
	samples, err := storage.ForDevice("router-a").LoadSamples(ctx, at.Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != 1 || samples[0].ConnectionCount != 321 {
		t.Fatalf("unexpected connection history: %#v", samples)
	}
	other, err := storage.ForDevice("router-b").LoadSamples(ctx, at.Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(other) != 0 {
		t.Fatalf("load history leaked across devices: %#v", other)
	}
}

func TestUpdateTerminalMetadataPersistsCustomNameAndRemark(t *testing.T) {
	ctx := context.Background()
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = storage.Close() })

	id := "mac:00:11:22:33:44:55"
	if err := storage.UpsertTerminal(ctx, id, "00:11:22:33:44:55", "iphone", time.Now().UTC()); err != nil {
		t.Fatalf("upsert terminal: %v", err)
	}
	if err := storage.UpdateTerminalMetadata(ctx, id, "iPhone 13 PM", "Tom 的手机"); err != nil {
		t.Fatalf("update metadata: %v", err)
	}
	totals, err := storage.TerminalTotals(ctx, []string{id})
	if err != nil {
		t.Fatalf("load totals: %v", err)
	}
	if totals[id].AutoName != "iphone" || totals[id].CustomName != "iPhone 13 PM" || totals[id].Remark != "Tom 的手机" {
		t.Fatalf("unexpected metadata: %#v", totals[id])
	}
	if err := storage.UpdateTerminalMetadata(ctx, "missing", "name", "remark"); !errors.Is(err, ErrTerminalNotFound) {
		t.Fatalf("expected ErrTerminalNotFound, got %v", err)
	}
}

func TestMergeTerminalDeduplicatesAddresses(t *testing.T) {
	ctx := context.Background()
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = storage.Close() })

	fromID := "addr:fc00::20"
	toID := "mac:00:11:22:33:44:55"
	older := time.Unix(100, 0).UTC()
	newer := time.Unix(200, 0).UTC()
	if err := storage.UpsertTerminal(ctx, fromID, "", "temporary", newer); err != nil {
		t.Fatalf("upsert source terminal: %v", err)
	}
	if err := storage.UpsertTerminal(ctx, toID, "00:11:22:33:44:55", "known", older); err != nil {
		t.Fatalf("upsert target terminal: %v", err)
	}
	if err := storage.ReplaceTerminalAddresses(ctx, fromID, nil, []string{"fc00::20", "fe80::20"}, newer); err != nil {
		t.Fatalf("save source addresses: %v", err)
	}
	if err := storage.ReplaceTerminalAddresses(ctx, toID, nil, []string{"fc00::20"}, older); err != nil {
		t.Fatalf("save target addresses: %v", err)
	}

	if err := storage.MergeTerminal(ctx, fromID, toID); err != nil {
		t.Fatalf("merge terminal: %v", err)
	}

	rows, err := storage.db.QueryContext(ctx, `SELECT address, last_seen FROM terminal_addresses WHERE terminal_id = ? ORDER BY address`, toID)
	if err != nil {
		t.Fatalf("query merged addresses: %v", err)
	}
	defer rows.Close()

	got := map[string]int64{}
	for rows.Next() {
		var address string
		var lastSeen int64
		if err := rows.Scan(&address, &lastSeen); err != nil {
			t.Fatalf("scan merged address: %v", err)
		}
		got[address] = lastSeen
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate merged addresses: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected two unique target addresses, got %#v", got)
	}
	for _, address := range []string{"fc00::20", "fe80::20"} {
		if got[address] != newer.Unix() {
			t.Errorf("expected %s last_seen %d, got %d", address, newer.Unix(), got[address])
		}
	}

	var sourceRows int
	if err := storage.db.QueryRowContext(ctx, `SELECT count(*) FROM terminal_addresses WHERE terminal_id = ?`, fromID).Scan(&sourceRows); err != nil {
		t.Fatalf("count source addresses: %v", err)
	}
	if sourceRows != 0 {
		t.Fatalf("expected source addresses to be removed, got %d", sourceRows)
	}
}

func TestDeviceScopesKeepSameMACTerminalsSeparate(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })

	ctx := context.Background()
	at := time.Unix(1_700_000_000, 0).UTC()
	one := storage.ForDevice("one")
	two := storage.ForDevice("two")
	if err := one.UpsertTerminal(ctx, "mac:aa", "AA:BB:CC:DD:EE:FF", "One", at); err != nil {
		t.Fatal(err)
	}
	if err := two.UpsertTerminal(ctx, "mac:aa", "AA:BB:CC:DD:EE:FF", "Two", at); err != nil {
		t.Fatal(err)
	}
	if err := one.UpdateTerminalMetadata(ctx, "mac:aa", "Kitchen", "one only"); err != nil {
		t.Fatal(err)
	}
	oneTotals, err := one.TerminalTotals(ctx, []string{"mac:aa"})
	if err != nil {
		t.Fatal(err)
	}
	twoTotals, err := two.TerminalTotals(ctx, []string{"mac:aa"})
	if err != nil {
		t.Fatal(err)
	}
	if oneTotals["mac:aa"].Remark != "one only" || twoTotals["mac:aa"].Remark != "" {
		t.Fatalf("device metadata leaked: one=%#v two=%#v", oneTotals, twoTotals)
	}
	if err := storage.PurgeDevice(ctx, "one"); err != nil {
		t.Fatal(err)
	}
	oneTotals, err = one.TerminalTotals(ctx, []string{"mac:aa"})
	if err != nil {
		t.Fatal(err)
	}
	twoTotals, err = two.TerminalTotals(ctx, []string{"mac:aa"})
	if err != nil {
		t.Fatal(err)
	}
	if len(oneTotals) != 0 || len(twoTotals) != 1 {
		t.Fatalf("device purge crossed scopes: one=%#v two=%#v", oneTotals, twoTotals)
	}
}

func TestOpenDeviceMigratesLegacySharedRows(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })

	ctx := context.Background()
	if _, err := storage.db.ExecContext(ctx, `INSERT INTO terminals (device_id, id, mac, display_name, tracking_since, last_seen)
		VALUES ('edge', 'mac:edge', '00:11:22:33:44:55', 'edge terminal', 100, 200)`); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.db.ExecContext(ctx, `INSERT INTO terminal_totals (device_id, terminal_id, upload_bytes, download_bytes, tracking_since, updated_at)
		VALUES ('edge', 'mac:edge', 7, 8, 100, 200)`); err != nil {
		t.Fatal(err)
	}

	device, err := storage.OpenDevice("edge")
	if err != nil {
		t.Fatalf("open device store: %v", err)
	}
	totals, err := device.TerminalTotals(ctx, []string{"mac:edge"})
	if err != nil {
		t.Fatal(err)
	}
	if totals["mac:edge"].UploadBytes != 7 || totals["mac:edge"].DownloadBytes != 8 {
		t.Fatalf("legacy row was not migrated: %#v", totals)
	}
	other, err := storage.OpenDevice("other")
	if err != nil {
		t.Fatal(err)
	}
	otherTotals, err := other.TerminalTotals(ctx, []string{"mac:edge"})
	if err != nil {
		t.Fatal(err)
	}
	if len(otherTotals) != 0 {
		t.Fatalf("device migration leaked rows: %#v", otherTotals)
	}
}

func TestSafeDeviceFilenameAvoidsSanitizationCollisions(t *testing.T) {
	if safeDeviceFilename("a/b") == safeDeviceFilename("a_b") {
		t.Fatal("device filenames must remain unique after sanitization")
	}
}

func TestTerminalBatchPreservesConnectionDeltaAndHistory(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })

	ctx := context.Background()
	device := storage.ForDevice("edge")
	at := time.Unix(1_700_000_000, 0).UTC()
	if _, err := device.ApplyTerminalMetadata(ctx, nil, []TerminalSnapshot{{ID: "mac:edge", MAC: "00:11:22:33:44:55", DisplayName: "edge", IPv4: []string{"192.0.2.10"}, SeenAt: at}}); err != nil {
		t.Fatal(err)
	}
	first, err := device.ApplyTerminalRuntime(ctx, []TerminalPresence{{ID: "mac:edge", State: "online", SeenAt: at}}, []ConnectionSnapshot{{Key: "conn-1", TerminalID: "mac:edge", UploadBytes: 100, DownloadBytes: 200, SeenAt: at}})
	if err != nil {
		t.Fatal(err)
	}
	if first["mac:edge"].UploadBytes != 0 || first["mac:edge"].DownloadBytes != 0 {
		t.Fatalf("first connection snapshot invented a delta: %#v", first)
	}
	second, err := device.ApplyTerminalRuntime(ctx, []TerminalPresence{{ID: "mac:edge", State: "online", SeenAt: at.Add(time.Minute)}}, []ConnectionSnapshot{{Key: "conn-1", TerminalID: "mac:edge", UploadBytes: 140, DownloadBytes: 260, SeenAt: at.Add(time.Minute)}})
	if err != nil {
		t.Fatal(err)
	}
	if second["mac:edge"].UploadBytes != 40 || second["mac:edge"].DownloadBytes != 60 {
		t.Fatalf("connection delta was not preserved: %#v", second)
	}
	if err := device.SaveTerminalHistories(ctx, []TerminalHistorySnapshot{{TerminalID: "mac:edge", At: at, UploadBytes: 0, DownloadBytes: 0}}); err != nil {
		t.Fatal(err)
	}
	history, err := device.TerminalHistories(ctx, []string{"mac:edge"}, 30)
	if err != nil {
		t.Fatal(err)
	}
	if len(history["mac:edge"]) != 1 || history["mac:edge"][0].Timestamp.Unix() != at.Unix()-at.Unix()%60 {
		t.Fatalf("unexpected terminal history: %#v", history)
	}
}

func TestThirtyDeviceStoresBatchWritesIndependently(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })

	devices := make([]*Store, 30)
	for index := range devices {
		device, err := storage.OpenDevice(fmt.Sprintf("device-%02d", index))
		if err != nil {
			t.Fatalf("open device %d: %v", index, err)
		}
		devices[index] = device
	}

	ctx := context.Background()
	at := time.Unix(1_700_000_000, 0).UTC()
	errorsCh := make(chan error, len(devices))
	var waitGroup sync.WaitGroup
	for index, device := range devices {
		waitGroup.Add(1)
		go func(index int, device *Store) {
			defer waitGroup.Done()
			metadata := make([]TerminalSnapshot, 0, 20)
			presence := make([]TerminalPresence, 0, 20)
			connections := make([]ConnectionSnapshot, 0, 40)
			for terminalIndex := 0; terminalIndex < 20; terminalIndex++ {
				id := fmt.Sprintf("mac:%02d:%02d", index, terminalIndex)
				metadata = append(metadata, TerminalSnapshot{ID: id, MAC: fmt.Sprintf("00:11:22:%02x:%02x:%02x", index, terminalIndex, terminalIndex), DisplayName: id, IPv4: []string{fmt.Sprintf("10.%d.0.%d", index+1, terminalIndex+1)}, SeenAt: at})
				presence = append(presence, TerminalPresence{ID: id, State: "online", SeenAt: at})
				connections = append(connections, ConnectionSnapshot{Key: fmt.Sprintf("conn-%d", terminalIndex*2), TerminalID: id, UploadBytes: 100, DownloadBytes: 200, SeenAt: at})
				connections = append(connections, ConnectionSnapshot{Key: fmt.Sprintf("conn-%d", terminalIndex*2+1), TerminalID: id, UploadBytes: 300, DownloadBytes: 400, SeenAt: at})
			}
			if _, err := device.ApplyTerminalMetadata(ctx, nil, metadata); err != nil {
				errorsCh <- fmt.Errorf("device %d metadata: %w", index, err)
				return
			}
			if _, err := device.ApplyTerminalRuntime(ctx, presence, connections); err != nil {
				errorsCh <- fmt.Errorf("device %d runtime: %w", index, err)
			}
		}(index, device)
	}
	waitGroup.Wait()
	close(errorsCh)
	for err := range errorsCh {
		t.Fatal(err)
	}
}
