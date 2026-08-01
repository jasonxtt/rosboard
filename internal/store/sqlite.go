package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"rosboard/internal/model"
)

var ErrTerminalNotFound = errors.New("terminal not found")

type Store struct {
	db         *sql.DB
	deviceID   string
	owner      bool
	closeable  bool
	dataDir    string
	dbPath     string
	childrenMu sync.Mutex
	children   map[string]*Store
}

const defaultDeviceID = "default"

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

type InterfaceSample struct {
	Name        string
	DownloadBps float64
	UploadBps   float64
}

type TerminalMerge struct {
	FromID string
	ToID   string
}

type TerminalSnapshot struct {
	ID          string
	MAC         string
	DisplayName string
	IPv4        []string
	IPv6        []string
	LastSeen    time.Time
	SeenAt      time.Time
}

type TerminalPresence struct {
	ID     string
	State  string
	SeenAt time.Time
}

type TerminalHistorySnapshot struct {
	TerminalID    string
	At            time.Time
	OnlineSeconds int64
	UploadBytes   int64
	DownloadBytes int64
}

func Open(dataDir string) (*Store, error) {
	dbPath := filepath.Join(dataDir, "rosboard.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)

	store := &Store{db: db, deviceID: defaultDeviceID, owner: true, closeable: true, dataDir: dataDir, dbPath: dbPath, children: map[string]*Store{}}
	if err := store.initSchema(); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	if !s.closeable {
		return nil
	}
	if !s.owner {
		return s.db.Close()
	}
	s.childrenMu.Lock()
	children := make([]*Store, 0, len(s.children))
	for _, child := range s.children {
		children = append(children, child)
	}
	s.children = map[string]*Store{}
	s.childrenMu.Unlock()
	var closeErr error
	for _, child := range children {
		if err := child.db.Close(); err != nil && closeErr == nil {
			closeErr = err
		}
	}
	if err := s.db.Close(); err != nil && closeErr == nil {
		closeErr = err
	}
	return closeErr
}

func (s *Store) ForDevice(deviceID string) *Store {
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		deviceID = defaultDeviceID
	}
	if deviceID == defaultDeviceID || !s.owner {
		return &Store{db: s.db, deviceID: deviceID, closeable: false}
	}
	child, err := s.OpenDevice(deviceID)
	if err != nil {
		// Keep the legacy helper's no-error signature for tests and callers that
		// only need a device view. Production monitor startup uses OpenDevice and
		// returns the initialization error instead of falling back silently.
		return &Store{db: s.db, deviceID: deviceID}
	}
	return child
}

// OpenDevice opens the isolated monitoring database for one device. The
// default device remains in the owner database for backward compatibility.
func (s *Store) OpenDevice(deviceID string) (*Store, error) {
	if !s.owner {
		return nil, errors.New("device stores can only be opened from the owner store")
	}
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" || deviceID == defaultDeviceID {
		return &Store{db: s.db, deviceID: defaultDeviceID, closeable: false}, nil
	}

	s.childrenMu.Lock()
	defer s.childrenMu.Unlock()
	if child := s.children[deviceID]; child != nil {
		return child, nil
	}

	deviceDir := filepath.Join(s.dataDir, "devices")
	if err := os.MkdirAll(deviceDir, 0o755); err != nil {
		return nil, fmt.Errorf("create device data directory: %w", err)
	}
	dbPath := filepath.Join(deviceDir, safeDeviceFilename(deviceID)+".db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open device sqlite: %w", err)
	}
	// Each device has its own database, so one serialized connection keeps the
	// writer order deterministic without creating a global cross-device queue.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	child := &Store{db: db, deviceID: deviceID, closeable: true, dataDir: deviceDir, dbPath: dbPath}
	if err := child.initDeviceSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := child.migrateLegacyDevice(s.dbPath, deviceID); err != nil {
		_ = db.Close()
		return nil, err
	}
	s.children[deviceID] = child
	return child, nil
}

func safeDeviceFilename(deviceID string) string {
	var builder strings.Builder
	for _, character := range deviceID {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || character == '-' || character == '_' || character == '.' {
			builder.WriteRune(character)
		} else {
			builder.WriteByte('_')
		}
	}
	if builder.Len() == 0 {
		builder.WriteString("device")
	}
	digest := sha256.Sum256([]byte(deviceID))
	return fmt.Sprintf("%s-%x", builder.String(), digest[:8])
}

func (s *Store) DeviceID() string {
	return s.deviceID
}

func (s *Store) initSchema() error {
	for _, statement := range []string{
		`PRAGMA journal_mode = WAL`,
		`PRAGMA busy_timeout = 5000`,
		`PRAGMA wal_autocheckpoint = 1000`,
		`PRAGMA foreign_keys = ON`,
	} {
		if _, err := s.db.Exec(statement); err != nil {
			return fmt.Errorf("init sqlite pragmas: %w", err)
		}
	}
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
	if _, err := s.db.Exec(`ALTER TABLE load_samples ADD COLUMN connection_count INTEGER NOT NULL DEFAULT -1`); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
		return fmt.Errorf("add load_samples.connection_count: %w", err)
	}
	if err := s.migrateDeviceScope(); err != nil {
		return err
	}
	return s.initAuthSchema()
}

