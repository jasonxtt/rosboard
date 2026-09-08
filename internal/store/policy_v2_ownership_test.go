package store

import (
	"context"
	"reflect"
	"testing"

	"rosboard/internal/accesscontrol"
	"rosboard/internal/policyv2"
)

func TestPolicyV2SeparateManagersDoNotCleanForeignAccessGraph(t *testing.T) {
	storageA, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storageA.Close()
	storageB, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storageB.Close()
	ctx := context.Background()
	target := seedCanonicalTarget(t, storageA.PolicyRepository(), "manager-isolation-target", policyv2.KindIP, policyv2.TargetListRule{RuleType: "IP-CIDR", Domain: "203.0.113.0/24"})
	if _, err := storageA.AccessRepository().SaveRule(ctx, accesscontrol.AccessRule{
		ID: "manager-isolation-rule", Name: "Manager isolation", TargetScope: accesscontrol.TargetScopeTargets,
		TargetListIDs: []string{target.ID}, Enabled: true,
	}, []accesscontrol.RuleMember{{RuleID: "manager-isolation-rule", TerminalID: "terminal", Binding: accesscontrol.BindingFixed, PinnedIPv4: []string{"10.0.0.20"}}}, "test"); err != nil {
		t.Fatal(err)
	}
	router := newPolicyV2FakeRouter()
	managerA := policyv2.NewManager(nil)
	if err := managerA.RegisterApplier("default", &policyv2.Applier{Reader: router, Mutation: router, Repo: storageA.PolicyRepository(), Access: storageA.AccessRepository()}); err != nil {
		t.Fatal(err)
	}
	planA, err := managerA.GeneratePlanWithOptions(ctx, "default", "manager-a-apply", policyv2.PlanOptions{Domain: policyv2.PolicyDomainAccess})
	if err != nil {
		t.Fatal(err)
	}
	if len(planA.Plan.Blockers) != 0 {
		t.Fatalf("manager A plan was blocked: %#v", planA.Plan.Blockers)
	}
	jobA, err := managerA.ApplyPlan(ctx, "default", planA.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	if jobA = waitPolicyV2Job(t, storageA.PolicyRepository(), jobA.ID); jobA.State != "committed" {
		t.Fatalf("manager A apply did not commit: %#v", jobA)
	}
	before := rootReviewAccessSnapshot(t, router)
	router.mu.Lock()
	writesBefore := router.writes
	router.mu.Unlock()

	managerB := policyv2.NewManager(nil)
	if err := managerB.RegisterApplier("default", &policyv2.Applier{Reader: router, Mutation: router, Repo: storageB.PolicyRepository(), Access: storageB.AccessRepository()}); err != nil {
		t.Fatal(err)
	}
	planB, err := managerB.GeneratePlanWithOptions(ctx, "default", "manager-b-empty", policyv2.PlanOptions{Domain: policyv2.PolicyDomainAccess})
	if err != nil {
		t.Fatal(err)
	}
	if len(planB.Plan.Blockers) != 0 || len(planB.Plan.Operations) != 0 {
		t.Fatalf("manager B empty desired graph acted on manager A: plan=%#v", planB.Plan)
	}
	jobB, err := managerB.ApplyPlan(ctx, "default", planB.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	if jobB = waitPolicyV2Job(t, storageB.PolicyRepository(), jobB.ID); jobB.State != "committed" {
		t.Fatalf("manager B empty apply did not commit: %#v", jobB)
	}
	router.mu.Lock()
	writesAfter := router.writes
	router.mu.Unlock()
	if writesAfter != writesBefore {
		t.Fatalf("manager B emitted RouterOS mutations for manager A objects: before=%d after=%d", writesBefore, writesAfter)
	}
	if got := rootReviewAccessSnapshot(t, router); !reflect.DeepEqual(got, before) {
		t.Fatalf("manager B changed manager A access graph: before=%#v after=%#v", before, got)
	}
}

func TestPolicyV2AccessTerminalRefreshIsIdempotentAfterCommittedApply(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	ctx := context.Background()
	policyRepository := storage.PolicyRepository()
	accessRepository := storage.AccessRepository()
	target := seedCanonicalTarget(t, policyRepository, "ownership-refresh-target", policyv2.KindIP, policyv2.TargetListRule{RuleType: "IP-CIDR", Domain: "203.0.113.0/24"})
	rootReviewAddAccessConsumer(t, ctx, accessRepository, "ownership-refresh-rule", target.ID)
	router := newPolicyV2FakeRouter()
	manager := policyv2.NewManager(nil)
	if err := manager.RegisterApplier("default", &policyv2.Applier{Reader: router, Mutation: router, Repo: policyRepository, Access: accessRepository}); err != nil {
		t.Fatal(err)
	}
	plan, err := manager.GeneratePlanWithOptions(ctx, "default", "access-initial", policyv2.PlanOptions{Domain: policyv2.PolicyDomainAccess})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Plan.Blockers) != 0 {
		t.Fatalf("initial access plan was blocked: %#v", plan.Plan.Blockers)
	}
	job, err := manager.ApplyPlan(ctx, "default", plan.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	job = waitPolicyV2Job(t, policyRepository, job.ID)
	if job.State != "committed" {
		t.Fatalf("initial access apply did not commit: %#v", job)
	}
	state, err := accessRepository.GetState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !state.Applied() {
		t.Fatalf("initial access state is not applied: %#v", state)
	}
	initialJobID := job.ID
	router.mu.Lock()
	capabilityChecks := router.capabilityChecks
	router.mu.Unlock()

	for round := 0; round < 3; round++ {
		refreshPlan, err := manager.GeneratePlan(ctx, "default", "access-terminal-refresh")
		if err != nil {
			t.Fatal(err)
		}
		if len(refreshPlan.Plan.Operations) != 0 || refreshPlan.Plan.AccessResolutionCount != 0 {
			t.Fatalf("unchanged terminal refresh was not idempotent in round %d: operations=%#v resolutions=%d", round, refreshPlan.Plan.Operations, refreshPlan.Plan.AccessResolutionCount)
		}
		if err := manager.ReconcileAccess(ctx); err != nil {
			t.Fatal(err)
		}
		state, err = accessRepository.GetState(ctx)
		if err != nil {
			t.Fatal(err)
		}
		deviceState, err := policyRepository.GetDeviceState(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if deviceState.Job.ID != initialJobID || state.DesiredRevision != state.AppliedRevision {
			t.Fatalf("unchanged terminal refresh created work in round %d: accessState=%#v deviceState=%#v", round, state, deviceState)
		}
	}
	router.mu.Lock()
	deferredCapabilityChecks := router.capabilityChecks
	router.mu.Unlock()
	if deferredCapabilityChecks != capabilityChecks {
		t.Fatalf("unchanged terminal refresh performed a RouterOS capability probe: before=%d after=%d", capabilityChecks, deferredCapabilityChecks)
	}
}
