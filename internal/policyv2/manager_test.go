package policyv2

import (
	"context"
	"errors"
	"testing"
	"time"

	"rosboard/internal/accesscontrol"
	"rosboard/internal/routeros"
)

func TestSourceScheduleIntervalDefaultsToSevenDays(t *testing.T) {
	interval, ok := sourceScheduleInterval("")
	if !ok || interval != 7*24*time.Hour {
		t.Fatalf("empty source schedule = %s, %v; want 7d, true", interval, ok)
	}
}

type moveRecorder struct {
	moves []routeros.MoveRequest
}

func (r *moveRecorder) List(context.Context, routeros.MutationMenu, routeros.MutationQuery) ([]routeros.RouterOSObject, error) {
	return nil, nil
}

func (r *moveRecorder) Create(context.Context, routeros.MutationMenu, routeros.RouterOSFields) (routeros.RouterOSObject, error) {
	return routeros.RouterOSObject{".id": "*created"}, nil
}

func (r *moveRecorder) Patch(context.Context, routeros.MutationMenu, string, routeros.RouterOSFields) (routeros.RouterOSObject, error) {
	return nil, nil
}

func (r *moveRecorder) Delete(context.Context, routeros.MutationMenu, string) error {
	return nil
}

func (r *moveRecorder) Move(_ context.Context, _ routeros.MutationMenu, request routeros.MoveRequest) (routeros.MutationResponse, error) {
	r.moves = append(r.moves, request)
	return routeros.MutationResponse{}, nil
}

func (r *moveRecorder) SetDNSSettings(context.Context, routeros.RouterOSFields) error {
	return nil
}

func (r *moveRecorder) FlushDNSCache(context.Context) error {
	return nil
}

type listedMoveRecorder struct {
	moveRecorder
	objects map[routeros.MutationMenu][]routeros.RouterOSObject
}

func (r *listedMoveRecorder) List(_ context.Context, menu routeros.MutationMenu, _ routeros.MutationQuery) ([]routeros.RouterOSObject, error) {
	return r.objects[menu], nil
}

func accessJumpForTest(logicalID string, order int) DesiredObject {
	return DesiredObject{
		LogicalID: logicalID,
		Menu:      string(routeros.MenuIPFirewallFilter),
		Order:     order,
		Phase:     "activation",
		Fields: map[string]string{
			"chain":   "forward",
			"action":  "jump",
			"comment": accesscontrol.ManagedComment("manager", "device", logicalID, "test"),
		},
	}
}

func accessDirectForTest(logicalID string, order int) DesiredObject {
	return DesiredObject{
		LogicalID: logicalID,
		Menu:      string(routeros.MenuIPFirewallFilter),
		Order:     order,
		Phase:     "activation",
		Fields: map[string]string{
			"chain":            "forward",
			"src-address-list": "rbac_rule_test",
			"out-interface":    "pppoe-out1",
			"action":           "drop",
			"comment":          accesscontrol.ManagedComment("manager", "device", logicalID, "访问控制出站接口"),
		},
	}
}

func accessRuleForTest(logicalID string, order int) DesiredObject {
	return DesiredObject{
		LogicalID: logicalID,
		Menu:      string(routeros.MenuIPFirewallFilter),
		Order:     order,
		Phase:     "activation",
		Fields: map[string]string{
			"chain":   "access-chain",
			"action":  "drop",
			"comment": accesscontrol.ManagedComment("manager", "device", logicalID, "test"),
		},
	}
}

func TestEnsureAccessJumpsFirstSkipsAlreadyCorrectOrder(t *testing.T) {
	jumpOut := accessJumpForTest("access:policy-a:ipv4:jump-out", 1)
	jumpIn := accessJumpForTest("access:policy-a:ipv4:jump-in", 2)
	recorder := &listedMoveRecorder{objects: map[routeros.MutationMenu][]routeros.RouterOSObject{
		routeros.MenuIPFirewallFilter: {
			{".id": "*jump-out", "comment": jumpOut.Fields["comment"]},
			{".id": "*jump-in", "comment": jumpIn.Fields["comment"]},
			{".id": "*foreign", "comment": "foreign"},
		},
	}}

	if err := ensureAccessJumpsFirst(context.Background(), recorder, []DesiredObject{jumpOut, jumpIn}); err != nil {
		t.Fatal(err)
	}
	if len(recorder.moves) != 0 {
		t.Fatalf("already ordered access jumps caused moves: %#v", recorder.moves)
	}
}

