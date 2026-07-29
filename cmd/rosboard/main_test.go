package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"rosboard/internal/auth"
	"rosboard/internal/store"
)

func fastAuthFactory(randomByte byte) func(*store.Store) *auth.Service {
	return func(storage *store.Store) *auth.Service {
		return auth.NewWithOptions(storage, auth.Options{
			Random: bytes.NewReader(bytes.Repeat([]byte{randomByte}, 2048)),
			PasswordParams: auth.PasswordParams{
				Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 8, KeyLength: 16,
			},
		})
	}
}

func TestAdminResetPasswordUpdatesHashAndRevokesSessions(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("data_dir: "+dir+"\npoll_interval_seconds: 10\nrealtime_poll_interval_seconds: 1\nterminal_poll_interval_seconds: 3\nsample_retention_hours: 48\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	storage, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	initial := fastAuthFactory(1)(storage)
	session, err := initial.CreateAdmin(context.Background(), "admin", "1234", "1234")
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.Close(); err != nil {
		t.Fatal(err)
	}

	passwords := []string{"new pass", "new pass"}
	reader := func(string) (string, error) {
		value := passwords[0]
		passwords = passwords[1:]
		return value, nil
	}
	if err := runAdminCommand([]string{"reset-password", "-config", configPath}, reader, fastAuthFactory(2)); err != nil {
		t.Fatal(err)
	}

	storage, err = store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	updated := fastAuthFactory(3)(storage)
	if _, err := updated.Authenticate(context.Background(), session.Token); !errors.Is(err, auth.ErrInvalidSession) {
		t.Fatalf("old session survived reset: %v", err)
	}
	if _, err := updated.Login(context.Background(), "127.0.0.1", "admin", "1234"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("old password survived reset: %v", err)
	}
	if _, err := updated.Login(context.Background(), "127.0.0.1", "admin", "new pass"); err != nil {
		t.Fatalf("new password failed: %v", err)
	}
}

func TestAdminCommandRejectsNonInteractiveReadFailure(t *testing.T) {
	err := runAdminCommand([]string{"reset-password", "-config", "/unused"}, func(string) (string, error) {
		return "", errors.New("not a terminal")
	}, fastAuthFactory(1))
	if err == nil {
		t.Fatal("expected failure")
	}
}
