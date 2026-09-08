package store

import (
	"context"
	"errors"
	"testing"

	"rosboard/internal/policyv2"
)

func TestPolicyV2RepositoryEgressRevisionAndFamilies(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	repository := storage.PolicyRepository()
	ctx := context.Background()

	created, err := repository.SaveEgress(ctx, policyv2.Egress{
		ID: "wan-a", Name: "WAN A", Priority: 10, ListMode: policyv2.ListModeShared,
		ListName: "proxy-a", Enabled: true,
		Families: []policyv2.EgressFamily{{Family: policyv2.FamilyIPv4, Enabled: true, WANInterface: "ether1", Gateway: "192.0.2.1", RouteTable: "proxy-a", NATMode: "masquerade"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Revision != 1 {
		t.Fatalf("created revision = %d, want 1", created.Revision)
	}
	state, err := repository.GetDeviceState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if state.DesiredRevision != 1 || state.Applied() {
		t.Fatalf("unexpected state after create: %#v", state)
	}

	loaded, err := repository.GetEgress(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Families) != 1 || loaded.Families[0].NATMode != "masquerade" {
		t.Fatalf("families did not round-trip: %#v", loaded.Families)
	}
	stale := loaded
	stale.Revision = 0
	if _, err := repository.SaveEgress(ctx, stale); !errors.Is(err, policyv2.ErrRevisionStale) {
		t.Fatalf("stale update error = %v", err)
	}
	loaded.Name = "WAN A updated"
	updated, err := repository.SaveEgress(ctx, loaded)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Revision != 2 {
		t.Fatalf("updated revision = %d, want 2", updated.Revision)
	}
}

func TestPolicyV2RepositoryPendingSourceVersionAndRuleQuery(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	repository := storage.PolicyRepository()
	ctx := context.Background()
	if _, err := repository.SaveEgress(ctx, policyv2.Egress{ID: "wan-a", Name: "WAN A", ListMode: policyv2.ListModeShared, ListName: "proxy-a"}); err != nil {
		t.Fatal(err)
	}
	source, err := repository.SaveSource(ctx, policyv2.Source{ID: "source-a", EgressID: "wan-a", Type: "upload", Name: "Rules", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	version := policyv2.SourceVersion{ID: "version-a", SourceID: source.ID, SHA256: "abc", CompressedYAML: []byte("gzip"), Counts: map[string]int{"valid": 3}}
	rules := []policyv2.SourceRule{
		{RuleType: "DOMAIN", Domain: "api.example.com"},
		{RuleType: "DOMAIN-SUFFIX", Domain: "example.com"},
		{RuleType: "DOMAIN-SUFFIX", Domain: "openai.com"},
	}
	if err := repository.SavePendingSourceVersion(ctx, version, rules); err != nil {
		t.Fatal(err)
	}
	loaded, err := repository.GetSource(ctx, source.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.PendingVersionID != version.ID || loaded.Revision != 2 || len(loaded.Versions) != 1 || loaded.Versions[0].State != "pending" {
		t.Fatalf("pending version did not round-trip: %#v", loaded)
	}
	page, hasNext, err := repository.ListSourceRules(ctx, version.ID, policyv2.RuleQuery{RuleType: "DOMAIN-SUFFIX", Query: "example", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if hasNext || len(page) != 1 || page[0].Domain != "example.com" {
		t.Fatalf("unexpected filtered rule page: %#v hasNext=%v", page, hasNext)
	}
}

func TestPolicyV2RepositoryAppliedDeleteCreatesTombstoneAndDetachesSources(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	repository := storage.PolicyRepository()
	ctx := context.Background()
	egress, err := repository.SaveEgress(ctx, policyv2.Egress{ID: "wan-a", Name: "WAN A", ListMode: policyv2.ListModeShared, ListName: "proxy-a"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.SaveSource(ctx, policyv2.Source{ID: "source-a", EgressID: egress.ID, Type: "upload", Name: "Rules"}); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.db.Exec(`UPDATE policy_v2_egresses SET applied = 1 WHERE id = ?`, egress.ID); err != nil {
		t.Fatal(err)
	}
	if err := repository.DeleteEgress(ctx, egress.ID, egress.Revision); err != nil {
		t.Fatal(err)
	}
	deleted, err := repository.GetEgress(ctx, egress.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !deleted.PendingDeletion || deleted.Enabled || deleted.Revision != egress.Revision+1 {
		t.Fatalf("applied egress was not tombstoned: %#v", deleted)
	}
	source, err := repository.GetSource(ctx, "source-a")
	if err != nil {
		t.Fatal(err)
	}
	if source.EgressID != "" || source.Revision != 2 {
		t.Fatalf("source was not detached: %#v", source)
	}
}

func TestPolicyV2RepositoryIsDeviceScoped(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	a, err := storage.OpenDevice("edge-a")
	if err != nil {
		t.Fatal(err)
	}
	b, err := storage.OpenDevice("edge-b")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := a.PolicyRepository().SaveEgress(ctx, policyv2.Egress{ID: "same", Name: "A", ListMode: policyv2.ListModeShared}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.PolicyRepository().SaveEgress(ctx, policyv2.Egress{ID: "same", Name: "B", ListMode: policyv2.ListModeShared}); err != nil {
		t.Fatal(err)
	}
	gotA, _ := a.PolicyRepository().GetEgress(ctx, "same")
	gotB, _ := b.PolicyRepository().GetEgress(ctx, "same")
	if gotA.Name != "A" || gotB.Name != "B" {
		t.Fatalf("device stores crossed: A=%#v B=%#v", gotA, gotB)
	}
}
