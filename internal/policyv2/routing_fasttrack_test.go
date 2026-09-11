package policyv2

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"rosboard/internal/routeros"
)

type fastTrackTestRepo struct {
	Repository
	RoutingRuleRepository
	state    FastTrackState
	count    int
	saves    int
	failSave bool
}

func (r *fastTrackTestRepo) DeviceID() string                                  { return "device" }
func (r *fastTrackTestRepo) ManagerInstanceID(context.Context) (string, error) { return "manager", nil }
func (r *fastTrackTestRepo) ListRoutingRules(context.Context) ([]RoutingRule, error) {
	return make([]RoutingRule, r.count), nil
}
func (r *fastTrackTestRepo) LoadFastTrackState(context.Context) (FastTrackState, error) {
	var copy FastTrackState
	data, _ := json.Marshal(r.state)
	_ = json.Unmarshal(data, &copy)
	return copy, nil
}
func (r *fastTrackTestRepo) SaveFastTrackState(_ context.Context, state FastTrackState) error {
	if r.failSave {
		return errors.New("disk failure")
	}
	r.state = state
	r.saves++
	return nil
}

type fastTrackTestRouter struct {
	listedMoveRecorder
	repo               *fastTrackTestRepo
	patches            int
	failPatch          bool
	externalAfterPatch bool
}

