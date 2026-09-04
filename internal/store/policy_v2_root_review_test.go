package store

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"rosboard/internal/accesscontrol"
	"rosboard/internal/ownership"
	"rosboard/internal/policyv2"
	"rosboard/internal/routeros"
)

var rootReviewRoutingMenus = []routeros.MutationMenu{
	routeros.MenuInterfaceList,
	routeros.MenuInterfaceListMember,
	routeros.MenuRoutingTable,
	routeros.MenuIPRoute,
	routeros.MenuIPv6Route,
	routeros.MenuRoutingRule,
	routeros.MenuIPFirewallMangle,
	routeros.MenuIPv6FirewallMangle,
	routeros.MenuIPFirewallNAT,
	routeros.MenuIPv6FirewallNAT,
}

var rootReviewAccessMenus = []routeros.MutationMenu{
	routeros.MenuInterfaceList,
	routeros.MenuInterfaceListMember,
	routeros.MenuIPDNSForwarders,
	routeros.MenuIPDNSStatic,
	routeros.MenuIPFirewallAddressList,
	routeros.MenuIPv6FirewallAddressList,
	routeros.MenuIPFirewallFilter,
	routeros.MenuIPv6FirewallFilter,
}

func rootReviewAddRoutingConsumer(t *testing.T, ctx context.Context, repository *PolicyRepository, egressID, targetID string) {
	t.Helper()
	if _, err := repository.SaveEgress(ctx, policyv2.Egress{
		ID: egressID, Name: egressID, ListMode: policyv2.ListModeShared, ListName: "route_" + egressID,
		FailureMode: "strict", Enabled: true,
		Families: []policyv2.EgressFamily{{Family: policyv2.FamilyIPv4, Enabled: true, WANInterface: "wan1", Gateway: "192.0.2.1", RouteMode: "strict", NATMode: "masquerade"}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.SaveRoutingRule(ctx, policyv2.RoutingRule{
		ID: "routing-rule-" + egressID, Name: "routing-rule-" + egressID, EgressID: egressID,
		Subject:       policyv2.Subject{Mode: policyv2.SubjectModeSelected, Prefixes: []string{"10.0.0.20/32"}},
		TargetListIDs: []string{targetID}, Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
}

func rootReviewAddAccessConsumer(t *testing.T, ctx context.Context, repository *AccessRepository, ruleID, targetID string) {
	rootReviewAddAccessConsumerWithEnabled(t, ctx, repository, ruleID, targetID, true)
}

func rootReviewAddAccessConsumerWithEnabled(t *testing.T, ctx context.Context, repository *AccessRepository, ruleID, targetID string, enabled bool) {
	t.Helper()
	rule := canonicalRule(ruleID, targetID)
	rule.Enabled = enabled
	if _, err := repository.SaveRule(ctx, rule, []accesscontrol.RuleMember{canonicalRuleMember(ruleID)}, "root review"); err != nil {
		t.Fatal(err)
	}
}

func rootReviewRouterSnapshot(t *testing.T, router *policyV2FakeRouter, menus []routeros.MutationMenu) map[routeros.MutationMenu][]routeros.RouterOSObject {
	t.Helper()
	snapshot := make(map[routeros.MutationMenu][]routeros.RouterOSObject, len(menus))
	for _, menu := range menus {
		objects, err := router.List(context.Background(), menu, routeros.MutationQuery{})
		if err != nil {
			t.Fatal(err)
		}
		snapshot[menu] = objects
	}
	return snapshot
}

func rootReviewAccessSnapshot(t *testing.T, router *policyV2FakeRouter) map[routeros.MutationMenu][]routeros.RouterOSObject {
	t.Helper()
	snapshot := make(map[routeros.MutationMenu][]routeros.RouterOSObject, len(rootReviewAccessMenus))
	for _, menu := range rootReviewAccessMenus {
		objects, err := router.List(context.Background(), menu, routeros.MutationQuery{})
		if err != nil {
			t.Fatal(err)
		}
		filtered := make([]routeros.RouterOSObject, 0, len(objects))
		for _, object := range objects {
			if rootReviewAccessObject(object) {
				filtered = append(filtered, object)
			}
		}
		snapshot[menu] = filtered
	}
	return snapshot
}

func rootReviewHasBlocker(plan policyv2.Plan, code string) bool {
	for _, blocker := range plan.Blockers {
		if blocker.Code == code {
			return true
		}
	}
	return false
}

func rootReviewHasWarning(plan policyv2.Plan, code string) bool {
	for _, warning := range plan.Warnings {
		if warning.Code == code {
			return true
		}
	}
	return false
}

func rootReviewOnlyPromotion(t *testing.T, plan policyv2.Plan, targetID, versionID string) {
	t.Helper()
	if len(plan.TargetPromotions) != 1 || plan.TargetPromotions[0].TargetListID != targetID || plan.TargetPromotions[0].VersionID != versionID {
		t.Fatalf("plan promoted an unexpected target: %#v", plan.TargetPromotions)
	}
}

func TestPolicyV2TargetMutationUsesExactConsumerDomain(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	ctx := context.Background()
	policyRepository := storage.PolicyRepository()
	accessRepository := storage.AccessRepository()
	routingTarget := seedCanonicalTarget(t, policyRepository, "root-routing-target", policyv2.KindIP, policyv2.TargetListRule{RuleType: "IP-CIDR", Domain: "203.0.113.0/24"})
	accessTarget := seedCanonicalTarget(t, policyRepository, "root-access-target", policyv2.KindIP, policyv2.TargetListRule{RuleType: "IP-CIDR", Domain: "198.51.100.0/24"})
	rootReviewAddRoutingConsumer(t, ctx, policyRepository, "root-routing-egress", routingTarget.ID)
	rootReviewAddAccessConsumer(t, ctx, accessRepository, "root-access-rule", accessTarget.ID)
	router := newPolicyV2FakeRouter()
	manager := policyv2.NewManager(nil)
	if err := manager.RegisterApplier("default", &policyv2.Applier{Reader: router, Mutation: router, Repo: policyRepository, Access: accessRepository}); err != nil {
		t.Fatal(err)
	}

	policyBefore, err := policyRepository.GetDeviceState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	routingBefore := rootReviewRouterSnapshot(t, router, rootReviewRoutingMenus)

	accessPlan, err := manager.GeneratePlan(ctx, "default", "target-list-refresh:"+accessTarget.ID)
	if err != nil {
		t.Fatal(err)
	}
	if accessPlan.Plan.Domain != policyv2.PolicyDomainAccess || rootReviewHasBlocker(accessPlan.Plan, "target_list_split_required") || len(accessPlan.Plan.Blockers) != 0 {
		t.Fatalf("access-only target was not isolated to the access domain: %#v", accessPlan.Plan)
	}
	rootReviewOnlyPromotion(t, accessPlan.Plan, accessTarget.ID, accessTarget.PendingVersionID)
	job, err := manager.ApplyPlan(ctx, "default", accessPlan.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	if job = waitPolicyV2Job(t, policyRepository, job.ID); job.State != "committed" {
		t.Fatalf("access-only target apply failed: %#v", job)
	}
	policyAfter, err := policyRepository.GetDeviceState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if policyAfter.DesiredRevision != policyBefore.DesiredRevision || policyAfter.AppliedRevision != policyBefore.AppliedRevision || policyAfter.AppliedHash != policyBefore.AppliedHash || !policyAfter.AppliedAt.Equal(policyBefore.AppliedAt) {
		t.Fatalf("access-only target changed routing state: before=%#v after=%#v", policyBefore, policyAfter)
	}
	if got := rootReviewRouterSnapshot(t, router, rootReviewRoutingMenus); !reflect.DeepEqual(got, routingBefore) {
		t.Fatalf("access-only target changed routing RouterOS objects: before=%#v after=%#v", routingBefore, got)
	}
	accessAfter, err := accessRepository.GetState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !accessAfter.Applied() {
		t.Fatalf("access-only target did not commit access state: %#v", accessAfter)
	}
	accessAfterObjects := rootReviewAccessSnapshot(t, router)

	if err := policyRepository.SavePendingTargetListVersion(ctx, policyv2.TargetListVersion{
		ID: "root-routing-target:v2", TargetListID: routingTarget.ID, SHA256: "root-routing-v2", State: "pending", CompressedYAML: []byte("root-routing-v2"),
	}, []policyv2.TargetListRule{{RuleType: "IP-CIDR", Domain: "203.0.113.0/25"}}); err != nil {
		t.Fatal(err)
	}
	routingPlan, err := manager.GeneratePlan(ctx, "default", "target-list-refresh:"+routingTarget.ID)
	if err != nil {
		t.Fatal(err)
	}
	if routingPlan.Plan.Domain != policyv2.PolicyDomainRouting || len(routingPlan.Plan.Blockers) != 0 {
		t.Fatalf("routing-only target was not isolated to the routing domain: %#v", routingPlan.Plan)
	}
	rootReviewOnlyPromotion(t, routingPlan.Plan, routingTarget.ID, "root-routing-target:v2")
	job, err = manager.ApplyPlan(ctx, "default", routingPlan.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	if job = waitPolicyV2Job(t, policyRepository, job.ID); job.State != "committed" {
		t.Fatalf("routing-only target apply failed: %#v", job)
	}
	accessAfterRouting, err := accessRepository.GetState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(accessAfterRouting, accessAfter) {
		t.Fatalf("routing-only target changed access state: before=%#v after=%#v", accessAfter, accessAfterRouting)
	}
	if got := rootReviewAccessSnapshot(t, router); !reflect.DeepEqual(got, accessAfterObjects) {
		t.Fatalf("routing-only target changed access RouterOS objects: before=%#v after=%#v", accessAfterObjects, got)
	}
}

func TestPolicyV2SharedTargetUsesIndependentDomainAppliesAndScopedPromotion(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	ctx := context.Background()
	policyRepository := storage.PolicyRepository()
	accessRepository := storage.AccessRepository()
	sharedTarget := seedCanonicalTarget(t, policyRepository, "root-shared-target", policyv2.KindIP, policyv2.TargetListRule{RuleType: "IP-CIDR", Domain: "203.0.113.0/24"})
	unrelatedTarget := seedCanonicalTarget(t, policyRepository, "root-unrelated-target", policyv2.KindIP, policyv2.TargetListRule{RuleType: "IP-CIDR", Domain: "198.51.100.0/24"})
	rootReviewAddRoutingConsumer(t, ctx, policyRepository, "root-shared-egress", sharedTarget.ID)
	rootReviewAddRoutingConsumer(t, ctx, policyRepository, "root-unrelated-egress", unrelatedTarget.ID)
	rootReviewAddAccessConsumer(t, ctx, accessRepository, "root-shared-access", sharedTarget.ID)
	router := newPolicyV2FakeRouter()
	manager := policyv2.NewManager(nil)
	if err := manager.RegisterApplier("default", &policyv2.Applier{Reader: router, Mutation: router, Repo: policyRepository, Access: accessRepository}); err != nil {
		t.Fatal(err)
	}

	routingPlan, err := manager.GeneratePlanWithOptions(ctx, "default", "target-list-refresh:"+sharedTarget.ID, policyv2.PlanOptions{Domain: policyv2.PolicyDomainRouting, TargetListIDs: []string{sharedTarget.ID}})
	if err != nil {
		t.Fatal(err)
	}
	accessPlan, err := manager.GeneratePlanWithOptions(ctx, "default", "target-list-refresh:"+sharedTarget.ID, policyv2.PlanOptions{Domain: policyv2.PolicyDomainAccess, TargetListIDs: []string{sharedTarget.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if routingPlan.Plan.Domain != policyv2.PolicyDomainRouting || accessPlan.Plan.Domain != policyv2.PolicyDomainAccess || routingPlan.Plan.Domain == policyv2.PolicyDomainCombined || accessPlan.Plan.Domain == policyv2.PolicyDomainCombined {
		t.Fatalf("shared target did not produce two explicit domains: routing=%#v access=%#v", routingPlan.Plan, accessPlan.Plan)
	}
	if len(routingPlan.Plan.Blockers) != 0 || len(accessPlan.Plan.Blockers) != 0 {
		t.Fatalf("shared target plans were blocked: routing=%#v access=%#v", routingPlan.Plan.Blockers, accessPlan.Plan.Blockers)
	}
	rootReviewOnlyPromotion(t, routingPlan.Plan, sharedTarget.ID, sharedTarget.PendingVersionID)
	rootReviewOnlyPromotion(t, accessPlan.Plan, sharedTarget.ID, sharedTarget.PendingVersionID)
	if _, err := manager.GeneratePlan(ctx, "default", "target-list-refresh:"+sharedTarget.ID); !errors.Is(err, policyv2.ErrTargetListSplitRequired) {
		t.Fatalf("legacy single-plan shared target path returned err=%v, want split-required guard", err)
	}

	initialJob, err := manager.GenerateAndApplyTarget(ctx, "default", "target-list-refresh", sharedTarget.ID)
	if err != nil {
		t.Fatal(err)
	}
	finalJob := waitRootReviewSharedJob(t, policyRepository, accessRepository, sharedTarget.ID, sharedTarget.PendingVersionID, initialJob.ID, false)
	if finalJob.PlanID == initialJob.PlanID || finalJob.State != "committed" {
		t.Fatalf("shared target did not finish the independent follow-up apply: initial=%#v final=%#v", initialJob, finalJob)
	}
	sharedAfter, err := policyRepository.GetTargetList(ctx, sharedTarget.ID)
	if err != nil {
		t.Fatal(err)
	}
	unrelatedAfter, err := policyRepository.GetTargetList(ctx, unrelatedTarget.ID)
	if err != nil {
		t.Fatal(err)
	}
	if sharedAfter.ActiveVersionID != sharedTarget.PendingVersionID || sharedAfter.PendingVersionID != "" {
		t.Fatalf("shared target version was not promoted: %#v", sharedAfter)
	}
	if unrelatedAfter.ActiveVersionID != "" || unrelatedAfter.PendingVersionID != unrelatedTarget.PendingVersionID {
		t.Fatalf("unrelated pending target was broad-promoted: %#v", unrelatedAfter)
	}
}

func TestPolicyV2AccessCommitKeepsFollowUpJobNonTerminal(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	ctx := context.Background()
	policyRepository := storage.PolicyRepository()
	accessRepository := storage.AccessRepository()
	if _, err := accessRepository.SaveRule(ctx, accesscontrol.AccessRule{ID: "follow-up-rule", Name: "follow-up", TargetScope: accesscontrol.TargetScopeInternet, Enabled: true}, []accesscontrol.RuleMember{{RuleID: "follow-up-rule", TerminalID: "terminal", Binding: accesscontrol.BindingFixed, PinnedIPv4: []string{"10.0.0.20"}}}, "test"); err != nil {
		t.Fatal(err)
	}
	accessState, err := accessRepository.GetState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	job := policyv2.ApplyJob{ID: "follow-up-job", PlanID: "access-plan", State: "follow_up", Phase: "follow_up", CreatedAt: now, StartedAt: now}
	if err := policyRepository.SaveApplyJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	if err := policyRepository.CommitAccessApplyWithJobState(ctx, accessState.DesiredRevision, "access-hash", job, nil, nil, false); err != nil {
		t.Fatal(err)
	}
	storedJob, err := policyRepository.GetApplyJob(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if storedJob.State != "follow_up" || storedJob.Phase != "follow_up" || !storedJob.FinishedAt.IsZero() {
		t.Fatalf("non-terminal Access commit exposed a terminal job state: %#v", storedJob)
	}
	accessAfter, err := accessRepository.GetState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !accessAfter.Applied() {
		t.Fatalf("Access state was not committed with the follow-up job state: %#v", accessAfter)
	}
}

type rootReviewBlockAccessCommitRepository struct {
	*PolicyRepository
	committed chan struct{}
	release   chan struct{}
	releaseMu sync.Once
}

func (r *rootReviewBlockAccessCommitRepository) CommitAccessApplyWithJobState(ctx context.Context, accessRevision int64, appliedHash string, job policyv2.ApplyJob, resolutions []accesscontrol.MemberResolution, promotions []policyv2.TargetVersionPromotion, terminal bool) error {
	if err := r.PolicyRepository.CommitAccessApplyWithJobState(ctx, accessRevision, appliedHash, job, resolutions, promotions, terminal); err != nil {
		return err
	}
	close(r.committed)
	<-r.release
	return nil
}

func (r *rootReviewBlockAccessCommitRepository) releaseCommit() {
	r.releaseMu.Do(func() { close(r.release) })
}

func TestPolicyV2SharedDomainCommitPublishesNonTerminalJobBeforeFollowUpPlanning(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	ctx := context.Background()
	policyRepository := storage.PolicyRepository()
	accessRepository := storage.AccessRepository()
	target := seedCanonicalTarget(t, policyRepository, "root-follow-up-state", policyv2.KindDomain, policyv2.TargetListRule{RuleType: "DOMAIN-SUFFIX", Domain: "youtube.com"})
	rootReviewAddRoutingConsumer(t, ctx, policyRepository, "root-follow-up-egress", target.ID)
	rootReviewAddAccessConsumer(t, ctx, accessRepository, "root-follow-up-access", target.ID)
	router := newPolicyV2FakeRouter()
	router.dnsServers = "192.0.2.53"
	repository := &rootReviewBlockAccessCommitRepository{PolicyRepository: policyRepository, committed: make(chan struct{}), release: make(chan struct{})}
	manager := policyv2.NewManager(nil)
	if err := manager.RegisterApplier("default", &policyv2.Applier{Reader: router, Mutation: router, Repo: repository, Access: accessRepository}); err != nil {
		t.Fatal(err)
	}
	initialJob, err := manager.GenerateAndApplyTarget(ctx, "default", "target-list-refresh", target.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer repository.releaseCommit()
	select {
	case <-repository.committed:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the first domain commit")
	}
	storedJob, err := policyRepository.GetApplyJob(ctx, initialJob.ID)
	if err != nil {
		t.Fatal(err)
	}
	if storedJob.State != "follow_up" || storedJob.Terminal() {
		t.Fatalf("first domain commit exposed a terminal job before follow-up planning: %#v", storedJob)
	}
	repository.releaseCommit()
	finalJob := waitRootReviewSharedJob(t, policyRepository, accessRepository, target.ID, target.PendingVersionID, initialJob.ID, false)
	if finalJob.State != "committed" {
		t.Fatalf("shared domain follow-up did not finish: %#v", finalJob)
	}
}

func TestPolicyV2SharedDomainTargetAccessFirstFailurePreventsRouting(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	ctx := context.Background()
	policyRepository := storage.PolicyRepository()
	accessRepository := storage.AccessRepository()
	sharedTarget := seedCanonicalTarget(t, policyRepository, "root-failed-shared-domain", policyv2.KindDomain, policyv2.TargetListRule{RuleType: "DOMAIN-SUFFIX", Domain: "youtube.com"})
	rootReviewAddRoutingConsumer(t, ctx, policyRepository, "root-failed-shared-egress", sharedTarget.ID)
	rootReviewAddAccessConsumer(t, ctx, accessRepository, "root-failed-shared-access", sharedTarget.ID)
	baseRouter := newPolicyV2FakeRouter()
	baseRouter.dnsServers = "192.0.2.53"
	router := &rootReviewFailAccessRouter{policyV2FakeRouter: baseRouter, failAccess: true}
	manager := policyv2.NewManager(nil)
	if err := manager.RegisterApplier("default", &policyv2.Applier{Reader: router, Mutation: router, Repo: policyRepository, Access: accessRepository}); err != nil {
		t.Fatal(err)
	}
	initialJob, err := manager.GenerateAndApplyTarget(ctx, "default", "target-list-refresh", sharedTarget.ID)
	if err != nil {
		t.Fatal(err)
	}
	failedJob := waitRootReviewInitialFailedJob(t, policyRepository, initialJob.ID, initialJob.PlanID)
	if failedJob.PlanID != initialJob.PlanID || failedJob.State != "failed" {
		t.Fatalf("access-first failure was not recorded against the initial plan: initial=%#v failed=%#v", initialJob, failedJob)
	}
	policyState, err := policyRepository.GetDeviceState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	accessState, err := accessRepository.GetState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if policyState.Applied() {
		t.Fatalf("routing domain was applied even though Access failed first: %#v", policyState)
	}
	if accessState.Applied() {
		t.Fatalf("failed access domain was falsely reported as applied: %#v", accessState)
	}
}

func TestPolicyV2SharedIPTargetRoutingFirstFailurePreservesRouting(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	ctx := context.Background()
	policyRepository := storage.PolicyRepository()
	accessRepository := storage.AccessRepository()
	sharedTarget := seedCanonicalTarget(t, policyRepository, "root-failed-shared-ip", policyv2.KindIP, policyv2.TargetListRule{RuleType: "IP-CIDR", Domain: "203.0.113.0/24"})
	rootReviewAddRoutingConsumer(t, ctx, policyRepository, "root-failed-shared-ip-egress", sharedTarget.ID)
	rootReviewAddAccessConsumer(t, ctx, accessRepository, "root-failed-shared-ip-access", sharedTarget.ID)
	baseRouter := newPolicyV2FakeRouter()
	router := &rootReviewFailAccessRouter{policyV2FakeRouter: baseRouter, failAccess: true}
	manager := policyv2.NewManager(nil)
	if err := manager.RegisterApplier("default", &policyv2.Applier{Reader: router, Mutation: router, Repo: policyRepository, Access: accessRepository}); err != nil {
		t.Fatal(err)
	}
	initialJob, err := manager.GenerateAndApplyTarget(ctx, "default", "target-list-refresh", sharedTarget.ID)
	if err != nil {
		t.Fatal(err)
	}
	failedJob := waitRootReviewSharedJob(t, policyRepository, accessRepository, sharedTarget.ID, sharedTarget.PendingVersionID, initialJob.ID, true)
	if failedJob.PlanID == initialJob.PlanID || failedJob.State != "failed" {
		t.Fatalf("IP shared target did not retain Routing-first follow-up failure semantics: initial=%#v failed=%#v", initialJob, failedJob)
	}
	policyState, err := policyRepository.GetDeviceState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	accessState, err := accessRepository.GetState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !policyState.Applied() {
		t.Fatalf("Routing domain was not committed before the failed Access follow-up: %#v", policyState)
	}
	if accessState.Applied() {
		t.Fatalf("failed Access follow-up was falsely reported as applied: %#v", accessState)
	}
}

func TestPolicyV2SharedDomainTargetAppliesAccessBeforeRoutingDNS(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	ctx := context.Background()
	policyRepository := storage.PolicyRepository()
	accessRepository := storage.AccessRepository()
	target := seedCanonicalTarget(t, policyRepository, "root-shared-domain", policyv2.KindDomain, policyv2.TargetListRule{RuleType: "DOMAIN-SUFFIX", Domain: "youtube.com"})
	rootReviewAddRoutingConsumer(t, ctx, policyRepository, "root-shared-domain-egress", target.ID)
	rootReviewAddAccessConsumer(t, ctx, accessRepository, "root-shared-domain-access", target.ID)
	router := newPolicyV2FakeRouter()
	router.dnsServers = "192.0.2.53"
	manager := policyv2.NewManager(nil)
	if err := manager.RegisterApplier("default", &policyv2.Applier{Reader: router, Mutation: router, Repo: policyRepository, Access: accessRepository}); err != nil {
		t.Fatal(err)
	}
	initialJob, err := manager.GenerateAndApplyTarget(ctx, "default", "target-list-refresh", target.ID)
	if err != nil {
		t.Fatal(err)
	}
	waitRootReviewSharedJob(t, policyRepository, accessRepository, target.ID, target.PendingVersionID, initialJob.ID, false)
	desired, err := policyv2.BuildDesiredWithAccessOptions(ctx, policyRepository, router, accessRepository, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	actual, _, err := policyv2.ScanManaged(ctx, router, policyRepository, desired.Objects)
	if err != nil {
		t.Fatal(err)
	}
	positions := make(map[string]int)
	present := make(map[string]bool)
	for _, object := range actual {
		if object.Menu == string(routeros.MenuIPDNSStatic) && object.Ownership == "owned" {
			positions[object.LogicalID] = object.Position
			present[object.LogicalID] = true
		}
	}
	accessLogicalID := "access-target-dns:" + target.ID + ":DOMAIN-SUFFIX:youtube.com"
	routingLogicalID := "routing-dns:root-shared-domain-egress:" + target.ID + ":DOMAIN-SUFFIX:youtube.com"
	if !present[accessLogicalID] || !present[routingLogicalID] || positions[accessLogicalID] >= positions[routingLogicalID] {
		t.Fatalf("Access DNS static must precede Routing DNS static: access=%d routing=%d actual=%#v", positions[accessLogicalID], positions[routingLogicalID], actual)
	}
}

func TestPolicyV2AccessApplyMovesExistingRoutingDNSAfterCrossDomainOverlap(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	ctx := context.Background()
	policyRepository := storage.PolicyRepository()
	accessRepository := storage.AccessRepository()
	target := seedCanonicalTarget(t, policyRepository, "root-existing-routing-domain", policyv2.KindDomain, policyv2.TargetListRule{RuleType: "DOMAIN-SUFFIX", Domain: "youtube.com"})
	rootReviewAddRoutingConsumer(t, ctx, policyRepository, "root-existing-routing-egress", target.ID)
	router := newPolicyV2FakeRouter()
	router.dnsServers = "192.0.2.53"
	manager := policyv2.NewManager(nil)
	if err := manager.RegisterApplier("default", &policyv2.Applier{Reader: router, Mutation: router, Repo: policyRepository, Access: accessRepository}); err != nil {
		t.Fatal(err)
	}
	routingJob, err := manager.GenerateAndApply(ctx, "default", "routing-policy")
	if err != nil {
		t.Fatal(err)
	}
	if routingJob = waitPolicyV2Job(t, policyRepository, routingJob.ID); routingJob.State != "committed" {
		t.Fatalf("initial Routing apply failed: %#v", routingJob)
	}
	rootReviewAddAccessConsumer(t, ctx, accessRepository, "root-existing-routing-access", target.ID)
	accessPlan, err := manager.GeneratePlanWithOptions(ctx, "default", "access-policy", policyv2.PlanOptions{Domain: policyv2.PolicyDomainAccess})
	if err != nil {
		t.Fatal(err)
	}
	if rootReviewHasBlocker(accessPlan.Plan, "cross_domain_access_precedence_unavailable") || accessPlan.Plan.Summary.Move == 0 {
		t.Fatalf("Access plan did not include the owned cross-domain move: %#v", accessPlan.Plan)
	}
	accessJob, err := manager.ApplyPlan(ctx, "default", accessPlan.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	if accessJob = waitPolicyV2Job(t, policyRepository, accessJob.ID); accessJob.State != "committed" {
		t.Fatalf("Access apply with an existing Routing projection failed: %#v", accessJob)
	}
	actual, err := router.List(ctx, routeros.MenuIPDNSStatic, routeros.MutationQuery{})
	if err != nil {
		t.Fatal(err)
	}
	desired, err := policyv2.BuildDesiredWithAccessOptions(ctx, policyRepository, router, accessRepository, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	managed, _, err := policyv2.ScanManaged(ctx, router, policyRepository, desired.Objects)
	if err != nil {
		t.Fatal(err)
	}
	logicalPositions := make(map[string]int)
	for _, object := range managed {
		if object.Menu == string(routeros.MenuIPDNSStatic) && object.Ownership == "owned" {
			logicalPositions[object.LogicalID] = object.Position
		}
	}
	accessLogicalID := "access-target-dns:" + target.ID + ":DOMAIN-SUFFIX:youtube.com"
	routingLogicalID := "routing-dns:root-existing-routing-egress:" + target.ID + ":DOMAIN-SUFFIX:youtube.com"
	if logicalPositions[accessLogicalID] >= logicalPositions[routingLogicalID] {
		t.Fatalf("Access DNS static was not moved before existing Routing static: positions=%#v actual=%#v", logicalPositions, actual)
	}
	var accessRouterID, routingRouterID string
	for _, object := range managed {
		if object.LogicalID == accessLogicalID {
			accessRouterID = object.RouterID
		}
		if object.LogicalID == routingLogicalID {
			routingRouterID = object.RouterID
		}
	}
	if accessRouterID == "" || routingRouterID == "" {
		t.Fatalf("cross-domain DNS statics did not retain RouterOS IDs: %#v", managed)
	}
	routingPlan, err := manager.GeneratePlanWithOptions(ctx, "default", "routing-policy", policyv2.PlanOptions{Domain: policyv2.PolicyDomainRouting})
	if err != nil {
		t.Fatal(err)
	}
	accessPlan, err = manager.GeneratePlanWithOptions(ctx, "default", "access-policy-stale-check", policyv2.PlanOptions{Domain: policyv2.PolicyDomainAccess})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := router.Move(ctx, routeros.MenuIPDNSStatic, routeros.MoveRequest{ID: routingRouterID, BeforeID: accessRouterID}); err != nil {
		t.Fatal(err)
	}
	for _, plan := range []policyv2.PlanEnvelope{routingPlan, accessPlan} {
		if _, err := manager.ApplyPlan(ctx, "default", plan.PlanID); !errors.Is(err, policyv2.ErrPlanStale) {
			t.Fatalf("cross-domain DNS order drift must stale the %s plan, got %v", plan.Plan.Domain, err)
		}
	}
}

type rootReviewNoopMoveRouter struct {
	*policyV2FakeRouter
}

func (r *rootReviewNoopMoveRouter) Move(context.Context, routeros.MutationMenu, routeros.MoveRequest) (routeros.MutationResponse, error) {
	return routeros.MutationResponse{}, nil
}

func TestPolicyV2RoutingApplyRejectsUnconvergedDisabledDNSBeforeActivation(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	ctx := context.Background()
	policyRepository := storage.PolicyRepository()
	accessRepository := storage.AccessRepository()
	target := seedCanonicalTarget(t, policyRepository, "root-preactivation-order", policyv2.KindDomain, policyv2.TargetListRule{RuleType: "DOMAIN-SUFFIX", Domain: "youtube.com"})
	rootReviewAddRoutingConsumer(t, ctx, policyRepository, "root-preactivation-egress", target.ID)
	rootReviewAddAccessConsumer(t, ctx, accessRepository, "root-preactivation-access", target.ID)
	baseRouter := newPolicyV2FakeRouter()
	baseRouter.dnsServers = "192.0.2.53"
	manager := policyv2.NewManager(nil)
	if err := manager.RegisterApplier("default", &policyv2.Applier{Reader: baseRouter, Mutation: baseRouter, Repo: policyRepository, Access: accessRepository}); err != nil {
		t.Fatal(err)
	}
	routingPlan, err := manager.GeneratePlanWithOptions(ctx, "default", "routing-seed", policyv2.PlanOptions{Domain: policyv2.PolicyDomainRouting})
	if err != nil {
		t.Fatal(err)
	}
	var routingDNS policyv2.DesiredObject
	for _, operation := range routingPlan.Plan.Operations {
		if operation.Action == "create" && strings.HasPrefix(operation.LogicalID, "routing-dns:") {
			routingDNS = policyv2.DesiredObject{LogicalID: operation.LogicalID, Menu: operation.Menu, Fields: operation.After}
			break
		}
	}
	if routingDNS.LogicalID == "" {
		t.Fatalf("routing seed plan did not contain a DNS static: %#v", routingPlan.Plan.Operations)
	}
	fields := make(routeros.RouterOSFields, len(routingDNS.Fields))
	for key, value := range routingDNS.Fields {
		fields[key] = value
	}
	fields["disabled"] = "yes"
	if _, err := baseRouter.Create(ctx, routeros.MenuIPDNSStatic, fields); err != nil {
		t.Fatal(err)
	}
	accessPlan, err := manager.GeneratePlanWithOptions(ctx, "default", "access-seed", policyv2.PlanOptions{Domain: policyv2.PolicyDomainAccess})
	if err != nil {
		t.Fatal(err)
	}
	if accessPlan.Plan.Summary.Move == 0 || len(accessPlan.Plan.Blockers) != 0 {
		t.Fatalf("Access seed plan did not establish precedence: %#v", accessPlan.Plan)
	}
	accessJob, err := manager.ApplyPlan(ctx, "default", accessPlan.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	if accessJob = waitPolicyV2Job(t, policyRepository, accessJob.ID); accessJob.State != "committed" {
		t.Fatalf("Access seed apply failed: %#v", accessJob)
	}
	desired, err := policyv2.BuildDesiredWithAccessOptions(ctx, policyRepository, baseRouter, accessRepository, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	managed, _, err := policyv2.ScanManaged(ctx, baseRouter, policyRepository, desired.Objects)
	if err != nil {
		t.Fatal(err)
	}
	var accessID, routingID string
	for _, object := range managed {
		if object.LogicalID == "access-target-dns:"+target.ID+":DOMAIN-SUFFIX:youtube.com" {
			accessID = object.RouterID
		}
		if object.LogicalID == routingDNS.LogicalID {
			routingID = object.RouterID
		}
	}
	if accessID == "" || routingID == "" {
		t.Fatalf("seeded Access/Routing DNS statics were not found: %#v", managed)
	}
	if _, err := baseRouter.Move(ctx, routeros.MenuIPDNSStatic, routeros.MoveRequest{ID: routingID, BeforeID: accessID}); err != nil {
		t.Fatal(err)
	}
	if _, err := baseRouter.Patch(ctx, routeros.MenuIPDNSStatic, routingID, routeros.RouterOSFields{"disabled": "yes"}); err != nil {
		t.Fatal(err)
	}
	noOpRouter := &rootReviewNoopMoveRouter{policyV2FakeRouter: baseRouter}
	routingManager := policyv2.NewManager(nil)
	if err := routingManager.RegisterApplier("default", &policyv2.Applier{Reader: noOpRouter, Mutation: noOpRouter, Repo: policyRepository, Access: accessRepository}); err != nil {
		t.Fatal(err)
	}
	routingPlan, err = routingManager.GeneratePlanWithOptions(ctx, "default", "routing-order-check", policyv2.PlanOptions{Domain: policyv2.PolicyDomainRouting})
	if err != nil {
		t.Fatal(err)
	}
	routingJob, err := routingManager.ApplyPlan(ctx, "default", routingPlan.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	if routingJob = waitPolicyV2Job(t, policyRepository, routingJob.ID); routingJob.State != "failed" {
		t.Fatalf("routing apply with a no-op move must fail before activation: %#v", routingJob)
	}
	objects, err := baseRouter.List(ctx, routeros.MenuIPDNSStatic, routeros.MutationQuery{})
	if err != nil {
		t.Fatal(err)
	}
	for _, object := range objects {
		if object.ID() == routingID && !strings.EqualFold(strings.TrimSpace(object["disabled"]), "yes") && !strings.EqualFold(strings.TrimSpace(object["disabled"]), "true") {
			t.Fatalf("Routing DNS static was activated despite failed preactivation order verification: %#v", object)
		}
	}
}

func TestPolicyV2RefreshDueBatchesExactConsumerDomains(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	ctx := context.Background()
	policyRepository := storage.PolicyRepository()
	accessRepository := storage.AccessRepository()
	routingTarget, err := policyRepository.SaveTargetList(ctx, policyv2.TargetList{
		ID: "root-scheduled-routing-target", Name: "scheduled routing", Kind: policyv2.KindIP, SourceType: policyv2.TargetSourceTypeURL,
		Schedule: "1h", Enabled: true, NextRunAt: time.Now().Add(-time.Hour), URL: "https://example.invalid/routing",
	})
	if err != nil {
		t.Fatal(err)
	}
	accessTarget, err := policyRepository.SaveTargetList(ctx, policyv2.TargetList{
		ID: "root-scheduled-access-target", Name: "scheduled access", Kind: policyv2.KindIP, SourceType: policyv2.TargetSourceTypeURL,
		Schedule: "1h", Enabled: true, NextRunAt: time.Now().Add(-time.Hour), URL: "https://example.invalid/access",
	})
	if err != nil {
		t.Fatal(err)
	}
	rootReviewAddRoutingConsumer(t, ctx, policyRepository, "root-scheduled-egress", routingTarget.ID)
	rootReviewAddAccessConsumer(t, ctx, accessRepository, "root-scheduled-access-rule", accessTarget.ID)
	router := newPolicyV2FakeRouter()
	manager := policyv2.NewManager(nil)
	if err := manager.RegisterApplier("default", &policyv2.Applier{
		Reader: router, Mutation: router, Repo: policyRepository, Access: accessRepository,
		Refresh: func(_ context.Context, source policyv2.Source) (policyv2.SourceRefresh, error) {
			versionID := source.ID + ":scheduled-v2"
			return policyv2.SourceRefresh{
				Version: &policyv2.SourceVersion{ID: versionID, SourceID: source.ID, SHA256: versionID, State: "pending", CompressedYAML: []byte(versionID)},
				Rules:   []policyv2.SourceRule{{RuleType: "IP-CIDR", Domain: "203.0.113.0/24"}},
			}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := manager.RefreshDue(ctx, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	state, err := policyRepository.GetDeviceState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if state.Job.ID == "" {
		t.Fatal("scheduled refresh with routing and access consumers did not start an apply")
	}
	finalJob := waitRootReviewSharedJob(t, policyRepository, accessRepository, routingTarget.ID, routingTarget.ID+":scheduled-v2", state.Job.ID, false)
	if finalJob.PlanID == state.Job.PlanID {
		t.Fatalf("scheduled refresh did not run the independent follow-up domain plan: initial=%#v final=%#v", state.Job, finalJob)
	}
	routingAfter, err := policyRepository.GetTargetList(ctx, routingTarget.ID)
	if err != nil {
		t.Fatal(err)
	}
	accessAfter, err := policyRepository.GetTargetList(ctx, accessTarget.ID)
	if err != nil {
		t.Fatal(err)
	}
	if routingAfter.ActiveVersionID != routingTarget.ID+":scheduled-v2" || routingAfter.PendingVersionID != "" || accessAfter.ActiveVersionID != accessTarget.ID+":scheduled-v2" || accessAfter.PendingVersionID != "" {
		t.Fatalf("scheduled refresh did not apply each target in its consumer domain: routing=%#v access=%#v", routingAfter, accessAfter)
	}
}

func TestPolicyV2UnreferencedTargetMutationAndDeleteDoNotApplyOtherDomains(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	ctx := context.Background()
	policyRepository := storage.PolicyRepository()
	accessRepository := storage.AccessRepository()
	unreferenced := seedCanonicalTarget(t, policyRepository, "root-unreferenced-target", policyv2.KindIP, policyv2.TargetListRule{RuleType: "IP-CIDR", Domain: "192.0.2.0/24"})
	routingTarget := seedCanonicalTarget(t, policyRepository, "root-delete-routing-target", policyv2.KindIP, policyv2.TargetListRule{RuleType: "IP-CIDR", Domain: "203.0.113.0/24"})
	accessTarget := seedCanonicalTarget(t, policyRepository, "root-delete-access-target", policyv2.KindIP, policyv2.TargetListRule{RuleType: "IP-CIDR", Domain: "198.51.100.0/24"})
	rootReviewAddRoutingConsumer(t, ctx, policyRepository, "root-delete-routing-egress", routingTarget.ID)
	rootReviewAddAccessConsumer(t, ctx, accessRepository, "root-delete-access-rule", accessTarget.ID)
	router := newPolicyV2FakeRouter()
	manager := policyv2.NewManager(nil)
	if err := manager.RegisterApplier("default", &policyv2.Applier{Reader: router, Mutation: router, Repo: policyRepository, Access: accessRepository}); err != nil {
		t.Fatal(err)
	}
	policyBefore, err := policyRepository.GetDeviceState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	accessBefore, err := accessRepository.GetState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	before := rootReviewRouterSnapshot(t, router, append(append([]routeros.MutationMenu{}, rootReviewRoutingMenus...), rootReviewAccessMenus...))
	if err := policyRepository.SavePendingTargetListVersion(ctx, policyv2.TargetListVersion{
		ID: "root-unreferenced-target:v2", TargetListID: unreferenced.ID, SHA256: "root-unreferenced-v2", State: "pending", CompressedYAML: []byte("root-unreferenced-v2"),
	}, []policyv2.TargetListRule{{RuleType: "IP-CIDR", Domain: "192.0.2.128/25"}}); err != nil {
		t.Fatal(err)
	}
	job, err := manager.GenerateAndApplyTarget(ctx, "default", "target-list-save", unreferenced.ID)
	if err != nil {
		t.Fatal(err)
	}
	if job.ID != "" {
		t.Fatalf("unreferenced target unexpectedly started an apply: %#v", job)
	}
	if got := rootReviewRouterSnapshot(t, router, append(append([]routeros.MutationMenu{}, rootReviewRoutingMenus...), rootReviewAccessMenus...)); !reflect.DeepEqual(got, before) {
		t.Fatalf("unreferenced target changed RouterOS objects: before=%#v after=%#v", before, got)
	}

	loaded, err := policyRepository.GetTargetList(ctx, unreferenced.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := policyRepository.DeleteTargetList(ctx, unreferenced.ID, loaded.Revision); err != nil {
		t.Fatal(err)
	}
	job, err = manager.GenerateAndApplyTarget(ctx, "default", "target-list-delete", unreferenced.ID)
	if err != nil {
		t.Fatal(err)
	}
	if job.ID != "" {
		t.Fatalf("unreferenced target delete unexpectedly started an apply: %#v", job)
	}
	policyAfter, err := policyRepository.GetDeviceState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	accessAfter, err := accessRepository.GetState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if policyAfter.DesiredRevision != policyBefore.DesiredRevision || policyAfter.AppliedRevision != policyBefore.AppliedRevision || policyAfter.AppliedHash != policyBefore.AppliedHash || accessAfter.DesiredRevision != accessBefore.DesiredRevision || accessAfter.AppliedRevision != accessBefore.AppliedRevision {
		t.Fatalf("unreferenced target changed another domain state: policy before=%#v after=%#v access before=%#v after=%#v", policyBefore, policyAfter, accessBefore, accessAfter)
	}
	if _, err := policyRepository.GetTargetList(ctx, unreferenced.ID); !errors.Is(err, policyv2.ErrTargetListNotFound) {
		t.Fatalf("unreferenced target was not deleted from metadata: %v", err)
	}
}

func TestPolicyV2AccessSharesOnePhysicalProjectionPerTarget(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	ctx := context.Background()
	policyRepository := storage.PolicyRepository()
	accessRepository := storage.AccessRepository()
	target := seedCanonicalTarget(t, policyRepository, "root-shared-access-domain", policyv2.KindDomain, policyv2.TargetListRule{RuleType: "DOMAIN-SUFFIX", Domain: "youtube.com"})
	rootReviewAddAccessConsumer(t, ctx, accessRepository, "root-access-rule-a", target.ID)
	rootReviewAddAccessConsumer(t, ctx, accessRepository, "root-access-rule-b", target.ID)
	router := newPolicyV2FakeRouter()
	router.dnsServers = "192.0.2.53"
	desired, err := policyv2.BuildAccessDesiredWithOptions(ctx, policyRepository, router, accessRepository, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(desired.Blockers) != 0 {
		t.Fatalf("shared access target was blocked: %#v", desired.Blockers)
	}
	managerID, err := policyRepository.ManagerInstanceID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	listName := policyv2.AccessTargetListName(managerID, policyRepository.DeviceID(), target.ID)
	dnsProjections, filterReferences := 0, 0
	for _, object := range desired.Objects {
		if object.LogicalID == "access-target-dns:"+target.ID+":DOMAIN-SUFFIX:youtube.com" {
			dnsProjections++
			if object.Fields["address-list"] != listName {
				t.Fatalf("canonical DNS projection uses the wrong shared list: %#v", object)
			}
		}
		if object.Menu == string(routeros.MenuIPFirewallFilter) && (object.Fields["src-address-list"] == listName || object.Fields["dst-address-list"] == listName || object.Fields["address-list"] == listName) {
			filterReferences++
		}
	}
	if dnsProjections != 1 || filterReferences < 2 {
		t.Fatalf("access target projection was duplicated or filters lost the shared list: dns=%d filterReferences=%d list=%q objects=%#v", dnsProjections, filterReferences, listName, desired.Objects)
	}
}

func TestPolicyV2DisabledAccessRuleDoesNotActivateTargetProjection(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	ctx := context.Background()
	policyRepository := storage.PolicyRepository()
	accessRepository := storage.AccessRepository()
	google := seedCanonicalTarget(t, policyRepository, "root-disabled-google", policyv2.KindDomain, policyv2.TargetListRule{RuleType: "DOMAIN", Domain: "google.com"})
	youtube := seedCanonicalTarget(t, policyRepository, "root-disabled-youtube", policyv2.KindDomain, policyv2.TargetListRule{RuleType: "DOMAIN", Domain: "youtube.com"})
	rootReviewAddAccessConsumer(t, ctx, accessRepository, "root-enabled-google", google.ID)
	rootReviewAddAccessConsumerWithEnabled(t, ctx, accessRepository, "root-disabled-youtube-rule", youtube.ID, false)
	router := newPolicyV2FakeRouter()
	router.dnsServers = "192.0.2.53"
	desired, err := policyv2.BuildAccessDesiredWithOptions(ctx, policyRepository, router, accessRepository, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(desired.Blockers) != 0 {
		t.Fatalf("disabled access target unexpectedly blocked the desired graph: %#v", desired.Blockers)
	}
	if len(desired.AccessTargetIDs) != 1 || desired.AccessTargetIDs[0] != google.ID {
		t.Fatalf("disabled-only target was retained as an active access target: %#v", desired.AccessTargetIDs)
	}
	for _, object := range desired.Objects {
		if strings.Contains(object.LogicalID, youtube.ID) && strings.HasPrefix(object.LogicalID, "access-target") {
			t.Fatalf("disabled-only access target was materialized: %#v", object)
		}
	}
}

func TestPolicyV2DisabledAccessTargetDoesNotCreateCrossDomainDNSConflict(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	ctx := context.Background()
	policyRepository := storage.PolicyRepository()
	accessRepository := storage.AccessRepository()
	google := seedCanonicalTarget(t, policyRepository, "root-overlap-google", policyv2.KindDomain, policyv2.TargetListRule{RuleType: "DOMAIN", Domain: "google.com"})
	youtube := seedCanonicalTarget(t, policyRepository, "root-overlap-youtube", policyv2.KindDomain, policyv2.TargetListRule{RuleType: "DOMAIN", Domain: "youtube.com"})
	rootReviewAddRoutingConsumer(t, ctx, policyRepository, "root-overlap-egress", youtube.ID)
	rootReviewAddAccessConsumer(t, ctx, accessRepository, "root-overlap-google-rule", google.ID)
	rootReviewAddAccessConsumerWithEnabled(t, ctx, accessRepository, "root-overlap-youtube-rule", youtube.ID, false)
	router := newPolicyV2FakeRouter()
	router.dnsServers = "192.0.2.53"
	manager := policyv2.NewManager(nil)
	if err := manager.RegisterApplier("default", &policyv2.Applier{Reader: router, Mutation: router, Repo: policyRepository, Access: accessRepository}); err != nil {
		t.Fatal(err)
	}
	routingPlan, err := manager.GeneratePlanWithOptions(ctx, "default", "routing-policy", policyv2.PlanOptions{Domain: policyv2.PolicyDomainRouting})
	if err != nil {
		t.Fatal(err)
	}
	accessPlan, err := manager.GeneratePlanWithOptions(ctx, "default", "access-policy", policyv2.PlanOptions{Domain: policyv2.PolicyDomainAccess})
	if err != nil {
		t.Fatal(err)
	}
	if rootReviewHasWarning(routingPlan.Plan, "cross_domain_access_priority_shadowed") || rootReviewHasWarning(accessPlan.Plan, "cross_domain_access_priority_shadowed") {
		t.Fatalf("disabled YouTube access consumer created a cross-domain issue: routing=%#v access=%#v", routingPlan.Plan, accessPlan.Plan)
	}
	routingDesired, err := policyv2.BuildRoutingDesired(ctx, policyRepository, router, nil)
	if err != nil {
		t.Fatal(err)
	}
	accessDesired, err := policyv2.BuildAccessDesiredWithOptions(ctx, policyRepository, router, accessRepository, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	routingProjectionFound := false
	for _, object := range routingDesired.Objects {
		if object.Menu != string(routeros.MenuIPDNSStatic) || object.Fields["name"] != "youtube.com" {
			continue
		}
		routingProjectionFound = true
		if object.Fields["disabled"] != "no" {
			t.Fatalf("routing YouTube projection was not active: %#v", object)
		}
	}
	if !routingProjectionFound {
		t.Fatal("routing YouTube projection was not emitted")
	}
	for _, object := range accessDesired.Objects {
		if strings.HasPrefix(object.LogicalID, "access-target") && strings.Contains(object.LogicalID, youtube.ID) {
			t.Fatalf("disabled access YouTube projection was emitted: %#v", object)
		}
	}
}

func TestPolicyV2MixedAccessConsumersShareActiveTargetProjection(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	ctx := context.Background()
	policyRepository := storage.PolicyRepository()
	accessRepository := storage.AccessRepository()
	target := seedCanonicalTarget(t, policyRepository, "root-mixed-access-target", policyv2.KindDomain, policyv2.TargetListRule{RuleType: "DOMAIN-SUFFIX", Domain: "youtube.com"})
	rootReviewAddAccessConsumer(t, ctx, accessRepository, "root-mixed-enabled-rule", target.ID)
	rootReviewAddAccessConsumerWithEnabled(t, ctx, accessRepository, "root-mixed-disabled-rule", target.ID, false)
	router := newPolicyV2FakeRouter()
	router.dnsServers = "192.0.2.53"
	desired, err := policyv2.BuildAccessDesiredWithOptions(ctx, policyRepository, router, accessRepository, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(desired.Blockers) != 0 {
		t.Fatalf("mixed access consumers were unexpectedly blocked: %#v", desired.Blockers)
	}
	projectionCount := 0
	for _, object := range desired.Objects {
		if object.LogicalID == "access-target-dns:"+target.ID+":DOMAIN-SUFFIX:youtube.com" {
			projectionCount++
			if object.Fields["disabled"] != "no" {
				t.Fatalf("mixed consumers disabled their shared target projection: %#v", object)
			}
		}
	}
	if projectionCount != 1 {
		t.Fatalf("mixed consumers did not produce one canonical target projection: %d", projectionCount)
	}
	for _, ruleID := range []string{"root-mixed-enabled-rule", "root-mixed-disabled-rule"} {
		wantDisabled := "no"
		if ruleID == "root-mixed-disabled-rule" {
			wantDisabled = "yes"
		}
		found := false
		prefix := "access:" + ruleID + ":ipv4:jump-out:target:" + target.ID
		for _, object := range desired.Objects {
			if object.LogicalID == prefix {
				found = true
				if object.Fields["disabled"] != wantDisabled {
					t.Fatalf("rule %s target filter disabled=%q, want %q: %#v", ruleID, object.Fields["disabled"], wantDisabled, object)
				}
			}
		}
		if !found {
			t.Fatalf("rule %s did not retain its target filter", ruleID)
		}
	}
}

func TestPolicyV2LastEnabledAccessConsumerCleansTargetProjection(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	ctx := context.Background()
	policyRepository := storage.PolicyRepository()
	accessRepository := storage.AccessRepository()
	target := seedCanonicalTarget(t, policyRepository, "root-last-access-target", policyv2.KindDomain, policyv2.TargetListRule{RuleType: "DOMAIN", Domain: "youtube.com"})
	rootReviewAddAccessConsumer(t, ctx, accessRepository, "root-last-access-rule", target.ID)
	router := newPolicyV2FakeRouter()
	router.dnsServers = "192.0.2.53"
	manager := policyv2.NewManager(nil)
	if err := manager.RegisterApplier("default", &policyv2.Applier{Reader: router, Mutation: router, Repo: policyRepository, Access: accessRepository}); err != nil {
		t.Fatal(err)
	}
	plan, err := manager.GeneratePlanWithOptions(ctx, "default", "access-rule-save", policyv2.PlanOptions{Domain: policyv2.PolicyDomainAccess})
	if err != nil {
		t.Fatal(err)
	}
	job, err := manager.ApplyPlan(ctx, "default", plan.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	if job = waitPolicyV2Job(t, policyRepository, job.ID); job.State != "committed" {
		t.Fatalf("initial access projection apply failed: %#v", job)
	}
	rules, err := accessRepository.ListRules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 {
		t.Fatalf("unexpected access rules: %#v", rules)
	}
	rules[0].Enabled = false
	if _, err := accessRepository.SaveRule(ctx, rules[0], nil, "root review"); err != nil {
		t.Fatal(err)
	}
	plan, err = manager.GeneratePlanWithOptions(ctx, "default", "access-rule-disable", policyv2.PlanOptions{Domain: policyv2.PolicyDomainAccess})
	if err != nil {
		t.Fatal(err)
	}
	job, err = manager.ApplyPlan(ctx, "default", plan.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	if job = waitPolicyV2Job(t, policyRepository, job.ID); job.State != "committed" {
		t.Fatalf("last enabled access consumer disable apply failed: %#v", job)
	}
	managerID, err := policyRepository.ManagerInstanceID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	listName := policyv2.AccessTargetListName(managerID, policyRepository.DeviceID(), target.ID)
	for _, menu := range []routeros.MutationMenu{routeros.MenuIPDNSStatic, routeros.MenuIPFirewallAddressList, routeros.MenuIPv6FirewallAddressList} {
		objects, err := router.List(ctx, menu, routeros.MutationQuery{})
		if err != nil {
			t.Fatal(err)
		}
		for _, object := range objects {
			if object["list"] == listName || object["address-list"] == listName {
				t.Fatalf("last enabled access consumer left a target projection active or stale: menu=%s object=%#v", menu, object)
			}
		}
	}
}

func TestPolicyV2AccessDomainProjectionBlockers(t *testing.T) {
	tests := []struct {
		name        string
		ruleA       policyv2.TargetListRule
		ruleB       policyv2.TargetListRule
		wantBlocker bool
	}{
		{name: "different non-overlap", ruleA: policyv2.TargetListRule{RuleType: "DOMAIN", Domain: "youtube.com"}, ruleB: policyv2.TargetListRule{RuleType: "DOMAIN-SUFFIX", Domain: "example.com"}},
		{name: "exact suffix overlap", ruleA: policyv2.TargetListRule{RuleType: "DOMAIN", Domain: "youtube.com"}, ruleB: policyv2.TargetListRule{RuleType: "DOMAIN-SUFFIX", Domain: "youtube.com"}, wantBlocker: true},
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
			first := seedCanonicalTarget(t, policyRepository, "root-domain-a", policyv2.KindDomain, test.ruleA)
			second := seedCanonicalTarget(t, policyRepository, "root-domain-b", policyv2.KindDomain, test.ruleB)
			rootReviewAddAccessConsumer(t, ctx, accessRepository, "root-domain-rule-a", first.ID)
			rootReviewAddAccessConsumer(t, ctx, accessRepository, "root-domain-rule-b", second.ID)
			router := newPolicyV2FakeRouter()
			router.dnsServers = "192.0.2.53"
			desired, err := policyv2.BuildAccessDesiredWithOptions(ctx, policyRepository, router, accessRepository, nil, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			gotBlocker := rootReviewHasBlocker(policyv2.Plan{Blockers: desired.Blockers}, "access_domain_projection_ambiguous")
			if gotBlocker != test.wantBlocker {
				t.Fatalf("access domain overlap blocker=%v, want %v: %#v", gotBlocker, test.wantBlocker, desired.Blockers)
			}
		})
	}
}

func TestPolicyV2CrossDomainDNSProjectionPrecedence(t *testing.T) {
	tests := []struct {
		name        string
		kind        string
		ruleA       policyv2.TargetListRule
		ruleB       policyv2.TargetListRule
		wantWarning bool
	}{
		{name: "same domain target", kind: policyv2.KindDomain, ruleA: policyv2.TargetListRule{RuleType: "DOMAIN", Domain: "youtube.com"}, ruleB: policyv2.TargetListRule{RuleType: "DOMAIN", Domain: "youtube.com"}, wantWarning: true},
		{name: "different overlapping domain targets", kind: policyv2.KindDomain, ruleA: policyv2.TargetListRule{RuleType: "DOMAIN", Domain: "youtube.com"}, ruleB: policyv2.TargetListRule{RuleType: "DOMAIN-SUFFIX", Domain: "youtube.com"}, wantWarning: true},
		{name: "shared IP target", kind: policyv2.KindIP, ruleA: policyv2.TargetListRule{RuleType: "IP-CIDR", Domain: "203.0.113.0/24"}, ruleB: policyv2.TargetListRule{RuleType: "IP-CIDR", Domain: "203.0.113.0/24"}},
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
			first := seedCanonicalTarget(t, policyRepository, "root-cross-a", test.kind, test.ruleA)
			secondID := first.ID
			if test.name != "same domain target" {
				second := seedCanonicalTarget(t, policyRepository, "root-cross-b", test.kind, test.ruleB)
				secondID = second.ID
			}
			rootReviewAddRoutingConsumer(t, ctx, policyRepository, "root-cross-egress", first.ID)
			rootReviewAddAccessConsumer(t, ctx, accessRepository, "root-cross-access-rule", secondID)
			router := newPolicyV2FakeRouter()
			router.dnsServers = "192.0.2.53"
			manager := policyv2.NewManager(nil)
			if err := manager.RegisterApplier("default", &policyv2.Applier{Reader: router, Mutation: router, Repo: policyRepository, Access: accessRepository}); err != nil {
				t.Fatal(err)
			}
			routingPlan, err := manager.GeneratePlan(ctx, "default", "routing-policy")
			if err != nil {
				t.Fatal(err)
			}
			accessPlan, err := manager.GeneratePlan(ctx, "default", "access-policy")
			if err != nil {
				t.Fatal(err)
			}
			routingWarning := rootReviewHasWarning(routingPlan.Plan, "cross_domain_access_priority_shadowed")
			accessWarning := rootReviewHasWarning(accessPlan.Plan, "cross_domain_access_priority_shadowed")
			if routingWarning != test.wantWarning || accessWarning != test.wantWarning {
				t.Fatalf("cross-domain warning routing=%v access=%v want=%v: routing=%#v access=%#v", routingWarning, accessWarning, test.wantWarning, routingPlan.Plan, accessPlan.Plan)
			}
			if test.wantWarning && !rootReviewHasBlocker(routingPlan.Plan, "cross_domain_access_precedence_unavailable") {
				t.Fatalf("routing plan did not fail closed while Access DNS projection is absent: %#v", routingPlan.Plan.Blockers)
			}
			if !test.wantWarning && rootReviewHasBlocker(routingPlan.Plan, "cross_domain_access_precedence_unavailable") {
				t.Fatalf("non-overlapping routing plan unexpectedly received an Access precedence blocker: %#v", routingPlan.Plan.Blockers)
			}
			if routingPlan.Plan.Domain == policyv2.PolicyDomainCombined || accessPlan.Plan.Domain == policyv2.PolicyDomainCombined {
				t.Fatalf("cross-domain validation unexpectedly used Combined: routing=%s access=%s", routingPlan.Plan.Domain, accessPlan.Plan.Domain)
			}
		})
	}
}

type rootReviewFailAccessRouter struct {
	*policyV2FakeRouter
	failAccess bool
}

func rootReviewAccessFields(fields routeros.RouterOSFields) bool {
	for _, value := range fields {
		value := strings.TrimSpace(fmt.Sprint(value))
		if ownership.IsCanonical(value) {
			continue
		}
		if ownership.IsNamespace(value) || strings.HasPrefix(value, "rb_ac_") || strings.HasPrefix(value, "rbac_") || strings.HasPrefix(value, "rosboard_access_") || strings.Contains(value, "访问控制") {
			return true
		}
	}
	return false
}

func rootReviewAccessObject(object routeros.RouterOSObject) bool {
	for _, value := range object {
		value = strings.TrimSpace(value)
		if ownership.IsCanonical(value) {
			continue
		}
		if ownership.IsNamespace(value) || strings.HasPrefix(value, "rb_ac_") || strings.HasPrefix(value, "rbac_") || strings.HasPrefix(value, "rosboard_access_") || strings.Contains(value, "访问控制") {
			return true
		}
	}
	return false
}

func (r *rootReviewFailAccessRouter) Create(ctx context.Context, menu routeros.MutationMenu, fields routeros.RouterOSFields) (routeros.RouterOSObject, error) {
	if r.failAccess && rootReviewAccessFields(fields) {
		return nil, errors.New("injected access-domain write failure")
	}
	return r.policyV2FakeRouter.Create(ctx, menu, fields)
}

func (r *rootReviewFailAccessRouter) Patch(ctx context.Context, menu routeros.MutationMenu, id string, fields routeros.RouterOSFields) (routeros.RouterOSObject, error) {
	return r.policyV2FakeRouter.Patch(ctx, menu, id, fields)
}

func (r *rootReviewFailAccessRouter) Delete(ctx context.Context, menu routeros.MutationMenu, id string) error {
	return r.policyV2FakeRouter.Delete(ctx, menu, id)
}

func (r *rootReviewFailAccessRouter) Move(ctx context.Context, menu routeros.MutationMenu, request routeros.MoveRequest) (routeros.MutationResponse, error) {
	return r.policyV2FakeRouter.Move(ctx, menu, request)
}

func waitRootReviewSharedJob(t *testing.T, policyRepository *PolicyRepository, accessRepository accesscontrol.Repository, targetID, versionID, initialJobID string, wantFailure bool) policyv2.ApplyJob {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		job, jobErr := policyRepository.GetApplyJob(context.Background(), initialJobID)
		target, targetErr := policyRepository.GetTargetList(context.Background(), targetID)
		policyState, policyErr := policyRepository.GetDeviceState(context.Background())
		accessState, accessErr := accessRepository.GetState(context.Background())
		if jobErr == nil && targetErr == nil && policyErr == nil && accessErr == nil {
			if wantFailure && job.State == "failed" && job.PlanID != "" && job.PlanID != initialJobID {
				return job
			}
			if !wantFailure && job.State == "committed" && job.PlanID != initialJobID && target.ActiveVersionID == versionID && target.PendingVersionID == "" && policyState.Applied() && accessState.Applied() {
				return job
			}
			if job.State == "failed" && !wantFailure {
				t.Fatalf("shared target apply failed: %#v", job)
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	job, err := policyRepository.GetApplyJob(context.Background(), initialJobID)
	if err != nil {
		t.Fatal(err)
	}
	t.Fatalf("timed out waiting for shared target apply: %#v", job)
	return policyv2.ApplyJob{}
}

func waitRootReviewFailedJob(t *testing.T, policyRepository *PolicyRepository, initialJobID string) policyv2.ApplyJob {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		job, err := policyRepository.GetApplyJob(context.Background(), initialJobID)
		if err == nil && job.State == "failed" && job.PlanID != initialJobID {
			return job
		}
		time.Sleep(10 * time.Millisecond)
	}
	job, err := policyRepository.GetApplyJob(context.Background(), initialJobID)
	if err != nil {
		t.Fatal(err)
	}
	t.Fatalf("timed out waiting for follow-up failure: %#v", job)
	return policyv2.ApplyJob{}
}

func waitRootReviewInitialFailedJob(t *testing.T, policyRepository *PolicyRepository, initialJobID, initialPlanID string) policyv2.ApplyJob {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		job, err := policyRepository.GetApplyJob(context.Background(), initialJobID)
		if err == nil && job.State == "failed" && job.PlanID == initialPlanID {
			return job
		}
		time.Sleep(10 * time.Millisecond)
	}
	job, err := policyRepository.GetApplyJob(context.Background(), initialJobID)
	if err != nil {
		t.Fatal(err)
	}
	t.Fatalf("timed out waiting for initial apply failure: %#v", job)
	return policyv2.ApplyJob{}
}
