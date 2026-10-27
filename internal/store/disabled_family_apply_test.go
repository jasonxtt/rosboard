package store

import (
	"context"
	"strings"
	"testing"

	"rosboard/internal/policyv2"
	"rosboard/internal/routeros"
)

func TestDisabledFamilyApplyTransitionsAndReplay(t *testing.T) {
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	ctx := context.Background()
	repo := storage.PolicyRepository()
	_, err = repo.SaveEgress(ctx, policyv2.Egress{ID: "wan", Name: "WAN", Enabled: true, Families: []policyv2.EgressFamily{{Family: policyv2.FamilyIPv4, Enabled: true, Gateway: "198.51.100.1", WANInterface: "ether1"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.SaveTrafficIngress(ctx, []byte(`{"interfaceLists":["LAN"],"interfaces":[]}`)); err != nil {
		t.Fatal(err)
	}
	saveIPSource(t, repo, "targets", "wan", "targets", []policyv2.SourceRule{{RuleType: "IP-CIDR", Domain: "192.0.2.0/24"}, {RuleType: "IP-CIDR6", Domain: "2001:db8::/32"}})
	router := newPolicyV2FakeRouter()
	manager := policyv2.NewManager(nil)
	if err = manager.RegisterApplier("default", &policyv2.Applier{Reader: router, Mutation: router, Repo: repo}); err != nil {
		t.Fatal(err)
	}
	apply := func() {
		t.Helper()
		job, e := manager.GenerateAndApply(ctx, "default", "structural")
		if e != nil {
			t.Fatal(e)
		}
		if job = waitPolicyV2Job(t, repo, job.ID); job.State != "committed" {
			t.Fatalf("apply: %#v", job)
		}
	}
	plan, err := manager.GeneratePlan(ctx, "default", "structural")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, op := range plan.Plan.Operations {
		if strings.HasPrefix(op.LogicalID, "routing-rule-block:") && op.Menu == string(routeros.MenuIPv6FirewallFilter) && op.After["action"] == "drop" {
			found = true
		}
	}
	if !found || len(plan.Plan.Warnings) == 0 {
		t.Fatalf("preview missing block/warning: %#v", plan.Plan)
	}
	apply()
	plan, err = manager.GeneratePlan(ctx, "default", "replay")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Plan.Operations) != 0 {
		t.Fatalf("replay operations: %#v", plan.Plan.Operations)
	}
	e, err := repo.GetEgress(ctx, "wan")
	if err != nil {
		t.Fatal(err)
	}
	e.Families = append(e.Families, policyv2.EgressFamily{Family: policyv2.FamilyIPv6, Enabled: true, Gateway: "2001:db8::1", WANInterface: "ether1"})
	if _, err = repo.SaveEgress(ctx, e); err != nil {
		t.Fatal(err)
	}
	apply()
	desired, err := policyv2.BuildDesired(ctx, repo, router)
	if err != nil {
		t.Fatal(err)
	}
	if got := desiredObjectsByLogicalPrefix(desired.Objects, "routing-rule-block:wan:ipv6:"); len(got) != 0 {
		t.Fatalf("block remained after family enabled: %#v", got)
	}
	plan, err = manager.GeneratePlan(ctx, "default", "replay")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Plan.Operations) != 0 {
		t.Fatalf("dual-stack replay operations: %#v", plan.Plan.Operations)
	}
}
