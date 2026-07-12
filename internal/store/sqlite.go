package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"rosboard/internal/model"
)

var ErrTerminalNotFound = errors.New("terminal not found")

type Store struct {
	db *sql.DB
}

type TerminalTotal struct {
	UploadBytes   int64
	DownloadBytes int64
	TrackingSince time.Time
	Remark        string
	AutoName      string
	CustomName    string
	State         string
	OnlineSince   time.Time
	LastSeen      time.Time
}

type ConnectionSnapshot struct {
	Key           string
	TerminalID    string
	UploadBytes   int64
	DownloadBytes int64
	SeenAt        time.Time
}

func Open(dataDir string) (*Store, error) {
	dbPath := filepath.Join(dataDir, "rosboard.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)

	store := &Store{db: db}
	if err := store.initSchema(); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) initSchema() error {
	schema := []string{
		`CREATE TABLE IF NOT EXISTS interface_samples (
			ts INTEGER NOT NULL,
			interface_name TEXT NOT NULL,
			rx_bps REAL NOT NULL,
			tx_bps REAL NOT NULL,
			PRIMARY KEY (ts, interface_name)
		);`,
		`CREATE TABLE IF NOT EXISTS terminals (
			id TEXT PRIMARY KEY,
			mac TEXT,
			display_name TEXT NOT NULL,
			remark TEXT NOT NULL DEFAULT '',
			tracking_since INTEGER NOT NULL,
			last_seen INTEGER NOT NULL
		);`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_terminals_mac ON terminals(mac) WHERE mac IS NOT NULL AND mac <> '';`,
		`CREATE TABLE IF NOT EXISTS terminal_addresses (
			terminal_id TEXT NOT NULL,
			family TEXT NOT NULL,
			address TEXT NOT NULL,
			last_seen INTEGER NOT NULL,
			PRIMARY KEY (terminal_id, family, address)
		);`,
		`CREATE TABLE IF NOT EXISTS terminal_totals (
			terminal_id TEXT PRIMARY KEY,
			upload_bytes INTEGER NOT NULL,
			download_bytes INTEGER NOT NULL,
			tracking_since INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS connection_state (
			conn_key TEXT PRIMARY KEY,
			terminal_id TEXT NOT NULL,
			upload_bytes INTEGER NOT NULL,
			download_bytes INTEGER NOT NULL,
			last_seen INTEGER NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS terminal_history (
			terminal_id TEXT NOT NULL,
			ts INTEGER NOT NULL,
			online_seconds INTEGER NOT NULL,
			upload_bytes INTEGER NOT NULL,
			download_bytes INTEGER NOT NULL,
			PRIMARY KEY (terminal_id, ts)
		);`,
		`CREATE TABLE IF NOT EXISTS load_samples (
			ts INTEGER PRIMARY KEY,
			cpu_percent REAL NOT NULL,
			memory_percent REAL NOT NULL,
			online_terminals INTEGER NOT NULL,
			upload_bps REAL NOT NULL,
			download_bps REAL NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS protocol_samples (
			ts INTEGER NOT NULL,
			name TEXT NOT NULL,
			kind TEXT NOT NULL,
			connections INTEGER NOT NULL,
			upload_bps REAL NOT NULL,
			download_bps REAL NOT NULL,
			PRIMARY KEY (ts, name, kind)
		);`,
	}

	for _, statement := range schema {
		if _, err := s.db.Exec(statement); err != nil {
			return fmt.Errorf("init schema: %w", err)
		}
	}
	if _, err := s.db.Exec(`ALTER TABLE terminals ADD COLUMN remark TEXT NOT NULL DEFAULT ''`); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
		return fmt.Errorf("add terminals.remark: %w", err)
	}
	if _, err := s.db.Exec(`ALTER TABLE terminals ADD COLUMN state TEXT NOT NULL DEFAULT 'offline'`); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
		return fmt.Errorf("add terminals.state: %w", err)
	}
	if _, err := s.db.Exec(`ALTER TABLE terminals ADD COLUMN online_since INTEGER NOT NULL DEFAULT 0`); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
		return fmt.Errorf("add terminals.online_since: %w", err)
	}
	if _, err := s.db.Exec(`ALTER TABLE terminals ADD COLUMN custom_name TEXT NOT NULL DEFAULT ''`); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
		return fmt.Errorf("add terminals.custom_name: %w", err)
	}
	if _, err := s.db.Exec(`ALTER TABLE load_samples ADD COLUMN storage_percent REAL NOT NULL DEFAULT 0`); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
		return fmt.Errorf("add load_samples.storage_percent: %w", err)
	}
	return nil
}

func (s *Store) SaveLoadSample(ctx context.Context, sample model.LoadSample) error {
	bucket := sample.Timestamp.Unix() - sample.Timestamp.Unix()%60
	_, err := s.db.ExecContext(ctx, `INSERT OR REPLACE INTO load_samples
		(ts, cpu_percent, memory_percent, online_terminals, upload_bps, download_bps, storage_percent)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, bucket, sample.CPULoadPercent, sample.MemoryUsedPercent, sample.OnlineTerminalCount, sample.UploadBps, sample.DownloadBps, sample.StorageUsedPercent)
	if err != nil {
		return fmt.Errorf("save load sample: %w", err)
	}
	return nil
}

func (s *Store) LoadSamples(ctx context.Context, since time.Time) ([]model.LoadSample, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT ts, cpu_percent, memory_percent, online_terminals, upload_bps, download_bps, storage_percent
		FROM load_samples WHERE ts >= ? ORDER BY ts ASC`, since.Unix())
	if err != nil {
		return nil, fmt.Errorf("query load samples: %w", err)
	}
	defer rows.Close()
	result := make([]model.LoadSample, 0)
	for rows.Next() {
		var sample model.LoadSample
		var ts int64
		if err := rows.Scan(&ts, &sample.CPULoadPercent, &sample.MemoryUsedPercent, &sample.OnlineTerminalCount, &sample.UploadBps, &sample.DownloadBps, &sample.StorageUsedPercent); err != nil {
			return nil, fmt.Errorf("scan load sample: %w", err)
		}
		sample.Timestamp = time.Unix(ts, 0).UTC()
		result = append(result, sample)
	}
	return result, rows.Err()
}

func (s *Store) SaveProtocolSamples(ctx context.Context, at time.Time, stats []model.ProtocolStat) error {
	bucket := at.Unix() - at.Unix()%60
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin protocol samples: %w", err)
	}
	defer tx.Rollback()
	for _, stat := range stats {
		if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO protocol_samples (ts, name, kind, connections, upload_bps, download_bps) VALUES (?, ?, ?, ?, ?, ?)`, bucket, stat.Name, stat.Kind, stat.Connections, stat.UploadBps, stat.DownloadBps); err != nil {
			return fmt.Errorf("save protocol sample: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit protocol samples: %w", err)
	}
	return nil
}

func (s *Store) ProtocolSamples(ctx context.Context, since time.Time) ([]model.ProtocolHistorySample, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT ts, name, kind, connections, upload_bps, download_bps FROM protocol_samples WHERE ts >= ? ORDER BY ts ASC, name ASC`, since.Unix())
	if err != nil {
		return nil, fmt.Errorf("query protocol samples: %w", err)
	}
	defer rows.Close()
	result := make([]model.ProtocolHistorySample, 0)
	for rows.Next() {
		var sample model.ProtocolHistorySample
		var ts int64
		if err := rows.Scan(&ts, &sample.Name, &sample.Kind, &sample.Connections, &sample.UploadBps, &sample.DownloadBps); err != nil {
			return nil, fmt.Errorf("scan protocol sample: %w", err)
		}
		sample.Timestamp = time.Unix(ts, 0).UTC()
		result = append(result, sample)
	}
	return result, rows.Err()
}

func (s *Store) SaveInterfaceSample(ctx context.Context, at time.Time, interfaceName string, rxBps, txBps float64) error {
	_, err := s.db.ExecContext(
		ctx,
		`INSERT OR REPLACE INTO interface_samples (ts, interface_name, rx_bps, tx_bps) VALUES (?, ?, ?, ?)`,
		at.Unix(),
		interfaceName,
		rxBps,
		txBps,
	)
	if err != nil {
		return fmt.Errorf("save interface sample: %w", err)
	}
	return nil
}

func (s *Store) LoadInterfaceSamples(ctx context.Context, interfaceNames []string, since time.Time) ([]model.RateSample, error) {
	if len(interfaceNames) == 0 {
		return nil, nil
	}

	query := fmt.Sprintf(
		`SELECT ts, rx_bps, tx_bps FROM interface_samples WHERE interface_name IN (%s) AND ts >= ? ORDER BY ts ASC`,
		placeholders(len(interfaceNames)),
	)
	args := make([]any, 0, len(interfaceNames)+1)
	for _, name := range interfaceNames {
		args = append(args, name)
	}
	args = append(args, since.Unix())

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query interface samples: %w", err)
	}
	defer rows.Close()

	aggregated := map[int64]model.RateSample{}
	order := make([]int64, 0)
	for rows.Next() {
		var ts int64
		var rxBps float64
		var txBps float64
		if err := rows.Scan(&ts, &rxBps, &txBps); err != nil {
			return nil, fmt.Errorf("scan interface sample: %w", err)
		}
		sample, exists := aggregated[ts]
		if !exists {
			sample = model.RateSample{Timestamp: time.Unix(ts, 0).UTC()}
			order = append(order, ts)
		}
		sample.DownloadBps += rxBps
		sample.UploadBps += txBps
		aggregated[ts] = sample
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate interface samples: %w", err)
	}

	result := make([]model.RateSample, 0, len(order))
	for _, ts := range order {
		result = append(result, aggregated[ts])
	}
	return result, nil
}

func (s *Store) PruneInterfaceSamples(ctx context.Context, before time.Time) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM interface_samples WHERE ts < ?`, before.Unix())
	if err != nil {
		return fmt.Errorf("prune interface samples: %w", err)
	}
	return nil
}

func (s *Store) UpsertTerminal(ctx context.Context, id, mac, displayName string, seenAt time.Time) error {
	trackingAt := seenAt
	if trackingAt.IsZero() {
		trackingAt = time.Now().UTC()
	}
	lastSeen := int64(0)
	if !seenAt.IsZero() {
		lastSeen = seenAt.Unix()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin upsert terminal: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(
		ctx,
		`INSERT INTO terminals (id, mac, display_name, tracking_since, last_seen)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   mac = CASE WHEN excluded.mac <> '' THEN excluded.mac ELSE terminals.mac END,
		   display_name = CASE WHEN excluded.display_name <> '' THEN excluded.display_name ELSE terminals.display_name END,
		   last_seen = CASE WHEN excluded.last_seen > 0 THEN excluded.last_seen ELSE terminals.last_seen END`,
		id,
		nullIfEmpty(mac),
		displayName,
		trackingAt.Unix(),
		lastSeen,
	)
	if err != nil {
		return fmt.Errorf("upsert terminal row: %w", err)
	}

	_, err = tx.ExecContext(
		ctx,
		`INSERT INTO terminal_totals (terminal_id, upload_bytes, download_bytes, tracking_since, updated_at)
		 VALUES (?, 0, 0, ?, ?)
		 ON CONFLICT(terminal_id) DO NOTHING`,
		id,
		trackingAt.Unix(),
		trackingAt.Unix(),
	)
	if err != nil {
		return fmt.Errorf("ensure terminal totals row: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit upsert terminal: %w", err)
	}
	return nil
}

func (s *Store) UpdateTerminalPresence(ctx context.Context, id, state string, seenAt time.Time) (time.Time, time.Time, error) {
	now := seenAt.Unix()
	if seenAt.IsZero() {
		now = time.Now().UTC().Unix()
	}
	_, err := s.db.ExecContext(ctx, `UPDATE terminals SET
		state = ?,
		online_since = CASE
			WHEN ? <> 'online' THEN 0
			WHEN state <> 'online' OR online_since = 0 THEN ?
			ELSE online_since END,
		last_seen = CASE WHEN ? = 'online' THEN ? ELSE last_seen END
		WHERE id = ?`, state, state, now, state, now, id)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("update terminal presence: %w", err)
	}
	var onlineSince int64
	var lastSeen int64
	if err := s.db.QueryRowContext(ctx, `SELECT online_since, last_seen FROM terminals WHERE id = ?`, id).Scan(&onlineSince, &lastSeen); err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("load terminal presence: %w", err)
	}
	var onlineAt time.Time
	if onlineSince > 0 {
		onlineAt = time.Unix(onlineSince, 0).UTC()
	}
	return onlineAt, time.Unix(lastSeen, 0).UTC(), nil
}

func (s *Store) MergeTerminal(ctx context.Context, fromID, toID string) error {
	if fromID == "" || toID == "" || fromID == toID {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin merge terminal: %w", err)
	}
	defer tx.Rollback()

	row := tx.QueryRowContext(ctx, `SELECT upload_bytes, download_bytes FROM terminal_totals WHERE terminal_id = ?`, fromID)
	var up int64
	var down int64
	switch err := row.Scan(&up, &down); err {
	case nil:
		_, err = tx.ExecContext(
			ctx,
			`INSERT INTO terminal_totals (terminal_id, upload_bytes, download_bytes, tracking_since, updated_at)
			 VALUES (?, ?, ?, strftime('%s','now'), strftime('%s','now'))
			 ON CONFLICT(terminal_id) DO UPDATE SET
			   upload_bytes = terminal_totals.upload_bytes + excluded.upload_bytes,
			   download_bytes = terminal_totals.download_bytes + excluded.download_bytes,
			   updated_at = excluded.updated_at`,
			toID,
			up,
			down,
		)
		if err != nil {
			return fmt.Errorf("merge terminal totals: %w", err)
		}
		_, _ = tx.ExecContext(ctx, `DELETE FROM terminal_totals WHERE terminal_id = ?`, fromID)
	case sql.ErrNoRows:
	default:
		return fmt.Errorf("load terminal totals to merge: %w", err)
	}

	_, err = tx.ExecContext(ctx, `INSERT INTO terminal_addresses (terminal_id, family, address, last_seen)
		SELECT ?, family, address, last_seen FROM terminal_addresses WHERE terminal_id = ?
		ON CONFLICT(terminal_id, family, address) DO UPDATE SET
			last_seen = max(terminal_addresses.last_seen, excluded.last_seen)`, toID, fromID)
	if err != nil {
		return fmt.Errorf("move terminal addresses: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM terminal_addresses WHERE terminal_id = ?`, fromID); err != nil {
		return fmt.Errorf("delete merged terminal addresses: %w", err)
	}
	_, err = tx.ExecContext(ctx, `UPDATE connection_state SET terminal_id = ? WHERE terminal_id = ?`, toID, fromID)
	if err != nil {
		return fmt.Errorf("move connection state: %w", err)
	}
	_, _ = tx.ExecContext(ctx, `DELETE FROM terminals WHERE id = ?`, fromID)

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit merge terminal: %w", err)
	}
	return nil
}

func (s *Store) ReplaceTerminalAddresses(ctx context.Context, terminalID string, ipv4, ipv6 []string, seenAt time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin replace addresses: %w", err)
	}
	defer tx.Rollback()

	for _, address := range uniqueStrings(ipv4) {
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO terminal_addresses (terminal_id, family, address, last_seen)
			 VALUES (?, 'ipv4', ?, ?)
			 ON CONFLICT(terminal_id, family, address) DO UPDATE SET last_seen = excluded.last_seen`,
			terminalID,
			address,
			seenAt.Unix(),
		); err != nil {
			return fmt.Errorf("upsert ipv4 address: %w", err)
		}
	}
	for _, address := range uniqueStrings(ipv6) {
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO terminal_addresses (terminal_id, family, address, last_seen)
			 VALUES (?, 'ipv6', ?, ?)
			 ON CONFLICT(terminal_id, family, address) DO UPDATE SET last_seen = excluded.last_seen`,
			terminalID,
			address,
			seenAt.Unix(),
		); err != nil {
			return fmt.Errorf("upsert ipv6 address: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit replace addresses: %w", err)
	}
	return nil
}

