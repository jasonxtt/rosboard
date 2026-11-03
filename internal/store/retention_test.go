package store

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestTerminalHistoriesReadsOnlyLatestRows(t *testing.T) {
	root, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	s, err := root.OpenDevice("history")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	// An invalid oldest row detects scanning beyond LIMIT, without flaky timing
	// assertions: the previous all-history query attempted to scan this value.
	if _, err := s.db.Exec(`INSERT INTO terminal_history VALUES (?, 'a', 60, 'invalid-old-row', 0, 0)`, s.deviceID); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b"} {
		for i := 2; i <= 40; i++ {
			if err := s.SaveTerminalHistory(ctx, id, time.Unix(int64(i*60), 0), int64(i), int64(i*2), int64(i*3)); err != nil {
				t.Fatal(err)
			}
		}
	}
	// A foreign device row in the same database must never be returned.
	if _, err := s.db.Exec(`INSERT INTO terminal_history VALUES ('foreign', 'a', 999999, 9, 9, 9)`); err != nil {
		t.Fatal(err)
	}
	got, err := s.TerminalHistories(ctx, []string{"b", "a", "a", "missing"}, 30)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("unexpected map keys: %v", got)
	}
	for _, id := range []string{"a", "b"} {
		want, err := s.TerminalHistory(ctx, id, 30)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got[id], want) || len(got[id]) != 30 {
			t.Fatalf("wrong history for %s", id)
		}
		for i, entry := range got[id] {
			n := int64(40 - i)
			if entry.Timestamp.Unix() != n*60 || entry.OnlineSeconds != n || entry.TotalUploadBytes != n*2 || entry.TotalDownloadBytes != n*3 {
				t.Fatalf("wrong history entry: %+v", entry)
			}
		}
	}
	for _, limit := range []int{0, -1} {
		got, err := s.TerminalHistories(ctx, []string{"a"}, limit)
		if err != nil || len(got) != 0 {
			t.Fatalf("limit %d: %v %v", limit, got, err)
		}
	}
	got, err = s.TerminalHistories(ctx, nil, 30)
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("empty ids: %v %v", got, err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if got, err := s.TerminalHistories(canceled, []string{"a"}, 30); got != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation returned %v %v", got, err)
	}
}

