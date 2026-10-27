package policyv2

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"rosboard/internal/routeros"
)

func TestDisabledFamilyClassificationAndPriority(t *testing.T) {
	target := &routingTargetProjection{id: "target", list: "targets", source: Source{ID: "target", Kind: KindDomain}, plainRules: []SourceRule{{RuleType: "DOMAIN", Domain: "example.test"}}}
	result := DesiredResult{}
	add := func(id string, menu routeros.MutationMenu, phase string, fields map[string]string) {
		result.Objects = append(result.Objects, DesiredObject{LogicalID: id, Menu: string(menu), Fields: fields, Order: len(result.Objects) + 1})
	}
	low := RoutingRule{ID: "low", Priority: 100, EgressID: "route", Enabled: true, TargetListIDs: []string{"target"}, SourceScope: &RoutingSourceScope{Kind: RoutingSourceInterface, Name: "lan"}}
	high := RoutingRule{ID: "high", Priority: 10, EgressID: "blocked", Enabled: true, TargetListIDs: []string{"target"}, SourceScope: &RoutingSourceScope{Kind: RoutingSourceInterface, Name: "lan"}}
	buildRoutingMangleFamily(&result, add, Egress{ID: "route", Enabled: true}, EgressFamily{Family: FamilyIPv6}, nil, nil, "table", []*routingTargetProjection{target}, []RoutingRule{low}, nil, "no", "manager", "device")
	buildRoutingMangleFamily(&result, add, Egress{ID: "blocked", Enabled: true, RouterOutput: true}, EgressFamily{Family: FamilyIPv6}, nil, nil, "", []*routingTargetProjection{target}, []RoutingRule{high}, nil, "no", "manager", "device")
	reorderRoutingConnectionObjects(&result)
	var conn []DesiredObject
	var forward, output DesiredObject
	for _, o := range result.Objects {
		if isRoutingPreroutingConnection(o) {
			conn = append(conn, o)
		}
		if strings.HasPrefix(o.LogicalID, "routing-rule-block:") {
			forward = o
		}
		if strings.HasPrefix(o.LogicalID, "routing-router-block:") {
			output = o
		}
	}
	if len(conn) != 2 || !strings.Contains(conn[0].LogicalID, ":high:") {
		t.Fatalf("winner order: %#v", conn)
	}
	if conn[0].Fields["in-interface"] != "lan" || conn[0].Fields["dst-address-list"] != "targets" || conn[0].Fields["connection-state"] != "new" {
		t.Fatalf("matcher: %#v", conn[0])
	}
	if forward.Fields["action"] != "drop" || forward.Fields["connection-mark"] != conn[0].Fields["new-connection-mark"] || forward.Fields["chain"] != "forward" {
		t.Fatalf("block: %#v", forward)
	}
	if output.Fields["action"] != "drop" || output.Fields["chain"] != "output" {
		t.Fatalf("router output block: %#v", output)
	}
}

type trackingReader map[routeros.ReadMenu][]routeros.RouterOSObject

func (r trackingReader) PolicyList(_ context.Context, menu routeros.ReadMenu, _ []string) ([]routeros.RouterOSObject, error) {
	return r[menu], nil
}
func TestRoutingBlockTrackingPreflight(t *testing.T) {
	desired := []DesiredObject{{LogicalID: "routing-rule-block:e:ipv6:x", Menu: string(routeros.MenuIPv6FirewallFilter), Fields: map[string]string{"disabled": "no"}}}
	for _, tc := range []struct {
		name, enabled string
		raw           []routeros.RouterOSObject
		want          string
	}{
		{"safe", "auto", nil, ""}, {"notrack", "yes", []routeros.RouterOSObject{{".id": "*1", "action": "notrack"}}, "routing_block_notrack"}, {"disabled raw", "yes", []routeros.RouterOSObject{{"action": "notrack", "disabled": "true"}}, ""}, {"tracking disabled", "no", nil, "routing_block_tracking_disabled"}, {"unknown", "", nil, "routing_block_tracking_unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := trackingReader{routeros.ReadMenuFirewallTracking: {{"enabled": tc.enabled}}, routeros.ReadMenuIPv6FirewallRaw: tc.raw}
			got, err := routingBlockTrackingIssues(context.Background(), r, desired)
			if err != nil {
				t.Fatal(err)
			}
			if tc.want == "" && len(got) != 0 || tc.want != "" && (len(got) != 1 || got[0].Code != tc.want) {
				t.Fatalf("issues=%#v", got)
			}
		})
	}
}

