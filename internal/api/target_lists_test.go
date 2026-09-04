package api

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"strings"
	"testing"

	"rosboard/internal/policy"
	"rosboard/internal/policyv2"
)

func targetListAPIRequest(t *testing.T, server *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	requestPath := "/api/target-lists" + path
	if strings.Contains(requestPath, "?") {
		requestPath += "&device=edge"
	} else {
		requestPath += "?device=edge"
	}
	request := httptest.NewRequest(method, requestPath, bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	return response
}

func TestTargetListAPICanonicalCRUDReusesPreviewAndRules(t *testing.T) {
	server, storage := newPolicyV2APIServer(t)
	defer storage.Close()

	for _, body := range []string{
		`{"name":"Mixed targets","sourceType":"manual","kind":"mixed","enabled":true}`,
		`{"name":"Unknown targets","sourceType":"manual","kind":"unknown","enabled":true}`,
		`{"name":"Unsupported source","sourceType":"bar","kind":"domain","enabled":true}`,
	} {
		invalid := targetListAPIRequest(t, server, http.MethodPost, "", body)
		if invalid.Code != http.StatusUnprocessableEntity || !strings.Contains(invalid.Body.String(), `"code":"invalid_target_list"`) {
			t.Fatalf("invalid canonical target status=%d body=%s", invalid.Code, invalid.Body.String())
		}
	}

	previewContent, err := policy.PrepareSourceContent([]byte("payload:\n  - DOMAIN-SUFFIX,example.com\n"), policy.KindDomain)
	if err != nil {
		t.Fatal(err)
	}
	previewID := server.savePolicyPreview(policyPreviewEntry{DeviceID: "edge", SourceType: policyv2.TargetSourceTypeManual, Kind: policyv2.KindDomain, Content: previewContent})
	created := targetListAPIRequest(t, server, http.MethodPost, "", `{"name":"Example domains","sourceType":"manual","kind":"domain","enabled":true,"previewId":"`+previewID+`","deferApply":true}`)
	if created.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	var target policyv2.TargetList
	if err := json.Unmarshal(created.Body.Bytes(), &target); err != nil {
		t.Fatal(err)
	}
	if target.ID == "" || target.SourceType != policyv2.TargetSourceTypeManual || target.Kind != policyv2.KindDomain || target.Revision != 2 || len(target.Versions) != 1 || target.PendingVersionID == "" || target.Counts["valid"] != 1 {
		t.Fatalf("unexpected canonical target: %#v", target)
	}
	if strings.Contains(created.Body.String(), "egressId") {
		t.Fatalf("canonical target response exposed egress ownership: %s", created.Body.String())
	}

	listed := targetListAPIRequest(t, server, http.MethodGet, "", "")
	if listed.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listed.Code, listed.Body.String())
	}
	var listPayload struct {
		TargetLists []policyv2.TargetList `json:"targetLists"`
	}
	if err := json.Unmarshal(listed.Body.Bytes(), &listPayload); err != nil {
		t.Fatal(err)
	}
	if len(listPayload.TargetLists) != 1 || listPayload.TargetLists[0].ID != target.ID {
		t.Fatalf("unexpected target list collection: %#v", listPayload)
	}
	if listPayload.TargetLists[0].Usage.AccessRuleCount != 0 || listPayload.TargetLists[0].Usage.RoutingRuleCount != 0 {
		t.Fatalf("unexpected target usage: %#v", listPayload.TargetLists[0].Usage)
	}
	deviceStorage, err := storage.OpenDevice("edge")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := deviceStorage.PolicyRepository().SaveTargetList(context.Background(), policyv2.TargetList{ID: "preset-youtube-domain", Name: "YouTube · Domain", Kind: policyv2.KindDomain, SourceType: policyv2.TargetSourceTypePreset, PresetID: "youtube", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	hiddenPreset := targetListAPIRequest(t, server, http.MethodGet, "", "")
	if hiddenPreset.Code != http.StatusOK || strings.Contains(hiddenPreset.Body.String(), "preset-youtube-domain") {
		t.Fatalf("preset target list leaked into default collection: status=%d body=%s", hiddenPreset.Code, hiddenPreset.Body.String())
	}
	visiblePreset := targetListAPIRequest(t, server, http.MethodGet, "?includePreset=true", "")
	if visiblePreset.Code != http.StatusOK || !strings.Contains(visiblePreset.Body.String(), "preset-youtube-domain") {
		t.Fatalf("includePreset collection did not expose backing row: status=%d body=%s", visiblePreset.Code, visiblePreset.Body.String())
	}
	protected := targetListAPIRequest(t, server, http.MethodDelete, "/preset-youtube-domain?revision=1", "")
	if protected.Code != http.StatusConflict || !strings.Contains(protected.Body.String(), `"code":"preset_target_list_protected"`) {
		t.Fatalf("preset target list was not protected from deletion: status=%d body=%s", protected.Code, protected.Body.String())
	}

	rules := targetListAPIRequest(t, server, http.MethodGet, "/"+target.ID+"/rules", "")
	if rules.Code != http.StatusOK {
		t.Fatalf("rules status=%d body=%s", rules.Code, rules.Body.String())
	}
	var rulesPayload struct {
		TargetListID string           `json:"targetListId"`
		Rules        []map[string]any `json:"rules"`
	}
	if err := json.Unmarshal(rules.Body.Bytes(), &rulesPayload); err != nil {
		t.Fatal(err)
	}
	if rulesPayload.TargetListID != target.ID || len(rulesPayload.Rules) != 1 || rulesPayload.Rules[0]["domain"] != "example.com" {
		t.Fatalf("unexpected canonical rules response: %#v", rulesPayload)
	}

	manualPreview := targetListAPIRequest(t, server, http.MethodPost, "/manual/preview", `{"text":"192.0.2.1","kind":"ip"}`)
	if manualPreview.Code != http.StatusOK || !strings.Contains(manualPreview.Body.String(), `"kind":"ip"`) {
		t.Fatalf("canonical manual preview status=%d body=%s", manualPreview.Code, manualPreview.Body.String())
	}

	rejected := targetListAPIRequest(t, server, http.MethodPut, "/"+target.ID, `{"name":"Example domains","sourceType":"manual","kind":"ip","enabled":true,"revision":2,"deferApply":true}`)
	if rejected.Code != http.StatusUnprocessableEntity {
		t.Fatalf("kind change status=%d body=%s", rejected.Code, rejected.Body.String())
	}
	stale := targetListAPIRequest(t, server, http.MethodPut, "/"+target.ID, `{"name":"Stale update","sourceType":"manual","kind":"domain","enabled":true,"revision":1,"deferApply":true}`)
	if stale.Code != http.StatusConflict || !strings.Contains(stale.Body.String(), `"code":"revision_stale"`) {
		t.Fatalf("stale update status=%d body=%s", stale.Code, stale.Body.String())
	}
	typeChange := targetListAPIRequest(t, server, http.MethodPut, "/"+target.ID, `{"name":"Type change","sourceType":"upload","kind":"domain","enabled":true,"revision":2,"deferApply":true}`)
	if typeChange.Code != http.StatusUnprocessableEntity || !strings.Contains(typeChange.Body.String(), `"code":"invalid_target_list"`) {
		t.Fatalf("source type change status=%d body=%s", typeChange.Code, typeChange.Body.String())
	}
}

func TestTargetListAPIDetailReturnsSavedManualContent(t *testing.T) {
	server, storage := newPolicyV2APIServer(t)
	defer storage.Close()

	content, err := policy.PrepareDomainLines("example.com\nfull:full.example.com\n")
	if err != nil {
		t.Fatal(err)
	}
	previewID := server.savePolicyPreview(policyPreviewEntry{DeviceID: "edge", SourceType: policyv2.TargetSourceTypeManual, Kind: policyv2.KindDomain, Content: content})
	created := targetListAPIRequest(t, server, http.MethodPost, "", `{"name":"Editable domains","sourceType":"manual","kind":"domain","enabled":true,"previewId":"`+previewID+`","deferApply":true}`)
	if created.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	var target policyv2.TargetList
	if err := json.Unmarshal(created.Body.Bytes(), &target); err != nil {
		t.Fatal(err)
	}

	type targetListDetail struct {
		policyv2.TargetList
		EditableContent string `json:"editableContent"`
	}
	readDetail := func() targetListDetail {
		t.Helper()
		response := targetListAPIRequest(t, server, http.MethodGet, "/"+target.ID, "")
		if response.Code != http.StatusOK {
			t.Fatalf("detail status=%d body=%s", response.Code, response.Body.String())
		}
		var detail targetListDetail
		if err := json.Unmarshal(response.Body.Bytes(), &detail); err != nil {
			t.Fatal(err)
		}
		return detail
	}

	detail := readDetail()
	const initialEditableContent = "DOMAIN-SUFFIX,example.com\nDOMAIN,full.example.com"
	if detail.EditableContent != initialEditableContent || detail.PendingVersionID != target.PendingVersionID || detail.Revision != target.Revision {
		t.Fatalf("unexpected editable detail: %#v", detail)
	}

	unsavedContent, err := policy.PrepareDomainLines("unsaved.example\n")
	if err != nil {
		t.Fatal(err)
	}
	server.savePolicyPreview(policyPreviewEntry{DeviceID: "edge", SourceType: policyv2.TargetSourceTypeManual, Kind: policyv2.KindDomain, Content: unsavedContent})
	unchanged := readDetail()
	if unchanged.EditableContent != initialEditableContent || unchanged.Revision != detail.Revision || unchanged.PendingVersionID != detail.PendingVersionID {
		t.Fatalf("detail exposed unsaved preview: %#v", unchanged)
	}

	renameBody, err := json.Marshal(map[string]any{
		"name":       "Renamed domains",
		"sourceType": "manual",
		"kind":       "domain",
		"enabled":    true,
		"revision":   detail.Revision,
		"deferApply": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	renamed := targetListAPIRequest(t, server, http.MethodPut, "/"+target.ID, string(renameBody))
	if renamed.Code != http.StatusOK {
		t.Fatalf("metadata update status=%d body=%s", renamed.Code, renamed.Body.String())
	}
	var renamedTarget policyv2.TargetList
	if err := json.Unmarshal(renamed.Body.Bytes(), &renamedTarget); err != nil {
		t.Fatal(err)
	}
	if renamedTarget.Name != "Renamed domains" || renamedTarget.Revision != detail.Revision+1 || renamedTarget.PendingVersionID != detail.PendingVersionID {
		t.Fatalf("metadata update replaced content: %#v", renamedTarget)
	}

	updatedContent, err := policy.PrepareDomainLines("example.com\nnew.example\n")
	if err != nil {
		t.Fatal(err)
	}
	updatedPreviewID := server.savePolicyPreview(policyPreviewEntry{DeviceID: "edge", SourceType: policyv2.TargetSourceTypeManual, Kind: policyv2.KindDomain, Content: updatedContent})
	updateBody, err := json.Marshal(map[string]any{
		"name":       renamedTarget.Name,
		"sourceType": "manual",
		"kind":       "domain",
		"enabled":    true,
		"revision":   renamedTarget.Revision,
		"previewId":  updatedPreviewID,
		"deferApply": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	updated := targetListAPIRequest(t, server, http.MethodPut, "/"+target.ID, string(updateBody))
	if updated.Code != http.StatusOK {
		t.Fatalf("content update status=%d body=%s", updated.Code, updated.Body.String())
	}
	updatedDetail := readDetail()
	const updatedEditableContent = "DOMAIN-SUFFIX,example.com\nDOMAIN-SUFFIX,new.example"
	if updatedDetail.ID != target.ID || updatedDetail.EditableContent != updatedEditableContent || updatedDetail.Revision <= renamedTarget.Revision {
		t.Fatalf("updated editable detail is incorrect: %#v", updatedDetail)
	}
}

func TestTargetListAPIUnreferencedUploadStaysStandbyWithoutApply(t *testing.T) {
	server, storage := newPolicyV2APIServer(t)
	defer storage.Close()

	content, err := policy.PrepareSourceContent([]byte("payload:\n  - DOMAIN-SUFFIX,uploaded.example\n"), policy.KindDomain)
	if err != nil {
		t.Fatal(err)
	}
	previewID := server.savePolicyPreview(policyPreviewEntry{DeviceID: "edge", SourceType: policyv2.TargetSourceTypeUpload, Kind: policyv2.KindDomain, Content: content})
	created := targetListAPIRequest(t, server, http.MethodPost, "", `{"name":"Uploaded domains","sourceType":"upload","kind":"domain","enabled":true,"previewId":"`+previewID+`"}`)
	if created.Code != http.StatusOK {
		t.Fatalf("unreferenced upload status=%d body=%s", created.Code, created.Body.String())
	}
	if strings.Contains(created.Body.String(), `"jobId"`) || strings.Contains(created.Body.String(), `"job"`) {
		t.Fatalf("unreferenced upload unexpectedly triggered apply: %s", created.Body.String())
	}
	var target policyv2.TargetList
	if err := json.Unmarshal(created.Body.Bytes(), &target); err != nil {
		t.Fatal(err)
	}
	if target.ActiveVersionID != "" || target.PendingVersionID == "" || target.Counts["valid"] != 1 || target.Usage.RoutingRuleCount != 0 || target.Usage.AccessRuleCount != 0 {
		t.Fatalf("unreferenced upload was not left as usable standby content: %#v", target)
	}

	rules := targetListAPIRequest(t, server, http.MethodGet, "/"+target.ID+"/rules", "")
	if rules.Code != http.StatusOK || !strings.Contains(rules.Body.String(), "uploaded.example") {
		t.Fatalf("standby upload rules status=%d body=%s", rules.Code, rules.Body.String())
	}
}

func TestTargetListAPIDeletesAppliedUnreferencedTargetSynchronously(t *testing.T) {
	server, storage := newPolicyV2APIServer(t)
	defer storage.Close()

	content, err := policy.PrepareSourceContent([]byte("payload:\n  - DOMAIN-SUFFIX,applied.example\n"), policy.KindDomain)
	if err != nil {
		t.Fatal(err)
	}
	previewID := server.savePolicyPreview(policyPreviewEntry{DeviceID: "edge", SourceType: policyv2.TargetSourceTypeUpload, Kind: policyv2.KindDomain, Content: content})
	created := targetListAPIRequest(t, server, http.MethodPost, "", `{"name":"Applied upload","sourceType":"upload","kind":"domain","enabled":true,"previewId":"`+previewID+`","deferApply":true}`)
	if created.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	var target policyv2.TargetList
	if err := json.Unmarshal(created.Body.Bytes(), &target); err != nil {
		t.Fatal(err)
	}
	deviceStorage, err := storage.OpenDevice("edge")
	if err != nil {
		t.Fatal(err)
	}
	repository := deviceStorage.PolicyRepository()
	state, err := repository.GetDeviceState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.CommitRoutingApply(context.Background(), state.DesiredRevision, "api-applied-hash", policyv2.ApplyJob{ID: "api-applied-job", PlanID: "api-applied-plan"}, []policyv2.TargetVersionPromotion{{TargetListID: target.ID, VersionID: target.PendingVersionID}}); err != nil {
		t.Fatal(err)
	}
	applied, err := repository.GetTargetList(context.Background(), target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !applied.Applied || applied.ActiveVersionID == "" {
		t.Fatalf("target was not prepared as applied: %#v", applied)
	}

	deleted := targetListAPIRequest(t, server, http.MethodDelete, "/"+target.ID+"?revision="+strconv.FormatInt(applied.Revision, 10), "")
	if deleted.Code != http.StatusOK || !strings.Contains(deleted.Body.String(), `"deleted":true`) || strings.Contains(deleted.Body.String(), "pendingDeletion") {
		t.Fatalf("applied unreferenced delete status=%d body=%s", deleted.Code, deleted.Body.String())
	}
	if _, err := repository.GetTargetList(context.Background(), target.ID); !errors.Is(err, policyv2.ErrTargetListNotFound) {
		t.Fatalf("applied target remains after API delete: %v", err)
	}
}

type targetListTestResolver struct{}

func (targetListTestResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil
}

func TestTargetListPreviewStrictKindValidation(t *testing.T) {
	server, storage := newPolicyV2APIServer(t)
	defer storage.Close()

	assertInvalidCanonical := func(response *httptest.ResponseRecorder) {
		t.Helper()
		if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), `"code":"invalid_target_list"`) {
			t.Fatalf("canonical preview status=%d body=%s", response.Code, response.Body.String())
		}
	}
	for _, kind := range []string{"", "mixed", "unknown"} {
		body, err := json.Marshal(map[string]string{"text": "example.com", "kind": kind})
		if err != nil {
			t.Fatal(err)
		}
		assertInvalidCanonical(targetListAPIRequest(t, server, http.MethodPost, "/manual/preview", string(body)))
	}

	fetchCalled := false
	server.sourceFetcher = policy.NewSourceFetcher(policy.FetcherOptions{
		Resolver: targetListTestResolver{},
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			fetchCalled = true
			return nil, errors.New("unexpected URL fetch")
		},
	})
	assertInvalidCanonical(targetListAPIRequest(t, server, http.MethodPost, "/url/preview", `{"url":"https://lists.example.test/rules.txt","kind":"unknown"}`))
	if fetchCalled {
		t.Fatal("canonical URL preview fetched before rejecting invalid kind")
	}

	uploadRequest := httptest.NewRequest(http.MethodPost, "/api/target-lists/upload/preview?device=edge&kind=mixed", bytes.NewBufferString("not parsed"))
	uploadRequest.Header.Set("Content-Type", "text/plain")
	uploadResponse := httptest.NewRecorder()
	server.ServeHTTP(uploadResponse, uploadRequest)
	assertInvalidCanonical(uploadResponse)

}

func TestTargetListAPIReusesURLAndUploadPreviewAndNotModifiedRefresh(t *testing.T) {
	server, storage := newPolicyV2APIServer(t)
	defer storage.Close()
	server.cfg.DataDir = t.TempDir()

	const (
		etag         = "etag-target-list"
		lastModified = "Wed, 21 Oct 2015 07:28:00 GMT"
	)
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("If-None-Match") == etag {
			writer.WriteHeader(http.StatusNotModified)
			return
		}
		writer.Header().Set("Content-Type", "text/plain")
		writer.Header().Set("ETag", etag)
		writer.Header().Set("Last-Modified", lastModified)
		_, _ = writer.Write([]byte("DOMAIN-SUFFIX,example.com\n"))
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

	urlValue := "https://lists.example.test/rules.txt"
	preview := targetListAPIRequest(t, server, http.MethodPost, "/url/preview", `{"url":"`+urlValue+`","kind":"domain"}`)
	if preview.Code != http.StatusOK {
		t.Fatalf("URL preview status=%d body=%s", preview.Code, preview.Body.String())
	}
	var previewPayload struct {
		PreviewID    string `json:"previewId"`
		URL          string `json:"url"`
		ETag         string `json:"etag"`
		LastModified string `json:"lastModified"`
		ValidRules   int    `json:"validRules"`
	}
	if err := json.Unmarshal(preview.Body.Bytes(), &previewPayload); err != nil {
		t.Fatal(err)
	}
	if previewPayload.PreviewID == "" || previewPayload.URL != urlValue || previewPayload.ETag != etag || previewPayload.LastModified != lastModified || previewPayload.ValidRules != 1 {
		t.Fatalf("unexpected URL preview: %#v", previewPayload)
	}

	createBody, err := json.Marshal(map[string]any{
		"name":       "URL domains",
		"sourceType": policyv2.TargetSourceTypeURL,
		"kind":       policyv2.KindDomain,
		"url":        urlValue,
		"schedule":   "1h",
		"enabled":    true,
		"previewId":  previewPayload.PreviewID,
		"deferApply": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	created := targetListAPIRequest(t, server, http.MethodPost, "", string(createBody))
	if created.Code != http.StatusOK {
		t.Fatalf("URL target save status=%d body=%s", created.Code, created.Body.String())
	}
	var target policyv2.TargetList
	if err := json.Unmarshal(created.Body.Bytes(), &target); err != nil {
		t.Fatal(err)
	}
	if target.ID == "" || target.URL != urlValue || target.ETag != etag || target.LastModified != lastModified || target.Schedule != "1h" || target.PendingVersionID == "" {
		t.Fatalf("unexpected URL target: %#v", target)
	}

	uploadBody := new(bytes.Buffer)
	upload := multipart.NewWriter(uploadBody)
	part, err := upload.CreateFormFile("file", "targets.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("DOMAIN,upload.example.com\n")); err != nil {
		t.Fatal(err)
	}
	if err := upload.Close(); err != nil {
		t.Fatal(err)
	}
	uploadRequest := httptest.NewRequest(http.MethodPost, "/api/target-lists/upload/preview?device=edge&kind=domain", uploadBody)
	uploadRequest.Header.Set("Content-Type", upload.FormDataContentType())
	uploadResponse := httptest.NewRecorder()
	server.ServeHTTP(uploadResponse, uploadRequest)
	if uploadResponse.Code != http.StatusOK || !strings.Contains(uploadResponse.Body.String(), `"validRules":1`) {
		t.Fatalf("upload preview status=%d body=%s", uploadResponse.Code, uploadResponse.Body.String())
	}

	refreshed := targetListAPIRequest(t, server, http.MethodPost, "/"+target.ID+"/refresh", "")
	if refreshed.Code != http.StatusOK || !strings.Contains(refreshed.Body.String(), `"notModified":true`) {
		t.Fatalf("not-modified refresh status=%d body=%s", refreshed.Code, refreshed.Body.String())
	}
	deviceStore, err := storage.OpenDevice("edge")
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := deviceStore.PolicyRepository().GetTargetList(context.Background(), target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Revision != target.Revision+1 || loaded.ETag != etag || loaded.LastModified != lastModified || loaded.NextRunAt.IsZero() {
		t.Fatalf("not-modified refresh did not preserve HTTP state: %#v", loaded)
	}
}

func TestTargetListAPIRulesPaginationForDomainAndIP(t *testing.T) {
	server, storage := newPolicyV2APIServer(t)
	defer storage.Close()
	deviceStore, err := storage.OpenDevice("edge")
	if err != nil {
		t.Fatal(err)
	}
	repository := deviceStore.PolicyRepository()
	ctx := context.Background()
	for _, fixture := range []struct {
		id       string
		kind     string
		ruleType string
		values   []string
	}{
		{id: "page-domain", kind: policyv2.KindDomain, ruleType: "DOMAIN", values: []string{"one.example", "two.example", "three.example"}},
		{id: "page-ip", kind: policyv2.KindIP, ruleType: "IP-CIDR", values: []string{"192.0.2.0/24", "198.51.100.0/24", "203.0.113.0/24"}},
	} {
		target, err := repository.SaveTargetList(ctx, policyv2.TargetList{
			ID: fixture.id, Name: fixture.id, Kind: fixture.kind, SourceType: policyv2.TargetSourceTypeManual, Enabled: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		rules := make([]policyv2.TargetListRule, len(fixture.values))
		for index, value := range fixture.values {
			rules[index] = policyv2.TargetListRule{RuleType: fixture.ruleType, Domain: value}
		}
		if err := repository.SavePendingTargetListVersion(ctx, policyv2.TargetListVersion{
			ID: "page-version-" + fixture.kind, TargetListID: target.ID, SHA256: fixture.id, CompressedYAML: []byte("payload"), State: "pending", Counts: map[string]int{"valid": len(rules)},
		}, rules); err != nil {
			t.Fatal(err)
		}

		firstResponse := targetListAPIRequest(t, server, http.MethodGet, "/"+target.ID+"/rules?limit=2", "")
		if firstResponse.Code != http.StatusOK {
			t.Fatalf("%s first page status=%d body=%s", fixture.kind, firstResponse.Code, firstResponse.Body.String())
		}
		var first struct {
			Rules      []map[string]any `json:"rules"`
			NextCursor string           `json:"nextCursor"`
		}
		if err := json.Unmarshal(firstResponse.Body.Bytes(), &first); err != nil {
			t.Fatal(err)
		}
		if len(first.Rules) != 2 || first.NextCursor == "" {
			t.Fatalf("%s first page = %#v", fixture.kind, first)
		}
		field := "domain"
		if fixture.kind == policyv2.KindIP {
			field = "address"
		}
		seen := make(map[string]bool, len(fixture.values))
		for _, rule := range first.Rules {
			value, ok := rule[field].(string)
			if !ok || rule["type"] != fixture.ruleType {
				t.Fatalf("%s first page rule has wrong shape: %#v", fixture.kind, rule)
			}
			if seen[value] {
				t.Fatalf("%s first page repeated %q", fixture.kind, value)
			}
			seen[value] = true
		}

		secondResponse := targetListAPIRequest(t, server, http.MethodGet, "/"+target.ID+"/rules?limit=2&cursor="+first.NextCursor, "")
		if secondResponse.Code != http.StatusOK {
			t.Fatalf("%s second page status=%d body=%s", fixture.kind, secondResponse.Code, secondResponse.Body.String())
		}
		var second struct {
			Rules      []map[string]any `json:"rules"`
			NextCursor string           `json:"nextCursor"`
		}
		if err := json.Unmarshal(secondResponse.Body.Bytes(), &second); err != nil {
			t.Fatal(err)
		}
		if len(second.Rules) != 1 || second.NextCursor != "" {
			t.Fatalf("%s second page = %#v", fixture.kind, second)
		}
		for _, rule := range second.Rules {
			value := rule[field].(string)
			if seen[value] {
				t.Fatalf("%s second page repeated %q", fixture.kind, value)
			}
			seen[value] = true
		}
		for _, value := range fixture.values {
			if !seen[value] {
				t.Fatalf("%s pagination omitted %q", fixture.kind, value)
			}
		}
	}
}

func TestTargetListAPISavePreservesLegacyRoutingAssociation(t *testing.T) {
	server, storage := newPolicyV2APIServer(t)
	defer storage.Close()
	deviceStore, err := storage.OpenDevice("edge")
	if err != nil {
		t.Fatal(err)
	}
	repository := deviceStore.PolicyRepository()
	ctx := context.Background()
	if _, err := repository.SaveEgress(ctx, policyv2.Egress{ID: "wan-a", Name: "WAN A", ListMode: policyv2.ListModeShared, ListName: "proxy-a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.SaveSource(ctx, policyv2.Source{ID: "legacy-target", EgressID: "wan-a", Type: policyv2.TargetSourceTypeManual, Kind: policyv2.KindIP, Name: "Legacy IPs", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	target, err := repository.GetTargetList(ctx, "legacy-target")
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]any{
		"name":       "Renamed IPs",
		"sourceType": target.SourceType,
		"kind":       target.Kind,
		"enabled":    true,
		"revision":   target.Revision,
		"deferApply": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	response := targetListAPIRequest(t, server, http.MethodPut, "/legacy-target", string(body))
	if response.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "egressId") {
		t.Fatalf("canonical target update exposed egress ownership: %s", response.Body.String())
	}
	legacySource, err := repository.GetSource(ctx, target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if legacySource.EgressID != "wan-a" {
		t.Fatalf("canonical target update cleared legacy routing association: %q", legacySource.EgressID)
	}
}
