package api

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"rosboard/internal/accesscontrol"
	"rosboard/internal/applicationpreset"
	"rosboard/internal/policy"
	"rosboard/internal/policyv2"
	"rosboard/internal/routeros"
)

type proposalPolicyRouter struct {
	policyV2Router
	mu      sync.Mutex
	nextID  int
	objects map[routeros.MutationMenu]map[string]routeros.RouterOSObject
	order   map[routeros.MutationMenu][]string
}

func (r *proposalPolicyRouter) List(_ context.Context, menu routeros.MutationMenu, _ routeros.MutationQuery) ([]routeros.RouterOSObject, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]routeros.RouterOSObject, 0, len(r.objects[menu]))
	seen := make(map[string]bool, len(r.order[menu]))
	for _, id := range r.order[menu] {
		if object := r.objects[menu][id]; object != nil {
			result = append(result, cloneProposalRouterObject(object))
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
		result = append(result, cloneProposalRouterObject(r.objects[menu][id]))
	}
	return result, nil
}

func (r *proposalPolicyRouter) Create(_ context.Context, menu routeros.MutationMenu, fields routeros.RouterOSFields) (routeros.RouterOSObject, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
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
	return cloneProposalRouterObject(object), nil
}

func (r *proposalPolicyRouter) Patch(_ context.Context, menu routeros.MutationMenu, id string, fields routeros.RouterOSFields) (routeros.RouterOSObject, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	object := r.objects[menu][id]
	if object == nil {
		return nil, fmt.Errorf("missing object %s", id)
	}
	for key, value := range fields {
		object[key] = fmt.Sprint(value)
	}
	return cloneProposalRouterObject(object), nil
}

func (r *proposalPolicyRouter) Delete(_ context.Context, menu routeros.MutationMenu, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.objects[menu], id)
	r.removeFromOrder(menu, id)
	return nil
}

func (r *proposalPolicyRouter) Move(_ context.Context, menu routeros.MutationMenu, request routeros.MoveRequest) (routeros.MutationResponse, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.objects[menu][request.ID] == nil {
		return routeros.MutationResponse{}, fmt.Errorf("missing move object %s", request.ID)
	}
	r.removeFromOrder(menu, request.ID)
	if request.BeforeID == "" {
		r.order[menu] = append(r.order[menu], request.ID)
		return routeros.MutationResponse{}, nil
	}
	for index, id := range r.order[menu] {
		if id == request.BeforeID {
			r.order[menu] = append(append(append([]string(nil), r.order[menu][:index]...), request.ID), r.order[menu][index:]...)
			return routeros.MutationResponse{}, nil
		}
	}
	return routeros.MutationResponse{}, fmt.Errorf("missing move destination %s", request.BeforeID)
}

func (r *proposalPolicyRouter) removeFromOrder(menu routeros.MutationMenu, id string) {
	for index, current := range r.order[menu] {
		if current == id {
			r.order[menu] = append(r.order[menu][:index], r.order[menu][index+1:]...)
			return
		}
	}
}

func cloneProposalRouterObject(object routeros.RouterOSObject) routeros.RouterOSObject {
	result := make(routeros.RouterOSObject, len(object))
	for key, value := range object {
		result[key] = value
	}
	return result
}