func TestBlockLifecycleAndDomain(t *testing.T) {
	target := &routingTargetProjection{id: "t", list: "targets", source: Source{ID: "t", Kind: KindDomain}, plainRules: []SourceRule{{RuleType: "DOMAIN", Domain: "example.test"}}}
	for _, tc := range []struct {
		name                                     string
		egressEnabled, ruleEnabled, routerOutput bool
		table                                    string
		wantBlock, wantOutput                    bool
	}{
		{"block", true, true, false, "", true, false}, {"router output", true, true, true, "", true, true}, {"egress off", false, true, true, "", false, false}, {"rule off", true, false, false, "", false, false}, {"routing", true, true, true, "route", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := DesiredResult{}
			add := func(id string, menu routeros.MutationMenu, phase string, fields map[string]string) {
				result.Objects = append(result.Objects, DesiredObject{LogicalID: id, Menu: string(menu), Fields: fields})
			}
			rule := RoutingRule{ID: "rule", EgressID: "e", Enabled: tc.ruleEnabled, TargetListIDs: []string{"t"}, SourceScope: &RoutingSourceScope{Kind: RoutingSourceInterface, Name: "lan"}}
			disabled := "no"
			if !tc.egressEnabled {
				disabled = "yes"
			}
			buildRoutingMangleFamily(&result, add, Egress{ID: "e", Enabled: tc.egressEnabled, RouterOutput: tc.routerOutput}, EgressFamily{Family: FamilyIPv4}, nil, nil, tc.table, []*routingTargetProjection{target}, []RoutingRule{rule}, nil, disabled, "manager", "device")
			forward, output := false, false
			for _, o := range result.Objects {
				if isRoutingBlockFilter(o) {
					if managedActualDomain(ActualObject{LogicalID: o.LogicalID, Menu: o.Menu, Fields: o.Fields}) != PolicyDomainRouting {
						t.Fatalf("block misclassified: %#v", o)
					}
					if o.Fields["disabled"] != "yes" {
						if o.Fields["chain"] == "forward" {
							forward = true
						}
						if o.Fields["chain"] == "output" {
							output = true
						}
					}
				}
			}
			if forward != tc.wantBlock || output != tc.wantOutput {
				t.Fatalf("active block forward=%v output=%v objects=%#v", forward, output, result.Objects)
			}
		})
	}
}

func TestRoutingBlockOrderAndOwnership(t *testing.T) {
	block := DesiredObject{LogicalID: "routing-rule-block:e:ipv4:direct:enabled", Menu: string(routeros.MenuIPFirewallFilter), Phase: "activation", Order: 1, Fields: map[string]string{"chain": "forward", "action": "drop", "connection-mark": "rb_bl_x", "comment": managedComment("manager", "device", "routing-rule-block:e:ipv4:direct:enabled")}}
	access := accessJumpForTest("access:rule:ipv4:jump", 2)
	recorder := &applyingListedMoveRecorder{listedMoveRecorder: listedMoveRecorder{objects: map[routeros.MutationMenu][]routeros.RouterOSObject{routeros.MenuIPFirewallFilter: {{".id": "*A", "chain": "forward", "action": "accept"}, {".id": "*B", "chain": "forward", "action": "jump", "comment": access.Fields["comment"]}, {".id": "*C", "chain": "forward", "action": "drop", "comment": block.Fields["comment"]}}}}}
	moves, err := planRoutingBlocksFirst(context.Background(), recorder, []DesiredObject{block})
	if err != nil || len(moves) != 1 || moves[0].RouterID != "*C" || moves[0].Anchor.RouterID != "*A" {
		t.Fatalf("plan=%#v err=%v", moves, err)
	}
	if err := ensureRoutingBlocksFirst(context.Background(), recorder, []DesiredObject{block}); err != nil {
		t.Fatal(err)
	}
	if len(recorder.moves) != 1 || recorder.moves[0].ID != "*C" || recorder.moves[0].BeforeID != "*A" {
		t.Fatalf("moves=%#v", recorder.moves)
	}
	if err := ensureRoutingBlocksFirst(context.Background(), recorder, []DesiredObject{block}); err != nil || len(recorder.moves) != 1 {
		t.Fatalf("non-idempotent: %#v %v", recorder.moves, err)
	}
	if managedActualDomain(ActualObject{LogicalID: block.LogicalID, Menu: block.Menu, Fields: block.Fields, Ownership: "owned"}) != PolicyDomainRouting {
		t.Fatal("routing block adopted by Access")
	}
}