func (s *Store) initDeviceSchema() error {
	statements := []string{
		`PRAGMA journal_mode = WAL`,
		`PRAGMA busy_timeout = 5000`,
		`PRAGMA wal_autocheckpoint = 1000`,
		`PRAGMA foreign_keys = ON`,
		`CREATE TABLE IF NOT EXISTS interface_samples (device_id TEXT NOT NULL, ts INTEGER NOT NULL, interface_name TEXT NOT NULL, rx_bps REAL NOT NULL, tx_bps REAL NOT NULL, PRIMARY KEY (device_id, ts, interface_name))`,
		`CREATE TABLE IF NOT EXISTS terminals (device_id TEXT NOT NULL, id TEXT NOT NULL, mac TEXT, display_name TEXT NOT NULL, remark TEXT NOT NULL DEFAULT '', tracking_since INTEGER NOT NULL, last_seen INTEGER NOT NULL, state TEXT NOT NULL DEFAULT 'offline', online_since INTEGER NOT NULL DEFAULT 0, custom_name TEXT NOT NULL DEFAULT '', PRIMARY KEY (device_id, id))`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_terminals_device_mac ON terminals(device_id, mac) WHERE mac IS NOT NULL AND mac <> ''`,
		`CREATE TABLE IF NOT EXISTS terminal_addresses (device_id TEXT NOT NULL, terminal_id TEXT NOT NULL, family TEXT NOT NULL, address TEXT NOT NULL, last_seen INTEGER NOT NULL, PRIMARY KEY (device_id, terminal_id, family, address))`,
		`CREATE TABLE IF NOT EXISTS terminal_totals (device_id TEXT NOT NULL, terminal_id TEXT NOT NULL, upload_bytes INTEGER NOT NULL, download_bytes INTEGER NOT NULL, tracking_since INTEGER NOT NULL, updated_at INTEGER NOT NULL, PRIMARY KEY (device_id, terminal_id))`,
		`CREATE TABLE IF NOT EXISTS connection_state (device_id TEXT NOT NULL, conn_key TEXT NOT NULL, terminal_id TEXT NOT NULL, upload_bytes INTEGER NOT NULL, download_bytes INTEGER NOT NULL, last_seen INTEGER NOT NULL, PRIMARY KEY (device_id, conn_key))`,
		`CREATE TABLE IF NOT EXISTS terminal_history (device_id TEXT NOT NULL, terminal_id TEXT NOT NULL, ts INTEGER NOT NULL, online_seconds INTEGER NOT NULL, upload_bytes INTEGER NOT NULL, download_bytes INTEGER NOT NULL, PRIMARY KEY (device_id, terminal_id, ts))`,
		`CREATE TABLE IF NOT EXISTS load_samples (device_id TEXT NOT NULL, ts INTEGER NOT NULL, cpu_percent REAL NOT NULL, memory_percent REAL NOT NULL, online_terminals INTEGER NOT NULL, connection_count INTEGER NOT NULL DEFAULT -1, upload_bps REAL NOT NULL, download_bps REAL NOT NULL, storage_percent REAL NOT NULL DEFAULT 0, PRIMARY KEY (device_id, ts))`,
		`CREATE TABLE IF NOT EXISTS protocol_samples (device_id TEXT NOT NULL, ts INTEGER NOT NULL, name TEXT NOT NULL, kind TEXT NOT NULL, connections INTEGER NOT NULL, upload_bps REAL NOT NULL, download_bps REAL NOT NULL, PRIMARY KEY (device_id, ts, name, kind))`,
		`CREATE TABLE IF NOT EXISTS store_meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
	}
	for _, statement := range statements {
		if _, err := s.db.Exec(statement); err != nil {
			return fmt.Errorf("init device schema: %w", err)
		}
	}
	if _, err := s.db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		return fmt.Errorf("checkpoint device schema: %w", err)
	}
	return nil
}

