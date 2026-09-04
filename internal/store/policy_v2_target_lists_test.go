package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"rosboard/internal/accesscontrol"
	"rosboard/internal/policyv2"
)

func TestPolicyV2TargetListPreservesSourceIdentityAndLegacyRoutingAssociation(t *testing.T) {
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

	nextRun := time.Unix(500, 0).UTC()
	_, err = repository.SaveSource(ctx, policyv2.Source{
		ID:                "domain-target",
		EgressID:          "wan-a",
		Type:              policyv2.TargetSourceTypeURL,
		Kind:              policyv2.KindDomain,
		Name:              "Original domains",
		URL:               "https://example.test/domains.yaml",
		Schedule:          "1h",
		Enabled:           true,
		ActiveVersionID:   "version-active",
		PendingVersionID:  "version-pending",
		LastGoodVersionID: "version-good",
		ETag:              "etag-1",
		LastModified:      "Wed, 21 Oct 2015 07:28:00 GMT",
		NextRunAt:         nextRun,
	})
	if err != nil {
		t.Fatal(err)
	}
	insertTargetVersion(t, storage, "version-active", "domain-target", "active-sha", "active", 101)
	insertTargetVersion(t, storage, "version-pending", "domain-target", "pending-sha", "pending", 102)
	if _, err := storage.db.Exec(`INSERT INTO policy_v2_source_rules (version_id, rule_type, domain) VALUES (?, ?, ?), (?, ?, ?)`, "version-active", "DOMAIN", "active.example", "version-pending", "DOMAIN-SUFFIX", "pending.example"); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.db.Exec(`UPDATE policy_v2_sources SET revision = 7 WHERE id = ?`, "domain-target"); err != nil {
		t.Fatal(err)
	}

	target, err := repository.GetTargetList(ctx, "domain-target")
	if err != nil {
		t.Fatal(err)
	}
	if target.Kind != policyv2.KindDomain || target.SourceType != policyv2.TargetSourceTypeURL || target.Revision != 7 {
		t.Fatalf("unexpected canonical target: %#v", target)
	}
	if target.ActiveVersionID != "version-active" || target.PendingVersionID != "version-pending" || target.LastGoodVersionID != "version-good" || target.ETag != "etag-1" || target.LastModified == "" || !target.NextRunAt.Equal(nextRun) {
		t.Fatalf("target refresh/version state was not preserved: %#v", target)
	}
	if len(target.Versions) != 2 || target.Versions[0].TargetListID != target.ID {
		t.Fatalf("target version identity was not preserved: %#v", target.Versions)
	}

	target.Name = "Updated domains"
	saved, err := repository.SaveTargetList(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	if saved.ID != target.ID || saved.Revision != 8 || saved.Name != "Updated domains" {
		t.Fatalf("canonical save changed identity unexpectedly: %#v", saved)
	}
	if saved.ActiveVersionID != "version-active" || saved.PendingVersionID != "version-pending" || saved.LastGoodVersionID != "version-good" || len(saved.Versions) != 2 {
		t.Fatalf("canonical save lost version state: %#v", saved)
	}
	var egressID string
	if err := storage.db.QueryRow(`SELECT egress_id FROM policy_v2_sources WHERE id = ?`, target.ID).Scan(&egressID); err != nil {
		t.Fatal(err)
	}
	if egressID != "wan-a" {
		t.Fatalf("canonical save changed temporary legacy routing association to %q", egressID)
	}
	encoded, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "egressId") {
		t.Fatalf("canonical target response exposed egress ownership: %s", encoded)
	}
}