func TestRoutingBlockRejectsForeignPreclassifiers(t *testing.T) {
	desired := []DesiredObject{{LogicalID: "routing-rule-block:e:ipv6:x", Menu: string(routeros.MenuIPv6FirewallFilter), Fields: map[string]string{"chain": "forward", "disabled": "no"}}}
	recorder := &listedMoveRecorder{objects: map[routeros.MutationMenu][]routeros.RouterOSObject{routeros.MenuIPv6FirewallMangle: {{".id": "*1", "chain": "prerouting", "action": "mark-connection"}, {".id": "*2", "chain": "prerouting", "action": "change-mss", "passthrough": "yes"}}}}
	got, err := routingBlockMangleIssues(context.Background(), recorder, desired, nil)
	if err != nil || len(got) != 1 || got[0].LogicalID != "*1" {
		t.Fatalf("issues=%#v error=%v", got, err)
	}
	actual := []ActualObject{{Menu: string(routeros.MenuIPv6FirewallMangle), RouterID: "*1", Ownership: "owned"}}
	got, err = routingBlockMangleIssues(context.Background(), recorder, desired, actual)
	if err != nil || len(got) != 0 {
		t.Fatalf("owned rule falsely blocked: %#v %v", got, err)
	}
}

func TestBlockMarksAreScopedToManagerAndDevice(t *testing.T) {
	target := &routingTargetProjection{id: "t", list: "targets", source: Source{ID: "t", Kind: KindDomain}, plainRules: []SourceRule{{RuleType: "DOMAIN", Domain: "example.test"}}}
	rule := RoutingRule{ID: "r", EgressID: "e", Enabled: true, TargetListIDs: []string{"t"}, SourceScope: &RoutingSourceScope{Kind: RoutingSourceInterface, Name: "lan"}}
	marks := map[string]bool{}
	for _, scope := range [][2]string{{"a", "x"}, {"b", "x"}, {"a", "y"}} {
		result := DesiredResult{}
		add := func(id string, menu routeros.MutationMenu, phase string, fields map[string]string) {
			result.Objects = append(result.Objects, DesiredObject{LogicalID: id, Menu: string(menu), Fields: fields})
		}
		buildRoutingMangleFamily(&result, add, Egress{ID: "e", Enabled: true, RouterOutput: true}, EgressFamily{Family: FamilyIPv4}, nil, nil, "", []*routingTargetProjection{target}, []RoutingRule{rule}, nil, "no", scope[0], scope[1])
		for _, o := range result.Objects {
			if isRoutingBlockFilter(o) {
				mark := o.Fields["connection-mark"]
				if marks[mark] {
					t.Fatalf("cross-scope mark reused: %s", mark)
				}
				marks[mark] = true
			}
		}
	}
	if len(marks) != 6 {
		t.Fatalf("marks=%#v", marks)
	}
}

type matcherTransitionRecorder struct {
	moveRecorder
	steps []string
}

