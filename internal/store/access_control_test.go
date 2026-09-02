package store

import (
	"context"
	"errors"
	"testing"

	"rosboard/internal/accesscontrol"
	"rosboard/internal/policyv2"
)

func accessSourcesRule(id string, sourceIDs ...string) accesscontrol.AccessRule {
	return accesscontrol.AccessRule{ID: id, Name: "规则 " + id, TargetScope: accesscontrol.TargetScopeSources, SourceIDs: sourceIDs, Enabled: true}
}

func accessFixedMember(ruleID, terminalID, ipv4 string) accesscontrol.RuleMember {
	return accesscontrol.RuleMember{RuleID: ruleID, TerminalID: terminalID, Binding: accesscontrol.BindingFixed, PinnedIPv4: []string{ipv4}}
}

func accessAutoMember(ruleID, terminalID string) accesscontrol.RuleMember {
	return accesscontrol.RuleMember{RuleID: ruleID, TerminalID: terminalID, Binding: accesscontrol.BindingAuto, AnchorMAC: "AA:BB:CC:DD:EE:FF"}
}

func seedAccessPolicySources(t *testing.T, repository *PolicyRepository, sourceIDs ...string) {
	t.Helper()
	for _, sourceID := range sourceIDs {
		if _, err := repository.SaveSource(context.Background(), policyv2.Source{ID: sourceID, Type: "manual", Kind: policyv2.KindIP, Name: sourceID, Enabled: true}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAccessRulesKeepLogicalIdentityAndMemberProjection(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	ctx := context.Background()
	repository := storage.AccessRepository()
	seedAccessPolicySources(t, storage.PolicyRepository(), "source-a", "source-b")

	rule, err := repository.SaveRule(ctx, accessSourcesRule("rule-a", "source-a", "source-b"), []accesscontrol.RuleMember{
		accessFixedMember("rule-a", "addr:10.0.0.20", "10.0.0.20"),
		accessAutoMember("rule-a", "mac:aa"),
	}, "tom")
	if err != nil {
		t.Fatal(err)
	}
	if rule.ID == "" || rule.Revision != 1 || len(rule.SourceIDs) != 2 {
		t.Fatalf("unexpected created rule: %#v", rule)
	}

	members, err := repository.ListMembers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 2 {
		t.Fatalf("expected two members, got %#v", members)
	}
	if err := repository.SaveMemberResolutions(ctx, rule.ID, "mac:aa", []string{"10.0.0.30"}, nil); err != nil {
		t.Fatal(err)
	}

	state, err := repository.GetState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rules, err := repository.ListRules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 || rules[0].Revision != 1 {
		t.Fatalf("unexpected rules: %#v", rules)
	}
	if state.DesiredRevision != 1 {
		t.Fatalf("save did not bump desired revision: %#v", state)
	}

	members, err = repository.ListMembers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, member := range members {
		if member.TerminalID != "mac:aa" {
			continue
		}
		if member.AnchorMAC != "AA:BB:CC:DD:EE:FF" {
			t.Fatalf("auto member MAC anchor was not persisted canonically: %#v", member)
		}
		if len(member.LastIPv4) != 1 || member.LastIPv4[0] != "10.0.0.30" {
			t.Fatalf("trusted projection was not recorded: %#v", member)
		}
		if len(member.PinnedIPv4) != 0 {
			t.Fatalf("auto member must not carry pinned addresses: %#v", member)
		}
	}

	if _, err := repository.SaveRule(ctx, accesscontrol.AccessRule{ID: rule.ID, Name: "改名", TargetScope: accesscontrol.TargetScopeSources, SourceIDs: rule.SourceIDs, Enabled: true, Revision: rule.Revision}, []accesscontrol.RuleMember{
		accessFixedMember(rule.ID, "addr:10.0.0.20", "10.0.0.20"),
		accessAutoMember(rule.ID, "mac:aa"),
	}, "tom"); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.SaveRule(ctx, accesscontrol.AccessRule{ID: rule.ID, Name: "过期", TargetScope: accesscontrol.TargetScopeSources, SourceIDs: rule.SourceIDs, Revision: rule.Revision}, []accesscontrol.RuleMember{
		accessFixedMember(rule.ID, "addr:10.0.0.20", "10.0.0.20"),
	}, "tom"); !errors.Is(err, accesscontrol.ErrRevisionStale) {
		t.Fatalf("stale revision must be rejected: %v", err)
	}
	if _, err := repository.SaveRule(ctx, accessSourcesRule("dup", "source-a"), []accesscontrol.RuleMember{
		accessFixedMember("dup", "t1", "10.0.0.1"), accessFixedMember("dup", "t1", "10.0.0.2"),
	}, "tom"); !errors.Is(err, accesscontrol.ErrMemberDuplicate) {
		t.Fatalf("duplicate member must be rejected: %v", err)
	}

	members, err = repository.ListMembers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, member := range members {
		if member.TerminalID == "mac:aa" && (len(member.LastIPv4) != 1 || member.LastIPv4[0] != "10.0.0.30") {
			t.Fatalf("edit must keep the member trusted projection: %#v", member)
		}
	}

	if err := repository.DeleteRule(ctx, rule.ID, rule.Revision+1, "tom"); err != nil {
		t.Fatal(err)
	}
	rules, _ = repository.ListRules(ctx)
	members, _ = repository.ListMembers(ctx)
	if len(rules) != 0 || len(members) != 0 {
		t.Fatalf("rule delete must clean rules and members: rules=%#v members=%#v", rules, members)
	}
}

func TestAccessAutoMemberAnchorCannotBeChangedSilently(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	ctx := context.Background()
	repository := storage.AccessRepository()
	seedAccessPolicySources(t, storage.PolicyRepository(), "source-a")
	rule, err := repository.SaveRule(ctx, accessSourcesRule("rule-a", "source-a"), []accesscontrol.RuleMember{accessAutoMember("rule-a", "mac:aa")}, "tom")
	if err != nil {
		t.Fatal(err)
	}
	changed := accessAutoMember("rule-a", "mac:aa")
	changed.AnchorMAC = "BA:BB:BB:BB:BB:BB"
	if _, err := repository.SaveRule(ctx, accesscontrol.AccessRule{ID: rule.ID, Name: rule.Name, TargetScope: rule.TargetScope, SourceIDs: rule.SourceIDs, Enabled: true, Revision: rule.Revision}, []accesscontrol.RuleMember{changed}, "tom"); !errors.Is(err, accesscontrol.ErrMemberAnchorChanged) {
		t.Fatalf("changing an auto-follow identity anchor must be rejected: %v", err)
	}
	members, err := repository.ListMembers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 1 || members[0].AnchorMAC != "AA:BB:CC:DD:EE:FF" {
		t.Fatalf("rejected anchor change must leave the original member intact: %#v", members)
	}
}

func TestAccessAutoMemberWithoutAnchorIsRejected(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	seedAccessPolicySources(t, storage.PolicyRepository(), "source-a")
	_, err = storage.AccessRepository().SaveRule(context.Background(), accessSourcesRule("rule-a", "source-a"), []accesscontrol.RuleMember{{RuleID: "rule-a", TerminalID: "mac:aa", Binding: accesscontrol.BindingAuto}}, "tom")
	if !errors.Is(err, accesscontrol.ErrMemberAnchorRequired) {
		t.Fatalf("new auto-follow member without an anchor must be rejected: %v", err)
	}
}

func TestAccessRuleCannotReferenceMissingSource(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()

	_, err = storage.AccessRepository().SaveRule(context.Background(), accessSourcesRule("rule-a", "missing"), []accesscontrol.RuleMember{
		accessFixedMember("rule-a", "t1", "10.0.0.20"),
	}, "tom")
	if !errors.Is(err, policyv2.ErrSourceNotFound) {
		t.Fatalf("access rule must reject a missing source: %v", err)
	}
}

func TestAccessRuleApplicationsRoundTripAndStayIndependentFromSources(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	ctx := context.Background()
	repository := storage.AccessRepository()
	rule, err := repository.SaveRule(ctx, accesscontrol.AccessRule{
		ID: "application-rule", Name: "应用规则", TargetScope: accesscontrol.TargetScopeApplications,
		ApplicationIDs: []string{" oaf:2002 ", "oaf:1001", "oaf:2002"}, Enabled: true,
	}, []accesscontrol.RuleMember{accessFixedMember("application-rule", "t1", "10.0.0.20")}, "tom")
	if err != nil {
		t.Fatal(err)
	}
	if len(rule.SourceIDs) != 0 || len(rule.ApplicationIDs) != 2 || rule.ApplicationIDs[0] != "oaf:1001" || rule.ApplicationIDs[1] != "oaf:2002" {
		t.Fatalf("application IDs were not normalized independently: %#v", rule)
	}

	rules, err := repository.ListRules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 || len(rules[0].SourceIDs) != 0 || len(rules[0].ApplicationIDs) != 2 || rules[0].ApplicationIDs[0] != "oaf:1001" || rules[0].ApplicationIDs[1] != "oaf:2002" {
		t.Fatalf("application relation did not round-trip in position order: %#v", rules)
	}
	var sourceRows, applicationRows int
	if err := storage.db.QueryRowContext(ctx, `SELECT count(*) FROM access_rule_sources WHERE rule_id = ?`, rule.ID).Scan(&sourceRows); err != nil {
		t.Fatal(err)
	}
	if err := storage.db.QueryRowContext(ctx, `SELECT count(*) FROM access_rule_applications WHERE rule_id = ?`, rule.ID).Scan(&applicationRows); err != nil {
		t.Fatal(err)
	}
	if sourceRows != 0 || applicationRows != 2 {
		t.Fatalf("application rule polluted the source relation: sources=%d applications=%d", sourceRows, applicationRows)
	}

	rule, err = repository.SaveRule(ctx, accesscontrol.AccessRule{
		ID: rule.ID, Name: rule.Name, TargetScope: accesscontrol.TargetScopeApplications,
		ApplicationIDs: []string{"oaf:3003"}, Enabled: true, Revision: rule.Revision,
	}, []accesscontrol.RuleMember{accessFixedMember(rule.ID, "t1", "10.0.0.20")}, "tom")
	if err != nil {
		t.Fatal(err)
	}
	if rule.Revision != 2 {
		t.Fatalf("application update did not advance revision: %#v", rule)
	}
	if err := storage.db.QueryRowContext(ctx, `SELECT count(*) FROM access_rule_applications WHERE rule_id = ? AND application_id = ?`, rule.ID, "oaf:1001").Scan(&applicationRows); err != nil {
		t.Fatal(err)
	}
	if applicationRows != 0 {
		t.Fatal("application relation replacement left the old ID")
	}
	if err := repository.DeleteRule(ctx, rule.ID, rule.Revision, "tom"); err != nil {
		t.Fatal(err)
	}
	if err := storage.db.QueryRowContext(ctx, `SELECT count(*) FROM access_rule_applications WHERE rule_id = ?`, rule.ID).Scan(&applicationRows); err != nil {
		t.Fatal(err)
	}
	if applicationRows != 0 {
		t.Fatal("deleting an access rule left application relation rows")
	}
}

func TestAccessBindingSwitchClearsAutoTrustedProjection(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	ctx := context.Background()
	repository := storage.AccessRepository()
	seedAccessPolicySources(t, storage.PolicyRepository(), "source-a")
	rule, err := repository.SaveRule(ctx, accessSourcesRule("rule-a", "source-a"), []accesscontrol.RuleMember{accessAutoMember("rule-a", "mac:aa")}, "tom")
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.SaveMemberResolutions(ctx, rule.ID, "mac:aa", []string{"10.0.0.20"}, []string{"fd00::20"}); err != nil {
		t.Fatal(err)
	}
	fixed := accessFixedMember(rule.ID, "mac:aa", "10.0.0.20")
	rule, err = repository.SaveRule(ctx, accesscontrol.AccessRule{ID: rule.ID, Name: rule.Name, TargetScope: rule.TargetScope, SourceIDs: rule.SourceIDs, Enabled: true, Revision: rule.Revision}, []accesscontrol.RuleMember{fixed}, "tom")
	if err != nil {
		t.Fatal(err)
	}
	auto := accessAutoMember(rule.ID, "mac:aa")
	if _, err := repository.SaveRule(ctx, accesscontrol.AccessRule{ID: rule.ID, Name: rule.Name, TargetScope: rule.TargetScope, SourceIDs: rule.SourceIDs, Enabled: true, Revision: rule.Revision}, []accesscontrol.RuleMember{auto}, "tom"); err != nil {
		t.Fatal(err)
	}
	members, err := repository.ListMembers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 1 || len(members[0].LastIPv4) != 0 || len(members[0].LastIPv6) != 0 {
		t.Fatalf("switching fixed/auto must not resurrect an old trusted projection: %#v", members)
	}
}

func TestMemberResolutionCommitsWithPolicyApplyAtomically(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	ctx := context.Background()
	accessRepository := storage.AccessRepository()
	policyRepository := storage.PolicyRepository()
	seedAccessPolicySources(t, policyRepository, "source-a")
	if _, err := accessRepository.SaveRule(ctx, accessSourcesRule("rule-a", "source-a"), []accesscontrol.RuleMember{accessAutoMember("rule-a", "mac:aa")}, "tom"); err != nil {
		t.Fatal(err)
	}
	accessState, err := accessRepository.GetState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	policyState, err := policyRepository.GetDeviceState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	resolution := accesscontrol.MemberResolution{RuleID: "rule-a", TerminalID: "mac:aa", AnchorMAC: "aa:bb:cc:dd:ee:ff", IPv4: []string{"10.0.0.20"}}
	if err := policyRepository.CommitApply(ctx, policyState.DesiredRevision, accessState.DesiredRevision, "hash", policyv2.ApplyJob{ID: "job", PlanID: "plan"}, []accesscontrol.MemberResolution{resolution}, true); err != nil {
		t.Fatal(err)
	}
	members, err := accessRepository.ListMembers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 1 || len(members[0].LastIPv4) != 1 || members[0].LastIPv4[0] != "10.0.0.20" {
		t.Fatalf("successful policy commit did not persist the trusted resolution: %#v", members)
	}

	if err := accessRepository.SaveMemberResolutions(ctx, "rule-a", "mac:aa", []string{"10.0.0.21"}, nil); err != nil {
		t.Fatalf("legacy direct resolution update should still work for an anchored member: %v", err)
	}
}

func TestLegacyAccessPolicySchemaIsReplacedOnce(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	ctx := context.Background()
	device, err := storage.OpenDevice("edge")
	if err != nil {
		t.Fatal(err)
	}
	// 模拟旧 MVP schema 残留：清掉迁移标记、手工建回 access_policies 并插入一行。
	if _, err := device.db.Exec(`DELETE FROM access_schema_meta`); err != nil {
		t.Fatal(err)
	}
	if _, err := device.db.Exec(`DROP TABLE IF EXISTS access_policies`); err != nil {
		t.Fatal(err)
	}
	if _, err := device.db.Exec(`CREATE TABLE access_policies (device_id TEXT NOT NULL, id TEXT NOT NULL, terminal_id TEXT NOT NULL, identity_mode TEXT NOT NULL, pinned_ipv4_json TEXT NOT NULL, pinned_ipv6_json TEXT NOT NULL, source_id TEXT NOT NULL, enabled INTEGER NOT NULL, revision INTEGER NOT NULL, applied INTEGER NOT NULL, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL, PRIMARY KEY (device_id, id))`); err != nil {
		t.Fatal(err)
	}
	if _, err := device.db.Exec(`INSERT INTO access_policies VALUES ('edge', 'legacy', 't', 'mac', '[]', '[]', 's', 1, 1, 0, 0, 0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := device.db.Exec(`DROP TABLE access_audit`); err != nil {
		t.Fatal(err)
	}
	if _, err := device.db.Exec(`CREATE TABLE access_audit (device_id TEXT NOT NULL, id INTEGER PRIMARY KEY AUTOINCREMENT, actor TEXT NOT NULL, action TEXT NOT NULL, rule_id TEXT NOT NULL, before_json TEXT NOT NULL, after_json TEXT NOT NULL, result TEXT NOT NULL, created_at INTEGER NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := device.db.Exec(`INSERT INTO access_audit (device_id, actor, action, rule_id, before_json, after_json, result, created_at) VALUES ('edge', 'legacy', 'save', 'legacy', '{}', '{}', 'success', 1)`); err != nil {
		t.Fatal(err)
	}
	if err := device.initAccessControlSchema(); err != nil {
		t.Fatal(err)
	}
	seedAccessPolicySources(t, device.PolicyRepository(), "source-a")
	var count int
	if err := device.db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'access_policies'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("legacy access_policies table must be dropped for the unreleased MVP schema")
	}
	var auditCount int
	if err := device.db.QueryRowContext(ctx, `SELECT count(*) FROM access_audit`).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount != 0 {
		t.Fatal("legacy access_audit rows must not survive the logical-rule migration")
	}
	if _, err := device.AccessRepository().SaveRule(ctx, accessSourcesRule("rule-a", "source-a"), []accesscontrol.RuleMember{accessFixedMember("rule-a", "t1", "10.0.0.20")}, "tom"); err != nil {
		t.Fatal(err)
	}
	// 再次初始化不能重复迁移或破坏新表。
	if err := device.initAccessControlSchema(); err != nil {
		t.Fatal(err)
	}
	rules, err := device.AccessRepository().ListRules(ctx)
	if err != nil || len(rules) != 1 {
		t.Fatalf("logical rules must survive re-init: rules=%#v err=%v", rules, err)
	}
}

func TestMarkedAccessSchemaDoesNotSilentlyRecreateMissingTables(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	device, err := storage.OpenDevice("edge")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := device.db.Exec(`DROP TABLE access_rule_members`); err != nil {
		t.Fatal(err)
	}
	if err := device.initAccessControlSchema(); err == nil {
		t.Fatal("a marked but incomplete access schema must fail closed")
	}
}

func TestMarkedAccessSchemaAddsApplicationRelation(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	device, err := storage.OpenDevice("edge")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := device.db.Exec(`UPDATE access_schema_meta SET value = 'v1' WHERE key = 'logical_rules'`); err != nil {
		t.Fatal(err)
	}
	if _, err := device.db.Exec(`DROP TABLE access_rule_applications`); err != nil {
		t.Fatal(err)
	}
	if err := device.initAccessControlSchema(); err != nil {
		t.Fatalf("v1 access schema should receive the additive application relation: %v", err)
	}
	var marker string
	if err := device.db.QueryRow(`SELECT value FROM access_schema_meta WHERE key = 'logical_rules'`).Scan(&marker); err != nil {
		t.Fatal(err)
	}
	if marker != accessSchemaVersion {
		t.Fatalf("schema marker was not upgraded: %q", marker)
	}
	var count int
	if err := device.db.QueryRow(`SELECT count(*) FROM pragma_table_info('access_rule_applications')`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 4 {
		t.Fatalf("unexpected application relation schema: %d columns", count)
	}
}

func TestMergeTerminalMovesAccessRuleMembers(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	ctx := context.Background()
	repository := storage.AccessRepository()
	seedAccessPolicySources(t, storage.PolicyRepository(), "source-a")
	policyStateBefore, err := storage.PolicyRepository().GetDeviceState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.SaveRule(ctx, accessSourcesRule("rule-a", "source-a"), []accesscontrol.RuleMember{
		accessFixedMember("rule-a", "addr:10.0.0.20", "10.0.0.20"),
	}, "tom"); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.SaveRule(ctx, accessSourcesRule("rule-b", "source-a"), []accesscontrol.RuleMember{
		accessFixedMember("rule-b", "mac:aa", "10.0.0.21"),
	}, "tom"); err != nil {
		t.Fatal(err)
	}
	if err := storage.MergeTerminal(ctx, "addr:10.0.0.20", "mac:aa"); err != nil {
		t.Fatal(err)
	}
	members, err := repository.ListMembers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]string)
	for _, member := range members {
		seen[member.RuleID] = member.TerminalID
	}
	if seen["rule-a"] != "mac:aa" {
		t.Fatalf("member identity was not merged into the surviving terminal: %#v", members)
	}
	if seen["rule-b"] != "mac:aa" {
		t.Fatalf("unrelated rule member must stay untouched: %#v", members)
	}
	state, err := repository.GetState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if state.DesiredRevision != 3 {
		t.Fatalf("terminal merge must bump access desired revision once per change: %#v", state)
	}
	policyStateAfter, err := storage.PolicyRepository().GetDeviceState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if policyStateAfter.DesiredRevision != policyStateBefore.DesiredRevision {
		t.Fatalf("access-only terminal merge must not dirty policy routing state: before=%#v after=%#v", policyStateBefore, policyStateAfter)
	}
}

func TestDeleteSourceRejectsReferencesFromAccessRules(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	ctx := context.Background()
	policyRepository := storage.PolicyRepository()
	accessRepository := storage.AccessRepository()
	source, err := policyRepository.SaveSource(ctx, policyv2.Source{ID: "source-a", Type: "manual", Kind: policyv2.KindIP, Name: "Source A", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := accessRepository.SaveRule(ctx, accessSourcesRule("rule-a", "source-a"), []accesscontrol.RuleMember{
		accessFixedMember("rule-a", "t1", "10.0.0.20"),
	}, "tom"); err != nil {
		t.Fatal(err)
	}
	if err := policyRepository.DeleteSource(ctx, source.ID, source.Revision); !errors.Is(err, policyv2.ErrSourceInUse) {
		t.Fatalf("source referenced by an access rule was deletable: %v", err)
	}
	if err := accessRepository.DeleteRule(ctx, "rule-a", 1, "tom"); err != nil {
		t.Fatal(err)
	}
	if err := policyRepository.DeleteSource(ctx, source.ID, source.Revision); err != nil {
		t.Fatalf("deleting the source after the rule is gone must succeed: %v", err)
	}
}

func TestAccessRulesRemainDeviceScopedAndPurgeResetsOnlySelectedDevice(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	ctx := context.Background()
	one, err := storage.OpenDevice("one")
	if err != nil {
		t.Fatal(err)
	}
	two, err := storage.OpenDevice("two")
	if err != nil {
		t.Fatal(err)
	}
	for _, device := range []*Store{one, two} {
		seedAccessPolicySources(t, device.PolicyRepository(), "source-a")
		if _, err := device.AccessRepository().SaveRule(ctx, accessSourcesRule("rule-a", "source-a"), []accesscontrol.RuleMember{
			accessFixedMember("rule-a", "addr:10.0.0.20", "10.0.0.20"),
		}, "tom"); err != nil {
			t.Fatal(err)
		}
	}
	if err := storage.PurgeDevice(ctx, "one"); err != nil {
		t.Fatal(err)
	}
	oneRules, err := one.AccessRepository().ListRules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	twoRules, err := two.AccessRepository().ListRules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	oneState, err := one.AccessRepository().GetState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	twoState, err := two.AccessRepository().GetState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(oneRules) != 0 || oneState.DesiredRevision != 0 || oneState.AppliedRevision != 0 {
		t.Fatalf("selected device access state survived purge: rules=%#v state=%#v", oneRules, oneState)
	}
	if len(twoRules) != 1 || twoState.DesiredRevision != 1 {
		t.Fatalf("purge crossed device scope: rules=%#v state=%#v", twoRules, twoState)
	}
	var oneAudit, twoAudit int
	if err := one.db.QueryRowContext(ctx, `SELECT count(*) FROM access_audit WHERE device_id = ?`, "one").Scan(&oneAudit); err != nil {
		t.Fatal(err)
	}
	if err := two.db.QueryRowContext(ctx, `SELECT count(*) FROM access_audit WHERE device_id = ?`, "two").Scan(&twoAudit); err != nil {
		t.Fatal(err)
	}
	if oneAudit != 0 || twoAudit != 1 {
		t.Fatalf("access audit purge crossed device scope: one=%d two=%d", oneAudit, twoAudit)
	}
}