func TestApplicationPresetAPIListsPreviewsSplitsAndReusesTargetLists(t *testing.T) {
	server, storage := newPolicyV2APIServer(t)
	defer storage.Close()
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/plain")
		_, _ = writer.Write([]byte(strings.Join([]string{
			"DOMAIN,example.com",
			"DOMAIN-SUFFIX,cdn.example",
			"IP-CIDR,203.0.113.0/24",
			"DOMAIN-KEYWORD,ignored",
		}, "\n")))
	}))
	defer upstream.Close()
	localAddr := strings.TrimPrefix(upstream.URL, "https://")
	dialer := &net.Dialer{}
	server.sourceFetcher = policy.NewSourceFetcher(policy.FetcherOptions{
		Resolver: targetListTestResolver{},
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, localAddr)
		},
		TLSConfig: &tls.Config{InsecureSkipVerify: true}, // test server uses a localhost certificate.
	})
	server.SetApplicationPresetRegistry(applicationpreset.New([]applicationpreset.ApplicationPreset{{
		ID: "fixture", Name: "Fixture", Category: "测试", RuleURL: "https://preset.example.test/fixture.yaml",
	}}))

	listResponse := httptest.NewRecorder()
	server.ServeHTTP(listResponse, httptest.NewRequest(http.MethodGet, "/api/application-presets", nil))
	if listResponse.Code != http.StatusOK || !strings.Contains(listResponse.Body.String(), `"id":"fixture"`) {
		t.Fatalf("preset list status=%d body=%s", listResponse.Code, listResponse.Body.String())
	}
	previewRequest := httptest.NewRequest(http.MethodPost, "/api/application-presets/fixture/preview?device=edge", nil)
	previewResponse := httptest.NewRecorder()
	server.ServeHTTP(previewResponse, previewRequest)
	if previewResponse.Code != http.StatusOK {
		t.Fatalf("preset preview status=%d body=%s", previewResponse.Code, previewResponse.Body.String())
	}
	var preview struct {
		PreviewID string `json:"previewId"`
		Domain    struct {
			ValidRules int            `json:"validRules"`
			Ignored    map[string]int `json:"ignored"`
		} `json:"domain"`
		IP struct {
			ValidRules int `json:"validRules"`
		} `json:"ip"`
	}
	if err := json.Unmarshal(previewResponse.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	if preview.PreviewID == "" || preview.Domain.ValidRules != 2 || preview.Domain.Ignored["DOMAIN-KEYWORD"] != 1 || preview.IP.ValidRules != 1 {
		t.Fatalf("unexpected preset preview: %#v", preview)
	}

	materialize := func(previewID string, requestedKinds ...string) struct {
		TargetLists []policyv2.TargetList `json:"targetLists"`
	} {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"requestedKinds": requestedKinds})
		request := httptest.NewRequest(http.MethodPost, "/api/application-presets/fixture/target-lists?device=edge&previewId="+previewID, bytes.NewReader(body))
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("preset materialize status=%d body=%s", response.Code, response.Body.String())
		}
		var payload struct {
			TargetLists []policyv2.TargetList `json:"targetLists"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		return payload
	}
	defaultKinds := materialize(preview.PreviewID)
	if len(defaultKinds.TargetLists) != 1 || defaultKinds.TargetLists[0].Kind != policyv2.KindDomain {
		t.Fatalf("omitted requestedKinds did not default to the available Domain projection: %#v", defaultKinds.TargetLists)
	}
	freshPreviewRequest := httptest.NewRequest(http.MethodPost, "/api/application-presets/fixture/preview?device=edge", nil)
	freshPreviewResponse := httptest.NewRecorder()
	server.ServeHTTP(freshPreviewResponse, freshPreviewRequest)
	if freshPreviewResponse.Code != http.StatusOK {
		t.Fatalf("fresh preset preview status=%d body=%s", freshPreviewResponse.Code, freshPreviewResponse.Body.String())
	}
	var freshPreview struct {
		PreviewID string `json:"previewId"`
	}
	if err := json.Unmarshal(freshPreviewResponse.Body.Bytes(), &freshPreview); err != nil {
		t.Fatal(err)
	}
	first := materialize(freshPreview.PreviewID, policy.KindDomain, policy.KindIP)
	if len(first.TargetLists) != 2 || first.TargetLists[0].PresetID != "fixture" || first.TargetLists[1].PresetID != "fixture" {
		t.Fatalf("preset rules were not split into ordinary target lists: %#v", first.TargetLists)
	}
	versionsBefore := make(map[string]int, len(first.TargetLists))
	for _, target := range first.TargetLists {
		versionsBefore[target.ID] = len(target.Versions)
	}

	secondPreviewRequest := httptest.NewRequest(http.MethodPost, "/api/application-presets/fixture/preview?device=edge", nil)
	secondPreviewResponse := httptest.NewRecorder()
	server.ServeHTTP(secondPreviewResponse, secondPreviewRequest)
	if secondPreviewResponse.Code != http.StatusOK {
		t.Fatalf("second preset preview status=%d body=%s", secondPreviewResponse.Code, secondPreviewResponse.Body.String())
	}
	var secondPreview struct {
		PreviewID string `json:"previewId"`
	}
	if err := json.Unmarshal(secondPreviewResponse.Body.Bytes(), &secondPreview); err != nil {
		t.Fatal(err)
	}
	second := materialize(secondPreview.PreviewID, policy.KindDomain, policy.KindIP)
	if len(second.TargetLists) != 2 {
		t.Fatalf("second materialization changed target-list count: %#v", second.TargetLists)
	}
	for _, target := range second.TargetLists {
		if versionsBefore[target.ID] != len(target.Versions) {
			t.Fatalf("reusing preset target list created a duplicate version: before=%v after=%#v", versionsBefore, target)
		}
	}
}

func TestPolicyProposalReusesExistingPresetTargetList(t *testing.T) {
	server, storage := newPolicyV2APIServer(t)
	defer storage.Close()
	ctx := context.Background()
	device, ok := server.resolvePolicyDevice(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/?device=edge", nil))
	if !ok {
		t.Fatal("failed to resolve test policy device")
	}
	server.SetApplicationPresetRegistry(applicationpreset.New([]applicationpreset.ApplicationPreset{{
		ID: "fixture", Name: "Fixture", Category: "测试", RuleURL: "https://preset.example.test/fixture.yaml",
	}}))

	egress, err := device.repository.SaveEgress(ctx, policyv2.Egress{
		ID: "wan", Name: "WAN", Priority: 10, ListMode: policyv2.ListModeShared, ListName: "proxy", DNSUpstream: "1.1.1.1", FakeAlias: "192.0.2.70", FailureMode: "strict", Enabled: true,
		Families: []policyv2.EgressFamily{{Family: policyv2.FamilyIPv4, Enabled: true, WANInterface: "ether1", Gateway: "198.51.100.1", RouteMode: "strict", NATMode: "masquerade"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	target, err := device.repository.SaveTargetList(ctx, policyv2.TargetList{
		ID: "preset:fixture:domain", Name: "Fixture · 域名", Kind: policyv2.KindDomain,
		SourceType: policyv2.TargetSourceTypePreset, PresetID: "fixture", URL: "https://preset.example.test/fixture.yaml", Schedule: "7d", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	content, err := policy.PrepareSourceContent([]byte("DOMAIN,example.com\n"), policy.KindDomain)
	if err != nil {
		t.Fatal(err)
	}
	version, sourceRules, err := content.PendingVersion("edge", target.ID, "fixture-domain-v1")
	if err != nil {
		t.Fatal(err)
	}
	targetRules := make([]policyv2.TargetListRule, len(sourceRules))
	for index, rule := range sourceRules {
		targetRules[index] = policyv2.TargetListRule{VersionID: version.ID, RuleType: rule.RuleType, Domain: rule.Domain}
	}
	if err := device.repository.SavePendingTargetListVersion(ctx, policyv2.TargetListVersion{
		ID: version.ID, TargetListID: target.ID, SHA256: version.SHA256, CompressedYAML: version.CompressedYAML,
		State: "pending", Counts: map[string]int{"valid": len(targetRules)}, CreatedAt: version.CreatedAt,
	}, targetRules); err != nil {
		t.Fatal(err)
	}
	state, err := device.repository.GetDeviceState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := device.repository.CommitApply(ctx, state.DesiredRevision, -1, "seed", policyv2.ApplyJob{ID: "seed", PlanID: "seed", CreatedAt: time.Now().UTC()}, nil, true); err != nil {
		t.Fatal(err)
	}
	existing, err := device.repository.GetTargetList(ctx, target.ID)
	if err != nil {
		t.Fatal(err)
	}
	versionCount := len(existing.Versions)
	revision := existing.Revision
	activeVersionID := existing.ActiveVersionID
	router := &proposalPolicyRouter{objects: make(map[routeros.MutationMenu]map[string]routeros.RouterOSObject), order: make(map[routeros.MutationMenu][]string)}
	manager := policyv2.NewManager(nil)
	if err := manager.RegisterApplier(device.device.ID, &policyv2.Applier{Reader: router, Mutation: router, Repo: device.repository, Access: device.accessRepository}); err != nil {
		t.Fatal(err)
	}
	server.policy = manager

	previewID := server.saveApplicationPresetPreview(applicationPresetPreview{
		DeviceID: device.device.ID, PresetID: "fixture", Domain: content, URL: "https://preset.example.test/fixture.yaml",
		ExpiresAt: time.Now().UTC().Add(time.Minute),
	})
	proposal, err := server.preparePolicyPlanProposal(ctx, device, &policyPlanProposalPayload{
		RoutingRule: &policyv2.RoutingRule{
			ID: "rule-new", Name: "New rule", EgressID: egress.ID, TargetListIDs: []string{target.ID},
			Subject: policyv2.Subject{Mode: policyv2.SubjectModeSelected, Prefixes: []string{"192.0.2.10"}}, Priority: 10, Enabled: true,
		},
		PresetSelections: []policyPlanPresetSelection{{PresetID: "fixture", PreviewID: previewID, RequestedKinds: []string{policy.KindDomain}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(proposal.TargetLists) != 0 {
		t.Fatalf("existing preset backing target was re-materialized in proposal: %#v", proposal.TargetLists)
	}

	plan, err := server.policy.GeneratePlanWithOptions(ctx, device.device.ID, "structural", policyv2.PlanOptions{Proposal: proposal})
	if err != nil {
		t.Fatal(err)
	}
	job, err := server.policy.ApplyPlanWithHash(ctx, device.device.ID, plan.PlanID, plan.PlanHash)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		applied, getErr := device.repository.GetApplyJob(ctx, job.ID)
		if getErr == nil && applied.Terminal() {
			if applied.State != "committed" {
				t.Fatalf("preset reuse proposal apply failed: %#v", applied)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for preset reuse proposal apply: %v", getErr)
		}
		time.Sleep(10 * time.Millisecond)
	}

	finalTarget, err := device.repository.GetTargetList(ctx, target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if finalTarget.Revision != revision || len(finalTarget.Versions) != versionCount || finalTarget.ActiveVersionID != activeVersionID {
		t.Fatalf("reusing existing preset target changed its content state: before revision=%d versions=%d active=%q after=%#v", revision, versionCount, activeVersionID, finalTarget)
	}
	finalRule, err := device.repository.GetRoutingRule(ctx, "rule-new")
	if err != nil {
		t.Fatal(err)
	}
	if len(finalRule.TargetListIDs) != 1 || finalRule.TargetListIDs[0] != target.ID {
		t.Fatalf("new routing rule did not reference existing preset target: %#v", finalRule)
	}
}

func TestPolicyProposalAppliesNewPresetTargetLists(t *testing.T) {
	server, storage := newPolicyV2APIServer(t)
	defer storage.Close()
	ctx := context.Background()
	device, ok := server.resolvePolicyDevice(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/?device=edge", nil))
	if !ok {
		t.Fatal("failed to resolve test policy device")
	}
	server.SetApplicationPresetRegistry(applicationpreset.New([]applicationpreset.ApplicationPreset{{
		ID: "fixture", Name: "Fixture", Category: "测试", RuleURL: "https://preset.example.test/fixture.yaml",
	}}))
	egress, err := device.repository.SaveEgress(ctx, policyv2.Egress{
		ID: "wan", Name: "WAN", Priority: 10, ListMode: policyv2.ListModeShared, ListName: "proxy", DNSUpstream: "1.1.1.1", FakeAlias: "192.0.2.70", FailureMode: "strict", Enabled: true,
		Families: []policyv2.EgressFamily{{Family: policyv2.FamilyIPv4, Enabled: true, WANInterface: "ether1", Gateway: "198.51.100.1", RouteMode: "strict", NATMode: "masquerade"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	domainContent, err := policy.PrepareSourceContent([]byte("DOMAIN,example.com\n"), policy.KindDomain)
	if err != nil {
		t.Fatal(err)
	}
	ipContent, err := policy.PrepareSourceContent([]byte("IP-CIDR,203.0.113.0/24\n"), policy.KindIP)
	if err != nil {
		t.Fatal(err)
	}
	previewID := server.saveApplicationPresetPreview(applicationPresetPreview{
		DeviceID: device.device.ID, PresetID: "fixture", Domain: domainContent, IP: ipContent, URL: "https://preset.example.test/fixture.yaml",
		ExpiresAt: time.Now().UTC().Add(time.Minute),
	})
	router := &proposalPolicyRouter{objects: make(map[routeros.MutationMenu]map[string]routeros.RouterOSObject), order: make(map[routeros.MutationMenu][]string)}
	manager := policyv2.NewManager(nil)
	if err := manager.RegisterApplier(device.device.ID, &policyv2.Applier{Reader: router, Mutation: router, Repo: device.repository, Access: device.accessRepository}); err != nil {
		t.Fatal(err)
	}
	server.policy = manager

	proposal, err := server.preparePolicyPlanProposal(ctx, device, &policyPlanProposalPayload{
		RoutingRule: &policyv2.RoutingRule{
			ID: "rule-new-preset", Name: "New preset rule", EgressID: egress.ID,
			TargetListIDs: []string{"preset:fixture:domain", "preset:fixture:ip"},
			Subject:       policyv2.Subject{Mode: policyv2.SubjectModeSelected, Prefixes: []string{"192.0.2.10"}}, Priority: 10, Enabled: true,
		},
		PresetSelections: []policyPlanPresetSelection{{PresetID: "fixture", PreviewID: previewID, RequestedKinds: []string{policy.KindDomain, policy.KindIP}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(proposal.TargetLists) != 2 {
		t.Fatalf("new preset targets were not retained in proposal: %#v", proposal.TargetLists)
	}
	for _, target := range proposal.TargetLists {
		if target.Version.SHA256 == "" || len(target.Version.CompressedYAML) == 0 || len(target.Rules) == 0 {
			t.Fatalf("new preset target content is incomplete before planning: %#v", target)
		}
	}

	plan, err := server.policy.GeneratePlanWithOptions(ctx, device.device.ID, "structural", policyv2.PlanOptions{Proposal: proposal})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Plan.Blockers) != 0 {
		t.Fatalf("new preset target proposal was blocked: %#v", plan.Plan.Blockers)
	}
	job, err := server.policy.ApplyPlanWithHash(ctx, device.device.ID, plan.PlanID, plan.PlanHash)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		applied, getErr := device.repository.GetApplyJob(ctx, job.ID)
		if getErr == nil && applied.Terminal() {
			if applied.State != "committed" {
				t.Fatalf("new preset target proposal apply failed: %#v", applied)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for new preset target proposal apply: %v", getErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
	for _, targetID := range []string{"preset:fixture:domain", "preset:fixture:ip"} {
		target, err := device.repository.GetTargetList(ctx, targetID)
		if err != nil {
			t.Fatal(err)
		}
		versionID := target.PendingVersionID
		if versionID == "" {
			versionID = target.ActiveVersionID
		}
		if versionID == "" {
			t.Fatalf("new preset target has no applied or pending version: %#v", target)
		}
		versions, err := device.repository.ListTargetListVersions(ctx, targetID)
		if err != nil {
			t.Fatal(err)
		}
		foundContent := false
		for _, version := range versions {
			if version.ID == versionID && version.SHA256 != "" && len(version.CompressedYAML) > 0 {
				foundContent = true
				break
			}
		}
		if !foundContent {
			t.Fatalf("new preset target pending version has no content: %#v", versions)
		}
	}
}

func TestAccessRuleEditCanAddPresetTarget(t *testing.T) {
	server, storage := newPolicyV2APIServer(t)
	defer storage.Close()
	server.accessTerminalsFn = func(string) []accesscontrol.Terminal { return accessTestTerminals() }
	server.SetApplicationPresetRegistry(applicationpreset.New([]applicationpreset.ApplicationPreset{{
		ID: "fixture", Name: "Fixture", Category: "测试", RuleURL: "https://preset.example.test/fixture.yaml",
	}}))
	device, ok := server.resolvePolicyDevice(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/?device=edge", nil))
	if !ok {
		t.Fatal("failed to resolve test policy device")
	}
	router := &proposalPolicyRouter{objects: make(map[routeros.MutationMenu]map[string]routeros.RouterOSObject), order: make(map[routeros.MutationMenu][]string)}
	manager := policyv2.NewManager(nil)
	if err := manager.RegisterApplier(device.device.ID, &policyv2.Applier{Reader: router, Mutation: router, Repo: device.repository, Access: device.accessRepository}); err != nil {
		t.Fatal(err)
	}
	server.policy = manager
	ctx := context.Background()
	existing, err := device.repository.SaveTargetList(ctx, policyv2.TargetList{
		ID: "access-edit-domain", Name: "Existing domain", Kind: policyv2.KindDomain, SourceType: policyv2.TargetSourceTypeManual, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	content, err := policy.PrepareSourceContent([]byte("DOMAIN-SUFFIX,existing.example\n"), policy.KindDomain)
	if err != nil {
		t.Fatal(err)
	}
	version, rules, err := content.PendingVersion("edge", existing.ID, "access-edit-domain-v1")
	if err != nil {
		t.Fatal(err)
	}
	targetRules := make([]policyv2.TargetListRule, len(rules))
	for index, rule := range rules {
		targetRules[index] = policyv2.TargetListRule{VersionID: version.ID, RuleType: rule.RuleType, Domain: rule.Domain}
	}
	if err := device.repository.SavePendingTargetListVersion(ctx, policyv2.TargetListVersion{
		ID: version.ID, TargetListID: existing.ID, SHA256: version.SHA256, CompressedYAML: version.CompressedYAML, State: "pending", Counts: map[string]int{"valid": len(targetRules)}, CreatedAt: version.CreatedAt,
	}, targetRules); err != nil {
		t.Fatal(err)
	}
	previewContent, err := policy.PrepareSourceContent([]byte("DOMAIN-SUFFIX,preset.example\n"), policy.KindDomain)
	if err != nil {
		t.Fatal(err)
	}
	previewID := server.saveApplicationPresetPreview(applicationPresetPreview{
		DeviceID: device.device.ID, PresetID: "fixture", Domain: previewContent, URL: "https://preset.example.test/fixture.yaml", ExpiresAt: time.Now().UTC().Add(time.Minute),
	})
	waitJob := func(jobID string) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			job, jobErr := server.policy.GetJob(ctx, device.device.ID, jobID)
			if jobErr == nil && job.Terminal() {
				if job.State != "committed" {
					t.Fatalf("access apply failed: %#v", job)
				}
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("timed out waiting for access job %s", jobID)
	}
	create := accessControlRequest(server, http.MethodPost, "/rules", `{"id":"","name":"编辑测试","targetScope":"targets","targetListIds":["access-edit-domain"],"subject":{"mode":"selected","members":[{"terminalId":"mac:aa","binding":"fixed","pinnedIpv4":["10.0.0.20"],"pinnedIpv6":[]}]},"enabled":true,"revision":0}`)
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
	waitJob(created.JobID)
	update := accessControlRequest(server, http.MethodPut, "/rules/"+created.Rule.ID, `{"id":"`+created.Rule.ID+`","name":"编辑测试","targetScope":"targets","targetListIds":["access-edit-domain","preset:fixture:domain"],"subject":{"mode":"selected","members":[{"terminalId":"mac:aa","binding":"fixed","pinnedIpv4":["10.0.0.20"],"pinnedIpv6":[]}]},"enabled":true,"revision":1,"presetSelections":[{"presetId":"fixture","previewId":"`+previewID+`","requestedKinds":["domain"]}]}`)
	if update.Code != http.StatusAccepted {
		t.Fatalf("editing an access rule to add an application status=%d body=%s", update.Code, update.Body.String())
	}
	var updated struct {
		JobID string `json:"jobId"`
	}
	if err := json.Unmarshal(update.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	waitJob(updated.JobID)
	if _, err := device.repository.GetTargetList(ctx, "preset:fixture:domain"); err != nil {
		t.Fatalf("application target was not committed after editing the access rule: %v", err)
	}
}