func TestPlanAccessJumpsFirstTreatsForeignRulesAsOrderingBoundary(t *testing.T) {
	jumpOut := accessJumpForTest("access:policy-a:ipv4:jump-out", 1)
	jumpIn := accessJumpForTest("access:policy-a:ipv4:jump-in", 2)
	foreign := accesscontrol.ManagedComment("other-manager", "other-device", "access:foreign:ipv4:jump-out", "foreign")
	recorder := &listedMoveRecorder{objects: map[routeros.MutationMenu][]routeros.RouterOSObject{
		routeros.MenuIPFirewallFilter: {
			{".id": "*foreign", "comment": foreign},
			{".id": "*user", "comment": "user broad accept"},
			{".id": "*jump-out", "comment": jumpOut.Fields["comment"]},
			{".id": "*jump-in", "comment": jumpIn.Fields["comment"]},
		},
	}}
	moves, err := planAccessJumpsFirst(context.Background(), recorder, []DesiredObject{jumpOut, jumpIn})
	if err != nil {
		t.Fatal(err)
	}
	if len(moves) != 2 || moves[0].Anchor == nil || moves[0].Anchor.RouterID != "*foreign" || moves[1].Anchor == nil || moves[1].Anchor.LogicalID != jumpIn.LogicalID {
		t.Fatalf("foreign rules must remain untouched but must not be skipped as an ordering boundary: %#v", moves)
	}
}

func TestPlanAccessJumpsFirstUsesScannedRouterIDForLegacyAccessComment(t *testing.T) {
	jumpOut := accessJumpForTest("access:policy-a:ipv4:jump-out", 1)
	jumpIn := accessJumpForTest("access:policy-a:ipv4:jump-in", 2)
	actual := []ActualObject{
		{LogicalID: jumpOut.LogicalID, Menu: jumpOut.Menu, RouterID: "*legacy-out", Ownership: "owned"},
		{LogicalID: jumpIn.LogicalID, Menu: jumpIn.Menu, RouterID: "*legacy-in", Ownership: "owned"},
	}
	recorder := &listedMoveRecorder{objects: map[routeros.MutationMenu][]routeros.RouterOSObject{
		routeros.MenuIPFirewallFilter: {
			{".id": "*user", "comment": "user broad accept"},
			{".id": "*legacy-out", "comment": accesscontrol.LegacyManagedComment("manager", "device", jumpOut.LogicalID)},
			{".id": "*legacy-in", "comment": accesscontrol.LegacyManagedComment("manager", "device", jumpIn.LogicalID)},
		},
	}}

	moves, err := planAccessJumpsFirst(context.Background(), recorder, []DesiredObject{jumpOut, jumpIn}, actual)
	if err != nil {
		t.Fatal(err)
	}
	if len(moves) != 2 || moves[0].LogicalID != jumpIn.LogicalID || moves[0].RouterID != "*legacy-in" || moves[1].LogicalID != jumpOut.LogicalID || moves[1].RouterID != "*legacy-out" || moves[0].Anchor == nil || moves[0].Anchor.RouterID != "*user" || moves[1].Anchor == nil || moves[1].Anchor.LogicalID != jumpIn.LogicalID || moves[1].Anchor.RouterID != "*legacy-in" {
		t.Fatalf("legacy access jumps must be planned with their existing RouterOS IDs: %#v", moves)
	}
}

