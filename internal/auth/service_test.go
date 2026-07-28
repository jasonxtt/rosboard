package auth

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"rosboard/internal/store"
)

func testService(t *testing.T) (*Service, *store.Store, *time.Time) {
	t.Helper()
	storage, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	now := time.Date(2026, 7, 28, 10, 0, 0, 0, time.UTC)
	service := NewWithOptions(storage, Options{
		Now:    func() time.Time { return now },
		Random: bytes.NewReader(bytes.Repeat([]byte{0x42}, 4096)),
		PasswordParams: PasswordParams{
			Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 8, KeyLength: 16,
		},
	})
	return service, storage, &now
}

func TestAdminLifecycleAndPersistentSession(t *testing.T) {
	service, storage, now := testService(t)
	ctx := context.Background()

	session, err := service.CreateAdmin(ctx, " admin ", " 1234 ", " 1234 ")
	if err != nil {
		t.Fatal(err)
	}
	if session.Username != "admin" || session.Token == "" {
		t.Fatalf("unexpected initial session: %#v", session)
	}

	restarted := NewWithOptions(storage, Options{
		Now:    func() time.Time { return *now },
		Random: bytes.NewReader(bytes.Repeat([]byte{0x24}, 4096)),
		PasswordParams: PasswordParams{
			Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 8, KeyLength: 16,
		},
	})
	authenticated, err := restarted.Authenticate(ctx, session.Token)
	if err != nil || authenticated.Username != "admin" {
		t.Fatalf("persistent session failed: session=%#v err=%v", authenticated, err)
	}

	*now = now.Add(25 * time.Hour)
	authenticated, err = restarted.Authenticate(ctx, session.Token)
	if err != nil || !authenticated.Renewed || !authenticated.ExpiresAt.Equal(now.Add(SessionLifetime)) {
		t.Fatalf("session was not renewed: session=%#v err=%v", authenticated, err)
	}

	username, err := restarted.UpdateCredentials(ctx, " owner ", "new pass", "new pass")
	if err != nil || username != "owner" {
		t.Fatalf("update credentials: username=%q err=%v", username, err)
	}
	if _, err := restarted.Authenticate(ctx, session.Token); !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("old session survived credentials change: %v", err)
	}
	if _, err := restarted.Login(ctx, "127.0.0.1", "owner", " 1234 "); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("old password survived: %v", err)
	}
	if _, err := restarted.Login(ctx, "127.0.0.1", "owner", "new pass"); err != nil {
		t.Fatalf("new password login failed: %v", err)
	}
}

func TestPasswordValidationCountsUnicodeCharactersAndPreservesSpaces(t *testing.T) {
	if err := ValidatePassword("1234", "1234"); err != nil {
		t.Fatal(err)
	}
	password := string(bytes.Repeat([]byte("界"), 128))
	if err := ValidatePassword(password, password); err != nil {
		t.Fatalf("128 Unicode characters rejected: %v", err)
	}
	if err := ValidatePassword(password+"界", password+"界"); err == nil {
		t.Fatal("129 Unicode characters were accepted")
	}
	if err := ValidatePassword(" 12 ", " 12 "); err != nil {
		t.Fatalf("spaces should be preserved as password characters: %v", err)
	}
}

func TestConcurrentFirstAdminCreationAllowsOneWinner(t *testing.T) {
	storage, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	now := time.Now
	services := []*Service{
		NewWithOptions(storage, Options{Now: now, Random: bytes.NewReader(bytes.Repeat([]byte{1}, 512)), PasswordParams: PasswordParams{Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 8, KeyLength: 16}}),
		NewWithOptions(storage, Options{Now: now, Random: bytes.NewReader(bytes.Repeat([]byte{2}, 512)), PasswordParams: PasswordParams{Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 8, KeyLength: 16}}),
	}
	var wg sync.WaitGroup
	errorsFound := make(chan error, len(services))
	for index, service := range services {
		wg.Add(1)
		go func(index int, service *Service) {
			defer wg.Done()
			_, err := service.CreateAdmin(context.Background(), []string{"one", "two"}[index], "1234", "1234")
			errorsFound <- err
		}(index, service)
	}
	wg.Wait()
	close(errorsFound)
	winners, conflicts := 0, 0
	for err := range errorsFound {
		switch {
		case err == nil:
			winners++
		case errors.Is(err, ErrAdminExists):
			conflicts++
		default:
			t.Fatalf("unexpected create error: %v", err)
		}
	}
	if winners != 1 || conflicts != 1 {
		t.Fatalf("winners=%d conflicts=%d", winners, conflicts)
	}
}

func TestOnboardingStateIsIndependentFromAdmin(t *testing.T) {
	service, _, _ := testService(t)
	ctx := context.Background()
	if _, err := service.CreateAdmin(ctx, "admin", "1234", "1234"); err != nil {
		t.Fatal(err)
	}
	complete, err := service.OnboardingComplete(ctx)
	if err != nil || complete {
		t.Fatalf("administrator creation completed onboarding: complete=%v err=%v", complete, err)
	}
	if err := service.CompleteOnboarding(ctx); err != nil {
		t.Fatal(err)
	}
	complete, err = service.OnboardingComplete(ctx)
	if err != nil || !complete {
		t.Fatalf("onboarding completion not persisted: complete=%v err=%v", complete, err)
	}
}
