package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

const managerInstanceMetaKey = "manager_instance_id"

func (s *Store) initInstallationMetaSchema() error {
	if !s.owner {
		return errors.New("installation metadata requires the owner store")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin installation metadata migration: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS installation_meta (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	)`); err != nil {
		return fmt.Errorf("create installation metadata: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit installation metadata migration: %w", err)
	}
	return nil
}

// ManagerInstanceID returns the installation identity, creating it once in a
// transaction. INSERT ... ON CONFLICT makes concurrent first callers converge
// on the same UUID rather than replacing one another.
func (s *Store) ManagerInstanceID(ctx context.Context) (string, error) {
	if s == nil {
		return "", errors.New("manager instance ID requires a store")
	}
	if !s.owner {
		if strings.TrimSpace(s.managerInstanceID) == "" {
			return "", errors.New("manager instance ID requires the owner store")
		}
		return s.managerInstanceID, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("begin manager instance ID lookup: %w", err)
	}
	defer tx.Rollback()
	candidate := uuid.NewString()
	if _, err := tx.ExecContext(ctx, `INSERT INTO installation_meta (key, value) VALUES (?, ?) ON CONFLICT(key) DO NOTHING`, managerInstanceMetaKey, candidate); err != nil {
		return "", fmt.Errorf("initialize manager instance ID: %w", err)
	}
	var value string
	if err := tx.QueryRowContext(ctx, `SELECT value FROM installation_meta WHERE key = ?`, managerInstanceMetaKey).Scan(&value); err != nil {
		return "", fmt.Errorf("read manager instance ID: %w", err)
	}
	value = strings.TrimSpace(value)
	if _, err := uuid.Parse(value); err != nil {
		return "", fmt.Errorf("stored manager instance ID is invalid")
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit manager instance ID: %w", err)
	}
	return value, nil
}