func (s *Store) migrateLegacyDevice(sourcePath, deviceID string) error {
	var migrated string
	err := s.db.QueryRow(`SELECT value FROM store_meta WHERE key = 'legacy_shared_database_migrated'`).Scan(&migrated)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("inspect device migration marker: %w", err)
	}

	if _, err := s.db.Exec(`ATTACH DATABASE ? AS legacy`, sourcePath); err != nil {
		return fmt.Errorf("attach legacy database: %w", err)
	}
	detach := true
	defer func() {
		if detach {
			_, _ = s.db.Exec(`DETACH DATABASE legacy`)
		}
	}()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin device migration: %w", err)
	}
	defer tx.Rollback()
	statements := []string{
		`INSERT OR REPLACE INTO interface_samples SELECT device_id, ts, interface_name, rx_bps, tx_bps FROM legacy.interface_samples WHERE device_id = ?`,
		`INSERT OR REPLACE INTO terminals SELECT device_id, id, mac, display_name, remark, tracking_since, last_seen, state, online_since, custom_name FROM legacy.terminals WHERE device_id = ?`,
		`INSERT OR REPLACE INTO terminal_addresses SELECT device_id, terminal_id, family, address, last_seen FROM legacy.terminal_addresses WHERE device_id = ?`,
		`INSERT OR REPLACE INTO terminal_totals SELECT device_id, terminal_id, upload_bytes, download_bytes, tracking_since, updated_at FROM legacy.terminal_totals WHERE device_id = ?`,
		`INSERT OR REPLACE INTO connection_state SELECT device_id, conn_key, terminal_id, upload_bytes, download_bytes, last_seen FROM legacy.connection_state WHERE device_id = ?`,
		`INSERT OR REPLACE INTO terminal_history SELECT device_id, terminal_id, ts, online_seconds, upload_bytes, download_bytes FROM legacy.terminal_history WHERE device_id = ?`,
		`INSERT OR REPLACE INTO load_samples SELECT device_id, ts, cpu_percent, memory_percent, online_terminals, connection_count, upload_bps, download_bps, storage_percent FROM legacy.load_samples WHERE device_id = ?`,
		`INSERT OR REPLACE INTO protocol_samples SELECT device_id, ts, name, kind, connections, upload_bps, download_bps FROM legacy.protocol_samples WHERE device_id = ?`,
		`INSERT INTO store_meta (key, value) VALUES ('legacy_shared_database_migrated', ?)`,
	}
	for index, statement := range statements {
		value := any(deviceID)
		if index == len(statements)-1 {
			value = "1"
		}
		if _, err := tx.Exec(statement, value); err != nil {
			return fmt.Errorf("copy legacy device data: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit device migration: %w", err)
	}
	return nil
}

func (s *Store) migrateDeviceScope() error {
	rows, err := s.db.Query(`PRAGMA table_info(terminals)`)
	if err != nil {
		return fmt.Errorf("inspect terminal schema: %w", err)
	}
	hasDeviceID := false
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull, primaryKey int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			rows.Close()
			return fmt.Errorf("scan terminal schema: %w", err)
		}
		if name == "device_id" {
			hasDeviceID = true
		}
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close terminal schema rows: %w", err)
	}
	if hasDeviceID {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin device scope migration: %w", err)
	}
	defer tx.Rollback()
	statements := []string{
		`DROP INDEX IF EXISTS idx_terminals_mac`,
		`ALTER TABLE interface_samples RENAME TO interface_samples_legacy`,
		`ALTER TABLE terminals RENAME TO terminals_legacy`,
		`ALTER TABLE terminal_addresses RENAME TO terminal_addresses_legacy`,
		`ALTER TABLE terminal_totals RENAME TO terminal_totals_legacy`,
		`ALTER TABLE connection_state RENAME TO connection_state_legacy`,
		`ALTER TABLE terminal_history RENAME TO terminal_history_legacy`,
		`ALTER TABLE load_samples RENAME TO load_samples_legacy`,
		`ALTER TABLE protocol_samples RENAME TO protocol_samples_legacy`,
		`CREATE TABLE interface_samples (device_id TEXT NOT NULL, ts INTEGER NOT NULL, interface_name TEXT NOT NULL, rx_bps REAL NOT NULL, tx_bps REAL NOT NULL, PRIMARY KEY (device_id, ts, interface_name))`,
		`CREATE TABLE terminals (device_id TEXT NOT NULL, id TEXT NOT NULL, mac TEXT, display_name TEXT NOT NULL, remark TEXT NOT NULL DEFAULT '', tracking_since INTEGER NOT NULL, last_seen INTEGER NOT NULL, state TEXT NOT NULL DEFAULT 'offline', online_since INTEGER NOT NULL DEFAULT 0, custom_name TEXT NOT NULL DEFAULT '', PRIMARY KEY (device_id, id))`,
		`CREATE UNIQUE INDEX idx_terminals_device_mac ON terminals(device_id, mac) WHERE mac IS NOT NULL AND mac <> ''`,
		`CREATE TABLE terminal_addresses (device_id TEXT NOT NULL, terminal_id TEXT NOT NULL, family TEXT NOT NULL, address TEXT NOT NULL, last_seen INTEGER NOT NULL, PRIMARY KEY (device_id, terminal_id, family, address))`,
		`CREATE TABLE terminal_totals (device_id TEXT NOT NULL, terminal_id TEXT NOT NULL, upload_bytes INTEGER NOT NULL, download_bytes INTEGER NOT NULL, tracking_since INTEGER NOT NULL, updated_at INTEGER NOT NULL, PRIMARY KEY (device_id, terminal_id))`,
		`CREATE TABLE connection_state (device_id TEXT NOT NULL, conn_key TEXT NOT NULL, terminal_id TEXT NOT NULL, upload_bytes INTEGER NOT NULL, download_bytes INTEGER NOT NULL, last_seen INTEGER NOT NULL, PRIMARY KEY (device_id, conn_key))`,
		`CREATE TABLE terminal_history (device_id TEXT NOT NULL, terminal_id TEXT NOT NULL, ts INTEGER NOT NULL, online_seconds INTEGER NOT NULL, upload_bytes INTEGER NOT NULL, download_bytes INTEGER NOT NULL, PRIMARY KEY (device_id, terminal_id, ts))`,
		`CREATE TABLE load_samples (device_id TEXT NOT NULL, ts INTEGER NOT NULL, cpu_percent REAL NOT NULL, memory_percent REAL NOT NULL, online_terminals INTEGER NOT NULL, connection_count INTEGER NOT NULL DEFAULT -1, upload_bps REAL NOT NULL, download_bps REAL NOT NULL, storage_percent REAL NOT NULL DEFAULT 0, PRIMARY KEY (device_id, ts))`,
		`CREATE TABLE protocol_samples (device_id TEXT NOT NULL, ts INTEGER NOT NULL, name TEXT NOT NULL, kind TEXT NOT NULL, connections INTEGER NOT NULL, upload_bps REAL NOT NULL, download_bps REAL NOT NULL, PRIMARY KEY (device_id, ts, name, kind))`,
		`INSERT INTO interface_samples SELECT 'default', ts, interface_name, rx_bps, tx_bps FROM interface_samples_legacy`,
		`INSERT INTO terminals SELECT 'default', id, mac, display_name, remark, tracking_since, last_seen, state, online_since, custom_name FROM terminals_legacy`,
		`INSERT INTO terminal_addresses SELECT 'default', terminal_id, family, address, last_seen FROM terminal_addresses_legacy`,
		`INSERT INTO terminal_totals SELECT 'default', terminal_id, upload_bytes, download_bytes, tracking_since, updated_at FROM terminal_totals_legacy`,
		`INSERT INTO connection_state SELECT 'default', conn_key, terminal_id, upload_bytes, download_bytes, last_seen FROM connection_state_legacy`,
		`INSERT INTO terminal_history SELECT 'default', terminal_id, ts, online_seconds, upload_bytes, download_bytes FROM terminal_history_legacy`,
		`INSERT INTO load_samples SELECT 'default', ts, cpu_percent, memory_percent, online_terminals, connection_count, upload_bps, download_bps, storage_percent FROM load_samples_legacy`,
		`INSERT INTO protocol_samples SELECT 'default', ts, name, kind, connections, upload_bps, download_bps FROM protocol_samples_legacy`,
		`DROP TABLE interface_samples_legacy`, `DROP TABLE terminals_legacy`, `DROP TABLE terminal_addresses_legacy`, `DROP TABLE terminal_totals_legacy`,
		`DROP TABLE connection_state_legacy`, `DROP TABLE terminal_history_legacy`, `DROP TABLE load_samples_legacy`, `DROP TABLE protocol_samples_legacy`,
		`PRAGMA user_version = 2`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			return fmt.Errorf("migrate device scope: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit device scope migration: %w", err)
	}
	return nil
}