func (r *matcherTransitionRecorder) Patch(_ context.Context, _ routeros.MutationMenu, _ string, fields routeros.RouterOSFields) (routeros.RouterOSObject, error) {
	if fields["disabled"] != true {
		return nil, fmt.Errorf("patch must remain disabled: %#v", fields)
	}
	if _, exists := fields["in-interface"]; exists {
		return nil, fmt.Errorf("empty interface was patched")
	}
	if fields["in-interface-list"] != nil {
		r.steps = append(r.steps, "patch")
	} else {
		r.steps = append(r.steps, "disable")
	}
	return routeros.RouterOSObject{}, nil
}
func (r *matcherTransitionRecorder) UnsetFirewallMatcher(_ context.Context, _ routeros.MutationMenu, _ string, field string) error {
	r.steps = append(r.steps, "unset:"+field)
	return nil
}
func TestMatcherShapeTransitionDisablesAndUnsetsBeforePatch(t *testing.T) {
	r := &matcherTransitionRecorder{}
	op := PlanOperation{Action: "patch", Menu: string(routeros.MenuIPFirewallMangle), RouterID: "*A", After: map[string]string{"in-interface": "", "in-interface-list": "aggregate"}}
	if err := applyStagedOperation(context.Background(), r, op, nil); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(r.steps, ","); got != "disable,unset:in-interface,patch" {
		t.Fatalf("sequence %s", got)
	}
}

func TestRemovedRoutingBlockWarningRecognizesScopedStaleIdentity(t *testing.T) {
	op := PlanOperation{Action: "delete", Menu: string(routeros.MenuIPFirewallFilter), LogicalID: "stale:rbs_12345678_abcdef12", Ownership: "owned", Before: map[string]string{"action": "drop", "connection-mark": "rb_bl_123"}}
	if !isRemovedRoutingBlock(op) {
		t.Fatal("stale owned block omitted from warning")
	}
	op.Ownership = "foreign"
	if isRemovedRoutingBlock(op) {
		t.Fatal("foreign rule treated as ours")
	}
}

func TestRoutingBlockStaleOwnershipNeverEntersAccessPlan(t *testing.T) {
	id := "routing-rule-block:e:ipv4:direct:enabled"
	own := routeros.RouterOSObject{".id": "*A", "chain": "forward", "action": "drop", "connection-mark": "rb_bl_own", "comment": managedComment("manager-a", "device-a", id, "策略测试")}
	foreign := routeros.RouterOSObject{".id": "*B", "chain": "forward", "action": "drop", "connection-mark": "rb_bl_foreign", "comment": managedComment("manager-b", "device-a", id, "策略测试")}
	mutation := &fakeScanMutation{objects: map[routeros.MutationMenu][]routeros.RouterOSObject{routeros.MenuIPFirewallFilter: {own, foreign}}}
	repo := &fakeScanRepository{managerID: "manager-a", deviceID: "device-a"}
	access, _, err := ScanManagedForDomain(context.Background(), mutation, repo, nil, PolicyDomainAccess)
	if err != nil {
		t.Fatal(err)
	}
	ops, _ := DiffDesired(nil, access)
	if len(ops) != 0 {
		t.Fatalf("access adopted routing or foreign block: %#v", ops)
	}
	routing, _, err := ScanManagedForDomain(context.Background(), mutation, repo, nil, PolicyDomainRouting)
	if err != nil {
		t.Fatal(err)
	}
	ops, _ = DiffDesired(nil, routing)
	if len(ops) != 1 || ops[0].Action != "delete" || ops[0].RouterID != "*A" {
		t.Fatalf("routing cleanup crossed ownership scope: %#v", ops)
	}
}

