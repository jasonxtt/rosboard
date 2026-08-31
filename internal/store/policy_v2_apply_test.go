package store

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"rosboard/internal/accesscontrol"
	"rosboard/internal/policyv2"
	"rosboard/internal/routeros"
)

type policyV2FakeRouter struct {
	mu           sync.Mutex
	nextID       int
	objects      map[routeros.MutationMenu]map[string]routeros.RouterOSObject
	order        map[routeros.MutationMenu][]string
	flushes      int
	dnsCacheSize string
	failAt       int
	writes       int
}

func TestPolicyV2ManagerAppliesAccessOnlyPermanentDenyBeforeForeignFilters(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	ctx := context.Background()
	policyRepository := storage.PolicyRepository()
	accessRepository := storage.AccessRepository()
	source, err := policyRepository.SaveSource(ctx, policyv2.Source{ID: "blocked-service", Type: "manual", Kind: policyv2.KindIP, Name: "Blocked service", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := policyRepository.SavePendingSourceVersion(ctx, policyv2.SourceVersion{ID: "blocked-version", SourceID: source.ID, SHA256: "blocked", CompressedYAML: []byte("blocked")}, []policyv2.SourceRule{{RuleType: "IP-CIDR", Domain: "203.0.113.0/24"}}); err != nil {
		t.Fatal(err)
	}
	router := newPolicyV2FakeRouter()
	policyManager := policyv2.NewManager(nil)
	if err := policyManager.RegisterApplier("default", &policyv2.Applier{Reader: router, Mutation: router, Repo: policyRepository}); err != nil {
		t.Fatal(err)
	}
	policyJob, err := policyManager.GenerateAndApply(ctx, "default", "source-initial")
	if err != nil {
		t.Fatal(err)
	}
	if policyJob = waitPolicyV2Job(t, policyRepository, policyJob.ID); policyJob.State != "committed" {
		t.Fatalf("initial source activation failed: %#v", policyJob)
	}
	rule, err := accessRepository.SaveRule(ctx, accesscontrol.AccessRule{
		ID: "deny-a", Name: "阻断测试", TargetScope: accesscontrol.TargetScopeSources, SourceIDs: []string{source.ID}, Enabled: true,
	}, []accesscontrol.RuleMember{{
		RuleID: "deny-a", TerminalID: "addr:10.0.0.20", Binding: accesscontrol.BindingFixed, PinnedIPv4: []string{"10.0.0.20"},
	}}, "test")
	if err != nil {
		t.Fatal(err)
	}

	router.objects[routeros.MenuIPFirewallFilter] = map[string]routeros.RouterOSObject{
		"*f": {".id": "*f", "chain": "forward", "action": "accept", "comment": "existing broad accept"},
	}
	router.order[routeros.MenuIPFirewallFilter] = []string{"*f"}
	manager := policyv2.NewManager(nil)
	if err := manager.RegisterApplier("default", &policyv2.Applier{Reader: router, Mutation: router, Repo: policyRepository, Access: accessRepository}); err != nil {
		t.Fatal(err)
	}
	plan, err := manager.GeneratePlan(ctx, "default", "access-policy")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Plan.Blockers) != 0 {
		t.Fatalf("access plan blocked: %#v", plan.Plan.Blockers)
	}
	job, err := manager.ApplyPlan(ctx, "default", plan.PlanID)
	if err != nil {
		t.Fatalf("apply plan: %v blockers=%#v", err, plan.Plan.Blockers)
	}
	job = waitPolicyV2Job(t, policyRepository, job.ID)
	if job.State != "committed" {
		t.Fatalf("access apply failed: %#v", job)
	}

	filters, err := router.List(ctx, routeros.MenuIPFirewallFilter, routeros.MutationQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(filters) != 6 || filters[0]["action"] != "jump" || filters[1]["action"] != "jump" || filters[2]["comment"] != "existing broad accept" {
		t.Fatalf("managed jumps are not the first forward rules: %#v", filters)
	}
	if filters[3]["action"] != "reject" || filters[3]["reject-with"] != "tcp-reset" || filters[4]["action"] != "drop" || filters[5]["action"] != "drop" {
		t.Fatalf("permanent deny chain actions are incomplete: %#v", filters)
	}
	state, err := accessRepository.GetState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rules, err := accessRepository.ListRules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 || rules[0].ID != rule.ID {
		t.Fatalf("access rule was not loaded back: %#v", rules)
	}
	if !state.Applied() {
		t.Fatalf("access desired state was not committed: state=%#v", state)
	}
	second, err := manager.GeneratePlan(ctx, "default", "access-policy")
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Plan.Operations) != 0 {
		t.Fatalf("access reconcile is not idempotent: %#v", second.Plan.Operations)
	}
	router.mu.Lock()
	router.removeFromOrder(routeros.MenuIPFirewallFilter, "*f")
	router.order[routeros.MenuIPFirewallFilter] = append([]string{"*f"}, router.order[routeros.MenuIPFirewallFilter]...)
	router.mu.Unlock()
	driftPlan, err := manager.GeneratePlan(ctx, "default", "access-order-drift")
	if err != nil {
		t.Fatal(err)
	}
	if driftPlan.Plan.Summary.Move != 2 {
		t.Fatalf("absolute filter-order drift did not produce two planned moves: %#v", driftPlan.Plan.Operations)
	}
	if err := manager.ReconcileAccess(ctx); err != nil {
		t.Fatal(err)
	}
	deviceState, err := policyRepository.GetDeviceState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if reconciled := waitPolicyV2Job(t, policyRepository, deviceState.Job.ID); reconciled.State != "committed" {
		t.Fatalf("periodic access reconcile failed: %#v", reconciled)
	}
	filters, err = router.List(ctx, routeros.MenuIPFirewallFilter, routeros.MutationQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if filters[0]["action"] != "jump" || filters[1]["action"] != "jump" || filters[2]["comment"] != "existing broad accept" {
		t.Fatalf("periodic reconcile did not restore absolute jump order: %#v", filters)
	}
	if err := policyRepository.DeleteSource(ctx, source.ID, source.Revision+1); !errors.Is(err, policyv2.ErrSourceInUse) {
		t.Fatalf("source referenced by access control was deletable: %v", err)
	}
}

func TestAccessReconcileDoesNotApplyDeferredPolicyChanges(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	ctx := context.Background()
	policyRepository := storage.PolicyRepository()
	accessRepository := storage.AccessRepository()
	source, err := policyRepository.SaveSource(ctx, policyv2.Source{ID: "deferred-source", Type: "manual", Kind: policyv2.KindIP, Name: "Deferred source", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := policyRepository.SavePendingSourceVersion(ctx, policyv2.SourceVersion{ID: "deferred-version", SourceID: source.ID, SHA256: "deferred", CompressedYAML: []byte("deferred")}, []policyv2.SourceRule{{RuleType: "IP-CIDR", Domain: "203.0.113.0/24"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := accessRepository.SaveRule(ctx, accesscontrol.AccessRule{ID: "access-deferred", Name: "Deferred access", TargetScope: accesscontrol.TargetScopeSources, SourceIDs: []string{source.ID}, Enabled: true}, []accesscontrol.RuleMember{{RuleID: "access-deferred", TerminalID: "terminal-a", Binding: accesscontrol.BindingFixed, PinnedIPv4: []string{"10.0.0.20"}}}, "test"); err != nil {
		t.Fatal(err)
	}
	router := newPolicyV2FakeRouter()
	manager := policyv2.NewManager(nil)
	if err := manager.RegisterApplier("default", &policyv2.Applier{Reader: router, Mutation: router, Repo: policyRepository, Access: accessRepository}); err != nil {
		t.Fatal(err)
	}
	plan, err := manager.GeneratePlan(ctx, "default", "access-rule-save")
	if err != nil {
		t.Fatal(err)
	}
	if !hasPolicyV2Blocker(plan.Plan.Blockers, "policy_changes_pending") {
		t.Fatalf("access plan must remain blocked while policy changes are deferred: %#v", plan.Plan.Blockers)
	}
	if err := manager.ReconcileAccess(ctx); err != nil {
		t.Fatal(err)
	}
	loadedSource, err := policyRepository.GetSource(ctx, source.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loadedSource.ActiveVersionID != "" || loadedSource.PendingVersionID != "deferred-version" {
		t.Fatalf("access reconcile applied a deferred policy source: %#v", loadedSource)
	}
	state, err := policyRepository.GetDeviceState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if state.Job.ID != "" {
		t.Fatalf("access reconcile created a RouterOS job for a deferred policy change: %#v", state.Job)
	}
}

func TestAccessOnlyApplyDoesNotPromotePolicySources(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	ctx := context.Background()
	policyRepository := storage.PolicyRepository()
	source, err := policyRepository.SaveSource(ctx, policyv2.Source{ID: "active-source", Type: "manual", Kind: policyv2.KindIP, Name: "Active source", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := policyRepository.SavePendingSourceVersion(ctx, policyv2.SourceVersion{ID: "active-version", SourceID: source.ID, SHA256: "active", CompressedYAML: []byte("active")}, []policyv2.SourceRule{{RuleType: "IP-CIDR", Domain: "203.0.113.0/24"}}); err != nil {
		t.Fatal(err)
	}
	router := newPolicyV2FakeRouter()
	policyManager := policyv2.NewManager(nil)
	if err := policyManager.RegisterApplier("default", &policyv2.Applier{Reader: router, Mutation: router, Repo: policyRepository}); err != nil {
		t.Fatal(err)
	}
	job, err := policyManager.GenerateAndApply(ctx, "default", "source-activate")
	if err != nil {
		t.Fatal(err)
	}
	if job = waitPolicyV2Job(t, policyRepository, job.ID); job.State != "committed" {
		t.Fatalf("initial policy source activation failed: %#v", job)
	}
	stateBefore, err := policyRepository.GetDeviceState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	accessRepository := storage.AccessRepository()
	if _, err := accessRepository.SaveRule(ctx, accesscontrol.AccessRule{ID: "access-only", Name: "Access only", TargetScope: accesscontrol.TargetScopeSources, SourceIDs: []string{source.ID}, Enabled: true}, []accesscontrol.RuleMember{{RuleID: "access-only", TerminalID: "terminal-a", Binding: accesscontrol.BindingFixed, PinnedIPv4: []string{"10.0.0.20"}}}, "test"); err != nil {
		t.Fatal(err)
	}
	accessManager := policyv2.NewManager(nil)
	if err := accessManager.RegisterApplier("default", &policyv2.Applier{Reader: router, Mutation: router, Repo: policyRepository, Access: accessRepository}); err != nil {
		t.Fatal(err)
	}
	accessJob, err := accessManager.GenerateAndApply(ctx, "default", "access-rule-save")
	if err != nil {
		t.Fatal(err)
	}
	if accessJob = waitPolicyV2Job(t, policyRepository, accessJob.ID); accessJob.State != "committed" {
		t.Fatalf("access-only apply failed: %#v", accessJob)
	}
	stateAfter, err := policyRepository.GetDeviceState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stateAfter.DesiredRevision != stateBefore.DesiredRevision || stateAfter.AppliedRevision != stateBefore.AppliedRevision {
		t.Fatalf("access-only apply changed policy revision state: before=%#v after=%#v", stateBefore, stateAfter)
	}
	loadedSource, err := policyRepository.GetSource(ctx, source.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loadedSource.ActiveVersionID != "active-version" || loadedSource.PendingVersionID != "" {
		t.Fatalf("access-only apply changed policy source promotion: %#v", loadedSource)
	}
}

func TestAccessSourceOnDisabledEgressIsMaterializedOnlyByAccessProjection(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	ctx := context.Background()
	policyRepository := storage.PolicyRepository()
	accessRepository := storage.AccessRepository()
	if _, err := policyRepository.SaveTrafficIngress(ctx, []byte(`{"interfaceLists":["LAN"],"interfaces":[]}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := policyRepository.SaveEgress(ctx, policyv2.Egress{
		ID: "disabled-wan", Name: "Disabled WAN", ListMode: policyv2.ListModeShared, ListName: "disabled_mark",
		DNSUpstream: "1.1.1.1", FakeAlias: "192.0.2.90", Enabled: true,
		Families: []policyv2.EgressFamily{{Family: policyv2.FamilyIPv4, Enabled: true, WANInterface: "wan4", Gateway: "198.51.100.1"}},
	}); err != nil {
		t.Fatal(err)
	}
	source := saveIPSource(t, policyRepository, "access-ip", "disabled-wan", "Access IP", []policyv2.SourceRule{{RuleType: "IP-CIDR", Domain: "203.0.113.0/24"}})
	router := newPolicyV2FakeRouter()
	policyManager := policyv2.NewManager(nil)
	if err := policyManager.RegisterApplier("default", &policyv2.Applier{Reader: router, Mutation: router, Repo: policyRepository}); err != nil {
		t.Fatal(err)
	}
	initialDesired, err := policyv2.BuildDesired(ctx, policyRepository, router)
	if err != nil {
		t.Fatal(err)
	}
	if len(initialDesired.Blockers) != 0 {
		t.Fatalf("initial source desired is blocked: %#v", initialDesired.Blockers)
	}
	initialPlan, err := policyManager.GeneratePlan(ctx, "default", "activate-source")
	if err != nil {
		t.Fatal(err)
	}
	if len(initialPlan.Plan.Blockers) != 0 {
		t.Fatalf("initial source plan is blocked: %#v", initialPlan.Plan.Blockers)
	}
	job, err := policyManager.ApplyPlan(ctx, "default", initialPlan.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	if job = waitPolicyV2Job(t, policyRepository, job.ID); job.State != "committed" {
		t.Fatalf("initial source apply failed: %#v", job)
	}
	egress, err := policyRepository.GetEgress(ctx, "disabled-wan")
	if err != nil {
		t.Fatal(err)
	}
	egress.Enabled = false
	if _, err := policyRepository.SaveEgress(ctx, egress); err != nil {
		t.Fatal(err)
	}
	if _, err := accessRepository.SaveRule(ctx, accesscontrol.AccessRule{
		ID: "access-rule", Name: "Access rule", TargetScope: accesscontrol.TargetScopeSources, SourceIDs: []string{source.ID}, Enabled: true,
	}, []accesscontrol.RuleMember{{RuleID: "access-rule", TerminalID: "terminal-a", Binding: accesscontrol.BindingFixed, PinnedIPv4: []string{"10.0.0.20"}}}, "test"); err != nil {
		t.Fatal(err)
	}
	desired, err := policyv2.BuildDesiredWithAccess(ctx, policyRepository, router, accessRepository, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(desired.Blockers) != 0 {
		t.Fatalf("disabled-egress access projection is blocked: %#v", desired.Blockers)
	}
	entries := desiredObjectsByLogicalPrefix(desired.Objects, "source-addr:"+source.ID+":")
	if len(entries) != 1 {
		t.Fatalf("disabled-egress access source must have one address projection, got %#v", entries)
	}
	if entries[0].Fields["disabled"] != "no" {
		t.Fatalf("access source projection must remain enabled on a disabled egress: %#v", entries[0])
	}
}

func hasPolicyV2Blocker(blockers []policyv2.PlanIssue, code string) bool {
	for _, blocker := range blockers {
		if blocker.Code == code {
			return true
		}
	}
	return false
}

func routerHasList(router *policyV2FakeRouter, menu routeros.MutationMenu, list string) bool {
	for _, object := range router.objects[menu] {
		if object["list"] == list {
			return true
		}
	}
	return false
}

func routerHasDestinationList(router *policyV2FakeRouter, menu routeros.MutationMenu, list string) bool {
	for _, object := range router.objects[menu] {
		if object["dst-address-list"] == list {
			return true
		}
	}
	return false
}

func newPolicyV2FakeRouter() *policyV2FakeRouter {
	return &policyV2FakeRouter{
		objects: make(map[routeros.MutationMenu]map[string]routeros.RouterOSObject),
		order:   make(map[routeros.MutationMenu][]string),
	}
}

func (r *policyV2FakeRouter) PolicyList(_ context.Context, menu routeros.ReadMenu, _ []string) ([]routeros.RouterOSObject, error) {
	if menu == routeros.ReadMenuInterfaceList {
		return []routeros.RouterOSObject{{"name": "LAN"}}, nil
	}
	if menu == routeros.ReadMenuIPDNS && r.dnsCacheSize != "" {
		return []routeros.RouterOSObject{{"cache-size": r.dnsCacheSize}}, nil
	}
	return []routeros.RouterOSObject{}, nil
}

func (r *policyV2FakeRouter) List(_ context.Context, menu routeros.MutationMenu, _ routeros.MutationQuery) ([]routeros.RouterOSObject, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]routeros.RouterOSObject, 0, len(r.objects[menu]))
	seen := make(map[string]bool, len(r.order[menu]))
	for _, id := range r.order[menu] {
		if object := r.objects[menu][id]; object != nil {
			result = append(result, clonePolicyV2RouterObject(object))
			seen[id] = true
		}
	}
	missing := make([]string, 0, len(r.objects[menu])-len(result))
	for id := range r.objects[menu] {
		if !seen[id] {
			missing = append(missing, id)
		}
	}
	sort.Strings(missing)
	for _, id := range missing {
		result = append(result, clonePolicyV2RouterObject(r.objects[menu][id]))
	}
	return result, nil
}

func (r *policyV2FakeRouter) Create(_ context.Context, menu routeros.MutationMenu, fields routeros.RouterOSFields) (routeros.RouterOSObject, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.beforeWrite(); err != nil {
		return nil, err
	}
	r.nextID++
	id := fmt.Sprintf("*%d", r.nextID)
	object := routeros.RouterOSObject{".id": id}
	for key, value := range fields {
		object[key] = fmt.Sprint(value)
	}
	if r.objects[menu] == nil {
		r.objects[menu] = make(map[string]routeros.RouterOSObject)
	}
	r.objects[menu][id] = object
	r.order[menu] = append(r.order[menu], id)
	return clonePolicyV2RouterObject(object), nil
}

func (r *policyV2FakeRouter) Patch(_ context.Context, menu routeros.MutationMenu, id string, fields routeros.RouterOSFields) (routeros.RouterOSObject, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.beforeWrite(); err != nil {
		return nil, err
	}
	object := r.objects[menu][id]
	if object == nil {
		return nil, fmt.Errorf("missing object %s", id)
	}
	for key, value := range fields {
		object[key] = fmt.Sprint(value)
	}
	return clonePolicyV2RouterObject(object), nil
}

func (r *policyV2FakeRouter) Delete(_ context.Context, menu routeros.MutationMenu, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.beforeWrite(); err != nil {
		return err
	}
	delete(r.objects[menu], id)
	r.removeFromOrder(menu, id)
	return nil
}

func (r *policyV2FakeRouter) Move(_ context.Context, menu routeros.MutationMenu, request routeros.MoveRequest) (routeros.MutationResponse, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.beforeWrite(); err != nil {
		return routeros.MutationResponse{}, err
	}
	if r.objects[menu][request.ID] == nil || r.objects[menu][request.BeforeID] == nil {
		return routeros.MutationResponse{}, fmt.Errorf("missing move object")
	}
	r.removeFromOrder(menu, request.ID)
	order := r.order[menu]
	for index, id := range order {
		if id == request.BeforeID {
			order = append(append(append([]string(nil), order[:index]...), request.ID), order[index:]...)
			r.order[menu] = order
			return routeros.MutationResponse{}, nil
		}
	}
	return routeros.MutationResponse{}, fmt.Errorf("missing move destination")
}

func (r *policyV2FakeRouter) removeFromOrder(menu routeros.MutationMenu, id string) {
	order := r.order[menu]
	for index, current := range order {
		if current == id {
			r.order[menu] = append(order[:index], order[index+1:]...)
			return
		}
	}
}

func (r *policyV2FakeRouter) SetDNSSettings(_ context.Context, fields routeros.RouterOSFields) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if value, ok := fields["cache-size"]; ok {
		r.dnsCacheSize = fmt.Sprint(value)
	}
	return nil
}

func (r *policyV2FakeRouter) FlushDNSCache(context.Context) error {
	r.mu.Lock()
	r.flushes++
	r.mu.Unlock()
	return nil
}

func (r *policyV2FakeRouter) VerifyAccessControlCapabilities(context.Context, []routeros.MutationMenu) error {
	return nil
}

func (r *policyV2FakeRouter) beforeWrite() error {
	r.writes++
	if r.failAt > 0 && r.writes == r.failAt {
		return fmt.Errorf("injected write failure")
	}
	return nil
}

func clonePolicyV2RouterObject(object routeros.RouterOSObject) routeros.RouterOSObject {
	result := make(routeros.RouterOSObject, len(object))
	for key, value := range object {
		result[key] = value
	}
	return result
}

func TestPolicyV2ManagerAppliesAndCommitsSingleIPv4Egress(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	repository := storage.PolicyRepository()
	ctx := context.Background()
	egress, err := repository.SaveEgress(ctx, policyv2.Egress{
		ID: "wan-a", Name: "WAN A", Priority: 10, ListMode: policyv2.ListModeShared, ListName: "route_wan_a",
		DNSUpstream: "1.1.1.1", FakeAlias: "192.0.2.53", Enabled: true,
		Families: []policyv2.EgressFamily{{Family: policyv2.FamilyIPv4, Enabled: true, WANInterface: "ether2", Gateway: "198.51.100.1", RouteMode: "strict", NATMode: "masquerade"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	source, err := repository.SaveSource(ctx, policyv2.Source{ID: "source-a", EgressID: egress.ID, Type: "upload", Name: "OpenAI", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.SavePendingSourceVersion(ctx, policyv2.SourceVersion{ID: "version-a", SourceID: source.ID, SHA256: "abc", CompressedYAML: []byte("gzip"), Counts: map[string]int{"valid": 2}}, []policyv2.SourceRule{{RuleType: "DOMAIN", Domain: "api.openai.com"}, {RuleType: "DOMAIN-SUFFIX", Domain: "openai.com"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.SaveTrafficIngress(ctx, []byte(`{"interfaceLists":["LAN"],"interfaces":[]}`)); err != nil {
		t.Fatal(err)
	}

	router := newPolicyV2FakeRouter()
	router.objects[routeros.MenuIPFirewallMangle] = map[string]routeros.RouterOSObject{
		"*foreign": {".id": "*foreign", "chain": "prerouting", "action": "accept", "comment": "manual rule"},
	}
	manager := policyv2.NewManager(nil)
	if err := manager.RegisterApplier("default", &policyv2.Applier{Reader: router, Mutation: router, Repo: repository}); err != nil {
		t.Fatal(err)
	}
	envelope, err := manager.GeneratePlan(ctx, "default", "initial")
	if err != nil {
		t.Fatal(err)
	}
	if envelope.Plan.State != "ready" || envelope.Plan.Summary.Create == 0 || envelope.Plan.Summary.Delete != 0 {
		t.Fatalf("unexpected initial plan: %#v", envelope.Plan)
	}
	job, err := manager.ApplyPlan(ctx, "default", envelope.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	job = waitPolicyV2Job(t, repository, job.ID)
	if job.State != "committed" {
		t.Fatalf("apply failed: %#v", job)
	}
	state, err := repository.GetDeviceState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !state.Applied() || state.AppliedHash == "" || router.flushes != 1 || router.dnsCacheSize != "32768KiB" {
		t.Fatalf("apply was not committed: state=%#v flushes=%d", state, router.flushes)
	}
	if router.objects[routeros.MenuIPFirewallMangle]["*foreign"] == nil {
		t.Fatal("apply modified a foreign RouterOS object")
	}
	loadedSource, err := repository.GetSource(ctx, source.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loadedSource.ActiveVersionID != "version-a" || loadedSource.PendingVersionID != "" {
		t.Fatalf("source version was not activated: %#v", loadedSource)
	}

	second, err := manager.GeneratePlan(ctx, "default", "structural")
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Plan.Operations) != 0 {
		t.Fatalf("idempotent plan still has operations: %#v", second.Plan.Operations)
	}
	router.mu.Lock()
	router.dnsCacheSize = "62768KiB"
	router.mu.Unlock()

	loadedEgress, err := repository.GetEgress(ctx, egress.ID)
	if err != nil {
		t.Fatal(err)
	}
	loadedEgress.Name = "unicom"
	if _, err := repository.SaveEgress(ctx, loadedEgress); err != nil {
		t.Fatal(err)
	}
	renamePlan, err := manager.GeneratePlan(ctx, "default", "structural")
	if err != nil {
		t.Fatal(err)
	}
	commentPatches := 0
	for _, operation := range renamePlan.Plan.Operations {
		if operation.Action == "create" || operation.Action == "delete" {
			t.Fatalf("strategy rename replaced a managed object instead of patching its readable comment: %#v", operation)
		}
		if strings.Contains(operation.After["comment"], "策略 unicom") {
			commentPatches++
		}
	}
	if commentPatches == 0 {
		t.Fatalf("strategy rename did not produce readable comment patches: %#v", renamePlan.Plan.Operations)
	}
	renameJob, err := manager.ApplyPlan(ctx, "default", renamePlan.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	if renameJob = waitPolicyV2Job(t, repository, renameJob.ID); renameJob.State != "committed" {
		t.Fatalf("strategy rename apply failed: %#v", renameJob)
	}
	router.mu.Lock()
	cacheSizeAfterRename := router.dnsCacheSize
	router.mu.Unlock()
	if cacheSizeAfterRename != "62768KiB" {
		t.Fatalf("manual DNS cache size was overwritten: %q", cacheSizeAfterRename)
	}

	loadedEgress, err = repository.GetEgress(ctx, egress.ID)
	if err != nil {
		t.Fatal(err)
	}
	loadedEgress.Families[0].Gateway = "198.51.100.2"
	if _, err := repository.SaveEgress(ctx, loadedEgress); err != nil {
		t.Fatal(err)
	}
	failedPlan, err := manager.GeneratePlan(ctx, "default", "structural")
	if err != nil {
		t.Fatal(err)
	}
	router.failAt = router.writes + 1
	failedJob, err := manager.ApplyPlan(ctx, "default", failedPlan.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	failedJob = waitPolicyV2Job(t, repository, failedJob.ID)
	if failedJob.State != "failed" {
		t.Fatalf("injected failure was not recorded: %#v", failedJob)
	}
	router.failAt = 0
	retryPlan, err := manager.GeneratePlan(ctx, "default", "structural")
	if err != nil {
		t.Fatal(err)
	}
	retryJob, err := manager.ApplyPlan(ctx, "default", retryPlan.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	if retryJob = waitPolicyV2Job(t, repository, retryJob.ID); retryJob.State != "committed" {
		t.Fatalf("retry did not converge: %#v", retryJob)
	}
	stalePlan, err := manager.GeneratePlan(ctx, "default", "structural")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.SaveTrafficIngress(ctx, []byte(`{"interfaceLists":["LAN-CHANGED"],"interfaces":[]}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ApplyPlan(ctx, "default", stalePlan.PlanID); err != policyv2.ErrPlanStale {
		t.Fatalf("stale plan error=%v", err)
	}
}

func TestPolicyV2DesiredSupportsDualStackAndDedicatedLists(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	repository := storage.PolicyRepository()
	ctx := context.Background()
	_, err = repository.SaveEgress(ctx, policyv2.Egress{
		ID: "dual", Name: "Dual", ListMode: policyv2.ListModeDedicated, DNSUpstream: "1.1.1.1", FakeAlias: "192.0.2.60", FailureMode: "fallback", Enabled: true,
		Families: []policyv2.EgressFamily{
			{Family: policyv2.FamilyIPv4, Enabled: true, WANInterface: "wan4", Gateway: "198.51.100.1", RouteTable: "dual4"},
			{Family: policyv2.FamilyIPv6, Enabled: true, WANInterface: "wan6", Gateway: "2001:db8:1::1", RouteTable: "dual6", NATMode: "none"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.SaveTrafficIngress(ctx, []byte(`{"interfaceLists":["LAN"],"interfaces":[]}`)); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"one", "two"} {
		source, err := repository.SaveSource(ctx, policyv2.Source{ID: id, EgressID: "dual", Type: "upload", Name: id, Enabled: true})
		if err != nil {
			t.Fatal(err)
		}
		if err := repository.SavePendingSourceVersion(ctx, policyv2.SourceVersion{ID: "version-" + id, SourceID: id, SHA256: id, CompressedYAML: []byte("gzip")}, []policyv2.SourceRule{{RuleType: "DOMAIN", Domain: id + ".example"}}); err != nil {
			t.Fatal(err)
		}
		_ = source
	}
	router := newPolicyV2FakeRouter()
	desired, err := policyv2.BuildDesired(ctx, repository, router)
	if err != nil {
		t.Fatal(err)
	}
	if len(desired.Blockers) != 0 {
		t.Fatalf("dual-stack desired is blocked: %#v", desired.Blockers)
	}
	hasIPv6Route := false
	hasIPv4ReturnGuard := false
	hasIPv6ReturnGuard := false
	lists := make(map[string]bool)
	hasReadableStrategyComment := false
	hasReadableSourceComment := false
	for _, object := range desired.Objects {
		if object.Menu == string(routeros.MenuIPv6Route) && object.Fields["dst-address"] == "::/0" {
			hasIPv6Route = true
		}
		if object.Fields["action"] == "mark-routing" && object.Fields["chain"] == "prerouting" && object.Fields["dst-address-type"] == "!local" {
			if object.Menu == string(routeros.MenuIPFirewallMangle) {
				hasIPv4ReturnGuard = true
			}
			if object.Menu == string(routeros.MenuIPv6FirewallMangle) {
				hasIPv6ReturnGuard = true
			}
		}
		if value := object.Fields["dst-address-list"]; value != "" {
			lists[value] = true
		}
		if strings.Contains(object.Fields["comment"], "策略 Dual") {
			hasReadableStrategyComment = true
		}
		if object.Menu == string(routeros.MenuIPDNSStatic) && strings.Contains(object.Fields["comment"], "域名列表 one") {
			hasReadableSourceComment = true
		}
	}
	if !hasIPv6Route || !hasIPv4ReturnGuard || !hasIPv6ReturnGuard || len(lists) != 2 {
		t.Fatalf("dual/dedicated objects missing: ipv6=%v ipv4-return-guard=%v ipv6-return-guard=%v lists=%v", hasIPv6Route, hasIPv4ReturnGuard, hasIPv6ReturnGuard, lists)
	}
	for listName := range lists {
		if !strings.HasPrefix(listName, "rb_src_") {
			t.Fatalf("dedicated source list name is not stable and source-scoped: %q", listName)
		}
	}
	if !hasReadableStrategyComment || !hasReadableSourceComment {
		t.Fatalf("readable comments missing: strategy=%v source=%v", hasReadableStrategyComment, hasReadableSourceComment)
	}
}

func TestPolicyV2DesiredAggregatesListsAndWireGuardIngress(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	repository := storage.PolicyRepository()
	ctx := context.Background()
	if _, err := repository.SaveTrafficIngress(ctx, []byte(`{"interfaceLists":["VPN-LAN","LAN"],"interfaces":["wireguard1"]}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.SaveEgress(ctx, policyv2.Egress{
		ID: "wg", Name: "WireGuard ingress", ListMode: policyv2.ListModeShared, ListName: "route_wg", DNSUpstream: "1.1.1.1", FakeAlias: "192.0.2.80", Enabled: true,
		Families: []policyv2.EgressFamily{{Family: policyv2.FamilyIPv4, Enabled: true, WANInterface: "ether1", Gateway: "198.51.100.1"}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.SaveSource(ctx, policyv2.Source{ID: "wg-source", EgressID: "wg", Type: "upload", Name: "WG", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := repository.SavePendingSourceVersion(ctx, policyv2.SourceVersion{ID: "wg-version", SourceID: "wg-source", SHA256: "wg", CompressedYAML: []byte("wg")}, []policyv2.SourceRule{{RuleType: "DOMAIN", Domain: "example.com"}}); err != nil {
		t.Fatal(err)
	}
	router := newPolicyV2FakeRouter()
	desired, err := policyv2.BuildDesired(ctx, repository, router)
	if err != nil {
		t.Fatal(err)
	}
	var ingressList string
	hasWireGuardMember := false
	hasMangleReference := false
	for _, object := range desired.Objects {
		switch object.LogicalID {
		case "traffic-ingress:list":
			ingressList = object.Fields["name"]
			if object.Fields["include"] != "LAN,VPN-LAN" {
				t.Fatalf("unexpected included lists: %#v", object.Fields)
			}
		case "traffic-ingress:member:wireguard1":
			hasWireGuardMember = object.Fields["interface"] == "wireguard1"
		}
	}
	for _, object := range desired.Objects {
		if object.Menu == string(routeros.MenuIPFirewallMangle) && object.Fields["chain"] == "prerouting" && object.Fields["in-interface-list"] == ingressList {
			hasMangleReference = true
		}
	}
	if ingressList == "" || !hasWireGuardMember || !hasMangleReference {
		t.Fatalf("aggregate ingress objects missing: list=%q member=%v mangle=%v", ingressList, hasWireGuardMember, hasMangleReference)
	}
}

func TestPolicyV2DisableAndDeletePreserveSharedPolicyObjects(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	repository := storage.PolicyRepository()
	ctx := context.Background()
	if _, err := repository.SaveTrafficIngress(ctx, []byte(`{"interfaceLists":["LAN"],"interfaces":[]}`)); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"one", "two"} {
		_, err := repository.SaveEgress(ctx, policyv2.Egress{
			ID: id, Name: id, Priority: 10, ListMode: policyv2.ListModeShared, ListName: "shared_mark",
			DNSUpstream: "1.1.1.1", FakeAlias: map[string]string{"one": "192.0.2.81", "two": "192.0.2.82"}[id], Enabled: true,
			Families: []policyv2.EgressFamily{{Family: policyv2.FamilyIPv4, Enabled: true, WANInterface: "ether1", Gateway: "198.51.100.1", NATMode: "masquerade"}},
		})
		if err != nil {
			t.Fatal(err)
		}
		source, err := repository.SaveSource(ctx, policyv2.Source{ID: "source-" + id, EgressID: id, Type: "upload", Name: id, Enabled: true})
		if err != nil {
			t.Fatal(err)
		}
		if err := repository.SavePendingSourceVersion(ctx, policyv2.SourceVersion{ID: "version-" + id, SourceID: source.ID, SHA256: id, CompressedYAML: []byte(id)}, []policyv2.SourceRule{{RuleType: "DOMAIN", Domain: id + ".example"}}); err != nil {
			t.Fatal(err)
		}
	}

	router := newPolicyV2FakeRouter()
	manager := policyv2.NewManager(nil)
	if err := manager.RegisterApplier("default", &policyv2.Applier{Reader: router, Mutation: router, Repo: repository}); err != nil {
		t.Fatal(err)
	}
	initial, err := policyv2.BuildDesired(ctx, repository, router)
	if err != nil {
		t.Fatal(err)
	}
	job, err := manager.GenerateAndApply(ctx, "default", "initial")
	if err != nil {
		t.Fatal(err)
	}
	if job = waitPolicyV2Job(t, repository, job.ID); job.State != "committed" {
		t.Fatalf("initial apply failed: %#v", job)
	}

	one, err := repository.GetEgress(ctx, "one")
	if err != nil {
		t.Fatal(err)
	}
	one.Enabled = false
	if _, err := repository.SaveEgress(ctx, one); err != nil {
		t.Fatal(err)
	}
	job, err = manager.GenerateAndApply(ctx, "default", "egress-state")
	if err != nil {
		t.Fatal(err)
	}
	if job = waitPolicyV2Job(t, repository, job.ID); job.State != "committed" {
		t.Fatalf("disable apply failed: %#v", job)
	}
	disabledDesired, err := policyv2.BuildDesired(ctx, repository, router)
	if err != nil {
		t.Fatal(err)
	}
	for _, object := range disabledDesired.Objects {
		if !strings.Contains(object.LogicalID, "one") || object.Fields["disabled"] == "" {
			continue
		}
		actual := findPolicyV2ObjectByComment(router, routeros.MutationMenu(object.Menu), object.Fields["comment"])
		if actual == nil || actual["disabled"] != "true" {
			t.Fatalf("disabled object did not stay present and disabled: desired=%#v actual=%#v", object, actual)
		}
	}
	for _, object := range disabledDesired.Objects {
		if !strings.Contains(object.LogicalID, "two") || object.Fields["disabled"] == "" {
			continue
		}
		actual := findPolicyV2ObjectByComment(router, routeros.MutationMenu(object.Menu), object.Fields["comment"])
		if actual == nil || actual["disabled"] == "true" {
			t.Fatalf("shared peer was disabled with egress one: desired=%#v actual=%#v", object, actual)
		}
	}

	one, err = repository.GetEgress(ctx, "one")
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.DeleteEgress(ctx, "one", one.Revision); err != nil {
		t.Fatal(err)
	}
	job, err = manager.GenerateAndApply(ctx, "default", "egress-delete")
	if err != nil {
		t.Fatal(err)
	}
	if job = waitPolicyV2Job(t, repository, job.ID); job.State != "committed" {
		t.Fatalf("delete apply failed: %#v", job)
	}
	if _, err := repository.GetEgress(ctx, "one"); err != policyv2.ErrEgressNotFound {
		t.Fatalf("deleted egress remains: %v", err)
	}
	for _, object := range initial.Objects {
		if !strings.Contains(object.LogicalID, "one") {
			continue
		}
		if object.Menu == string(routeros.MenuRoutingTable) {
			if findPolicyV2ObjectByName(router, routeros.MenuRoutingTable, object.Fields["name"]) != nil {
				t.Fatalf("deleted egress routing table remains: %#v", object)
			}
			continue
		}
		if findPolicyV2ObjectByComment(router, routeros.MutationMenu(object.Menu), object.Fields["comment"]) != nil {
			t.Fatalf("deleted egress object remains: %#v", object)
		}
	}
	if peer, err := repository.GetEgress(ctx, "two"); err != nil || !peer.Applied {
		t.Fatalf("shared peer was removed or uncommitted: peer=%#v err=%v", peer, err)
	}
	managerID, err := repository.ManagerInstanceID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ingressListName := policyv2.ManagedIngressListName(managerID, repository.DeviceID())
	if findPolicyV2ObjectByName(router, routeros.MenuInterfaceList, ingressListName) == nil {
		t.Fatal("shared ingress list was removed while another policy still used it")
	}
	two, err := repository.GetEgress(ctx, "two")
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.DeleteEgress(ctx, "two", two.Revision); err != nil {
		t.Fatal(err)
	}
	job, err = manager.GenerateAndApply(ctx, "default", "egress-delete-last")
	if err != nil {
		t.Fatal(err)
	}
	if job = waitPolicyV2Job(t, repository, job.ID); job.State != "committed" {
		t.Fatalf("last delete apply failed: %#v", job)
	}
	if findPolicyV2ObjectByName(router, routeros.MenuInterfaceList, ingressListName) != nil {
		t.Fatal("aggregate traffic ingress list remains after the last policy was deleted")
	}
}

func findPolicyV2ObjectByComment(router *policyV2FakeRouter, menu routeros.MutationMenu, comment string) routeros.RouterOSObject {
	router.mu.Lock()
	defer router.mu.Unlock()
	for _, object := range router.objects[menu] {
		if object["comment"] == comment {
			return clonePolicyV2RouterObject(object)
		}
	}
	return nil
}

func findPolicyV2ObjectByName(router *policyV2FakeRouter, menu routeros.MutationMenu, name string) routeros.RouterOSObject {
	router.mu.Lock()
	defer router.mu.Unlock()
	for _, object := range router.objects[menu] {
		if object["name"] == name {
			return clonePolicyV2RouterObject(object)
		}
	}
	return nil
}

func TestPolicyV2ScheduledRefreshCreatesPendingVersion(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	repository := storage.PolicyRepository()
	ctx := context.Background()
	source, err := repository.SaveSource(ctx, policyv2.Source{ID: "scheduled", Type: "url", Name: "Scheduled", URL: "https://example.invalid/list.yaml", Schedule: "1h", Enabled: true, NextRunAt: time.Now().Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	router := newPolicyV2FakeRouter()
	manager := policyv2.NewManager(nil)
	refreshes := 0
	if err := manager.RegisterApplier("default", &policyv2.Applier{
		Reader: router, Mutation: router, Repo: repository,
		Refresh: func(context.Context, policyv2.Source) (policyv2.SourceRefresh, error) {
			refreshes++
			version := policyv2.SourceVersion{ID: "scheduled-version", SourceID: source.ID, SHA256: "sha", CompressedYAML: []byte("gzip"), State: "pending"}
			return policyv2.SourceRefresh{Version: &version, Rules: []policyv2.SourceRule{{RuleType: "DOMAIN", Domain: "scheduled.example"}}}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := manager.RefreshDue(ctx, now); err != nil {
		t.Fatal(err)
	}
	loaded, err := repository.GetSource(ctx, source.ID)
	if err != nil {
		t.Fatal(err)
	}
	if refreshes != 1 || loaded.PendingVersionID != "scheduled-version" || !loaded.NextRunAt.After(now) {
		t.Fatalf("scheduled refresh not persisted: refreshes=%d source=%#v", refreshes, loaded)
	}
}

func TestPolicyV2ScheduledRefreshAutoAppliesAssignedSource(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	repository := storage.PolicyRepository()
	ctx := context.Background()
	if _, err := repository.SaveEgress(ctx, policyv2.Egress{
		ID: "scheduled-wan", Name: "Scheduled WAN", ListMode: policyv2.ListModeShared, ListName: "scheduled", DNSUpstream: "1.1.1.1", FakeAlias: "192.0.2.71", FailureMode: "strict", Enabled: true,
		Families: []policyv2.EgressFamily{{Family: policyv2.FamilyIPv4, Enabled: true, Gateway: "198.51.100.2"}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.SaveTrafficIngress(ctx, []byte(`{"interfaceLists":["LAN"],"interfaces":[]}`)); err != nil {
		t.Fatal(err)
	}
	source, err := repository.SaveSource(ctx, policyv2.Source{
		ID: "scheduled-assigned", EgressID: "scheduled-wan", Type: "url", Name: "Scheduled", URL: "https://example.invalid/list.yaml", Schedule: "1h", Enabled: true, NextRunAt: time.Now().Add(-time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	router := newPolicyV2FakeRouter()
	manager := policyv2.NewManager(nil)
	if err := manager.RegisterApplier("default", &policyv2.Applier{
		Reader: router, Mutation: router, Repo: repository,
		Refresh: func(context.Context, policyv2.Source) (policyv2.SourceRefresh, error) {
			version := policyv2.SourceVersion{ID: "scheduled-assigned-version", SourceID: source.ID, SHA256: "sha", CompressedYAML: []byte("gzip"), State: "pending"}
			return policyv2.SourceRefresh{Version: &version, Rules: []policyv2.SourceRule{{RuleType: "DOMAIN", Domain: "scheduled.example"}}}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := manager.RefreshDue(ctx, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	state, err := repository.GetDeviceState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	job := waitPolicyV2Job(t, repository, state.Job.ID)
	if job.State != "committed" {
		t.Fatalf("scheduled source auto-apply failed: %#v", job)
	}
	loaded, err := repository.GetSource(ctx, source.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ActiveVersionID != "scheduled-assigned-version" || loaded.PendingVersionID != "" {
		t.Fatalf("scheduled source version was not activated: %#v", loaded)
	}
}

func TestDisablingAccessReferencedSourceUpdatesItsSharedProjection(t *testing.T) {
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
	if err := policyRepository.SavePendingSourceVersion(ctx, policyv2.SourceVersion{ID: "version-a", SourceID: source.ID, SHA256: "source-a", CompressedYAML: []byte("source-a")}, []policyv2.SourceRule{{RuleType: "IP-CIDR", Domain: "203.0.113.0/24"}}); err != nil {
		t.Fatal(err)
	}
	router := newPolicyV2FakeRouter()
	policyManager := policyv2.NewManager(nil)
	if err := policyManager.RegisterApplier("default", &policyv2.Applier{Reader: router, Mutation: router, Repo: policyRepository}); err != nil {
		t.Fatal(err)
	}
	initialJob, err := policyManager.GenerateAndApply(ctx, "default", "source-initial")
	if err != nil {
		t.Fatal(err)
	}
	if initialJob = waitPolicyV2Job(t, policyRepository, initialJob.ID); initialJob.State != "committed" {
		t.Fatalf("initial source activation failed: %#v", initialJob)
	}
	if _, err := accessRepository.SaveRule(ctx, accesscontrol.AccessRule{ID: "rule-a", Name: "Access A", TargetScope: accesscontrol.TargetScopeSources, SourceIDs: []string{source.ID}, Enabled: true}, []accesscontrol.RuleMember{{RuleID: "rule-a", TerminalID: "t1", Binding: accesscontrol.BindingFixed, PinnedIPv4: []string{"10.0.0.20"}}}, "test"); err != nil {
		t.Fatal(err)
	}
	manager := policyv2.NewManager(nil)
	if err := manager.RegisterApplier("default", &policyv2.Applier{Reader: router, Mutation: router, Repo: policyRepository, Access: accessRepository}); err != nil {
		t.Fatal(err)
	}
	job, err := manager.GenerateAndApply(ctx, "default", "source-initial")
	if err != nil {
		t.Fatal(err)
	}
	if job = waitPolicyV2Job(t, policyRepository, job.ID); job.State != "committed" {
		t.Fatalf("initial access source apply failed: %#v", job)
	}

	source, err = policyRepository.GetSource(ctx, source.ID)
	if err != nil {
		t.Fatal(err)
	}
	source.Enabled = false
	source, err = policyRepository.SaveSource(ctx, source)
	if err != nil {
		t.Fatal(err)
	}
	if !policyv2.SourceAutoApplyEligible(ctx, policyRepository, source, accessRepository) {
		t.Fatal("disabling a source referenced by an enabled access rule must trigger auto-apply")
	}
	job, err = manager.GenerateAndApply(ctx, "default", "source-disable")
	if err != nil {
		t.Fatal(err)
	}
	if job = waitPolicyV2Job(t, policyRepository, job.ID); job.State != "committed" {
		t.Fatalf("disabled access source apply failed: %#v", job)
	}
	managerID, err := policyRepository.ManagerInstanceID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	listName := policyv2.SourceListName(managerID, policyRepository.DeviceID(), source)
	objects, err := router.List(ctx, routeros.MenuIPFirewallAddressList, routeros.MutationQuery{})
	if err != nil {
		t.Fatal(err)
	}
	foundDisabled := false
	for _, object := range objects {
		if object["list"] == listName && object["address"] == "203.0.113.0/24" {
			foundDisabled = object["disabled"] == "true" || object["disabled"] == "yes"
		}
	}
	if !foundDisabled {
		t.Fatalf("disabled source must leave its shared address-list projection disabled, not stale and active: %#v", objects)
	}
}

func waitPolicyV2Job(t *testing.T, repository *PolicyRepository, id string) policyv2.ApplyJob {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		job, err := repository.GetApplyJob(context.Background(), id)
		if err == nil && job.Terminal() {
			return job
		}
		time.Sleep(10 * time.Millisecond)
	}
	job, err := repository.GetApplyJob(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return job
}
