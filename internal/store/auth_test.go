package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"rosboard/internal/policyv2"
)

func TestPasswordUpdateRevokesAllSessionsAtomically(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	if _, err := storage.CreateAdmin(ctx, "admin", "old-hash", now); err != nil {
		t.Fatal(err)
	}
	for _, token := range [][]byte{{1}, {2}} {
		if err := storage.CreateAuthSession(ctx, AuthSession{TokenHash: token, AdminID: 1, CreatedAt: now, LastSeen: now, ExpiresAt: now.Add(time.Hour)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := storage.UpdateAdminPassword(ctx, "new-hash", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	for _, token := range [][]byte{{1}, {2}} {
		if _, err := storage.AuthSession(ctx, token); !errors.Is(err, ErrSessionNotFound) {
			t.Fatalf("session %v survived: %v", token, err)
		}
	}
	account, err := storage.Admin(ctx)
	if err != nil || account.PasswordHash != "new-hash" {
		t.Fatalf("password hash not updated: account=%#v err=%v", account, err)
	}
}

func TestCredentialsUpdateChangesUsernameAndRevokesSessionsAtomically(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	if _, err := storage.CreateAdmin(ctx, "admin", "old-hash", now); err != nil {
		t.Fatal(err)
	}
	if err := storage.CreateAuthSession(ctx, AuthSession{TokenHash: []byte{1}, AdminID: 1, CreatedAt: now, LastSeen: now, ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := storage.UpdateAdminCredentials(ctx, "owner", "new-hash", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	account, err := storage.Admin(ctx)
	if err != nil || account.Username != "owner" || account.PasswordHash != "new-hash" {
		t.Fatalf("credentials not updated: account=%#v err=%v", account, err)
	}
	if _, err := storage.AuthSession(ctx, []byte{1}); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("session survived credentials update: %v", err)
	}
}

func TestResetAllClearsAuthenticationStateAndMonitoringData(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	if _, err := storage.CreateAdmin(ctx, "admin", "hash", now); err != nil {
		t.Fatal(err)
	}
	if err := storage.CreateAuthSession(ctx, AuthSession{TokenHash: []byte{1}, AdminID: 1, CreatedAt: now, LastSeen: now, ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := storage.SetOnboardingComplete(ctx, now); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.db.ExecContext(ctx, `INSERT INTO interface_samples (device_id, ts, interface_name, rx_bps, tx_bps) VALUES ('edge', 1, 'ether1', 1, 2)`); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.PolicyRepository().SaveEgress(ctx, policyv2.Egress{ID: "owner-egress", Name: "Owner WAN", ListMode: policyv2.ListModeShared}); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.PolicyRepository().SaveSource(ctx, policyv2.Source{ID: "owner-source", EgressID: "owner-egress", Type: "manual", Name: "Owner source", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	device, err := storage.OpenDevice("edge")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := device.PolicyRepository().SaveEgress(ctx, policyv2.Egress{ID: "edge-egress", Name: "Edge WAN", ListMode: policyv2.ListModeShared}); err != nil {
		t.Fatal(err)
	}
	if _, err := device.PolicyRepository().SaveSource(ctx, policyv2.Source{ID: "edge-source", EgressID: "edge-egress", Type: "manual", Name: "Edge source", Enabled: true}); err != nil {
		t.Fatal(err)
	}

	if err := storage.ResetAll(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.Admin(ctx); !errors.Is(err, ErrAdminNotFound) {
		t.Fatalf("administrator survived reset: %v", err)
	}
	if _, err := storage.AuthSession(ctx, []byte{1}); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("session survived reset: %v", err)
	}
	if complete, err := storage.OnboardingComplete(ctx); err != nil || complete {
		t.Fatalf("onboarding state survived reset: complete=%v err=%v", complete, err)
	}
	var samples int
	if err := storage.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM interface_samples`).Scan(&samples); err != nil || samples != 0 {
		t.Fatalf("monitoring data survived reset: count=%d err=%v", samples, err)
	}
	for name, repository := range map[string]*PolicyRepository{"owner": storage.PolicyRepository(), "edge": device.PolicyRepository()} {
		egresses, err := repository.ListEgresses(ctx)
		if err != nil {
			t.Fatalf("list %s egresses after reset: %v", name, err)
		}
		sources, err := repository.ListSources(ctx, "")
		if err != nil {
			t.Fatalf("list %s sources after reset: %v", name, err)
		}
		state, err := repository.GetDeviceState(ctx)
		if err != nil {
			t.Fatalf("get %s policy state after reset: %v", name, err)
		}
		if len(egresses) != 0 || len(sources) != 0 || state.DesiredRevision != 0 || state.AppliedRevision != 0 {
			t.Fatalf("policy data survived %s reset: egresses=%#v sources=%#v state=%#v", name, egresses, sources, state)
		}
	}
}

func TestManagerInstanceIDIsStableAndConcurrencySafe(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()

	const callers = 32
	ids := make(chan string, callers)
	errs := make(chan error, callers)
	for range callers {
		go func() {
			id, err := storage.ManagerInstanceID(context.Background())
			if err != nil {
				errs <- err
				return
			}
			ids <- id
		}()
	}
	for range callers {
		select {
		case err := <-errs:
			t.Fatal(err)
		case id := <-ids:
			if id == "" {
				t.Fatal("manager instance ID is empty")
			}
			if len(id) != 36 {
				t.Fatalf("manager instance ID is not a UUID: %q", id)
			}
		}
	}

	first, err := storage.ManagerInstanceID(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := Open(storage.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	second, err := restarted.ManagerInstanceID(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("manager instance ID changed after reopen: first=%q second=%q", first, second)
	}
}
