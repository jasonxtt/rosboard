package store

import (
	"context"
	"testing"
	"time"
)

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
