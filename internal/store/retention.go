package store

import (
	"context"
	"fmt"
	"time"
)

const (
	retentionBatchSize        = 500
	retentionBatchPause       = 25 * time.Millisecond
	retentionStatementTimeout = 2 * time.Second
)

type retentionTable struct {
	name      string
	timestamp string
	before    time.Time
}

func (s *Store) initRetentionIndexes() error {
	for _, statement := range []string{
		`CREATE INDEX IF NOT EXISTS idx_terminal_history_device_ts ON terminal_history(device_id, ts)`,
		`CREATE INDEX IF NOT EXISTS idx_connection_state_device_last_seen ON connection_state(device_id, last_seen)`,
		// Terminal merges move connections by terminal_id every terminal refresh;
		// without this index each merge scans the whole connection_state table.
		`CREATE INDEX IF NOT EXISTS idx_connection_state_device_terminal ON connection_state(device_id, terminal_id)`,
		`CREATE INDEX IF NOT EXISTS idx_dns_features_last_seen ON dns_features(last_seen_ns)`,
	} {
		if _, err := s.db.Exec(statement); err != nil {
			return fmt.Errorf("init retention index: %w", err)
		}
	}
	return nil
}

// PruneMonitoringState expires all monitoring data without a long transaction.
// The caller bounds the whole pass with its own maintenance context.
func (s *Store) PruneMonitoringState(ctx context.Context, interfaceBefore, connectionBefore, terminalHistoryBefore, historyBefore time.Time) error {
	return s.pruneRetention(ctx, []retentionTable{
		{"interface_samples", "ts", interfaceBefore},
		{"connection_state", "last_seen", connectionBefore},
		{"terminal_history", "ts", terminalHistoryBefore},
		{"load_samples", "ts", historyBefore},
		{"protocol_samples", "ts", historyBefore},
	})
}

// PruneDNSState expires MosDNS data independently of the MosDNS synchronizer,
// so a device whose sync was disabled does not keep stale rows forever.
func (s *Store) PruneDNSState(ctx context.Context, observationsBefore, featuresBefore time.Time) error {
	return s.pruneRetentionWith(ctx, []retentionTable{
		{"dns_observations", "query_time_ns", observationsBefore},
		{"dns_features", "last_seen_ns", featuresBefore},
	}, s.pruneDNSRetentionBatch)
}

func (s *Store) PruneInterfaceSamples(ctx context.Context, before time.Time) error {
	return s.pruneRetention(ctx, []retentionTable{{"interface_samples", "ts", before}})
}

func (s *Store) PruneRuntimeState(ctx context.Context, connectionBefore, historyBefore time.Time) error {
	return s.pruneRetention(ctx, []retentionTable{
		{"connection_state", "last_seen", connectionBefore},
		{"terminal_history", "ts", historyBefore},
		{"load_samples", "ts", historyBefore},
		{"protocol_samples", "ts", historyBefore},
	})
}

func (s *Store) pruneRetention(ctx context.Context, tables []retentionTable) error {
	return s.pruneRetentionWith(ctx, tables, s.pruneRetentionBatch)
}

func (s *Store) pruneRetentionWith(ctx context.Context, tables []retentionTable, pruneBatch func(context.Context, retentionTable) (int64, error)) error {
	// Visit each table once per round so a large backlog cannot prevent other
	// tables from making progress before the pass deadline.
	for len(tables) > 0 {
		var pending []retentionTable
		for _, table := range tables {
			count, err := pruneBatch(ctx, table)
			if err != nil {
				return err
			}
			if count == retentionBatchSize {
				pending = append(pending, table)
			}
			if count > 0 {
				// Exec has committed and returned the only connection to the pool.
				timer := time.NewTimer(retentionBatchPause)
				select {
				case <-ctx.Done():
					timer.Stop()
					return ctx.Err()
				case <-timer.C:
				}
			}
		}
		tables = pending
	}
	return nil
}

func (s *Store) pruneRetentionBatch(ctx context.Context, table retentionTable) (int64, error) {
	// Table/column names come exclusively from the static lists above. Selecting
	// rowids avoids depending on SQLite's optional DELETE ... LIMIT extension.
	query := fmt.Sprintf(`DELETE FROM %s WHERE rowid IN (
  SELECT rowid FROM %s WHERE device_id = ? AND %s < ? ORDER BY %s LIMIT ?
 )`, table.name, table.name, table.timestamp, table.timestamp)
	batchCtx, cancel := context.WithTimeout(ctx, retentionStatementTimeout)
	defer cancel()
	result, err := s.db.ExecContext(batchCtx, query, s.deviceID, table.before.Unix(), retentionBatchSize)
	if err != nil {
		return 0, fmt.Errorf("prune %s batch: %w", table.name, err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count pruned %s rows: %w", table.name, err)
	}
	return count, nil
}

func (s *Store) pruneDNSRetentionBatch(ctx context.Context, table retentionTable) (int64, error) {
	// DNS tables have no device_id column and store Unix nanoseconds.
	query := fmt.Sprintf(`DELETE FROM %s WHERE rowid IN (
  SELECT rowid FROM %s WHERE %s < ? ORDER BY %s LIMIT ?
 )`, table.name, table.name, table.timestamp, table.timestamp)
	batchCtx, cancel := context.WithTimeout(ctx, retentionStatementTimeout)
	defer cancel()
	result, err := s.db.ExecContext(batchCtx, query, table.before.UnixNano(), retentionBatchSize)
	if err != nil {
		return 0, fmt.Errorf("prune %s batch: %w", table.name, err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count pruned %s rows: %w", table.name, err)
	}
	return count, nil
}

const (
	compactMinFreeBytes = 64 << 20
	compactMinFreeRatio = 4 // free pages must be at least 1/4 of the file
)

// CompactIfMostlyFree rebuilds the database file once retention has freed a
// large share of it. SQLite reuses free pages but never shrinks the file.
func (s *Store) CompactIfMostlyFree(ctx context.Context) (bool, error) {
	var pageSize, pageCount, freePages int64
	for _, item := range []struct {
		pragma string
		out    *int64
	}{{"page_size", &pageSize}, {"page_count", &pageCount}, {"freelist_count", &freePages}} {
		if err := s.db.QueryRowContext(ctx, `PRAGMA `+item.pragma).Scan(item.out); err != nil {
			return false, fmt.Errorf("read %s: %w", item.pragma, err)
		}
	}
	if freePages*pageSize < compactMinFreeBytes || freePages*compactMinFreeRatio < pageCount {
		return false, nil
	}
	if _, err := s.db.ExecContext(ctx, `VACUUM`); err != nil {
		return false, fmt.Errorf("vacuum: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		return true, fmt.Errorf("checkpoint after vacuum: %w", err)
	}
	return true, nil
}
