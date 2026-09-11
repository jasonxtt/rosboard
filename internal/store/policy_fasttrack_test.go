package store

import (
	"context"
	"errors"
	"testing"

	"rosboard/internal/policyv2"
	"rosboard/internal/routeros"
)

type fastTrackRouter struct {
	*policyV2FakeRouter
	failUnset         bool
	loseUnsetResponse bool
	rejectUnset       bool
	unsetCalls        int
	earlyRestore      bool
}

func (r *fastTrackRouter) UnsetFirewallConnectionMark(_ context.Context, menu routeros.MutationMenu, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.unsetCalls++
	for _, obj := range r.objects[routeros.MenuIPFirewallMangle] {
		if obj["action"] == "mark-routing" || obj["action"] == "mark-connection" {
			r.earlyRestore = true
		}
	}
	if r.rejectUnset {
		return &routeros.HTTPError{StatusCode: 400, Status: "Bad Request"}
	}
	if r.failUnset {
		return errors.New("temporary unavailable")
	}
	delete(r.objects[menu][id], "connection-mark")
	if r.loseUnsetResponse {
		return errors.New("response lost after unset")
	}
	return nil
}
func fastTrackSetup(t *testing.T) (*PolicyRepository, *fastTrackRouter, *policyv2.Manager, policyv2.RoutingRule) {
	t.Helper()
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { storage.Close() })
	repo := storage.PolicyRepository()
	ctx := context.Background()
	egress, err := repo.SaveEgress(ctx, policyv2.Egress{ID: "wan", Name: "WAN", ListMode: policyv2.ListModeShared, ListName: "target", DNSUpstream: "1.1.1.1", FakeAlias: "192.0.2.53", Enabled: true, Families: []policyv2.EgressFamily{{Family: policyv2.FamilyIPv4, Enabled: true, WANInterface: "ether2", Gateway: "198.51.100.1", RouteMode: "strict", NATMode: "masquerade"}}})
	if err != nil {
		t.Fatal(err)
	}
	target := seedCanonicalTarget(t, repo, "target", policyv2.KindIP, policyv2.TargetListRule{RuleType: "IP-CIDR", Domain: "203.0.113.0/24"})
	rule, err := repo.SaveRoutingRule(ctx, policyv2.RoutingRule{ID: "a", Name: "Rule A", EgressID: egress.ID, TargetListIDs: []string{target.ID}, Subject: policyv2.Subject{Mode: policyv2.SubjectModeSelected, Prefixes: []string{"192.0.2.10"}}, Priority: 10, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	router := &fastTrackRouter{policyV2FakeRouter: newPolicyV2FakeRouter()}
	_, err = router.Create(ctx, routeros.MenuIPFirewallFilter, routeros.RouterOSFields{"chain": "forward", "action": "fasttrack-connection", "connection-state": "established,related", "comment": "user filter"})
	if err != nil {
		t.Fatal(err)
	}
	manager := policyv2.NewManager(nil)
	if err := manager.RegisterApplier("default", &policyv2.Applier{Repo: repo, Reader: router, Mutation: router}); err != nil {
		t.Fatal(err)
	}
	return repo, router, manager, rule
}
func fastTrackApply(t *testing.T, repo *PolicyRepository, m *policyv2.Manager) policyv2.ApplyJob {
	t.Helper()
	ctx := context.Background()
	plan, err := m.GeneratePlan(ctx, "default", "structural")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Plan.Blockers) > 0 {
		t.Fatalf("blockers: %+v", plan.Plan.Blockers)
	}
	job, err := m.ApplyPlanWithHash(ctx, "default", plan.PlanID, plan.PlanHash)
	if err != nil {
		t.Fatal(err)
	}
	job = waitPolicyV2Job(t, repo, job.ID)
	if job.State != "committed" {
		t.Fatalf("job: %+v", job)
	}
	return job
}
func TestFastTrackSharedLifecycle(t *testing.T) {
	repo, router, m, a := fastTrackSetup(t)
	ctx := context.Background()
	fastTrackApply(t, repo, m)
	state, _ := repo.LoadFastTrackState(ctx)
	if len(state.Records) != 1 || state.Records[0].Status != "applied" {
		t.Fatalf("journal: %+v", state)
	}
	before := state.Records[0].Before
	b := a
	b.ID = "b"
	b.Name = "Rule B"
	b.Revision = 0
	b.Priority = 20
	b.Subject.Prefixes = []string{"192.0.2.11"}
	b, err := repo.SaveRoutingRule(ctx, b)
	if err != nil {
		t.Fatal(err)
	}
	fastTrackApply(t, repo, m)
	if err := repo.DeleteRoutingRule(ctx, a.ID, a.Revision); err != nil {
		t.Fatal(err)
	}
	fastTrackApply(t, repo, m)
	b.Enabled = false
	b, err = repo.SaveRoutingRule(ctx, b)
	if err != nil {
		t.Fatal(err)
	}
	fastTrackApply(t, repo, m)
	state, _ = repo.LoadFastTrackState(ctx)
	if router.unsetCalls != 0 || len(state.Records) != 1 || state.Records[0].Before["connection-mark"] != before["connection-mark"] {
		t.Fatal("non-last delete/disable released or resnapshotted")
	}
	if err := repo.DeleteRoutingRule(ctx, b.ID, b.Revision); err != nil {
		t.Fatal(err)
	}
	fastTrackApply(t, repo, m)
	state, _ = repo.LoadFastTrackState(ctx)
	if router.unsetCalls != 1 || router.earlyRestore || len(state.Records) != 0 {
		t.Fatalf("release: calls=%d early=%v state=%+v", router.unsetCalls, router.earlyRestore, state)
	}
}
func TestFastTrackReleasePreservesExternalChangesAndRetries(t *testing.T) {
	for _, mode := range []string{"changed", "deleted", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			repo, router, m, a := fastTrackSetup(t)
			ctx := context.Background()
			fastTrackApply(t, repo, m)
			state, _ := repo.LoadFastTrackState(ctx)
			record := state.Records[0]
			router.mu.Lock()
			switch mode {
			case "changed":
				router.objects[record.Menu][record.ID]["comment"] = "user update"
			case "deleted":
				delete(router.objects[record.Menu], record.ID)
			case "timeout":
				router.failUnset = true
			}
			router.mu.Unlock()
			if err := repo.DeleteRoutingRule(ctx, a.ID, a.Revision); err != nil {
				t.Fatal(err)
			}
			job := fastTrackApply(t, repo, m)
			state, _ = repo.LoadFastTrackState(ctx)
			if mode == "timeout" {
				if len(state.Records) != 1 || state.Records[0].Status != "restoring" {
					t.Fatalf("pending lost: %+v", state)
				}
				result, err := m.GetJob(ctx, "default", job.ID)
				if err != nil || len(result.Warnings) == 0 {
					t.Fatalf("missing pending warning: %+v %v", result, err)
				}
				router.failUnset = false
				fastTrackApply(t, repo, m)
				state, _ = repo.LoadFastTrackState(ctx)
				if len(state.Records) != 0 {
					t.Fatal("restore did not retry")
				}
			} else if router.unsetCalls != 0 || len(state.Records) != 0 || len(state.Events) == 0 {
				t.Fatalf("external state overwritten: %+v", state)
			}
		})
	}
}
func TestFastTrackStaleForeignProducerAndAck(t *testing.T) {
	repo, router, m, _ := fastTrackSetup(t)
	ctx := context.Background()
	plan, err := m.GeneratePlan(ctx, "default", "structural")
	if err != nil {
		t.Fatal(err)
	}
	_, err = router.Create(ctx, routeros.MenuIPFirewallMangle, routeros.RouterOSFields{"chain": "prerouting", "action": "mark-connection", "new-connection-mark": "vpn"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.ApplyPlanWithHash(ctx, "default", plan.PlanID, plan.PlanHash); !errors.Is(err, policyv2.ErrPlanStale) {
		t.Fatalf("stale accepted: %v", err)
	}
	plan, err = m.GeneratePlan(ctx, "default", "structural")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Plan.Acknowledgements) != 1 {
		t.Fatalf("ack missing %+v", plan.Plan)
	}
	if _, err = m.ApplyPlanWithHash(ctx, "default", plan.PlanID, plan.PlanHash); !errors.Is(err, policyv2.ErrAcknowledgementRequired) {
		t.Fatal(err)
	}
	job, err := m.ApplyPlanWithAcknowledgements(ctx, "default", plan.PlanID, plan.PlanHash, []string{plan.Plan.Acknowledgements[0].Code})
	if err != nil {
		t.Fatal(err)
	}
	job = waitPolicyV2Job(t, repo, job.ID)
	if job.State != "committed" {
		t.Fatal(job)
	}
	state, _ := repo.LoadFastTrackState(ctx)
	if len(state.Records) != 0 {
		t.Fatal("foreign mark risk auto modified")
	}
}
func TestFastTrackJournalSurvivesReopenAndDeviceIsolation(t *testing.T) {
	dir := t.TempDir()
	storage, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := storage.PolicyRepository().SaveFastTrackState(ctx, policyv2.FastTrackState{Records: []policyv2.FastTrackRecord{{ID: "*1", Status: "intent", Before: map[string]string{"comment": "original"}, Applied: map[string]string{"connection-mark": "no-mark"}}}}); err != nil {
		t.Fatal(err)
	}
	storage.Close()
	storage, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	state, err := storage.PolicyRepository().LoadFastTrackState(ctx)
	if err != nil || len(state.Records) != 1 {
		t.Fatalf("lost durable state: %+v %v", state, err)
	}
	child, err := storage.OpenDevice("other")
	if err != nil {
		t.Fatal(err)
	}
	state, err = child.PolicyRepository().LoadFastTrackState(ctx)
	if err != nil || len(state.Records) != 0 {
		t.Fatalf("device leaked state: %+v %v", state, err)
	}
}

func TestFastTrackApplyFailureKeepsSavedConsumerAndCompatibility(t *testing.T) {
	repo, router, m, _ := fastTrackSetup(t)
	ctx := context.Background()
	plan, err := m.GeneratePlan(ctx, "default", "structural")
	if err != nil {
		t.Fatal(err)
	}
	// The first write adjusts FastTrack; a later owned-object write fails.
	router.mu.Lock()
	router.failAt = router.writes + 3
	router.mu.Unlock()
	job, err := m.ApplyPlanWithHash(ctx, "default", plan.PlanID, plan.PlanHash)
	if err != nil {
		t.Fatal(err)
	}
	job = waitPolicyV2Job(t, repo, job.ID)
	if job.State != "failed" {
		t.Fatalf("expected injected failure: %+v", job)
	}
	state, _ := repo.LoadFastTrackState(ctx)
	rules, _ := repo.ListRoutingRules(ctx)
	if len(state.Records) != 1 || state.Records[0].Status != "applied" || len(rules) != 1 || router.unsetCalls != 0 {
		t.Fatalf("failed apply restored compatibility: %+v", state)
	}
	router.mu.Lock()
	router.failAt = 0
	router.mu.Unlock()
	fastTrackApply(t, repo, m)
}
func TestFastTrackLastDeleteFailureDoesNotRestore(t *testing.T) {
	repo, router, m, a := fastTrackSetup(t)
	ctx := context.Background()
	fastTrackApply(t, repo, m)
	if err := repo.DeleteRoutingRule(ctx, a.ID, a.Revision); err != nil {
		t.Fatal(err)
	}
	plan, err := m.GeneratePlan(ctx, "default", "routing-rule-delete")
	if err != nil {
		t.Fatal(err)
	}
	router.mu.Lock()
	router.failAt = router.writes + 1
	router.mu.Unlock()
	job, err := m.ApplyPlanWithHash(ctx, "default", plan.PlanID, plan.PlanHash)
	if err != nil {
		t.Fatal(err)
	}
	job = waitPolicyV2Job(t, repo, job.ID)
	state, _ := repo.LoadFastTrackState(ctx)
	if job.State != "failed" || router.unsetCalls != 0 || len(state.Records) != 1 {
		t.Fatalf("premature restore: %+v %+v", job, state)
	}
	router.mu.Lock()
	router.failAt = 0
	router.mu.Unlock()
	fastTrackApply(t, repo, m)
	if router.unsetCalls != 1 || router.earlyRestore {
		t.Fatal("cleanup retry did not safely restore")
	}
}
func TestFastTrackNonLastDeletionDoesNotAcquireNewFilter(t *testing.T) {
	repo, router, m, a := fastTrackSetup(t)
	ctx := context.Background()
	fastTrackApply(t, repo, m)
	b := a
	b.ID = "b"
	b.Revision = 0
	b.Priority = 20
	b.Subject.Prefixes = []string{"192.0.2.11"}
	if _, err := repo.SaveRoutingRule(ctx, b); err != nil {
		t.Fatal(err)
	}
	fastTrackApply(t, repo, m)
	newFilter, err := router.Create(ctx, routeros.MenuIPFirewallFilter, routeros.RouterOSFields{"action": "fasttrack-connection", "chain": "forward", "connection-state": "established,related"})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteRoutingRule(ctx, a.ID, a.Revision); err != nil {
		t.Fatal(err)
	}
	plan, err := m.GeneratePlan(ctx, "default", "routing-rule-delete")
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Plan.FastTrack.RetainOnly {
		t.Fatal("non-last delete did not retain")
	}
	job, err := m.ApplyPlanWithHash(ctx, "default", plan.PlanID, plan.PlanHash)
	if err != nil {
		t.Fatal(err)
	}
	job = waitPolicyV2Job(t, repo, job.ID)
	if job.State != "committed" {
		t.Fatal(job)
	}
	router.mu.Lock()
	mark := router.objects[routeros.MenuIPFirewallFilter][newFilter.ID()]["connection-mark"]
	router.mu.Unlock()
	if mark != "" || router.unsetCalls != 0 {
		t.Fatal("non-last delete modified FastTrack")
	}
	// A subsequent explicit synchronization can discover and adjust the new filter.
	fastTrackApply(t, repo, m)
	state, _ := repo.LoadFastTrackState(ctx)
	if len(state.Records) != 2 {
		t.Fatalf("new filter was not discovered: %+v", state)
	}
}

func TestFastTrackRestoreUnknownOutcomeAndRejectedWrite(t *testing.T) {
	for _, mode := range []string{"lost-response", "rejected"} {
		t.Run(mode, func(t *testing.T) {
			repo, router, m, a := fastTrackSetup(t)
			ctx := context.Background()
			fastTrackApply(t, repo, m)
			if err := repo.DeleteRoutingRule(ctx, a.ID, a.Revision); err != nil {
				t.Fatal(err)
			}
			router.loseUnsetResponse = mode == "lost-response"
			router.rejectUnset = mode == "rejected"
			fastTrackApply(t, repo, m)
			state, _ := repo.LoadFastTrackState(ctx)
			if len(state.Records) != 1 {
				t.Fatal("restore intent lost")
			}
			if mode == "rejected" && state.Records[0].Status != "restore_blocked" {
				t.Fatalf("permanent rejection will retry: %+v", state)
			}
			router.loseUnsetResponse = false
			router.rejectUnset = false
			fastTrackApply(t, repo, m)
			state, _ = repo.LoadFastTrackState(ctx)
			if len(state.Records) != 0 {
				t.Fatal("restore not finalized")
			}
			if mode == "lost-response" && router.unsetCalls != 1 {
				t.Fatal("completed restore repeated after lost response")
			}
		})
	}
}
func TestNoFastTrackDoesNotCreateJournal(t *testing.T) {
	repo, router, m, _ := fastTrackSetup(t)
	router.mu.Lock()
	router.objects[routeros.MenuIPFirewallFilter] = map[string]routeros.RouterOSObject{}
	router.order[routeros.MenuIPFirewallFilter] = nil
	router.mu.Unlock()
	fastTrackApply(t, repo, m)
	var count int
	if err := repo.store.db.QueryRow(`SELECT count(*) FROM policy_v2_fasttrack`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("no FastTrack created a compatibility journal")
	}
}

func TestFastTrackFirstProposalAndConcurrentCreation(t *testing.T) {
	repo, router, m, rule := fastTrackSetup(t)
	ctx := context.Background()
	if err := repo.DeleteRoutingRule(ctx, rule.ID, rule.Revision); err != nil {
		t.Fatal(err)
	}
	previews := make([]policyv2.PlanEnvelope, 2)
	for i, id := range []string{"new-a", "new-b"} {
		draft := rule
		draft.ID = id
		draft.Revision = 0
		plan, err := m.GeneratePlanWithOptions(ctx, "default", "structural", policyv2.PlanOptions{Proposal: &policyv2.PolicyProposal{RoutingRule: &draft}})
		if err != nil {
			t.Fatal(err)
		}
		if len(plan.Plan.Blockers) > 0 {
			t.Fatalf("proposal blockers: %+v", plan.Plan.Blockers)
		}
		previews[i] = plan
	}
	state, _ := repo.LoadFastTrackState(ctx)
	rules, _ := repo.ListRoutingRules(ctx)
	if len(state.Records) != 0 || len(rules) != 0 {
		t.Fatal("preview mutated persisted state")
	}
	type result struct {
		job policyv2.ApplyJob
		err error
	}
	results := make(chan result, 2)
	for _, plan := range previews {
		go func(plan policyv2.PlanEnvelope) {
			job, err := m.ApplyPlanWithHash(ctx, "default", plan.PlanID, plan.PlanHash)
			results <- result{job, err}
		}(plan)
	}
	successes := 0
	for range previews {
		result := <-results
		if result.err == nil {
			successes++
			job := waitPolicyV2Job(t, repo, result.job.ID)
			if job.State != "committed" {
				t.Fatalf("first proposal failed: %+v", job)
			}
		} else if !errors.Is(result.err, policyv2.ErrPlanStale) && !errors.Is(result.err, policyv2.ErrDeviceBusy) {
			t.Fatal(result.err)
		}
	}
	state, _ = repo.LoadFastTrackState(ctx)
	rules, _ = repo.ListRoutingRules(ctx)
	if successes != 1 || len(state.Records) != 1 || len(rules) != 1 || router.unsetCalls != 0 {
		t.Fatalf("concurrent acquisition: successes=%d records=%d rules=%d", successes, len(state.Records), len(rules))
	}
}