func TestRetentionIndexesUpgradeAndReopen(t *testing.T) {
	dir := t.TempDir()
	root, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	child, err := root.OpenDevice("upgrade")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []*Store{root, child} {
		if err := s.SaveTerminalHistory(context.Background(), "a", time.Unix(600, 0), 1, 2, 3); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"idx_terminal_history_device_ts", "idx_connection_state_device_last_seen"} {
			if _, err := s.db.Exec(`DROP INDEX ` + name); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := root.Close(); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		root, err = Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		child, err = root.OpenDevice("upgrade")
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range []*Store{root, child} {
			history, err := s.TerminalHistory(context.Background(), "a", 30)
			if err != nil || len(history) != 1 {
				t.Fatalf("upgrade lost history: %v %v", history, err)
			}
			for _, tc := range []struct{ table, column, index string }{
				{"terminal_history", "ts", "idx_terminal_history_device_ts"},
				{"connection_state", "last_seen", "idx_connection_state_device_last_seen"},
			} {
				rows, err := s.db.Query(fmt.Sprintf(`EXPLAIN QUERY PLAN SELECT rowid FROM %s WHERE device_id = ? AND %s < ? ORDER BY %s LIMIT 500`, tc.table, tc.column, tc.column), s.deviceID, 1000)
				if err != nil {
					t.Fatal(err)
				}
				var plan string
				for rows.Next() {
					var a, b, c int
					var detail string
					if err := rows.Scan(&a, &b, &c, &detail); err != nil {
						t.Fatal(err)
					}
					plan += detail
				}
				err = rows.Err()
				rows.Close()
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(plan, tc.index) || !strings.Contains(plan, tc.column+"<?") || strings.Contains(plan, "TEMP B-TREE") {
					t.Fatalf("unbounded retention plan: %s", plan)
				}
			}
		}
		if err := root.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func seedRetentionRows(t *testing.T, s *Store, count int) []retentionTable {
	t.Helper()
	cutoff := time.Unix(10000, 0)
	tables := []retentionTable{{"interface_samples", "ts", cutoff}, {"connection_state", "last_seen", cutoff}, {"terminal_history", "ts", cutoff}, {"load_samples", "ts", cutoff}, {"protocol_samples", "ts", cutoff}}
	inserts := []string{
		`INSERT INTO interface_samples SELECT ?, n, 'ether1', 1, 2 FROM nums`,
		`INSERT INTO connection_state SELECT ?, printf('conn-%d',n), 'a', 1, 2, n FROM nums`,
		`INSERT INTO terminal_history SELECT ?, 'a', n, 1, 2, 3 FROM nums`,
		`INSERT INTO load_samples SELECT ?, n, 1, 2, 3, 4, 5, 6, 7 FROM nums`,
		`INSERT INTO protocol_samples SELECT ?, n, 'tcp', 'protocol', 1, 2, 3 FROM nums`,
	}
	for i, q := range inserts {
		q = `WITH RECURSIVE nums(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM nums WHERE n < ?) ` + q
		if _, err := s.db.Exec(q, count, s.deviceID); err != nil {
			t.Fatal(err)
		}
		// Boundary and fresh rows stay, even after every expired batch drains.
		for _, ts := range []int{10000, 10001} {
			query := strings.Replace(inserts[i], "FROM nums", "FROM (SELECT ? AS n)", 1)
			if _, err := s.db.Exec(query, s.deviceID, ts); err != nil {
				t.Fatal(err)
			}
		}
		// Same file, different device: a row older than the cutoff must survive.
		query := strings.Replace(inserts[i], "FROM nums", "FROM (SELECT 1 AS n)", 1)
		if _, err := s.db.Exec(query, "foreign"); err != nil {
			t.Fatal(err)
		}
	}
	return tables
}

func TestRetentionBatchesPreserveBoundaryAndDeviceIsolation(t *testing.T) {
	root, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	s, err := root.OpenDevice("cleanup")
	if err != nil {
		t.Fatal(err)
	}
	tables := seedRetentionRows(t, s, retentionBatchSize*2+3)
	for _, table := range tables {
		n, err := s.pruneRetentionBatch(context.Background(), table)
		if err != nil || n != retentionBatchSize {
			t.Fatalf("%s batch: %d %v", table.name, n, err)
		}
		var remaining int
		if err := s.db.QueryRow(`SELECT count(*) FROM `+table.name+` WHERE device_id = ?`, s.deviceID).Scan(&remaining); err != nil {
			t.Fatal(err)
		}
		if remaining != retentionBatchSize+5 {
			t.Fatalf("%s was not bounded: remaining %d", table.name, remaining)
		}
	}
	cutoff := time.Unix(10000, 0)
	if err := s.PruneMonitoringState(context.Background(), cutoff, cutoff, cutoff, cutoff); err != nil {
		t.Fatal(err)
	}
	for _, table := range tables {
		var own, foreign, expired int
		for _, query := range []struct {
			sql string
			out *int
		}{
			{`SELECT count(*) FROM ` + table.name + ` WHERE device_id = ?`, &own},
			{`SELECT count(*) FROM ` + table.name + ` WHERE device_id != ?`, &foreign},
			{`SELECT count(*) FROM ` + table.name + ` WHERE device_id = ? AND ` + table.timestamp + ` < 10000`, &expired},
		} {
			if err := s.db.QueryRow(query.sql, s.deviceID).Scan(query.out); err != nil {
				t.Fatal(err)
			}
		}
		if own != 2 || foreign != 1 || expired != 0 {
			t.Fatalf("%s: own=%d foreign=%d expired=%d", table.name, own, foreign, expired)
		}
	}
}

func TestRetentionCancellationReleasesConnection(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	tables := seedRetentionRows(t, s, retentionBatchSize*2+3)
	conn, err := s.db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.pruneRetention(ctx, tables) }()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cleanup ignored cancellation while waiting for the connection")
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveInterfaceSample(context.Background(), time.Now(), "sample", 1, 2); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := s.db.QueryRow(`SELECT count(*) FROM terminal_history WHERE device_id = ?`, s.deviceID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != retentionBatchSize*2+5 {
		t.Fatalf("canceled pass deleted history: %d", count)
	}
}

func TestRetentionYieldsToSampling(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	tables := seedRetentionRows(t, s, retentionBatchSize*3+1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.pruneRetention(ctx, tables) }()
	// Wait until deletion has started, then sample while more batches remain.
	for {
		var n int
		if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM terminal_history WHERE device_id = ?`, s.deviceID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n < retentionBatchSize*3+3 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(time.Millisecond):
		}
	}
	for i := 0; i < 20; i++ {
		sampleCtx, stop := context.WithTimeout(ctx, time.Second)
		err := s.SaveInterfaceSample(sampleCtx, time.Now().Add(time.Duration(i)*time.Second), "live", 1, 2)
		stop()
		if err != nil {
			t.Fatalf("sampling during maintenance: %v", err)
		}
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// Run explicitly with -run '^$' -bench BenchmarkTerminalHistoriesLargeHistory
// -benchtime=3x. Setup represents the Issue #38 scale and is outside timing.
func BenchmarkTerminalHistoriesLargeHistory(b *testing.B) {
	s, err := Open(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close()
	_, err = s.db.Exec(`WITH RECURSIVE minutes(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM minutes WHERE n<51036), terminals(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM terminals WHERE n<49) INSERT INTO terminal_history SELECT ?, printf('terminal-%d', terminals.n), minutes.n*60, 1, 2, 3 FROM terminals CROSS JOIN minutes`, s.deviceID)
	if err != nil {
		b.Fatal(err)
	}
	ids := make([]string, 49)
	for i := range ids {
		ids[i] = fmt.Sprintf("terminal-%d", i+1)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		history, err := s.TerminalHistories(context.Background(), ids, 30)
		if err != nil {
			b.Fatal(err)
		}
		if len(history) != 49 || len(history[ids[0]]) != 30 {
			b.Fatal("unexpected history result")
		}
	}
}

func queryPlan(t *testing.T, s *Store, query string, args ...any) string {
	t.Helper()
	rows, err := s.db.Query(`EXPLAIN QUERY PLAN `+query, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan string
	for rows.Next() {
		var a, b, c int
		var detail string
		if err := rows.Scan(&a, &b, &c, &detail); err != nil {
			t.Fatal(err)
		}
		plan += detail
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return plan
}

func TestTerminalMergeUsesConnectionTerminalIndex(t *testing.T) {
	root, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	child, err := root.OpenDevice("merge")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []*Store{root, child} {
		plan := queryPlan(t, s, `UPDATE connection_state SET terminal_id = ? WHERE device_id = ? AND terminal_id = ?`, "to", s.deviceID, "from")
		if !strings.Contains(plan, "idx_connection_state_device_terminal") {
			t.Fatalf("terminal merge scans connection_state: %s", plan)
		}
	}
}

func TestPruneDNSStateKeepsRecentRows(t *testing.T) {
	root, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	s, err := root.OpenDevice("dns")
	if err != nil {
		t.Fatal(err)
	}
	cutoff := time.Unix(10000, 0)
	for i, at := range []time.Time{cutoff.Add(-time.Second), cutoff, cutoff.Add(time.Second)} {
		if _, err := s.db.Exec(`INSERT INTO dns_observations (dedupe_key, trace_id, client_ip, domain, answer_ip, query_type, query_time_ns, ttl, ingested_at_ns) VALUES (?, 't', 'c', 'd', 'a', 'A', ?, 60, 0)`, fmt.Sprint(i), at.UnixNano()); err != nil {
			t.Fatal(err)
		}
		if _, err := s.db.Exec(`INSERT INTO dns_features (client_ip, domain, answer_ip, query_type, first_seen_ns, last_seen_ns, hit_count, last_ttl) VALUES ('c', ?, 'a', 'A', 0, ?, 1, 60)`, fmt.Sprint(i), at.UnixNano()); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.PruneDNSState(context.Background(), cutoff, cutoff); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"dns_observations", "dns_features"} {
		var count int
		if err := s.db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 2 {
			t.Fatalf("%s kept %d rows, want 2", table, count)
		}
	}
	if plan := queryPlan(t, s, `SELECT rowid FROM dns_features WHERE last_seen_ns < ? ORDER BY last_seen_ns LIMIT 500`, 1); !strings.Contains(plan, "idx_dns_features_last_seen") || strings.Contains(plan, "TEMP B-TREE") {
		t.Fatalf("unbounded DNS feature retention plan: %s", plan)
	}
}

func TestCompactIfMostlyFreeShrinksOnlyLargeFreeSpace(t *testing.T) {
	root, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	s, err := root.OpenDevice("compact")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := s.db.Exec(`CREATE TABLE filler (data BLOB)`); err != nil {
		t.Fatal(err)
	}
	insert := func(rows int) {
		if _, err := s.db.Exec(`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < ?) INSERT INTO filler SELECT zeroblob(1048576) FROM n`, rows); err != nil {
			t.Fatal(err)
		}
	}
	pages := func() int64 {
		var count int64
		if err := s.db.QueryRow(`PRAGMA page_count`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count
	}
	// A small amount of free space is reused by SQLite and not worth a rebuild.
	insert(8)
	if _, err := s.db.Exec(`DELETE FROM filler`); err != nil {
		t.Fatal(err)
	}
	if compacted, err := s.CompactIfMostlyFree(ctx); err != nil || compacted {
		t.Fatalf("small free space compacted=%v err=%v", compacted, err)
	}
	insert(80)
	if _, err := s.db.Exec(`DELETE FROM filler`); err != nil {
		t.Fatal(err)
	}
	before := pages()
	compacted, err := s.CompactIfMostlyFree(ctx)
	if err != nil || !compacted {
		t.Fatalf("large free space compacted=%v err=%v", compacted, err)
	}
	if after := pages(); after*4 > before {
		t.Fatalf("page count %d -> %d, want a much smaller file", before, after)
	}
}