func TestPlanAccessJumpsFirstDoesNotPlanMovesForJumpsThatWillBeCreated(t *testing.T) {
	jumpOut := accessJumpForTest("access:policy-a:ipv4:jump-out", 1)
	jumpIn := accessJumpForTest("access:policy-a:ipv4:jump-in", 2)
	recorder := &listedMoveRecorder{objects: map[routeros.MutationMenu][]routeros.RouterOSObject{
		routeros.MenuIPFirewallFilter: {
			{".id": "*user", "comment": "user broad accept"},
		},
	}}

	moves, err := planAccessJumpsFirst(context.Background(), recorder, []DesiredObject{jumpOut, jumpIn})
	if err != nil {
		t.Fatal(err)
	}
	if len(moves) != 0 {
		t.Fatalf("missing access jumps must be created before ordering, not moved with empty IDs: %#v", moves)
	}
}

func TestPlanAccessJumpsFirstDoesNotPlanMovesWhenSomeJumpsAreMissing(t *testing.T) {
	jumpOut := accessJumpForTest("access:policy-a:ipv4:jump-out", 1)
	jumpIn := accessJumpForTest("access:policy-a:ipv4:jump-in", 2)
	recorder := &listedMoveRecorder{objects: map[routeros.MutationMenu][]routeros.RouterOSObject{
		routeros.MenuIPFirewallFilter: {
			{".id": "*user", "comment": "user broad accept"},
			{".id": "*jump-out", "comment": jumpOut.Fields["comment"]},
		},
	}}

	moves, err := planAccessJumpsFirst(context.Background(), recorder, []DesiredObject{jumpOut, jumpIn})
	if err != nil {
		t.Fatal(err)
	}
	if len(moves) != 0 {
		t.Fatalf("partially materialized access block must be completed before ordering: %#v", moves)
	}
}

func TestPlanAccessJumpsFirstIncludesDirectEgressRules(t *testing.T) {
	directOut := accessDirectForTest("access:policy-a:ipv4:out:pppoe-out1:other", 1)
	directIn := directOut
	directIn.LogicalID = "access:policy-a:ipv4:in:pppoe-out1:other"
	directIn.Fields = map[string]string{
		"chain":            "forward",
		"dst-address-list": "rbac_rule_test",
		"in-interface":     "pppoe-out1",
		"action":           "drop",
		"comment":          accesscontrol.ManagedComment("manager", "device", directIn.LogicalID, "访问控制回程接口"),
	}
	recorder := &listedMoveRecorder{objects: map[routeros.MutationMenu][]routeros.RouterOSObject{
		routeros.MenuIPFirewallFilter: {
			{".id": "*user", "comment": "user broad accept"},
			{".id": "*direct-out", "comment": directOut.Fields["comment"]},
			{".id": "*direct-in", "comment": directIn.Fields["comment"]},
		},
	}}
	moves, err := planAccessJumpsFirst(context.Background(), recorder, []DesiredObject{directOut, directIn})
	if err != nil {
		t.Fatal(err)
	}
	if len(moves) != 2 || moves[0].RouterID != "*direct-in" || moves[0].Anchor == nil || moves[0].Anchor.RouterID != "*user" || moves[1].RouterID != "*direct-out" || moves[1].Anchor == nil || moves[1].Anchor.LogicalID != directIn.LogicalID {
		t.Fatalf("direct egress rules must be planned before user filters: %#v", moves)
	}
}

func TestDesiredOrderMovesLeavesAccessJumpOrderingToAccessPlanner(t *testing.T) {
	jumpOutA := accessJumpForTest("access:policy-a:ipv4:jump-out", 1)
	jumpInA := accessJumpForTest("access:policy-a:ipv4:jump-in", 2)
	ruleA := accessRuleForTest("access:policy-a:ipv4:tcp", 3)
	jumpOutB := accessJumpForTest("access:policy-b:ipv4:jump-out", 6)
	jumpInB := accessJumpForTest("access:policy-b:ipv4:jump-in", 7)
	ruleB := accessRuleForTest("access:policy-b:ipv4:tcp", 8)
	desired := []DesiredObject{jumpOutA, jumpInA, ruleA, jumpOutB, jumpInB, ruleB}
	actual := []ActualObject{
		{LogicalID: jumpOutA.LogicalID, Menu: jumpOutA.Menu, RouterID: "*jump-out-a", Fields: jumpOutA.Fields},
		{LogicalID: jumpInA.LogicalID, Menu: jumpInA.Menu, RouterID: "*jump-in-a", Fields: jumpInA.Fields},
		{LogicalID: ruleA.LogicalID, Menu: ruleA.Menu, RouterID: "*rule-a", Fields: ruleA.Fields},
	}

	moves := desiredOrderMoves(desired, actual)
	if len(moves) != 0 {
		t.Fatalf("generic policy ordering must not move access jumps: %#v", moves)
	}
}