func TestPolicyV2TargetListSupportsDomainAndIPKinds(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	repository := storage.PolicyRepository()
	ctx := context.Background()
	for _, kind := range []string{policyv2.KindDomain, policyv2.KindIP} {
		target, err := repository.SaveTargetList(ctx, policyv2.TargetList{
			ID:         kind + "-target",
			Name:       kind + " target",
			Kind:       kind,
			SourceType: policyv2.TargetSourceTypeManual,
			Enabled:    true,
		})
		if err != nil {
			t.Fatalf("save %s target: %v", kind, err)
		}
		if target.Kind != kind || target.SourceType != policyv2.TargetSourceTypeManual {
			t.Fatalf("saved %s target = %#v", kind, target)
		}
	}
	targets, err := repository.ListTargetLists(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 2 || targets[0].Kind != policyv2.KindDomain || targets[1].Kind != policyv2.KindIP {
		t.Fatalf("target list kinds/order = %#v", targets)
	}
}

func TestPolicyV2TargetListUsesPendingCountsBeforeFirstApply(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	repository := storage.PolicyRepository()
	ctx := context.Background()

	target, err := repository.SaveTargetList(ctx, policyv2.TargetList{
		ID:         "standby-target",
		Name:       "Standby target",
		Kind:       policyv2.KindDomain,
		SourceType: policyv2.TargetSourceTypeUpload,
		Enabled:    true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.SavePendingTargetListVersion(ctx, policyv2.TargetListVersion{
		ID:             "standby-version",
		TargetListID:   target.ID,
		SHA256:         "standby-sha",
		CompressedYAML: []byte("payload"),
		State:          "pending",
		Counts:         map[string]int{"valid": 3},
	}, []policyv2.TargetListRule{{RuleType: "DOMAIN-SUFFIX", Domain: "example.com"}, {RuleType: "DOMAIN", Domain: "one.example"}, {RuleType: "DOMAIN", Domain: "two.example"}}); err != nil {
		t.Fatal(err)
	}

	loaded, err := repository.GetTargetList(ctx, target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ActiveVersionID != "" || loaded.PendingVersionID != "standby-version" || loaded.Counts["valid"] != 3 {
		t.Fatalf("pending-only target state = %#v", loaded)
	}
	listed, err := repository.ListTargetLists(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].Counts["valid"] != 3 {
		t.Fatalf("pending-only target list counts = %#v", listed)
	}
}

func TestPolicyV2TargetListRejectsInvalidCanonicalValues(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	repository := storage.PolicyRepository()
	ctx := context.Background()

	if _, err := repository.SaveTargetList(ctx, policyv2.TargetList{
		ID: "invalid-kind", Name: "Invalid kind", Kind: "mixed", SourceType: policyv2.TargetSourceTypeManual,
	}); !errors.Is(err, policyv2.ErrInvalidTargetListKind) {
		t.Fatalf("invalid kind error = %v, want ErrInvalidTargetListKind", err)
	}
	if _, err := repository.SaveTargetList(ctx, policyv2.TargetList{
		ID: "invalid-source-type", Name: "Invalid source type", Kind: policyv2.KindDomain, SourceType: "bar",
	}); !errors.Is(err, policyv2.ErrInvalidTargetListSourceType) {
		t.Fatalf("invalid source type error = %v, want ErrInvalidTargetListSourceType", err)
	}
	if _, err := repository.SaveTargetList(ctx, policyv2.TargetList{
		ID: "unsupported-preset", Name: "Unsupported preset", Kind: policyv2.KindDomain, SourceType: policyv2.TargetSourceTypePreset,
	}); err == nil || !strings.Contains(err.Error(), "presetId") {
		t.Fatalf("preset without identity error = %v, want presetId validation", err)
	}

	var rows int
	if err := storage.db.QueryRow(`SELECT count(*) FROM policy_v2_sources WHERE id IN (?, ?, ?)`, "invalid-kind", "invalid-source-type", "unsupported-preset").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("invalid canonical targets were persisted: %d", rows)
	}
}

func TestPolicyV2TargetListRejectsKindAndSourceTypeChanges(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	repository := storage.PolicyRepository()
	ctx := context.Background()
	target, err := repository.SaveTargetList(ctx, policyv2.TargetList{
		ID: "immutable-target", Name: "Immutable target", Kind: policyv2.KindDomain, SourceType: policyv2.TargetSourceTypeManual,
	})
	if err != nil {
		t.Fatal(err)
	}

	target.Kind = policyv2.KindIP
	if _, err := repository.SaveTargetList(ctx, target); !errors.Is(err, policyv2.ErrTargetListKindImmutable) {
		t.Fatalf("kind change error = %v, want ErrTargetListKindImmutable", err)
	}
	target.Kind = policyv2.KindDomain
	target.SourceType = policyv2.TargetSourceTypeUpload
	if _, err := repository.SaveTargetList(ctx, target); !errors.Is(err, policyv2.ErrTargetListTypeImmutable) {
		t.Fatalf("source type change error = %v, want ErrTargetListTypeImmutable", err)
	}
}

func TestPolicyV2TargetListReopenPreservesDomainAndIPState(t *testing.T) {
	dataDir := t.TempDir()
	storage, err := Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	repository := storage.PolicyRepository()
	ctx := context.Background()
	if _, err := repository.SaveEgress(ctx, policyv2.Egress{ID: "wan-a", Name: "WAN A", ListMode: policyv2.ListModeShared, ListName: "proxy-a"}); err != nil {
		t.Fatal(err)
	}

	fixtures := []struct {
		id         string
		kind       string
		sourceType string
		schedule   string
		url        string
		etag       string
		lastMod    string
		activeID   string
		pendingID  string
		nextRunAt  time.Time
		rules      []policyv2.SourceRule
	}{
		{
			id: "reopen-domain", kind: policyv2.KindDomain, sourceType: policyv2.TargetSourceTypeURL, schedule: "6h",
			url: "https://example.test/domains.yaml", etag: "domain-etag", lastMod: "Wed, 21 Oct 2015 07:28:00 GMT",
			activeID: "domain-active", pendingID: "domain-pending", nextRunAt: time.Unix(2000, 0).UTC(),
			rules: []policyv2.SourceRule{{RuleType: "DOMAIN", Domain: "domain.example"}, {RuleType: "DOMAIN-SUFFIX", Domain: "suffix.example"}},
		},
		{
			id: "reopen-ip", kind: policyv2.KindIP, sourceType: policyv2.TargetSourceTypeUpload, schedule: "manual",
			activeID: "ip-active", pendingID: "ip-pending", nextRunAt: time.Time{},
			rules: []policyv2.SourceRule{{RuleType: "IP-CIDR", Domain: "192.0.2.0/24"}, {RuleType: "IP-CIDR6", Domain: "2001:db8::/32"}},
		},
	}
	for _, fixture := range fixtures {
		if _, err := repository.SaveSource(ctx, policyv2.Source{
			ID: fixture.id, EgressID: "wan-a", Type: fixture.sourceType, Kind: fixture.kind, Name: fixture.id,
			URL: fixture.url, Schedule: fixture.schedule, Enabled: true,
			ActiveVersionID: fixture.activeID, PendingVersionID: fixture.pendingID, LastGoodVersionID: fixture.activeID,
			ETag: fixture.etag, LastModified: fixture.lastMod, NextRunAt: fixture.nextRunAt,
		}); err != nil {
			t.Fatal(err)
		}
		insertTargetVersionMetadata(t, storage, fixture.activeID, fixture.id, fixture.kind+"-active-sha", "active", "", 200, map[string]int{"valid": 2, "ignored": 1}, map[string]any{"added": 2}, time.Unix(2100, 0).UnixNano())
		insertTargetVersionMetadata(t, storage, fixture.pendingID, fixture.id, fixture.kind+"-pending-sha", "pending", "parser warning", 422, map[string]int{"valid": 2, "ignored": 3}, map[string]any{"added": 1, "removed": 1}, time.Unix(2200, 0).UnixNano())
		for _, rule := range fixture.rules {
			if _, err := storage.db.Exec(`INSERT INTO policy_v2_source_rules (version_id, rule_type, domain) VALUES (?, ?, ?)`, fixture.pendingID, rule.RuleType, rule.Domain); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := storage.db.Exec(`UPDATE policy_v2_sources SET revision = 7 WHERE id = ?`, fixture.id); err != nil {
			t.Fatal(err)
		}
	}
	if err := storage.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	reopenedRepository := reopened.PolicyRepository()
	routingRules, err := reopenedRepository.ListRoutingRules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(routingRules) != 1 || routingRules[0].EgressID != "wan-a" || len(routingRules[0].TargetListIDs) != len(fixtures) {
		t.Fatalf("reopened routing authority changed: %#v", routingRules)
	}
	targets, err := reopenedRepository.ListTargetLists(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != len(fixtures) {
		t.Fatalf("reopened target count = %d, want %d", len(targets), len(fixtures))
	}
	for _, fixture := range fixtures {
		target, err := reopenedRepository.GetTargetList(ctx, fixture.id)
		if err != nil {
			t.Fatal(err)
		}
		if target.ID != fixture.id || target.Kind != fixture.kind || target.SourceType != fixture.sourceType || target.Revision != 7 || target.ActiveVersionID != fixture.activeID || target.PendingVersionID != fixture.pendingID || target.LastGoodVersionID != fixture.activeID || target.URL != fixture.url || target.ETag != fixture.etag || target.LastModified != fixture.lastMod || !target.NextRunAt.Equal(fixture.nextRunAt) || target.Counts["valid"] != 2 || target.Counts["ignored"] != 1 {
			t.Fatalf("reopened target state changed: %#v", target)
		}
		if len(target.Versions) != 2 {
			t.Fatalf("reopened target versions = %#v", target.Versions)
		}
		var pending *policyv2.TargetListVersion
		for index := range target.Versions {
			if target.Versions[index].ID == fixture.pendingID {
				pending = &target.Versions[index]
				break
			}
		}
		if pending == nil || pending.TargetListID != fixture.id || pending.Error != "parser warning" || pending.HTTPStatus != 422 || pending.Counts["valid"] != 2 || pending.Counts["ignored"] != 3 || pending.Diff["added"] != float64(1) || pending.Diff["removed"] != float64(1) || !pending.CreatedAt.Equal(time.Unix(2200, 0).UTC()) {
			t.Fatalf("reopened version metadata changed: %#v", pending)
		}
		rules, hasNext, err := reopenedRepository.ListTargetListRules(ctx, fixture.pendingID, policyv2.RuleQuery{Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		if hasNext || len(rules) != len(fixture.rules) {
			t.Fatalf("reopened %s rules = %#v hasNext=%v", fixture.id, rules, hasNext)
		}
		for index, rule := range rules {
			if rule.RuleType != fixture.rules[index].RuleType || rule.Domain != fixture.rules[index].Domain {
				t.Fatalf("reopened %s rule %d changed: %#v", fixture.id, index, rule)
			}
		}
		var egressID string
		if err := reopened.db.QueryRow(`SELECT egress_id FROM policy_v2_sources WHERE id = ?`, fixture.id).Scan(&egressID); err != nil {
			t.Fatal(err)
		}
		if egressID != "" {
			t.Fatalf("reopened legacy egress association was not retired = %q", egressID)
		}
		encoded, err := json.Marshal(target)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(encoded), "egressId") {
			t.Fatalf("reopened canonical target exposed egress ownership: %s", encoded)
		}
	}
}

func TestPolicyV2TargetListRefreshPreservesVersionAndHTTPState(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	repository := storage.PolicyRepository()
	ctx := context.Background()
	target, err := repository.SaveTargetList(ctx, policyv2.TargetList{
		ID:         "url-target",
		Name:       "URL target",
		Kind:       policyv2.KindDomain,
		SourceType: policyv2.TargetSourceTypeURL,
		URL:        "https://example.test/rules.yaml",
		Schedule:   "1h",
		Enabled:    true,
	})
	if err != nil {
		t.Fatal(err)
	}
	nextRun := time.Unix(900, 0).UTC()
	if err := repository.SaveTargetListRefresh(ctx, target, policyv2.TargetListRefresh{
		ETag:         "etag-2",
		LastModified: "Thu, 22 Oct 2015 07:28:00 GMT",
		Version: &policyv2.TargetListVersion{
			ID:             "version-refresh",
			TargetListID:   target.ID,
			SHA256:         "sha-refresh",
			CompressedYAML: []byte("payload"),
			State:          "pending",
			Counts:         map[string]int{"valid": 1},
			Diff:           map[string]any{"added": 1},
		},
		Rules: []policyv2.TargetListRule{{RuleType: "DOMAIN-SUFFIX", Domain: "refresh.example"}},
	}, nextRun); err != nil {
		t.Fatal(err)
	}
	loaded, err := repository.GetTargetList(ctx, target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.PendingVersionID != "version-refresh" || loaded.ETag != "etag-2" || loaded.LastModified != "Thu, 22 Oct 2015 07:28:00 GMT" || !loaded.NextRunAt.Equal(nextRun) {
		t.Fatalf("target refresh state was not preserved: %#v", loaded)
	}
	if len(loaded.Versions) != 1 || loaded.Versions[0].TargetListID != target.ID || loaded.Versions[0].SHA256 != "sha-refresh" {
		t.Fatalf("target refresh version was not preserved: %#v", loaded.Versions)
	}
	rules, hasNext, err := repository.ListTargetListRules(ctx, "version-refresh", policyv2.RuleQuery{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if hasNext || len(rules) != 1 || rules[0].RuleType != "DOMAIN-SUFFIX" || rules[0].Domain != "refresh.example" {
		t.Fatalf("target refresh rules were not preserved: %#v hasNext=%v", rules, hasNext)
	}
}

func TestPolicyV2TargetListDeleteHonorsAccessRuleSourceReferences(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	repository := storage.PolicyRepository()
	accessRepository := storage.AccessRepository()
	ctx := context.Background()
	target, err := repository.SaveTargetList(ctx, policyv2.TargetList{
		ID:         "shared-target",
		Name:       "Shared target",
		Kind:       policyv2.KindIP,
		SourceType: policyv2.TargetSourceTypeManual,
		Enabled:    true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := accessRepository.SaveRule(ctx, accesscontrol.AccessRule{
		ID:          "access-rule",
		Name:        "Block shared target",
		TargetScope: accesscontrol.TargetScopeSources,
		SourceIDs:   []string{target.ID},
		Enabled:     true,
	}, []accesscontrol.RuleMember{{
		TerminalID: "addr:192.0.2.10",
		Binding:    accesscontrol.BindingFixed,
		PinnedIPv4: []string{"192.0.2.10"},
	}}, "test"); err != nil {
		t.Fatal(err)
	}
	if err := repository.DeleteTargetList(ctx, target.ID, target.Revision); !errors.Is(err, policyv2.ErrTargetListInUse) {
		t.Fatalf("delete referenced target error = %v, want target-in-use", err)
	}
}

func TestPolicyV2TargetListDeleteRemovesAppliedUnreferencedTarget(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	repository := storage.PolicyRepository()
	ctx := context.Background()
	target, err := repository.SaveTargetList(ctx, policyv2.TargetList{
		ID: "applied-unreferenced-target", Name: "Applied unreferenced target", Kind: policyv2.KindDomain,
		SourceType: policyv2.TargetSourceTypeUpload, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	versionID := target.ID + ":v1"
	if err := repository.SavePendingTargetListVersion(ctx, policyv2.TargetListVersion{
		ID: versionID, TargetListID: target.ID, SHA256: versionID, State: "pending", CompressedYAML: []byte(versionID),
	}, []policyv2.TargetListRule{{VersionID: versionID, RuleType: "DOMAIN-SUFFIX", Domain: "uploaded.example"}}); err != nil {
		t.Fatal(err)
	}
	state, err := repository.GetDeviceState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.CommitRoutingApply(ctx, state.DesiredRevision, "applied-unreferenced-hash", policyv2.ApplyJob{ID: "applied-unreferenced-job", PlanID: "applied-unreferenced-plan"}, []policyv2.TargetVersionPromotion{{TargetListID: target.ID, VersionID: versionID}}); err != nil {
		t.Fatal(err)
	}
	loaded, err := repository.GetTargetList(ctx, target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Applied || loaded.ActiveVersionID != versionID || loaded.PendingVersionID != "" {
		t.Fatalf("target was not prepared as applied: %#v", loaded)
	}

	if err := repository.DeleteTargetList(ctx, target.ID, loaded.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.GetTargetList(ctx, target.ID); !errors.Is(err, policyv2.ErrTargetListNotFound) {
		t.Fatalf("applied unreferenced target still exists: %v", err)
	}
	var versionCount, ruleCount int
	if err := storage.db.QueryRow(`SELECT count(*) FROM policy_v2_source_versions WHERE source_id = ?`, target.ID).Scan(&versionCount); err != nil {
		t.Fatal(err)
	}
	if err := storage.db.QueryRow(`SELECT count(*) FROM policy_v2_source_rules WHERE version_id = ?`, versionID).Scan(&ruleCount); err != nil {
		t.Fatal(err)
	}
	if versionCount != 0 || ruleCount != 0 {
		t.Fatalf("target content was not cascaded: versions=%d rules=%d", versionCount, ruleCount)
	}
}

func TestPolicyV2TargetListDeleteKeepsLegacyAppliedTargetPending(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	repository := storage.PolicyRepository()
	ctx := context.Background()
	if _, err := repository.SaveEgress(ctx, policyv2.Egress{ID: "legacy-egress", Name: "Legacy egress", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	source, err := repository.SaveSource(ctx, policyv2.Source{
		ID: "legacy-applied-target", EgressID: "legacy-egress", Type: policyv2.TargetSourceTypeUpload,
		Kind: policyv2.KindDomain, Name: "Legacy applied target", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	versionID := source.ID + ":v1"
	if err := repository.SavePendingSourceVersion(ctx, policyv2.SourceVersion{
		ID: versionID, SourceID: source.ID, SHA256: versionID, State: "pending", CompressedYAML: []byte(versionID),
	}, []policyv2.SourceRule{{RuleType: "DOMAIN-SUFFIX", Domain: "legacy.example"}}); err != nil {
		t.Fatal(err)
	}
	state, err := repository.GetDeviceState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.CommitRoutingApply(ctx, state.DesiredRevision, "legacy-applied-hash", policyv2.ApplyJob{ID: "legacy-applied-job", PlanID: "legacy-applied-plan"}, []policyv2.TargetVersionPromotion{{TargetListID: source.ID, VersionID: versionID}}); err != nil {
		t.Fatal(err)
	}
	loaded, err := repository.GetSource(ctx, source.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.DeleteSource(ctx, loaded.ID, loaded.Revision); err != nil {
		t.Fatal(err)
	}
	pending, err := repository.GetSource(ctx, source.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !pending.PendingDeletion || pending.EgressID != "" {
		t.Fatalf("legacy applied target did not retain staged cleanup: %#v", pending)
	}
}

func TestPolicyV2TargetListRevisionInvalidationFollowsConsumerDomain(t *testing.T) {
	tests := []struct {
		name       string
		routing    bool
		access     bool
		wantDomain policyv2.TargetConsumerDomains
	}{
		{name: "unreferenced"},
		{name: "routing-only", routing: true, wantDomain: policyv2.TargetConsumerDomains{Routing: true}},
		{name: "access-only", access: true, wantDomain: policyv2.TargetConsumerDomains{Access: true}},
		{name: "shared", routing: true, access: true, wantDomain: policyv2.TargetConsumerDomains{Routing: true, Access: true}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			storage, err := Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer storage.Close()
			ctx := context.Background()
			policyRepository := storage.PolicyRepository()
			accessRepository := storage.AccessRepository()
			target, err := policyRepository.SaveTargetList(ctx, policyv2.TargetList{
				ID: test.name + "-target", Name: test.name + " target", Kind: policyv2.KindIP,
				SourceType: policyv2.TargetSourceTypeManual, Enabled: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			if test.routing {
				if _, err := policyRepository.SaveEgress(ctx, policyv2.Egress{ID: test.name + "-egress", Name: test.name + " egress", Enabled: true}); err != nil {
					t.Fatal(err)
				}
				if _, err := policyRepository.SaveRoutingRule(ctx, policyv2.RoutingRule{
					ID: test.name + "-routing-rule", Name: test.name + " routing", Subject: policyv2.Subject{Mode: policyv2.SubjectModeSelected, Prefixes: []string{"10.0.0.20/32"}},
					TargetListIDs: []string{target.ID}, EgressID: test.name + "-egress", Enabled: true,
				}); err != nil {
					t.Fatal(err)
				}
			}
			if test.access {
				if _, err := accessRepository.SaveRule(ctx, accesscontrol.AccessRule{
					ID: test.name + "-access-rule", Name: test.name + " access", TargetScope: accesscontrol.TargetScopeTargets,
					TargetListIDs: []string{target.ID}, Enabled: true,
				}, []accesscontrol.RuleMember{{
					RuleID: test.name + "-access-rule", TerminalID: "terminal-a", Binding: accesscontrol.BindingFixed, PinnedIPv4: []string{"10.0.0.20"},
				}}, "test"); err != nil {
					t.Fatal(err)
				}
			}
			gotDomain, err := policyRepository.TargetConsumerDomains(ctx, target.ID)
			if err != nil {
				t.Fatal(err)
			}
			if gotDomain != test.wantDomain {
				t.Fatalf("target consumer domains = %#v, want %#v", gotDomain, test.wantDomain)
			}
			beforePolicy, err := policyRepository.GetDeviceState(ctx)
			if err != nil {
				t.Fatal(err)
			}
			beforeAccess, err := accessRepository.GetState(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if err := policyRepository.SavePendingSourceVersion(ctx, policyv2.SourceVersion{
				ID: test.name + "-version", SourceID: target.ID, SHA256: test.name, CompressedYAML: []byte(test.name), State: "pending",
			}, []policyv2.SourceRule{{RuleType: "IP-CIDR", Domain: "203.0.113.0/24"}}); err != nil {
				t.Fatal(err)
			}
			afterPolicy, err := policyRepository.GetDeviceState(ctx)
			if err != nil {
				t.Fatal(err)
			}
			afterAccess, err := accessRepository.GetState(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if afterPolicy.DesiredRevision != beforePolicy.DesiredRevision+boolToInt64(test.wantDomain.Routing) {
				t.Fatalf("policy desired revision changed unexpectedly: before=%d after=%d domain=%#v", beforePolicy.DesiredRevision, afterPolicy.DesiredRevision, test.wantDomain)
			}
			if afterAccess.DesiredRevision != beforeAccess.DesiredRevision+boolToInt64(test.wantDomain.Access) {
				t.Fatalf("access desired revision changed unexpectedly: before=%d after=%d domain=%#v", beforeAccess.DesiredRevision, afterAccess.DesiredRevision, test.wantDomain)
			}
		})
	}
}

func boolToInt64(value bool) int64 {
	if value {
		return 1
	}
	return 0
}

func TestPolicyV2PresetTargetListCannotBeDeletedThroughLegacySourcePath(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	repository := storage.PolicyRepository()
	target, err := repository.SaveTargetList(context.Background(), policyv2.TargetList{
		ID: "preset-youtube-domain", Name: "YouTube · Domain", Kind: policyv2.KindDomain,
		SourceType: policyv2.TargetSourceTypePreset, PresetID: "youtube", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.DeleteSource(context.Background(), target.ID, target.Revision); !errors.Is(err, policyv2.ErrPresetTargetListProtected) {
		t.Fatalf("legacy source delete error=%v, want preset protection", err)
	}
}

func insertTargetVersion(t *testing.T, storage *Store, id, targetID, sha, state string, createdAt int64) {
	t.Helper()
	insertTargetVersionMetadata(t, storage, id, targetID, sha, state, "", 200, map[string]int{"valid": 1}, map[string]any{"added": 1}, createdAt)
}

func insertTargetVersionMetadata(t *testing.T, storage *Store, id, targetID, sha, state, versionError string, httpStatus int, countsValue map[string]int, diffValue map[string]any, createdAt int64) {
	t.Helper()
	counts, err := json.Marshal(countsValue)
	if err != nil {
		t.Fatal(err)
	}
	diff, err := json.Marshal(diffValue)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := storage.db.Exec(`INSERT INTO policy_v2_source_versions (id, source_id, sha256, compressed_yaml, state, error, http_status, counts_json, diff_json, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, id, targetID, sha, []byte("payload"), state, versionError, httpStatus, string(counts), string(diff), createdAt); err != nil {
		t.Fatal(err)
	}
}