func (s *Store) ApplyConnectionSnapshot(ctx context.Context, connKey, terminalID string, currentUploadBytes, currentDownloadBytes int64, seenAt time.Time) error {
	return s.ApplyConnectionSnapshots(ctx, []ConnectionSnapshot{{Key: connKey, TerminalID: terminalID, UploadBytes: currentUploadBytes, DownloadBytes: currentDownloadBytes, SeenAt: seenAt}})
}

func (s *Store) ApplyConnectionSnapshots(ctx context.Context, snapshots []ConnectionSnapshot) error {
	if len(snapshots) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin apply connection snapshots: %w", err)
	}
	defer tx.Rollback()
	for _, snapshot := range snapshots {
		var previousUpload, previousDownload int64
		err = tx.QueryRowContext(ctx, `SELECT upload_bytes, download_bytes FROM connection_state WHERE conn_key = ?`, snapshot.Key).Scan(&previousUpload, &previousDownload)
		switch err {
		case nil:
		case sql.ErrNoRows:
			previousUpload, previousDownload = snapshot.UploadBytes, snapshot.DownloadBytes
		default:
			return fmt.Errorf("load connection state: %w", err)
		}
		if _, err = tx.ExecContext(ctx, `UPDATE terminal_totals SET upload_bytes = upload_bytes + ?, download_bytes = download_bytes + ?, updated_at = ? WHERE terminal_id = ?`, nonNegativeDelta(previousUpload, snapshot.UploadBytes), nonNegativeDelta(previousDownload, snapshot.DownloadBytes), snapshot.SeenAt.Unix(), snapshot.TerminalID); err != nil {
			return fmt.Errorf("update terminal totals from connection: %w", err)
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO connection_state (conn_key, terminal_id, upload_bytes, download_bytes, last_seen)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(conn_key) DO UPDATE SET
		   terminal_id = excluded.terminal_id,
		   upload_bytes = excluded.upload_bytes,
		   download_bytes = excluded.download_bytes,
		   last_seen = excluded.last_seen`, snapshot.Key, snapshot.TerminalID, snapshot.UploadBytes, snapshot.DownloadBytes, snapshot.SeenAt.Unix()); err != nil {
			return fmt.Errorf("upsert connection state: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit apply connection snapshots: %w", err)
	}
	return nil
}

func (s *Store) TerminalTotals(ctx context.Context, ids []string) (map[string]TerminalTotal, error) {
	result := make(map[string]TerminalTotal, len(ids))
	if len(ids) == 0 {
		return result, nil
	}

	query := fmt.Sprintf(
		`SELECT tt.terminal_id, tt.upload_bytes, tt.download_bytes, tt.tracking_since,
		        COALESCE(t.remark, ''), COALESCE(t.display_name, ''), COALESCE(t.custom_name, ''),
		        COALESCE(t.state, 'offline'), COALESCE(t.online_since, 0), COALESCE(t.last_seen, 0)
		 FROM terminal_totals tt
		 LEFT JOIN terminals t ON t.id = tt.terminal_id
		 WHERE tt.terminal_id IN (%s)`,
		placeholders(len(ids)),
	)
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query terminal totals: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id string
		var total TerminalTotal
		var trackingSince, onlineSince, lastSeen int64
		if err := rows.Scan(&id, &total.UploadBytes, &total.DownloadBytes, &trackingSince, &total.Remark, &total.AutoName, &total.CustomName, &total.State, &onlineSince, &lastSeen); err != nil {
			return nil, fmt.Errorf("scan terminal totals: %w", err)
		}
		total.TrackingSince = time.Unix(trackingSince, 0).UTC()
		if onlineSince > 0 {
			total.OnlineSince = time.Unix(onlineSince, 0).UTC()
		}
		if lastSeen > 0 {
			total.LastSeen = time.Unix(lastSeen, 0).UTC()
		}
		result[id] = total
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate terminal totals: %w", err)
	}

	return result, nil
}

func (s *Store) SaveTerminalHistory(ctx context.Context, terminalID string, at time.Time, onlineSeconds, uploadBytes, downloadBytes int64) error {
	bucket := at.Unix() - at.Unix()%60
	_, err := s.db.ExecContext(
		ctx,
		`INSERT OR REPLACE INTO terminal_history (terminal_id, ts, online_seconds, upload_bytes, download_bytes)
		 VALUES (?, ?, ?, ?, ?)`,
		terminalID,
		bucket,
		onlineSeconds,
		uploadBytes,
		downloadBytes,
	)
	if err != nil {
		return fmt.Errorf("save terminal history: %w", err)
	}
	return nil
}

func (s *Store) PruneRuntimeState(ctx context.Context, connectionBefore, historyBefore time.Time) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM connection_state WHERE last_seen < ?`, connectionBefore.Unix()); err != nil {
		return fmt.Errorf("prune connection state: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM terminal_history WHERE ts < ?`, historyBefore.Unix()); err != nil {
		return fmt.Errorf("prune terminal history: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM load_samples WHERE ts < ?`, historyBefore.Unix()); err != nil {
		return fmt.Errorf("prune load samples: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM protocol_samples WHERE ts < ?`, historyBefore.Unix()); err != nil {
		return fmt.Errorf("prune protocol samples: %w", err)
	}
	return nil
}

func (s *Store) TerminalHistory(ctx context.Context, terminalID string, limit int) ([]model.TerminalHistoryEntry, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT ts, online_seconds, upload_bytes, download_bytes
		 FROM terminal_history
		 WHERE terminal_id = ?
		 ORDER BY ts DESC
		 LIMIT ?`,
		terminalID,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query terminal history: %w", err)
	}
	defer rows.Close()

	result := make([]model.TerminalHistoryEntry, 0, limit)
	for rows.Next() {
		var entry model.TerminalHistoryEntry
		var ts int64
		if err := rows.Scan(&ts, &entry.OnlineSeconds, &entry.TotalUploadBytes, &entry.TotalDownloadBytes); err != nil {
			return nil, fmt.Errorf("scan terminal history: %w", err)
		}
		entry.Timestamp = time.Unix(ts, 0).UTC()
		result = append(result, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate terminal history: %w", err)
	}
	return result, nil
}

func (s *Store) UpdateTerminalRemark(ctx context.Context, terminalID, remark string) error {
	var customName string
	if err := s.db.QueryRowContext(ctx, `SELECT custom_name FROM terminals WHERE id = ?`, terminalID).Scan(&customName); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrTerminalNotFound
		}
		return fmt.Errorf("load terminal custom name: %w", err)
	}
	return s.UpdateTerminalMetadata(ctx, terminalID, customName, remark)
}

func (s *Store) UpdateTerminalMetadata(ctx context.Context, terminalID, customName, remark string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE terminals SET custom_name = ?, remark = ? WHERE id = ?`, customName, remark, terminalID)
	if err != nil {
		return fmt.Errorf("update terminal metadata: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read terminal metadata result: %w", err)
	}
	if rows == 0 {
		return ErrTerminalNotFound
	}
	return nil
}

func placeholders(count int) string {
	if count <= 0 {
		return ""
	}
	items := make([]string, count)
	for index := range count {
		items[index] = "?"
	}
	return strings.Join(items, ", ")
}

func nonNegativeDelta(previous, current int64) int64 {
	if current <= previous {
		return 0
	}
	return current - previous
}

func nullIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func uniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