func TestApplyOperationExecutesMoveWithResolvedIDs(t *testing.T) {
	recorder := &moveRecorder{}
	created := map[string]string{"rule-a": "*a", "rule-b": "*b"}
	operation := PlanOperation{
		Action:    "move",
		Menu:      string(routeros.MenuIPFirewallMangle),
		LogicalID: "rule-a",
		Anchor:    &PlanAnchor{LogicalID: "rule-b", Relation: "before"},
	}
	if err := applyOperation(context.Background(), recorder, operation, created); err != nil {
		t.Fatal(err)
	}
	if len(recorder.moves) != 1 || recorder.moves[0].ID != "*a" || recorder.moves[0].BeforeID != "*b" {
		t.Fatalf("unexpected move request: %#v", recorder.moves)
	}
}

type settlingScanMutation struct {
	fakeScanMutation
	stale bool
}

func (m *settlingScanMutation) List(ctx context.Context, menu routeros.MutationMenu, query routeros.MutationQuery) ([]routeros.RouterOSObject, error) {
	objects, err := m.fakeScanMutation.List(ctx, menu, query)
	if menu == routeros.MenuIPDNSStatic && m.stale && len(objects) > 0 {
		copy := append([]routeros.RouterOSObject(nil), objects...)
		copy[0] = routeros.RouterOSObject{
			".id":      copy[0].ID(),
			"comment":  copy[0]["comment"],
			"disabled": "true",
		}
		m.stale = false
		return copy, err
	}
	return objects, err
}

func TestVerifyDesiredRetriesTransientRouterOSReadAfterWrite(t *testing.T) {
	desired := []DesiredObject{{
		LogicalID: "dns:example.test",
		Menu:      string(routeros.MenuIPDNSStatic),
		Fields: map[string]string{
			"comment":  managedComment("manager", "edge", "dns:example.test", "策略测试"),
			"disabled": "no",
		},
	}}
	mutation := &settlingScanMutation{
		fakeScanMutation: fakeScanMutation{objects: map[routeros.MutationMenu][]routeros.RouterOSObject{
			routeros.MenuIPDNSStatic: {{".id": "*dns", "comment": desired[0].Fields["comment"], "disabled": "false"}},
		}},
		stale: true,
	}
	repository := &fakeScanRepository{managerID: "manager"}
	remaining, blockers, err := verifyDesiredWithRetry(context.Background(), mutation, repository, desired)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 0 || len(blockers) != 0 {
		t.Fatalf("transient RouterOS read should settle: operations=%#v blockers=%#v", remaining, blockers)
	}
}

type activationScanMutation struct {
	fakeScanMutation
	visible bool
	patches []string
}

func (m *activationScanMutation) List(ctx context.Context, menu routeros.MutationMenu, query routeros.MutationQuery) ([]routeros.RouterOSObject, error) {
	if menu == routeros.MenuIPDNSStatic && !m.visible {
		m.visible = true
		return nil, nil
	}
	return m.fakeScanMutation.List(ctx, menu, query)
}

func (m *activationScanMutation) Patch(_ context.Context, menu routeros.MutationMenu, id string, fields routeros.RouterOSFields) (routeros.RouterOSObject, error) {
	m.patches = append(m.patches, string(menu)+":"+id)
	if object := m.objects[menu]; len(object) > 0 {
		if value, ok := fields["disabled"].(bool); ok {
			if value {
				object[0]["disabled"] = "true"
			} else {
				object[0]["disabled"] = "false"
			}
		}
		return object[0], nil
	}
	return routeros.RouterOSObject{".id": id}, nil
}

