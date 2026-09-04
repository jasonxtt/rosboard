package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"rosboard/internal/accesscontrol"
	"rosboard/internal/policyv2"
)

func accessControlRequest(server *Server, method, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, "/api/access-control/devices/edge"+path, bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.serveAccessControlAPI(response, request)
	return response
}

func accessTestTerminals() []accesscontrol.Terminal {
	return []accesscontrol.Terminal{
		{ID: "mac:aa", DisplayName: "iPad", MACAddress: "AA:BB:CC:DD:EE:FF", IPv4: []string{"10.0.0.20"}, IPv6: []string{"fd00::20"}},
		{ID: "mac:bb", DisplayName: "客厅电视", MACAddress: "BA:BB:BB:BB:BB:BB", IPv4: []string{"10.0.0.21"}},
	}
}

func seedAccessSource(t *testing.T, server *Server, sourceID string) policyv2.Source {
	t.Helper()
	deviceStore, err := server.store.OpenDevice("edge")
	if err != nil {
		t.Fatal(err)
	}
	policyRepository := deviceStore.PolicyRepository()
	source, err := policyRepository.SaveSource(context.Background(), policyv2.Source{
		ID: sourceID, Type: "manual", Kind: policyv2.KindIP, Name: "Source " + sourceID, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := policyRepository.SavePendingSourceVersion(context.Background(), policyv2.SourceVersion{
		ID: sourceID + "-v1", SourceID: source.ID, SHA256: sourceID, CompressedYAML: []byte(sourceID),
	}, []policyv2.SourceRule{{RuleType: "IP-CIDR", Domain: "203.0.113.0/24"}}); err != nil {
		t.Fatal(err)
	}
	state, err := policyRepository.GetDeviceState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := policyRepository.CommitApply(context.Background(), state.DesiredRevision, -1, "seed-"+sourceID, policyv2.ApplyJob{ID: "seed-" + sourceID, PlanID: "seed-" + sourceID}, nil, true); err != nil {
		t.Fatal(err)
	}
	source, err = policyRepository.GetSource(context.Background(), sourceID)
	if err != nil {
		t.Fatal(err)
	}
	return source
}

func TestAccessControlRuleAPIAcceptsMultiClientMultiSourcePayload(t *testing.T) {
	server, storage := newPolicyV2APIServer(t)
	defer storage.Close()
	server.accessTerminalsFn = func(string) []accesscontrol.Terminal { return accessTestTerminals() }
	source := seedAccessSource(t, server, "bilibili")

	create := accessControlRequest(server, http.MethodPost, "/rules", `{
		"id":"","name":"儿童娱乐限制","targetScope":"targets","targetListIds":["bilibili"],"subject":{"mode":"selected","members":[
			{"terminalId":"mac:aa","binding":"auto","pinnedIpv4":[],"pinnedIpv6":[]},
			{"terminalId":"mac:bb","binding":"fixed","pinnedIpv4":["10.0.0.21"],"pinnedIpv6":[]}
		]},
		"enabled":true,"revision":0
	}`)
	if create.Code != http.StatusAccepted {
		t.Fatalf("create status=%d body=%s", create.Code, create.Body.String())
	}
	var created struct {
		Rule  accesscontrol.AccessRule `json:"rule"`
		JobID string                   `json:"jobId"`
	}
	if err := json.Unmarshal(create.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Rule.ID == "" || created.Rule.Revision != 1 || created.JobID == "" || len(created.Rule.TargetListIDs) != 1 || created.Rule.TargetListIDs[0] != source.ID {
		t.Fatalf("unexpected create response: %#v", created)
	}

	overview := accessControlRequest(server, http.MethodGet, "", "")
	if overview.Code != http.StatusOK {
		t.Fatalf("overview status=%d body=%s", overview.Code, overview.Body.String())
	}
	var payload struct {
		Device      map[string]any        `json:"device"`
		Rules       []accessRuleResponse  `json:"rules"`
		TargetLists []policyv2.TargetList `json:"targetLists"`
		Boundary    string                `json:"boundary"`
	}
	if err := json.Unmarshal(overview.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Device["id"] != "edge" || len(payload.Rules) != 1 || len(payload.TargetLists) != 1 || payload.Boundary == "" || !bytes.Contains([]byte(payload.Boundary), []byte("目标库中的域名/IP 列表")) || !bytes.Contains([]byte(payload.Boundary), []byte("并非 DPI")) {
		t.Fatalf("unexpected overview contract: %#v", payload)
	}
	rule := payload.Rules[0]
	if rule.Name != "儿童娱乐限制" || len(rule.Members) != 2 || rule.Status == "" {
		t.Fatalf("unexpected rule payload: %#v", rule)
	}

	// 自动跟随成员在保存时必须能解析：幽灵成员不允许。
	ghost := accessControlRequest(server, http.MethodPost, "/rules", `{
		"id":"","name":"幽灵","targetScope":"targets","targetListIds":["bilibili"],"subject":{"mode":"selected","members":[{"terminalId":"mac:zz","binding":"auto"}]},
		"enabled":true
	}`)
	if ghost.Code != http.StatusUnprocessableEntity || !bytes.Contains(ghost.Body.Bytes(), []byte("terminal_unavailable")) {
		t.Fatalf("ghost member must be rejected: status=%d body=%s", ghost.Code, ghost.Body.String())
	}
	// 固定 IP 只能固定该设备当前观察到的地址。
	stranger := accessControlRequest(server, http.MethodPost, "/rules", `{
		"id":"","name":"陌生人","targetScope":"targets","targetListIds":["bilibili"],"subject":{"mode":"selected","members":[{"terminalId":"mac:aa","binding":"fixed","pinnedIpv4":["192.0.2.9"]}]},
		"enabled":true
	}`)
	if stranger.Code != http.StatusUnprocessableEntity || !bytes.Contains(stranger.Body.Bytes(), []byte("invalid_pinned_address")) {
		t.Fatalf("pinned stranger address must be rejected: status=%d body=%s", stranger.Code, stranger.Body.String())
	}
	// internet 规则不允许携带来源。
	mixed := accessControlRequest(server, http.MethodPost, "/rules", `{
		"id":"","name":"混合","targetScope":"internet","targetListIds":["bilibili"],"subject":{"mode":"selected","members":[{"terminalId":"mac:aa","binding":"auto"}]},
		"enabled":true
	}`)
	if mixed.Code != http.StatusUnprocessableEntity {
		t.Fatalf("internet rule with sources must be rejected: status=%d body=%s", mixed.Code, mixed.Body.String())
	}
}

func TestAccessControlAPIRejectsLegacyApplicationAuthority(t *testing.T) {
	server, storage := newPolicyV2APIServer(t)
	defer storage.Close()
	server.accessTerminalsFn = func(string) []accesscontrol.Terminal { return accessTestTerminals() }
	legacy := accessControlRequest(server, http.MethodPost, "/rules", `{
		"id":"","name":"旧应用","targetScope":"applications","applicationIds":["oaf:1001"],
		"subject":{"mode":"selected","members":[{"terminalId":"mac:aa","binding":"fixed","pinnedIpv4":["10.0.0.20"]}]},"enabled":true
	}`)
	if legacy.Code != http.StatusUnprocessableEntity || !bytes.Contains(legacy.Body.Bytes(), []byte("canonical_access_rule_required")) {
		t.Fatalf("legacy application payload must be rejected after the authority cutover: status=%d body=%s", legacy.Code, legacy.Body.String())
	}
}

func TestAccessControlSyncExplainsUnavailableInternetEgress(t *testing.T) {
	server, storage := newPolicyV2APIServer(t)
	defer storage.Close()
	server.accessTerminalsFn = func(string) []accesscontrol.Terminal { return accessTestTerminals() }
	deviceStore, err := storage.OpenDevice("edge")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := deviceStore.AccessRepository().SaveRule(context.Background(), accesscontrol.AccessRule{
		ID: "internet-rule", Name: "睡觉断网", TargetScope: accesscontrol.TargetScopeInternet, Enabled: true,
	}, []accesscontrol.RuleMember{{
		RuleID: "internet-rule", TerminalID: "mac:aa", Binding: accesscontrol.BindingFixed, PinnedIPv4: []string{"10.0.0.20"},
	}}, "test"); err != nil {
		t.Fatal(err)
	}

	response := accessControlRequest(server, http.MethodPost, "/sync", "")
	if response.Code != http.StatusUnprocessableEntity || !bytes.Contains(response.Body.Bytes(), []byte("plan_blocked")) || !bytes.Contains(response.Body.Bytes(), []byte("默认路由出口接口")) {
		t.Fatalf("internet rule without a discovered egress must fail closed with a clear reason: status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestAccessControlEditKeepsExistingMembersWhenMonitorTemporarilyLosesIdentity(t *testing.T) {
	server, storage := newPolicyV2APIServer(t)
	defer storage.Close()
	terminals := accessTestTerminals()
	server.accessTerminalsFn = func(string) []accesscontrol.Terminal { return terminals }
	seedAccessSource(t, server, "games")
	deviceStore, err := storage.OpenDevice("edge")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := deviceStore.AccessRepository().SaveRule(context.Background(), accesscontrol.AccessRule{
		ID: "existing-access-rule", Name: "游戏阻断", TargetScope: accesscontrol.TargetScopeTargets, TargetListIDs: []string{"games"}, Enabled: true,
	}, []accesscontrol.RuleMember{{RuleID: "existing-access-rule", TerminalID: "mac:aa", Binding: accesscontrol.BindingAuto, AnchorMAC: "AA:BB:CC:DD:EE:FF"}}, "test"); err != nil {
		t.Fatal(err)
	}
	terminals = []accesscontrol.Terminal{{ID: "mac:aa", DisplayName: "iPad", MACAddress: "not-a-mac", IPv4: []string{"10.0.0.20"}}}
	update := accessControlRequest(server, http.MethodPut, "/rules/existing-access-rule", `{
		"name":"游戏阻断（改名）","targetScope":"targets","targetListIds":["games"],"subject":{"mode":"selected","members":[{"terminalId":"mac:aa","binding":"auto","pinnedIpv4":[],"pinnedIpv6":[]}]},
		"enabled":true,"revision":1
	}`)
	if update.Code != http.StatusAccepted {
		t.Fatalf("editing an existing temporarily unresolved member must be accepted: status=%d body=%s", update.Code, update.Body.String())
	}
}

func TestAccessControlSourcesScopeRuleAppliesOnTestRouter(t *testing.T) {
	server, storage := newPolicyV2APIServer(t)
	defer storage.Close()
	server.accessTerminalsFn = func(string) []accesscontrol.Terminal { return accessTestTerminals() }
	seedAccessSource(t, server, "games")

	create := accessControlRequest(server, http.MethodPost, "/rules", `{
		"id":"","name":"游戏阻断","targetScope":"targets","targetListIds":["games"],"subject":{"mode":"selected","members":[{"terminalId":"mac:bb","binding":"fixed","pinnedIpv4":["10.0.0.21"]}]},
		"enabled":true
	}`)
	if create.Code != http.StatusAccepted {
		t.Fatalf("create status=%d body=%s", create.Code, create.Body.String())
	}
	// 保存即自动 apply：job 已在后台执行，等待状态收敛即可。
	var created struct {
		JobID string `json:"jobId"`
	}
	if err := json.Unmarshal(create.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.JobID == "" {
		t.Fatal("save must trigger automatic apply")
	}
}

func TestAccessRuleStatusDependsOnAccessStateOnly(t *testing.T) {
	rule := accesscontrol.AccessRule{ID: "rule-a", Name: "规则", Enabled: true}
	members := []accessRuleMemberResponse{{TerminalID: "terminal-a", State: accesscontrol.MemberResolved}}
	accessState := accesscontrol.State{DesiredRevision: 1, AppliedRevision: 1}
	policyState := policyv2.DeviceState{DesiredRevision: 1, AppliedRevision: 1}

	if got := accessRuleStatus(rule, accessState, policyState, members); got != "applied" {
		t.Fatalf("fully applied rule status=%q, want applied", got)
	}
	policyState.AppliedRevision = 0
	if got := accessRuleStatus(rule, accessState, policyState, members); got != "applied" {
		t.Fatalf("rule with pending routing state status=%q, want applied", got)
	}
	policyState.Job = policyv2.ApplyJob{ID: "job-a", State: "failed"}
	if got := accessRuleStatus(rule, accessState, policyState, members); got != "failed" {
		t.Fatalf("rule with failed policy job status=%q, want failed", got)
	}
}

func TestAccessRuleStatusShowsPendingUntilDesiredRevisionIsApplied(t *testing.T) {
	rule := accesscontrol.AccessRule{ID: "rule-a", Name: "规则", Enabled: true}
	state := accesscontrol.State{DesiredRevision: 2, AppliedRevision: 1}
	policyState := policyv2.DeviceState{DesiredRevision: 1, AppliedRevision: 1}
	members := []accessRuleMemberResponse{{TerminalID: "terminal-a", State: accesscontrol.MemberResolved}}
	if got := accessRuleStatus(rule, state, policyState, members); got != "pending" {
		t.Fatalf("enabled rule with unapplied desired revision status=%q, want pending", got)
	}
}

func TestAccessRuleStatusShowsApplyingDuringQueuedAccessApply(t *testing.T) {
	rule := accesscontrol.AccessRule{ID: "rule-a", Name: "规则", Enabled: true}
	state := accesscontrol.State{DesiredRevision: 2, AppliedRevision: 1}
	members := []accessRuleMemberResponse{{TerminalID: "terminal-a", State: accesscontrol.MemberResolved}}
	for _, jobState := range []string{"queued", "staging", "verifying"} {
		policyState := policyv2.DeviceState{DesiredRevision: 1, AppliedRevision: 1, Job: policyv2.ApplyJob{ID: "job-a", State: jobState}}
		if got := accessRuleStatus(rule, state, policyState, members); got != "applying" {
			t.Fatalf("access job state=%s status=%q, want applying", jobState, got)
		}
	}
}

func TestAccessRuleStatusShowsAppliedOnlyAfterCommittedAccessState(t *testing.T) {
	rule := accesscontrol.AccessRule{ID: "rule-a", Name: "规则", Enabled: true}
	state := accesscontrol.State{DesiredRevision: 2, AppliedRevision: 2}
	policyState := policyv2.DeviceState{DesiredRevision: 1, AppliedRevision: 1, Job: policyv2.ApplyJob{ID: "job-a", State: "committed"}}
	members := []accessRuleMemberResponse{{TerminalID: "terminal-a", State: accesscontrol.MemberResolved}}
	if got := accessRuleStatus(rule, state, policyState, members); got != "applied" {
		t.Fatalf("committed access rule status=%q, want applied", got)
	}
}

func TestDisabledAccessRuleStatusDoesNotShowPending(t *testing.T) {
	rule := accesscontrol.AccessRule{ID: "rule-a", Name: "规则", Enabled: false}
	state := accesscontrol.State{DesiredRevision: 2, AppliedRevision: 1}
	policyState := policyv2.DeviceState{DesiredRevision: 1, AppliedRevision: 1}
	members := []accessRuleMemberResponse{{TerminalID: "terminal-a", State: accesscontrol.MemberResolved}}
	if got := accessRuleStatus(rule, state, policyState, members); got != "disabled" {
		t.Fatalf("disabled rule with an unapplied access revision status=%q, want disabled", got)
	}
}

func TestAccessRuleStatusIgnoresLegacyTargetEnabledFlag(t *testing.T) {
	rule := accesscontrol.AccessRule{ID: "rule-a", Name: "规则", Enabled: true, TargetScope: accesscontrol.TargetScopeTargets, TargetListIDs: []string{"target-a"}}
	state := accesscontrol.State{DesiredRevision: 1, AppliedRevision: 1}
	policyState := policyv2.DeviceState{DesiredRevision: 1, AppliedRevision: 1}
	targets := []policyv2.TargetList{{ID: "target-a", Name: "视频列表", Enabled: false, ActiveVersionID: "version-a"}}
	response := buildAccessRuleResponse(rule, []accesscontrol.RuleMember{{RuleID: rule.ID, TerminalID: "terminal-a", Binding: accesscontrol.BindingFixed, PinnedIPv4: []string{"10.0.0.20"}}}, []accesscontrol.Terminal{}, state, policyState, targets)
	if response.Status != "applied" || len(response.Issues) != 0 {
		t.Fatalf("legacy target enabled flag must not affect a referenced target: %#v", response)
	}
}

func TestAccessRuleStatusAcceptsReferencedPendingTargetVersion(t *testing.T) {
	rule := accesscontrol.AccessRule{ID: "rule-a", Name: "规则", Enabled: true, TargetScope: accesscontrol.TargetScopeTargets, TargetListIDs: []string{"target-a"}}
	state := accesscontrol.State{DesiredRevision: 1, AppliedRevision: 1}
	policyState := policyv2.DeviceState{DesiredRevision: 1, AppliedRevision: 1}
	targets := []policyv2.TargetList{{ID: "target-a", Name: "视频列表", Enabled: true, PendingVersionID: "pending-version"}}
	response := buildAccessRuleResponse(rule, []accesscontrol.RuleMember{{RuleID: rule.ID, TerminalID: "terminal-a", Binding: accesscontrol.BindingFixed, PinnedIPv4: []string{"10.0.0.20"}}}, []accesscontrol.Terminal{}, state, policyState, targets)
	if response.Status != "applied" || len(response.Issues) != 0 {
		t.Fatalf("pending referenced target must remain usable: %#v", response)
	}
}

func TestAccessRuleStatusShowsPendingWhenMonitorProjectionChanged(t *testing.T) {
	rule := accesscontrol.AccessRule{ID: "rule-a", Name: "规则", Enabled: true}
	state := accesscontrol.State{DesiredRevision: 1, AppliedRevision: 1}
	policyState := policyv2.DeviceState{DesiredRevision: 1, AppliedRevision: 1}
	members := []accesscontrol.RuleMember{{RuleID: rule.ID, TerminalID: "terminal-a", Binding: accesscontrol.BindingAuto, AnchorMAC: "AA:BB:CC:DD:EE:FF", LastIPv4: []string{"10.0.0.20"}}}
	response := buildAccessRuleResponse(rule, members, []accesscontrol.Terminal{{ID: "terminal-a", MACAddress: "AA:BB:CC:DD:EE:FF", IPv4: []string{"10.0.0.21"}}}, state, policyState, nil)
	if response.Status != "pending" {
		t.Fatalf("a changed monitor projection must not be reported as applied: %#v", response)
	}
}

func TestAccessRuleStatusDegradesWhenStoredAnchorIsInvalid(t *testing.T) {
	rule := accesscontrol.AccessRule{ID: "rule-a", Name: "规则", Enabled: true}
	state := accesscontrol.State{DesiredRevision: 1, AppliedRevision: 1}
	policyState := policyv2.DeviceState{DesiredRevision: 1, AppliedRevision: 1}
	members := []accesscontrol.RuleMember{{RuleID: rule.ID, TerminalID: "terminal-a", Binding: accesscontrol.BindingAuto, AnchorMAC: "not-a-mac", LastIPv4: []string{"10.0.0.20"}}}
	response := buildAccessRuleResponse(rule, members, nil, state, policyState, nil)
	if response.Status != "degraded" {
		t.Fatalf("an invalid stored identity anchor must be degraded, not pending forever: %#v", response)
	}
}

func TestAccessRuleStatusDoesNotDependOnLocalScopeForInternetRule(t *testing.T) {
	rule := accesscontrol.AccessRule{ID: "rule-a", Name: "规则", TargetScope: accesscontrol.TargetScopeInternet, Enabled: true}
	state := accesscontrol.State{DesiredRevision: 1, AppliedRevision: 1}
	policyState := policyv2.DeviceState{DesiredRevision: 1, AppliedRevision: 1}
	response := buildAccessRuleResponse(rule, []accesscontrol.RuleMember{{RuleID: rule.ID, TerminalID: "terminal-a", Binding: accesscontrol.BindingFixed, PinnedIPv4: []string{"10.0.0.20"}}}, nil, state, policyState, nil)
	if response.Status != "applied" || len(response.Issues) != 0 {
		t.Fatalf("an applied internet rule must not depend on a cached local scope: %#v", response)
	}
}

// 回归：概览接口里的数组字段必须序列化为 [] 而不是 null，
// 前端直接对数组做展开和 .length 访问。
func TestAccessControlOverviewNeverMarshalsNullArrays(t *testing.T) {
	server, storage := newPolicyV2APIServer(t)
	defer storage.Close()
	server.accessTerminalsFn = func(string) []accesscontrol.Terminal { return accessTestTerminals() }
	deviceStore, err := storage.OpenDevice("edge")
	if err != nil {
		t.Fatal(err)
	}
	seedAccessSource(t, server, "domain-list")
	if _, err := deviceStore.AccessRepository().SaveRule(context.Background(), accesscontrol.AccessRule{
		ID: "rule-a", Name: "规则", TargetScope: accesscontrol.TargetScopeSources, SourceIDs: []string{"domain-list"}, Enabled: true,
	}, []accesscontrol.RuleMember{{
		RuleID: "rule-a", TerminalID: "mac:aa", Binding: accesscontrol.BindingAuto, AnchorMAC: "AA:BB:CC:DD:EE:FF",
	}}, "test"); err != nil {
		t.Fatal(err)
	}

	response := accessControlRequest(server, http.MethodGet, "", "")
	if response.Code != http.StatusOK {
		t.Fatalf("overview status=%d body=%s", response.Code, response.Body.String())
	}
	body := response.Body.Bytes()
	if !bytes.Contains(body, []byte(`"macAddress"`)) {
		t.Fatalf("access-control overview must expose terminal MAC identities for routing selectors: %s", body)
	}
	for _, fragment := range []string{"\"ipv4\":null", "\"ipv6\":null", "\"routingIpv4\":null", "\"routingIpv6\":null", "\"targetListIds\":null", "\"members\":null", "\"issues\":null", "\"versions\":null", "\"terminals\":null", "\"targetLists\":null", "\"rules\":null"} {
		if bytes.Contains(body, []byte(fragment)) {
			t.Fatalf("overview response contains %s: %s", fragment, body)
		}
	}
}