func (s *Store) SaveLoadSample(ctx context.Context, sample model.LoadSample) error {
	bucket := sample.Timestamp.Unix() - sample.Timestamp.Unix()%60
	_, err := s.db.ExecContext(ctx, `INSERT OR REPLACE INTO load_samples
		(device_id, ts, cpu_percent, memory_percent, online_terminals, connection_count, upload_bps, download_bps, storage_percent)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, s.deviceID, bucket, sample.CPULoadPercent, sample.MemoryUsedPercent, sample.OnlineTerminalCount, sample.ConnectionCount, sample.UploadBps, sample.DownloadBps, sample.StorageUsedPercent)
	if err != nil {
		return fmt.Errorf("save load sample: %w", err)
	}
	return nil
}

func (s *Store) LoadSamples(ctx context.Context, since time.Time) ([]model.LoadSample, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT ts, cpu_percent, memory_percent, online_terminals, connection_count, upload_bps, download_bps, storage_percent
		FROM load_samples WHERE device_id = ? AND ts >= ? ORDER BY ts ASC`, s.deviceID, since.Unix())
	if err != nil {
		return nil, fmt.Errorf("query load samples: %w", err)
	}
	defer rows.Close()
	result := make([]model.LoadSample, 0)
	for rows.Next() {
		var sample model.LoadSample
		var ts int64
		if err := rows.Scan(&ts, &sample.CPULoadPercent, &sample.MemoryUsedPercent, &sample.OnlineTerminalCount, &sample.ConnectionCount, &sample.UploadBps, &sample.DownloadBps, &sample.StorageUsedPercent); err != nil {
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
		if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO protocol_samples (device_id, ts, name, kind, connections, upload_bps, download_bps) VALUES (?, ?, ?, ?, ?, ?, ?)`, s.deviceID, bucket, stat.Name, stat.Kind, stat.Connections, stat.UploadBps, stat.DownloadBps); err != nil {
			return fmt.Errorf("save protocol sample: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit protocol samples: %w", err)
	}
	return nil
}

func (s *Store) ProtocolSamples(ctx context.Context, since time.Time) ([]model.ProtocolHistorySample, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT ts, name, kind, connections, upload_bps, download_bps FROM protocol_samples WHERE device_id = ? AND ts >= ? ORDER BY ts ASC, name ASC`, s.deviceID, since.Unix())
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
	return s.SaveInterfaceSamples(ctx, at, []InterfaceSample{{Name: interfaceName, DownloadBps: rxBps, UploadBps: txBps}})
}

func (s *Store) SaveInterfaceSamples(ctx context.Context, at time.Time, samples []InterfaceSample) error {
	if len(samples) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin interface sample batch: %w", err)
	}
	defer tx.Rollback()
	for _, sample := range samples {
		if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO interface_samples (device_id, ts, interface_name, rx_bps, tx_bps) VALUES (?, ?, ?, ?, ?)`, s.deviceID, at.Unix(), sample.Name, sample.DownloadBps, sample.UploadBps); err != nil {
			return fmt.Errorf("save interface sample in batch: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit interface sample batch: %w", err)
	}
	return nil
}

func (s *Store) LoadInterfaceSamples(ctx context.Context, interfaceNames []string, since time.Time) ([]model.RateSample, error) {
	if len(interfaceNames) == 0 {
		return nil, nil
	}

	query := fmt.Sprintf(
		`SELECT ts, rx_bps, tx_bps FROM interface_samples WHERE device_id = ? AND interface_name IN (%s) AND ts >= ? ORDER BY ts ASC`,
		placeholders(len(interfaceNames)),
	)
	args := make([]any, 0, len(interfaceNames)+2)
	args = append(args, s.deviceID)
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
	_, err := s.db.ExecContext(ctx, `DELETE FROM interface_samples WHERE device_id = ? AND ts < ?`, s.deviceID, before.Unix())
	if err != nil {
		return fmt.Errorf("prune interface samples: %w", err)
	}
	return nil
}

func (s *Store) ApplyTerminalMetadata(ctx context.Context, merges []TerminalMerge, terminals []TerminalSnapshot) (map[string]TerminalTotal, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin terminal metadata batch: %w", err)
	}
	defer tx.Rollback()

	for _, merge := range merges {
		if err := mergeTerminalTx(ctx, tx, s.deviceID, merge.FromID, merge.ToID); err != nil {
			return nil, err
		}
	}
	ids := make([]string, 0, len(terminals))
	for _, terminal := range terminals {
		trackingAt := terminal.LastSeen
		if trackingAt.IsZero() {
			trackingAt = time.Now().UTC()
		}
		lastSeen := int64(0)
		if !terminal.LastSeen.IsZero() {
			lastSeen = terminal.LastSeen.Unix()
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO terminals (device_id, id, mac, display_name, tracking_since, last_seen)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(device_id, id) DO UPDATE SET
			  mac = CASE WHEN excluded.mac <> '' THEN excluded.mac ELSE terminals.mac END,
			  display_name = CASE WHEN excluded.display_name <> '' THEN excluded.display_name ELSE terminals.display_name END,
			  last_seen = CASE WHEN excluded.last_seen > 0 THEN excluded.last_seen ELSE terminals.last_seen END`,
			s.deviceID, terminal.ID, nullIfEmpty(terminal.MAC), terminal.DisplayName, trackingAt.Unix(), lastSeen); err != nil {
			return nil, fmt.Errorf("upsert terminal row in batch: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO terminal_totals (device_id, terminal_id, upload_bytes, download_bytes, tracking_since, updated_at)
			VALUES (?, ?, 0, 0, ?, ?)
			ON CONFLICT(device_id, terminal_id) DO NOTHING`, s.deviceID, terminal.ID, trackingAt.Unix(), trackingAt.Unix()); err != nil {
			return nil, fmt.Errorf("ensure terminal totals row in batch: %w", err)
		}
		seenAt := terminal.SeenAt
		if seenAt.IsZero() {
			seenAt = time.Now().UTC()
		}
		for _, address := range uniqueStrings(terminal.IPv4) {
			if _, err := tx.ExecContext(ctx, `INSERT INTO terminal_addresses (device_id, terminal_id, family, address, last_seen)
				VALUES (?, ?, 'ipv4', ?, ?)
				ON CONFLICT(device_id, terminal_id, family, address) DO UPDATE SET last_seen = excluded.last_seen`,
				s.deviceID, terminal.ID, address, seenAt.Unix()); err != nil {
				return nil, fmt.Errorf("upsert ipv4 address in batch: %w", err)
			}
		}
		for _, address := range uniqueStrings(terminal.IPv6) {
			if _, err := tx.ExecContext(ctx, `INSERT INTO terminal_addresses (device_id, terminal_id, family, address, last_seen)
				VALUES (?, ?, 'ipv6', ?, ?)
				ON CONFLICT(device_id, terminal_id, family, address) DO UPDATE SET last_seen = excluded.last_seen`,
				s.deviceID, terminal.ID, address, seenAt.Unix()); err != nil {
				return nil, fmt.Errorf("upsert ipv6 address in batch: %w", err)
			}
		}
		ids = append(ids, terminal.ID)
	}
	previous, err := s.terminalTotalsTx(ctx, tx, ids)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit terminal metadata batch: %w", err)
	}
	return previous, nil
}

func (s *Store) ApplyTerminalRuntime(ctx context.Context, presence []TerminalPresence, connections []ConnectionSnapshot) (map[string]TerminalTotal, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin terminal runtime batch: %w", err)
	}
	defer tx.Rollback()
	for _, item := range presence {
		now := item.SeenAt.Unix()
		if item.SeenAt.IsZero() {
			now = time.Now().UTC().Unix()
		}
		if _, err := tx.ExecContext(ctx, `UPDATE terminals SET
			state = ?,
			online_since = CASE
			  WHEN ? <> 'online' THEN 0
			  WHEN state <> 'online' OR online_since = 0 THEN ?
			  ELSE online_since END,
			last_seen = CASE WHEN ? = 'online' THEN ? ELSE last_seen END
			WHERE device_id = ? AND id = ?`, item.State, item.State, now, item.State, now, s.deviceID, item.ID); err != nil {
			return nil, fmt.Errorf("update terminal presence in batch: %w", err)
		}
	}
	for _, snapshot := range connections {
		if err := applyConnectionSnapshotTx(ctx, tx, s.deviceID, snapshot); err != nil {
			return nil, err
		}
	}
	ids := make([]string, 0, len(presence))
	seen := make(map[string]struct{}, len(presence))
	for _, item := range presence {
		if _, ok := seen[item.ID]; ok {
			continue
		}
		seen[item.ID] = struct{}{}
		ids = append(ids, item.ID)
	}
	totals, err := s.terminalTotalsTx(ctx, tx, ids)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit terminal runtime batch: %w", err)
	}
	return totals, nil
}

func (s *Store) SaveTerminalHistories(ctx context.Context, histories []TerminalHistorySnapshot) error {
	if len(histories) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin terminal history batch: %w", err)
	}
	defer tx.Rollback()
	for _, history := range histories {
		bucket := history.At.Unix() - history.At.Unix()%60
		if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO terminal_history (device_id, terminal_id, ts, online_seconds, upload_bytes, download_bytes)
			VALUES (?, ?, ?, ?, ?, ?)`, s.deviceID, history.TerminalID, bucket, history.OnlineSeconds, history.UploadBytes, history.DownloadBytes); err != nil {
			return fmt.Errorf("save terminal history in batch: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit terminal history batch: %w", err)
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
		`INSERT INTO terminals (device_id, id, mac, display_name, tracking_since, last_seen)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(device_id, id) DO UPDATE SET
		   mac = CASE WHEN excluded.mac <> '' THEN excluded.mac ELSE terminals.mac END,
		   display_name = CASE WHEN excluded.display_name <> '' THEN excluded.display_name ELSE terminals.display_name END,
		   last_seen = CASE WHEN excluded.last_seen > 0 THEN excluded.last_seen ELSE terminals.last_seen END`,
		s.deviceID,
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
		`INSERT INTO terminal_totals (device_id, terminal_id, upload_bytes, download_bytes, tracking_since, updated_at)
		 VALUES (?, ?, 0, 0, ?, ?)
		 ON CONFLICT(device_id, terminal_id) DO NOTHING`,
		s.deviceID,
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
		WHERE device_id = ? AND id = ?`, state, state, now, state, now, s.deviceID, id)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("update terminal presence: %w", err)
	}
	var onlineSince int64
	var lastSeen int64
	if err := s.db.QueryRowContext(ctx, `SELECT online_since, last_seen FROM terminals WHERE device_id = ? AND id = ?`, s.deviceID, id).Scan(&onlineSince, &lastSeen); err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("load terminal presence: %w", err)
	}
	var onlineAt time.Time
	if onlineSince > 0 {
		onlineAt = time.Unix(onlineSince, 0).UTC()
	}
	return onlineAt, time.Unix(lastSeen, 0).UTC(), nil
}

func mergeTerminalTx(ctx context.Context, tx *sql.Tx, deviceID, fromID, toID string) error {
	if fromID == "" || toID == "" || fromID == toID {
		return nil
	}
	row := tx.QueryRowContext(ctx, `SELECT upload_bytes, download_bytes FROM terminal_totals WHERE device_id = ? AND terminal_id = ?`, deviceID, fromID)
	var up int64
	var down int64
	switch err := row.Scan(&up, &down); err {
	case nil:
		if _, err = tx.ExecContext(ctx, `INSERT INTO terminal_totals (device_id, terminal_id, upload_bytes, download_bytes, tracking_since, updated_at)
			VALUES (?, ?, ?, ?, strftime('%s','now'), strftime('%s','now'))
			ON CONFLICT(device_id, terminal_id) DO UPDATE SET
			  upload_bytes = terminal_totals.upload_bytes + excluded.upload_bytes,
			  download_bytes = terminal_totals.download_bytes + excluded.download_bytes,
			  updated_at = excluded.updated_at`, deviceID, toID, up, down); err != nil {
			return fmt.Errorf("merge terminal totals: %w", err)
		}
		_, _ = tx.ExecContext(ctx, `DELETE FROM terminal_totals WHERE device_id = ? AND terminal_id = ?`, deviceID, fromID)
	case sql.ErrNoRows:
	default:
		return fmt.Errorf("load terminal totals to merge: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `INSERT INTO terminal_addresses (device_id, terminal_id, family, address, last_seen)
		SELECT ?, ?, family, address, last_seen FROM terminal_addresses WHERE device_id = ? AND terminal_id = ?
		ON CONFLICT(device_id, terminal_id, family, address) DO UPDATE SET
		  last_seen = max(terminal_addresses.last_seen, excluded.last_seen)`, deviceID, toID, deviceID, fromID); err != nil {
		return fmt.Errorf("move terminal addresses: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM terminal_addresses WHERE device_id = ? AND terminal_id = ?`, deviceID, fromID); err != nil {
		return fmt.Errorf("delete merged terminal addresses: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE connection_state SET terminal_id = ? WHERE device_id = ? AND terminal_id = ?`, toID, deviceID, fromID); err != nil {
		return fmt.Errorf("move connection state: %w", err)
	}
	_, _ = tx.ExecContext(ctx, `DELETE FROM terminals WHERE device_id = ? AND id = ?`, deviceID, fromID)
	return nil
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
	if err := mergeTerminalTx(ctx, tx, s.deviceID, fromID, toID); err != nil {
		return err
	}

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
			`INSERT INTO terminal_addresses (device_id, terminal_id, family, address, last_seen)
			 VALUES (?, ?, 'ipv4', ?, ?)
			 ON CONFLICT(device_id, terminal_id, family, address) DO UPDATE SET last_seen = excluded.last_seen`,
			s.deviceID,
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
			`INSERT INTO terminal_addresses (device_id, terminal_id, family, address, last_seen)
			 VALUES (?, ?, 'ipv6', ?, ?)
			 ON CONFLICT(device_id, terminal_id, family, address) DO UPDATE SET last_seen = excluded.last_seen`,
			s.deviceID,
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

func applyConnectionSnapshotTx(ctx context.Context, tx *sql.Tx, deviceID string, snapshot ConnectionSnapshot) error {
	var previousUpload, previousDownload int64
	err := tx.QueryRowContext(ctx, `SELECT upload_bytes, download_bytes FROM connection_state WHERE device_id = ? AND conn_key = ?`, deviceID, snapshot.Key).Scan(&previousUpload, &previousDownload)
	switch err {
	case nil:
	case sql.ErrNoRows:
		previousUpload, previousDownload = snapshot.UploadBytes, snapshot.DownloadBytes
	default:
		return fmt.Errorf("load connection state: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE terminal_totals SET upload_bytes = upload_bytes + ?, download_bytes = download_bytes + ?, updated_at = ? WHERE device_id = ? AND terminal_id = ?`, nonNegativeDelta(previousUpload, snapshot.UploadBytes), nonNegativeDelta(previousDownload, snapshot.DownloadBytes), snapshot.SeenAt.Unix(), deviceID, snapshot.TerminalID); err != nil {
		return fmt.Errorf("update terminal totals from connection: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO connection_state (device_id, conn_key, terminal_id, upload_bytes, download_bytes, last_seen)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(device_id, conn_key) DO UPDATE SET
		  terminal_id = excluded.terminal_id,
		  upload_bytes = excluded.upload_bytes,
		  download_bytes = excluded.download_bytes,
		  last_seen = excluded.last_seen`, deviceID, snapshot.Key, snapshot.TerminalID, snapshot.UploadBytes, snapshot.DownloadBytes, snapshot.SeenAt.Unix()); err != nil {
		return fmt.Errorf("upsert connection state: %w", err)
	}
	return nil
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
		if err := applyConnectionSnapshotTx(ctx, tx, s.deviceID, snapshot); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit apply connection snapshots: %w", err)
	}
	return nil
}

func (s *Store) terminalTotalsTx(ctx context.Context, tx *sql.Tx, ids []string) (map[string]TerminalTotal, error) {
	result := make(map[string]TerminalTotal, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	query := fmt.Sprintf(`SELECT tt.terminal_id, tt.upload_bytes, tt.download_bytes, tt.tracking_since,
		COALESCE(t.remark, ''), COALESCE(t.display_name, ''), COALESCE(t.custom_name, ''),
		COALESCE(t.state, 'offline'), COALESCE(t.online_since, 0), COALESCE(t.last_seen, 0)
		FROM terminal_totals tt
		LEFT JOIN terminals t ON t.device_id = tt.device_id AND t.id = tt.terminal_id
		WHERE tt.device_id = ? AND tt.terminal_id IN (%s)`, placeholders(len(ids)))
	args := make([]any, 0, len(ids)+1)
	args = append(args, s.deviceID)
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query terminal totals in batch: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var total TerminalTotal
		var trackingSince, onlineSince, lastSeen int64
		if err := rows.Scan(&id, &total.UploadBytes, &total.DownloadBytes, &trackingSince, &total.Remark, &total.AutoName, &total.CustomName, &total.State, &onlineSince, &lastSeen); err != nil {
			return nil, fmt.Errorf("scan terminal totals in batch: %w", err)
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
		return nil, fmt.Errorf("iterate terminal totals in batch: %w", err)
	}
	return result, nil
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
		 LEFT JOIN terminals t ON t.device_id = tt.device_id AND t.id = tt.terminal_id
		 WHERE tt.device_id = ? AND tt.terminal_id IN (%s)`,
		placeholders(len(ids)),
	)
	args := make([]any, 0, len(ids)+1)
	args = append(args, s.deviceID)
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
		`INSERT OR REPLACE INTO terminal_history (device_id, terminal_id, ts, online_seconds, upload_bytes, download_bytes)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		s.deviceID,
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
	if _, err := s.db.ExecContext(ctx, `DELETE FROM connection_state WHERE device_id = ? AND last_seen < ?`, s.deviceID, connectionBefore.Unix()); err != nil {
		return fmt.Errorf("prune connection state: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM terminal_history WHERE device_id = ? AND ts < ?`, s.deviceID, historyBefore.Unix()); err != nil {
		return fmt.Errorf("prune terminal history: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM load_samples WHERE device_id = ? AND ts < ?`, s.deviceID, historyBefore.Unix()); err != nil {
		return fmt.Errorf("prune load samples: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM protocol_samples WHERE device_id = ? AND ts < ?`, s.deviceID, historyBefore.Unix()); err != nil {
		return fmt.Errorf("prune protocol samples: %w", err)
	}
	return nil
}

func (s *Store) TerminalHistory(ctx context.Context, terminalID string, limit int) ([]model.TerminalHistoryEntry, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT ts, online_seconds, upload_bytes, download_bytes
		 FROM terminal_history
		 WHERE device_id = ? AND terminal_id = ?
		 ORDER BY ts DESC
		 LIMIT ?`,
		s.deviceID,
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

func (s *Store) TerminalHistories(ctx context.Context, terminalIDs []string, limit int) (map[string][]model.TerminalHistoryEntry, error) {
	result := make(map[string][]model.TerminalHistoryEntry, len(terminalIDs))
	if len(terminalIDs) == 0 || limit <= 0 {
		return result, nil
	}
	query := fmt.Sprintf(`SELECT terminal_id, ts, online_seconds, upload_bytes, download_bytes
		FROM terminal_history WHERE device_id = ? AND terminal_id IN (%s)
		ORDER BY terminal_id ASC, ts DESC`, placeholders(len(terminalIDs)))
	args := make([]any, 0, len(terminalIDs)+1)
	args = append(args, s.deviceID)
	for _, id := range terminalIDs {
		args = append(args, id)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query terminal histories: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var entry model.TerminalHistoryEntry
		var ts int64
		if err := rows.Scan(&id, &ts, &entry.OnlineSeconds, &entry.TotalUploadBytes, &entry.TotalDownloadBytes); err != nil {
			return nil, fmt.Errorf("scan terminal history batch: %w", err)
		}
		if len(result[id]) >= limit {
			continue
		}
		entry.Timestamp = time.Unix(ts, 0).UTC()
		result[id] = append(result[id], entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate terminal histories: %w", err)
	}
	return result, nil
}

func (s *Store) UpdateTerminalMetadata(ctx context.Context, terminalID, customName, remark string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE terminals SET custom_name = ?, remark = ? WHERE device_id = ? AND id = ?`, customName, remark, s.deviceID, terminalID)
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

func (s *Store) PurgeDevice(ctx context.Context, deviceID string) error {
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return errors.New("device id is required")
	}
	if s.owner && deviceID != defaultDeviceID {
		child, err := s.OpenDevice(deviceID)
		if err != nil {
			return fmt.Errorf("open device store for purge: %w", err)
		}
		if err := child.purgeDeviceData(ctx, deviceID); err != nil {
			return err
		}
		// Remove the legacy shared copy as well so a later rollback cannot
		// resurrect data that the user explicitly purged.
		return s.purgeDeviceData(ctx, deviceID)
	}
	return s.purgeDeviceData(ctx, deviceID)
}

func (s *Store) purgeDeviceData(ctx context.Context, deviceID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin purge device: %w", err)
	}
	defer tx.Rollback()
	for _, table := range []string{
		"interface_samples", "terminal_addresses", "terminal_totals", "connection_state",
		"terminal_history", "load_samples", "protocol_samples", "terminals",
	} {
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE device_id = ?`, deviceID); err != nil {
			return fmt.Errorf("purge device from %s: %w", table, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit purge device: %w", err)
	}
	return nil
}

func (s *Store) ResetAll(ctx context.Context) error {
	if !s.owner {
		return errors.New("full reset requires the owner store")
	}
	s.childrenMu.Lock()
	children := make([]*Store, 0, len(s.children))
	for _, child := range s.children {
		children = append(children, child)
	}
	s.childrenMu.Unlock()
	for _, child := range children {
		if err := child.purgeDeviceData(ctx, child.deviceID); err != nil {
			return err
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin full reset: %w", err)
	}
	defer tx.Rollback()
	for _, table := range []string{
		"interface_samples", "terminal_addresses", "terminal_totals", "connection_state",
		"terminal_history", "load_samples", "protocol_samples", "terminals",
		"auth_sessions", "admin_account", "app_state",
	} {
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+table); err != nil {
			return fmt.Errorf("reset %s: %w", table, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit full reset: %w", err)
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