func TestActivateDesiredRetriesTransientMissingObject(t *testing.T) {
	desired := []DesiredObject{{
		LogicalID: "routing-dns:egress:target:DOMAIN-SUFFIX:example.test",
		Menu:      string(routeros.MenuIPDNSStatic),
		Fields: map[string]string{
			"comment":  managedComment("manager", "edge", "routing-dns:egress:target:DOMAIN-SUFFIX:example.test", "策略测试"),
			"disabled": "no",
		},
	}}
	mutation := &activationScanMutation{fakeScanMutation: fakeScanMutation{objects: map[routeros.MutationMenu][]routeros.RouterOSObject{
		routeros.MenuIPDNSStatic: {{".id": "*dns", "comment": desired[0].Fields["comment"], "disabled": "yes"}},
	}}}
	applier := &Applier{Mutation: mutation, Repo: &fakeScanRepository{managerID: "manager"}}
	if err := activateDesiredObjectsForDomain(context.Background(), applier, desired, PolicyDomainRouting); err != nil {
		t.Fatal(err)
	}
	if len(mutation.patches) != 1 || mutation.patches[0] != string(routeros.MenuIPDNSStatic)+":*dns" {
		t.Fatalf("transiently missing object was not activated: %#v", mutation.patches)
	}
}

type capabilityReadFailure struct {
	moveRecorder
	err error
}

func (r *capabilityReadFailure) List(context.Context, routeros.MutationMenu, routeros.MutationQuery) ([]routeros.RouterOSObject, error) {
	return nil, r.err
}

func TestAccessCapabilityBlockerFailsClosedWhenFilterFamilyCannotBeVerified(t *testing.T) {
	errProbe := errors.New("read-only firewall menu")
	blockers, err := accessCapabilityBlockers(context.Background(), &capabilityReadFailure{err: errProbe}, []DesiredObject{
		accessRuleForTest("access:rule-a:ipv4:tcp", 1),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(blockers) != 1 || blockers[0].Code != "routeros_access_filter_capability_unverified" || blockers[0].Family != string(FamilyIPv4) {
		t.Fatalf("failed capability verification must block IPv4 access filters: %#v", blockers)
	}
}

func TestIsAccessPlanOperationRecognizesOnlyOwnedStaleApplicationDeletes(t *testing.T) {
	applicationDelete := PlanOperation{
		Action: "delete", Menu: string(routeros.MenuIPDNSStatic), LogicalID: "stale:rb_deadbeef", Ownership: "owned",
		Before: map[string]string{"address-list": applicationListPrefix + "123456789abc"},
	}
	if !isAccessPlanOperation(applicationDelete, nil) {
		t.Fatal("stale owned application DNS delete must remain in an access-only plan")
	}
	for _, operation := range []PlanOperation{
		{Action: "delete", Menu: string(routeros.MenuIPDNSStatic), LogicalID: "stale:rb_deadbeef", Ownership: "foreign", Before: map[string]string{"address-list": applicationListPrefix + "123456789abc"}},
		{Action: "patch", Menu: string(routeros.MenuIPDNSStatic), LogicalID: "stale:rb_deadbeef", Ownership: "owned", Before: map[string]string{"address-list": applicationListPrefix + "123456789abc"}},
		{Action: "delete", Menu: string(routeros.MenuIPDNSForwarders), LogicalID: "stale:rb_deadbeef", Ownership: "owned", Before: map[string]string{"address-list": applicationListPrefix + "123456789abc"}},
		{Action: "delete", Menu: string(routeros.MenuIPDNSStatic), LogicalID: "stale:rb_deadbeef", Ownership: "owned", Before: map[string]string{"address-list": "rb_src_123456789abc"}},
	} {
		if isAccessPlanOperation(operation, nil) {
			t.Fatalf("unrelated stale operation was classified as application access: %#v", operation)
		}
	}
}