func (r *fastTrackTestRouter) UnsetFirewallConnectionMark(context.Context, routeros.MutationMenu, string) error {
	return nil
}
func (r *fastTrackTestRouter) Patch(_ context.Context, menu routeros.MutationMenu, id string, fields routeros.RouterOSFields) (routeros.RouterOSObject, error) {
	if len(r.repo.state.Records) == 0 {
		return nil, errors.New("write without durable intent")
	}
	r.patches++
	for _, obj := range r.objects[menu] {
		if obj.ID() == id {
			for key, value := range fields {
				obj[key] = value.(string)
			}
			if r.externalAfterPatch {
				obj["comment"] = "external"
			}
			if r.failPatch {
				return nil, errors.New("response lost")
			}
			return obj, nil
		}
	}
	return nil, errors.New("missing rule")
}
func fastTrackFixture() (*fastTrackTestRepo, *fastTrackTestRouter, *Applier) {
	repo := &fastTrackTestRepo{count: 1}
	router := &fastTrackTestRouter{repo: repo, listedMoveRecorder: listedMoveRecorder{objects: map[routeros.MutationMenu][]routeros.RouterOSObject{routeros.MenuIPFirewallFilter: {{".id": "*1", "action": "fasttrack-connection", "chain": "forward", "connection-state": "established,related"}}}}}
	return repo, router, &Applier{Repo: repo, Mutation: router}
}
func TestFastTrackAnalysis(t *testing.T) {
	for _, tc := range []struct {
		name    string
		fields  map[string]string
		foreign bool
		want    string
	}{
		{"default", nil, false, "auto_fixable"}, {"no mark", map[string]string{"connection-mark": "no-mark"}, false, "compatible"},
		{"exact foreign", map[string]string{"connection-mark": "vpn"}, false, "compatible"}, {"negated", map[string]string{"connection-mark": "!vpn"}, false, "unverified"},
		{"address", map[string]string{"dst-address": "192.0.2.0/24"}, false, "unverified"}, {"custom chain", map[string]string{"chain": "custom"}, false, "unverified"},
		{"foreign producer", nil, true, "conflict"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, router, applier := fastTrackFixture()
			for k, v := range tc.fields {
				router.objects[routeros.MenuIPFirewallFilter][0][k] = v
			}
			if tc.foreign {
				router.objects[routeros.MenuIPFirewallMangle] = []routeros.RouterOSObject{{".id": "*2", "action": "mark-connection", "new-connection-mark": "rb_foreign"}}
			}
			report, err := inspectFastTrack(context.Background(), applier, repo, nil)
			if err != nil || report.Rules[0].Status != tc.want {
				t.Fatalf("%+v %v", report, err)
			}
		})
	}
}
func TestFastTrackJournalAndRecovery(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name                          string
		failSave, failPatch, external bool
	}{{"success", false, false, false}, {"disk failure", true, false, false}, {"lost response", false, true, false}, {"external readback", false, false, true}} {
		t.Run(tc.name, func(t *testing.T) {
			repo, router, applier := fastTrackFixture()
			report, err := inspectFastTrack(ctx, applier, repo, nil)
			if err != nil {
				t.Fatal(err)
			}
			repo.failSave = tc.failSave
			router.failPatch = tc.failPatch
			router.externalAfterPatch = tc.external
			err = ensureFastTrack(ctx, applier, report)
			if tc.failSave {
				if err == nil || router.patches != 0 {
					t.Fatal("write occurred without journal")
				}
				return
			}
			if tc.external {
				if err == nil || repo.state.Records[0].Status != "diverged" {
					t.Fatal("unexpected readback adopted")
				}
				return
			}
			if tc.failPatch {
				if err == nil || repo.state.Records[0].Status != "intent" {
					t.Fatal("unknown result lost")
				}
				router.failPatch = false
			}
			report, err = inspectFastTrack(ctx, applier, repo, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := ensureFastTrack(ctx, applier, report); err != nil {
				t.Fatal(err)
			}
			if router.patches != 1 || repo.state.Records[0].Status != "applied" {
				t.Fatalf("recovery repeated write: %d %+v", router.patches, repo.state)
			}
			router.objects[routeros.MenuIPFirewallFilter][0]["comment"] = "user changed"
			report, _ = inspectFastTrack(ctx, applier, repo, nil)
			if err := ensureFastTrack(ctx, applier, report); err != nil {
				t.Fatal(err)
			}
			if repo.state.Records[0].Status != "diverged" {
				t.Fatal("external change not remembered")
			}
			delete(router.objects[routeros.MenuIPFirewallFilter][0], "comment")
			report, _ = inspectFastTrack(ctx, applier, repo, nil)
			_ = ensureFastTrack(ctx, applier, report)
			if repo.state.Records[0].Status != "diverged" {
				t.Fatal("divergence not sticky")
			}
		})
	}
}
func TestFastTrackFingerprints(t *testing.T) {
	repo, router, applier := fastTrackFixture()
	ctx := context.Background()
	report, _ := inspectFastTrack(ctx, applier, repo, nil)
	plan := Plan{FastTrack: report, Domain: PolicyDomainRouting}
	router.objects[routeros.MenuIPFirewallFilter][0]["packets"] = "123"
	if err := checkFastTrackPlan(ctx, applier, repo, nil, plan); err != nil {
		t.Fatal("counter invalidated preview")
	}
	router.objects[routeros.MenuIPFirewallMangle] = []routeros.RouterOSObject{{".id": "*2", "action": "mark-connection", "new-connection-mark": "vpn"}}
	if err := checkFastTrackPlan(ctx, applier, repo, nil, plan); !errors.Is(err, ErrPlanStale) {
		t.Fatal("foreign producer did not invalidate preview")
	}
}
func TestFastTrackAcknowledgementCannotBeSkipped(t *testing.T) {
	repo, _, applier := fastTrackFixture()
	manager := NewManager(nil)
	manager.appliers[repo.DeviceID()] = applier
	manager.plans["plan"] = cachedPlan{Plan: Plan{DeviceID: repo.DeviceID(), PlanHash: "hash", ExpiresAt: time.Now().Add(time.Hour), Acknowledgements: []PlanAcknowledgement{{Code: "fasttrack_compatibility_unverified", Required: true}}}}
	for _, acks := range [][]string{nil, {"unrelated"}} {
		if _, err := manager.ApplyPlanWithAcknowledgements(context.Background(), repo.DeviceID(), "plan", "hash", acks); !errors.Is(err, ErrAcknowledgementRequired) {
			t.Fatalf("ack bypass: %v", err)
		}
	}
	if _, err := manager.ApplyPlanWithAcknowledgements(context.Background(), repo.DeviceID(), "plan", "", []string{"fasttrack_compatibility_unverified"}); !errors.Is(err, ErrAcknowledgementRequired) {
		t.Fatalf("unbound ack: %v", err)
	}
}

func TestValidatePlanAcknowledgements(t *testing.T) {
	plan := Plan{PlanHash: "reviewed", Acknowledgements: []PlanAcknowledgement{{Code: "risk", Required: true, Accepted: true}, {Code: "optional"}}}
	for _, tc := range []struct {
		name, hash string
		accepted   []string
		wantError  bool
	}{
		{"internal follow-up", "", nil, true},
		{"presentation flag is not approval", "reviewed", nil, true},
		{"missing hash", "", []string{"risk"}, true},
		{"stale hash", "old", []string{"risk"}, true},
		{"wrong issue", "reviewed", []string{"other"}, true},
		{"explicit approval", "reviewed", []string{"risk"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validatePlanAcknowledgements(plan, tc.hash, tc.accepted)
			if errors.Is(err, ErrAcknowledgementRequired) != tc.wantError {
				t.Fatalf("validation: %v", err)
			}
		})
	}
	if err := validatePlanAcknowledgements(Plan{Acknowledgements: []PlanAcknowledgement{{Code: "optional"}}}, "", nil); err != nil {
		t.Fatal(err)
	}
}