func TestRouteAndBlockUseSamePriorityWinner(t *testing.T) {
	for _, tc := range []struct {
		name                         string
		routePriority, blockPriority int
		blockEnabled, wantBlock      bool
	}{{"block wins", 100, 10, true, true}, {"route wins", 10, 100, true, false}, {"disabled high block", 100, 10, false, false}} {
		t.Run(tc.name, func(t *testing.T) {
			target := &routingTargetProjection{id: "t", list: "shared", source: Source{ID: "t", Kind: KindDomain}, plainRules: []SourceRule{{RuleType: "DOMAIN", Domain: "example.test"}}}
			result := DesiredResult{}
			add := func(id string, menu routeros.MutationMenu, phase string, fields map[string]string) {
				result.Objects = append(result.Objects, DesiredObject{LogicalID: id, Menu: string(menu), Fields: fields, Order: len(result.Objects) + 1})
			}
			for _, setup := range []struct {
				id, table string
				priority  int
				enabled   bool
			}{{"route", "route-table", tc.routePriority, true}, {"block", "", tc.blockPriority, tc.blockEnabled}} {
				rule := RoutingRule{ID: setup.id, EgressID: setup.id, Priority: setup.priority, Enabled: setup.enabled, TargetListIDs: []string{"t"}, SourceScope: &RoutingSourceScope{Kind: RoutingSourceInterface, Name: "lan"}}
				buildRoutingMangleFamily(&result, add, Egress{ID: setup.id, Enabled: true}, EgressFamily{Family: FamilyIPv4}, nil, nil, setup.table, []*routingTargetProjection{target}, []RoutingRule{rule}, nil, "no", "m", "d")
			}
			reorderRoutingConnectionObjects(&result)
			winner := ""
			for _, o := range result.Objects {
				if isRoutingPreroutingConnection(o) && o.Fields["disabled"] == "no" {
					winner = o.Fields["new-connection-mark"]
					break
				}
			}
			block := false
			route := false
			for _, o := range result.Objects {
				if o.Fields["disabled"] != "no" || o.Fields["connection-mark"] != winner {
					continue
				}
				if isRoutingBlockFilter(o) && o.Fields["action"] == "drop" {
					block = true
				}
				if isRoutingPreroutingRouting(o) && o.Fields["action"] == "mark-routing" {
					route = true
				}
			}
			if block != tc.wantBlock || route == tc.wantBlock {
				t.Fatalf("winner=%s block=%v route=%v objects=%#v", winner, block, route, result.Objects)
			}
		})
	}
}

func TestAccessOrderingPreservesRoutingBlockPrefix(t *testing.T) {
	id := "routing-rule-block:e:ipv4:direct:enabled"
	block := routeros.RouterOSObject{".id": "*A", "chain": "forward", "action": "drop", "connection-mark": "rb_bl_123", "comment": managedComment("manager", "device", id, "入口地址族阻断")}
	access := accessJumpForTest("access:rule:ipv4:jump", 1)
	foreign := routeros.RouterOSObject{".id": "*B", "chain": "forward", "action": "accept"}
	jump := routeros.RouterOSObject{".id": "*C", "chain": "forward", "action": "jump", "comment": access.Fields["comment"]}
	recorder := &applyingListedMoveRecorder{listedMoveRecorder: listedMoveRecorder{objects: map[routeros.MutationMenu][]routeros.RouterOSObject{routeros.MenuIPFirewallFilter: {block, foreign, jump}}}}
	planned, err := planAccessJumpsFirst(context.Background(), recorder, []DesiredObject{access})
	if err != nil || len(planned) != 1 || planned[0].RouterID != "*C" || planned[0].Anchor.RouterID != "*B" {
		t.Fatalf("plan=%#v err=%v", planned, err)
	}
	if err := ensureAccessJumpsFirst(context.Background(), recorder, []DesiredObject{access}); err != nil {
		t.Fatal(err)
	}
	objects := recorder.objects[routeros.MenuIPFirewallFilter]
	if objects[0].ID() != "*A" || objects[1].ID() != "*C" || len(recorder.moves) != 1 || recorder.moves[0].ID != "*C" {
		t.Fatalf("access reordered routing block: objects=%#v moves=%#v", objects, recorder.moves)
	}
	if err := ensureAccessJumpsFirst(context.Background(), recorder, []DesiredObject{access}); err != nil || len(recorder.moves) != 1 {
		t.Fatalf("non-idempotent: %#v %v", recorder.moves, err)
	}
	recorder.objects[routeros.MenuIPFirewallFilter] = []routeros.RouterOSObject{foreign, block, jump}
	if _, err := planAccessJumpsFirst(context.Background(), recorder, []DesiredObject{access}); err == nil {
		t.Fatal("non-contiguous routing block must require routing reconciliation")
	}
}
