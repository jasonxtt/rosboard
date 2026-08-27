package store

import (
	"context"
	"io"
	"log"
	"os"
	"testing"
	"time"

	"rosboard/internal/policyv2"
	"rosboard/internal/routeros"
)

func TestPolicyV2LiveRouterOSRoundTrip(t *testing.T) {
	baseURL := os.Getenv("ROSBOARD_LIVE_ROUTER_URL")
	username := os.Getenv("ROSBOARD_LIVE_ROUTER_USER")
	password := os.Getenv("ROSBOARD_LIVE_ROUTER_PASSWORD")
	if baseURL == "" || username == "" || password == "" {
		t.Skip("live RouterOS credentials are not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	storage, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	repository := storage.PolicyRepository()
	reader := routeros.NewClient(baseURL, username, password)
	mutation := routeros.NewMutationClient(baseURL, username, password)
	manager := policyv2.NewManager(log.New(io.Discard, "", 0))
	if err := manager.RegisterApplier("default", &policyv2.Applier{Reader: reader, Mutation: mutation, Repo: repository}); err != nil {
		t.Fatal(err)
	}

	egress, err := repository.SaveEgress(ctx, policyv2.Egress{
		ID: "live-egress", Name: "Rosboard V2 live validation", ListMode: policyv2.ListModeShared,
		ListName: "rosboard_v2_live_validation", DNSUpstream: "1.1.1.1", FakeAlias: "192.0.2.253",
		FailureMode: "strict", Enabled: true,
		Families: []policyv2.EgressFamily{{Family: policyv2.FamilyIPv4, Enabled: true, WANInterface: "pppoe-out1", Gateway: "pppoe-out1", RouteMode: "strict", NATMode: "none"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	source, err := repository.SaveSource(ctx, policyv2.Source{ID: "live-source", EgressID: egress.ID, Type: "upload", Name: "Live validation", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.SavePendingSourceVersion(ctx, policyv2.SourceVersion{ID: "live-version", SourceID: source.ID, SHA256: "live", CompressedYAML: []byte("live")}, []policyv2.SourceRule{{RuleType: "DOMAIN", Domain: "rosboard-v2-validation.invalid"}}); err != nil {
		t.Fatal(err)
	}
	discovery, err := policyv2.NewScanner(reader).Scan(ctx, "default")
	if err != nil {
		t.Fatal(err)
	}
	var ingress policyv2.TrafficIngressScope
	for _, candidate := range discovery.TrafficIngress {
		if candidate.Default && candidate.Kind == "interface-list" {
			ingress.InterfaceLists = []string{candidate.Name}
			break
		}
	}
	if len(ingress.InterfaceLists) == 0 {
		for _, candidate := range discovery.TrafficIngress {
			if candidate.Kind == "interface-list" {
				ingress.InterfaceLists = []string{candidate.Name}
				break
			}
		}
	}
	if len(ingress.InterfaceLists) == 0 {
		for _, candidate := range discovery.TrafficIngress {
			if candidate.Kind != "interface-list" {
				ingress.Interfaces = []string{candidate.Name}
				break
			}
		}
	}
	if len(ingress.InterfaceLists) == 0 && len(ingress.Interfaces) == 0 {
		t.Fatal("live RouterOS has no selectable traffic ingress")
	}
	ingressJSON, err := policyv2.MarshalTrafficIngressScope(ingress)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.SaveTrafficIngress(ctx, ingressJSON); err != nil {
		t.Fatal(err)
	}

	cleanup := func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), time.Minute)
		defer cleanupCancel()
		if loaded, loadErr := repository.GetSource(cleanupCtx, source.ID); loadErr == nil {
			_ = repository.DeleteSource(cleanupCtx, loaded.ID, loaded.Revision)
		}
		if loaded, loadErr := repository.GetEgress(cleanupCtx, egress.ID); loadErr == nil {
			_ = repository.DeleteEgress(cleanupCtx, loaded.ID, loaded.Revision)
		}
		_, _ = repository.SaveTrafficIngress(cleanupCtx, []byte(`{"interfaceLists":[],"interfaces":[]}`))
		if envelope, planErr := manager.GeneratePlan(cleanupCtx, "default", "disable_delete"); planErr == nil && len(envelope.Plan.Blockers) == 0 {
			if job, applyErr := manager.ApplyPlan(cleanupCtx, "default", envelope.PlanID); applyErr == nil {
				_ = waitPolicyV2Job(t, repository, job.ID)
			}
		}
	}
	defer cleanup()

	plan, err := manager.GeneratePlan(ctx, "default", "initial")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Plan.State != "ready" || len(plan.Plan.Operations) == 0 {
		t.Fatalf("live plan is not applicable: %#v", plan.Plan)
	}
	job, err := manager.ApplyPlan(ctx, "default", plan.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	if job = waitPolicyV2Job(t, repository, job.ID); job.State != "committed" {
		desired, desiredErr := policyv2.BuildDesired(ctx, repository)
		actual, _, scanErr := policyv2.ScanManaged(ctx, mutation, repository, desired.Objects)
		remaining, blockers := policyv2.DiffDesired(desired.Objects, actual)
		t.Fatalf("live apply failed: %#v desiredErr=%v scanErr=%v remaining=%#v blockers=%#v", job, desiredErr, scanErr, remaining, blockers)
	}
	second, err := manager.GeneratePlan(ctx, "default", "structural")
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Plan.Operations) != 0 {
		t.Fatalf("live re-scan did not converge: %#v", second.Plan.Operations)
	}

	cleanup()
	actual, _, err := policyv2.ScanManaged(ctx, mutation, repository, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(actual) != 0 {
		t.Fatalf("live cleanup left managed objects: %#v", actual)
	}
}
