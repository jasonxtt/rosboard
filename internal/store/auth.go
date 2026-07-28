package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrAdminExists     = errors.New("administrator already exists")
	ErrAdminNotFound   = errors.New("administrator not found")
	ErrSessionNotFound = errors.New("session not found")
)

const administratorID int64 = 1

type AdminAccount struct {
	ID           int64
	Username     string
	PasswordHash string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type AuthSession struct {
	TokenHash []byte
	AdminID   int64
	CreatedAt time.Time
	LastSeen  time.Time
	ExpiresAt time.Time
}

func (s *Store) initAuthSchema() error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS admin_account (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			username TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS auth_sessions (
			token_hash BLOB PRIMARY KEY,
			admin_id INTEGER NOT NULL,
			created_at INTEGER NOT NULL,
			last_seen INTEGER NOT NULL,
			expires_at INTEGER NOT NULL,
			FOREIGN KEY (admin_id) REFERENCES admin_account(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_auth_sessions_expires_at ON auth_sessions(expires_at)`,
		`CREATE TABLE IF NOT EXISTS app_state (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
	}
	for _, statement := range statements {
		if _, err := s.db.Exec(statement); err != nil {
			return fmt.Errorf("init auth schema: %w", err)
		}
	}
	return nil
}

func (s *Store) CreateAdmin(ctx context.Context, username, passwordHash string, now time.Time) (AdminAccount, error) {
	_, err := s.db.ExecContext(ctx, `INSERT INTO admin_account (id, username, password_hash, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`, administratorID, username, passwordHash, now.Unix(), now.Unix())
	if err != nil {
		if isConstraintError(err) {
			return AdminAccount{}, ErrAdminExists
		}
		return AdminAccount{}, fmt.Errorf("create administrator: %w", err)
	}
	return AdminAccount{ID: administratorID, Username: username, PasswordHash: passwordHash, CreatedAt: now.UTC(), UpdatedAt: now.UTC()}, nil
}

func (s *Store) CreateAdminWithSession(ctx context.Context, username, passwordHash string, session AuthSession, now time.Time) (AdminAccount, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return AdminAccount{}, fmt.Errorf("begin administrator creation: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO admin_account (id, username, password_hash, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`, administratorID, username, passwordHash, now.Unix(), now.Unix()); err != nil {
		if isConstraintError(err) {
			return AdminAccount{}, ErrAdminExists
		}
		return AdminAccount{}, fmt.Errorf("create administrator: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO auth_sessions (token_hash, admin_id, created_at, last_seen, expires_at) VALUES (?, ?, ?, ?, ?)`, session.TokenHash, administratorID, session.CreatedAt.Unix(), session.LastSeen.Unix(), session.ExpiresAt.Unix()); err != nil {
		return AdminAccount{}, fmt.Errorf("create initial auth session: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return AdminAccount{}, fmt.Errorf("commit administrator creation: %w", err)
	}
	return AdminAccount{ID: administratorID, Username: username, PasswordHash: passwordHash, CreatedAt: now.UTC(), UpdatedAt: now.UTC()}, nil
}

func (s *Store) Admin(ctx context.Context) (AdminAccount, error) {
	var account AdminAccount
	var createdAt, updatedAt int64
	err := s.db.QueryRowContext(ctx, `SELECT id, username, password_hash, created_at, updated_at FROM admin_account WHERE id = ?`, administratorID).Scan(&account.ID, &account.Username, &account.PasswordHash, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return AdminAccount{}, ErrAdminNotFound
	}
	if err != nil {
		return AdminAccount{}, fmt.Errorf("load administrator: %w", err)
	}
	account.CreatedAt = time.Unix(createdAt, 0).UTC()
	account.UpdatedAt = time.Unix(updatedAt, 0).UTC()
	return account, nil
}

func (s *Store) UpdateAdminPassword(ctx context.Context, passwordHash string, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin administrator password update: %w", err)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE admin_account SET password_hash = ?, updated_at = ? WHERE id = ?`, passwordHash, now.Unix(), administratorID)
	if err != nil {
		return fmt.Errorf("update administrator password: %w", err)
	}
	if err := requireChangedRow(result, ErrAdminNotFound); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM auth_sessions WHERE admin_id = ?`, administratorID); err != nil {
		return fmt.Errorf("revoke administrator sessions: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit administrator password update: %w", err)
	}
	return nil
}

func (s *Store) UpdateAdminCredentials(ctx context.Context, username, passwordHash string, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin administrator credentials update: %w", err)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE admin_account SET username = ?, password_hash = ?, updated_at = ? WHERE id = ?`, username, passwordHash, now.Unix(), administratorID)
	if err != nil {
		return fmt.Errorf("update administrator credentials: %w", err)
	}
	if err := requireChangedRow(result, ErrAdminNotFound); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM auth_sessions WHERE admin_id = ?`, administratorID); err != nil {
		return fmt.Errorf("revoke administrator sessions: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit administrator credentials update: %w", err)
	}
	return nil
}

func (s *Store) CreateAuthSession(ctx context.Context, session AuthSession) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO auth_sessions (token_hash, admin_id, created_at, last_seen, expires_at) VALUES (?, ?, ?, ?, ?)`, session.TokenHash, session.AdminID, session.CreatedAt.Unix(), session.LastSeen.Unix(), session.ExpiresAt.Unix())
	if err != nil {
		return fmt.Errorf("create auth session: %w", err)
	}
	return nil
}

func (s *Store) AuthSession(ctx context.Context, tokenHash []byte) (AuthSession, error) {
	var session AuthSession
	var createdAt, lastSeen, expiresAt int64
	err := s.db.QueryRowContext(ctx, `SELECT token_hash, admin_id, created_at, last_seen, expires_at FROM auth_sessions WHERE token_hash = ?`, tokenHash).Scan(&session.TokenHash, &session.AdminID, &createdAt, &lastSeen, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return AuthSession{}, ErrSessionNotFound
	}
	if err != nil {
		return AuthSession{}, fmt.Errorf("load auth session: %w", err)
	}
	session.CreatedAt = time.Unix(createdAt, 0).UTC()
	session.LastSeen = time.Unix(lastSeen, 0).UTC()
	session.ExpiresAt = time.Unix(expiresAt, 0).UTC()
	return session, nil
}

func (s *Store) RenewAuthSession(ctx context.Context, tokenHash []byte, lastSeen, expiresAt time.Time) error {
	result, err := s.db.ExecContext(ctx, `UPDATE auth_sessions SET last_seen = ?, expires_at = ? WHERE token_hash = ?`, lastSeen.Unix(), expiresAt.Unix(), tokenHash)
	if err != nil {
		return fmt.Errorf("renew auth session: %w", err)
	}
	return requireChangedRow(result, ErrSessionNotFound)
}

func (s *Store) DeleteAuthSession(ctx context.Context, tokenHash []byte) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM auth_sessions WHERE token_hash = ?`, tokenHash); err != nil {
		return fmt.Errorf("delete auth session: %w", err)
	}
	return nil
}

func (s *Store) DeleteExpiredAuthSessions(ctx context.Context, now time.Time) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM auth_sessions WHERE expires_at <= ?`, now.Unix()); err != nil {
		return fmt.Errorf("delete expired auth sessions: %w", err)
	}
	return nil
}

func (s *Store) OnboardingComplete(ctx context.Context) (bool, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM app_state WHERE key = 'onboarding_complete'`).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("load onboarding state: %w", err)
	}
	return value == "true", nil
}

func (s *Store) SetOnboardingComplete(ctx context.Context, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO app_state (key, value, updated_at) VALUES ('onboarding_complete', 'true', ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`, now.Unix())
	if err != nil {
		return fmt.Errorf("complete onboarding: %w", err)
	}
	return nil
}

func requireChangedRow(result sql.Result, notFound error) error {
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("inspect changed rows: %w", err)
	}
	if count == 0 {
		return notFound
	}
	return nil
}

func isConstraintError(err error) bool {
	return err != nil && (containsError(err, "constraint failed") || containsError(err, "UNIQUE constraint"))
}

func containsError(err error, fragment string) bool {
	return err != nil && strings.Contains(err.Error(), fragment)
}
