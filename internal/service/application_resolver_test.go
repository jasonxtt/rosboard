package service

import (
	"context"
	"testing"
	"time"

	"rosboard/internal/applicationpreset"
	"rosboard/internal/model"
	"rosboard/internal/policyv2"
	"rosboard/internal/routeros"
	"rosboard/internal/store"
)

func seedPresetDomain(t *testing.T, storage *store.Store, presetID string, rules ...policyv2.TargetListRule) {
	t.Helper()
	repository := storage.PolicyRepository()
	target, err := repository.SaveTargetList(context.Background(), policyv2.TargetList{
		ID: "preset:" + presetID + ":domain", Name: presetID + " domains", Kind: policyv2.KindDomain,
		SourceType: policyv2.TargetSourceTypePreset, PresetID: presetID, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	versionID := target.ID + ":v1"
	converted := make([]policyv2.TargetListRule, len(rules))
	for i, rule := range rules {
		rule.VersionID = versionID
		converted[i] = rule
	}
	if err := repository.SavePendingTargetListVersion(context.Background(), policyv2.TargetListVersion{
		ID: versionID, TargetListID: target.ID, SHA256: versionID, State: "pending", CompressedYAML: []byte(versionID),
	}, converted); err != nil {
		t.Fatal(err)
	}
}

func saveTestDNSObservation(t *testing.T, storage *store.Store, dedupeKey, clientIP, domain, answerIP string, queryTime time.Time, ttl int64) {
	t.Helper()
	_, err := storage.SaveDNSObservations(context.Background(), []model.DNSObservation{{
		DedupeKey: dedupeKey, TraceID: dedupeKey, ClientIP: clientIP, Domain: domain, AnswerIP: answerIP,
		QueryType: "A", QueryTime: queryTime, TTL: ttl, IngestedAt: queryTime,
	}}, store.DNSWatermark{QueryTime: queryTime, TraceID: dedupeKey})
	if err != nil {
		t.Fatal(err)
	}
}

func TestApplicationResolverUsesMaterializedPresetForRecentDNSObservation(t *testing.T) {
	storage, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	seedPresetDomain(t, storage, "youtube", policyv2.TargetListRule{RuleType: "DOMAIN-SUFFIX", Domain: "example.com"})
	queryTime := time.Date(2026, 8, 2, 1, 2, 3, 0, time.UTC)
	saveTestDNSObservation(t, storage, "dns-1", "10.0.0.8", "r3.example.com", "192.0.2.1", queryTime, 60)

	resolver := NewApplicationResolver(storage, true, 30)
	applicationID, application, domain, ok := resolver.Resolve(context.Background(), "10.0.0.8", "192.0.2.1", queryTime.Add(time.Second))
	if !ok || applicationID != "youtube" || application != "YouTube" || domain != "r3.example.com" {
		t.Fatalf("unexpected resolver result: id=%q application=%q domain=%q ok=%v", applicationID, application, domain, ok)
	}
	if _, _, _, ok := resolver.Resolve(context.Background(), "10.0.0.9", "192.0.2.1", queryTime.Add(time.Second)); ok {
		t.Fatal("resolver matched a different client")
	}
}

func TestTerminalConnectionUsesPresetApplicationFields(t *testing.T) {
	storage, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	seedPresetDomain(t, storage, "youtube", policyv2.TargetListRule{RuleType: "DOMAIN-SUFFIX", Domain: "example.com"})
	queryTime := time.Date(2026, 8, 2, 1, 2, 3, 0, time.UTC)
	saveTestDNSObservation(t, storage, "dns-row", "10.0.0.8", "r3.example.com", "192.0.2.1", queryTime, 60)
	resolver := NewApplicationResolver(storage, true, 30)

	row := terminalConnectionRow(context.Background(), resolver, queryTime.Add(time.Second), "ipv4", routeros.FirewallConnection{
		ID: "*1", Protocol: "tcp", SrcAddress: "10.0.0.8", SrcPort: "50000", DstAddress: "192.0.2.1", DstPort: "443",
	}, connectionView{LocalAddress: "10.0.0.8"}, routeMatcher{}, "", true)
	if row.ApplicationID != "youtube" || row.Application != "YouTube" || row.MatchedDomain != "r3.example.com" || row.ApplicationSource != "mosdns" || !row.Estimated {
		t.Fatalf("preset attribution fields are wrong: %#v", row)
	}
	if row.Service != "HTTP协议" || row.Protocol != "tcp" {
		t.Fatalf("service/protocol fields are wrong: %#v", row)
	}
}

func TestApplicationResolverDoesNotGuessAmbiguousPresetMatch(t *testing.T) {
	storage, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	registry := applicationpreset.New([]applicationpreset.ApplicationPreset{{ID: "one", Name: "One"}, {ID: "two", Name: "Two"}})
	seedPresetDomain(t, storage, "one", policyv2.TargetListRule{RuleType: "DOMAIN-SUFFIX", Domain: "shared.example"})
	seedPresetDomain(t, storage, "two", policyv2.TargetListRule{RuleType: "DOMAIN-SUFFIX", Domain: "shared.example"})
	queryTime := time.Date(2026, 8, 2, 1, 2, 3, 0, time.UTC)
	saveTestDNSObservation(t, storage, "dns-ambiguous", "10.0.0.8", "api.shared.example", "192.0.2.2", queryTime, 60)

	resolver := NewApplicationResolverWithRegistry(storage, registry, true, 30)
	applicationID, application, domain, ok := resolver.Resolve(context.Background(), "10.0.0.8", "192.0.2.2", queryTime.Add(time.Second))
	if ok || applicationID != "" || application != "" || domain != "api.shared.example" {
		t.Fatalf("ambiguous preset match was guessed: id=%q application=%q domain=%q ok=%v", applicationID, application, domain, ok)
	}
}

func TestApplicationResolverDoesNotGuessAmbiguousDifferentSpecificity(t *testing.T) {
	storage, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	registry := applicationpreset.New([]applicationpreset.ApplicationPreset{{ID: "one", Name: "One"}, {ID: "two", Name: "Two"}})
	seedPresetDomain(t, storage, "one", policyv2.TargetListRule{RuleType: "DOMAIN-SUFFIX", Domain: "example.com"})
	seedPresetDomain(t, storage, "two", policyv2.TargetListRule{RuleType: "DOMAIN", Domain: "api.example.com"})
	queryTime := time.Date(2026, 8, 2, 1, 2, 3, 0, time.UTC)
	saveTestDNSObservation(t, storage, "dns-ambiguous-specificity", "10.0.0.8", "api.example.com", "192.0.2.22", queryTime, 60)

	resolver := NewApplicationResolverWithRegistry(storage, registry, true, 30)
	applicationID, application, domain, ok := resolver.Resolve(context.Background(), "10.0.0.8", "192.0.2.22", queryTime.Add(time.Second))
	if ok || applicationID != "" || application != "" || domain != "api.example.com" {
		t.Fatalf("different-specificity preset match was guessed: id=%q application=%q domain=%q ok=%v", applicationID, application, domain, ok)
	}
}

func TestApplicationResolverKeepsObservedDomainWithoutPresetMatch(t *testing.T) {
	storage, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	seedPresetDomain(t, storage, "youtube", policyv2.TargetListRule{RuleType: "DOMAIN-SUFFIX", Domain: "known.example"})
	queryTime := time.Date(2026, 8, 2, 1, 2, 3, 0, time.UTC)
	saveTestDNSObservation(t, storage, "dns-unknown", "10.0.0.8", "unknown.example", "192.0.2.3", queryTime, 60)

	resolver := NewApplicationResolver(storage, true, 30)
	applicationID, application, domain, ok := resolver.Resolve(context.Background(), "10.0.0.8", "192.0.2.3", queryTime.Add(time.Second))
	if ok || applicationID != "" || application != "" || domain != "unknown.example" {
		t.Fatalf("unexpected unmatched result: id=%q application=%q domain=%q ok=%v", applicationID, application, domain, ok)
	}
}

func TestApplicationResolverDoesNotUseDurableDNSFeatures(t *testing.T) {
	storage, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	seedPresetDomain(t, storage, "youtube", policyv2.TargetListRule{RuleType: "DOMAIN-SUFFIX", Domain: "example.com"})
	queryTime := time.Date(2026, 8, 2, 1, 2, 3, 0, time.UTC)
	saveTestDNSObservation(t, storage, "dns-pruned", "10.0.0.8", "api.example.com", "192.0.2.4", queryTime, 60)
	if err := storage.PruneDNSObservations(context.Background(), queryTime.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	features, err := storage.DNSFeaturesForMatch(context.Background())
	if err != nil || len(features) != 1 {
		t.Fatalf("durable DNS feature was not retained for diagnostics: features=%#v err=%v", features, err)
	}

	resolver := NewApplicationResolver(storage, true, 30)
	applicationID, application, domain, ok := resolver.Resolve(context.Background(), "10.0.0.8", "192.0.2.4", queryTime.Add(time.Second))
	if ok || applicationID != "" || application != "" || domain != "" {
		t.Fatalf("durable DNS feature incorrectly provided realtime attribution: id=%q application=%q domain=%q ok=%v", applicationID, application, domain, ok)
	}
}

func TestApplicationResolverRechecksTTLAfterCacheHit(t *testing.T) {
	storage, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	seedPresetDomain(t, storage, "youtube", policyv2.TargetListRule{RuleType: "DOMAIN-SUFFIX", Domain: "example.com"})
	queryTime := time.Date(2026, 8, 2, 1, 2, 3, 0, time.UTC)
	saveTestDNSObservation(t, storage, "dns-ttl", "10.0.0.8", "api.example.com", "192.0.2.5", queryTime, 10)

	resolver := NewApplicationResolver(storage, true, 30)
	if _, _, _, ok := resolver.Resolve(context.Background(), "10.0.0.8", "192.0.2.5", queryTime.Add(time.Second)); !ok {
		t.Fatal("valid DNS observation did not match")
	}
	applicationID, application, domain, ok := resolver.Resolve(context.Background(), "10.0.0.8", "192.0.2.5", queryTime.Add(11*time.Second))
	if ok || applicationID != "" || application != "" || domain != "" {
		t.Fatalf("cached observation bypassed TTL: id=%q application=%q domain=%q ok=%v", applicationID, application, domain, ok)
	}
}

func TestApplicationResolverRespectsEvidenceWindow(t *testing.T) {
	storage, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	seedPresetDomain(t, storage, "youtube", policyv2.TargetListRule{RuleType: "DOMAIN-SUFFIX", Domain: "example.com"})
	queryTime := time.Date(2026, 8, 2, 1, 2, 3, 0, time.UTC)
	saveTestDNSObservation(t, storage, "dns-window", "10.0.0.8", "api.example.com", "192.0.2.6", queryTime, int64(time.Hour/time.Second))

	resolver := NewApplicationResolver(storage, true, 30)
	applicationID, application, domain, ok := resolver.Resolve(context.Background(), "10.0.0.8", "192.0.2.6", queryTime.Add(31*time.Minute))
	if ok || applicationID != "" || application != "" || domain != "" {
		t.Fatalf("observation outside evidence window matched: id=%q application=%q domain=%q ok=%v", applicationID, application, domain, ok)
	}
}

func TestApplicationResolverUsesNewestObservation(t *testing.T) {
	storage, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	seedPresetDomain(t, storage, "youtube", policyv2.TargetListRule{RuleType: "DOMAIN-SUFFIX", Domain: "old.example"})
	seedPresetDomain(t, storage, "netflix", policyv2.TargetListRule{RuleType: "DOMAIN-SUFFIX", Domain: "new.example"})
	queryTime := time.Date(2026, 8, 2, 1, 2, 3, 0, time.UTC)
	saveTestDNSObservation(t, storage, "dns-old", "10.0.0.8", "old.example", "192.0.2.7", queryTime, 60)
	saveTestDNSObservation(t, storage, "dns-new", "10.0.0.8", "new.example", "192.0.2.7", queryTime.Add(10*time.Second), 60)

	resolver := NewApplicationResolver(storage, true, 30)
	applicationID, application, domain, ok := resolver.Resolve(context.Background(), "10.0.0.8", "192.0.2.7", queryTime.Add(11*time.Second))
	if !ok || applicationID != "netflix" || application != "Netflix" || domain != "new.example" {
		t.Fatalf("newest observation did not win: id=%q application=%q domain=%q ok=%v", applicationID, application, domain, ok)
	}
}

func TestApplicationResolverKeepsDeviceEvidenceIsolated(t *testing.T) {
	storage, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	first, err := storage.OpenDevice("first")
	if err != nil {
		t.Fatal(err)
	}
	second, err := storage.OpenDevice("second")
	if err != nil {
		t.Fatal(err)
	}
	seedPresetDomain(t, first, "youtube", policyv2.TargetListRule{RuleType: "DOMAIN-SUFFIX", Domain: "first.example"})
	seedPresetDomain(t, second, "netflix", policyv2.TargetListRule{RuleType: "DOMAIN-SUFFIX", Domain: "second.example"})
	queryTime := time.Date(2026, 8, 2, 1, 2, 3, 0, time.UTC)
	saveTestDNSObservation(t, first, "dns-first", "10.0.0.8", "first.example", "192.0.2.8", queryTime, 60)
	saveTestDNSObservation(t, second, "dns-second", "10.0.0.8", "second.example", "192.0.2.8", queryTime, 60)

	firstResolver := NewApplicationResolver(first, true, 30)
	secondResolver := NewApplicationResolver(second, true, 30)
	firstID, firstName, firstDomain, firstOK := firstResolver.Resolve(context.Background(), "10.0.0.8", "192.0.2.8", queryTime.Add(time.Second))
	secondID, secondName, secondDomain, secondOK := secondResolver.Resolve(context.Background(), "10.0.0.8", "192.0.2.8", queryTime.Add(time.Second))
	if !firstOK || firstID != "youtube" || firstName != "YouTube" || firstDomain != "first.example" {
		t.Fatalf("first device attribution leaked or failed: id=%q name=%q domain=%q ok=%v", firstID, firstName, firstDomain, firstOK)
	}
	if !secondOK || secondID != "netflix" || secondName != "Netflix" || secondDomain != "second.example" {
		t.Fatalf("second device attribution leaked or failed: id=%q name=%q domain=%q ok=%v", secondID, secondName, secondDomain, secondOK)
	}
}

func TestApplicationResolverDisabledDoesNotAttribute(t *testing.T) {
	storage, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	if resolver := NewApplicationResolver(storage, false, 30); resolver != nil {
		t.Fatal("disabled MosDNS unexpectedly created an application resolver")
	}
}

func TestAggregateProtocolsUsesServiceWhenApplicationMissing(t *testing.T) {
	protocols := aggregateProtocols(map[string]model.TerminalDetail{
		"terminal": {Connections: []model.TerminalConnection{
			{ApplicationID: "youtube", Application: "YouTube", Service: "HTTP协议", Protocol: "tcp", ApplicationSource: "mosdns", UploadBps: 100, Estimated: true},
			{Service: "HTTP协议", Protocol: "tcp", UploadBps: 50, Estimated: true},
			{Service: "HTTP协议", Protocol: "tcp", UploadBps: 25, Estimated: true},
		}},
	})
	if len(protocols) != 2 {
		t.Fatalf("unexpected aggregate protocol count: %#v", protocols)
	}
	byName := make(map[string]model.ProtocolStat, len(protocols))
	for _, protocol := range protocols {
		if protocol.Name == "" {
			t.Fatal("aggregate protocol has an empty display name")
		}
		byName[protocol.Name] = protocol
	}
	if byName["YouTube"].ApplicationID != "youtube" || byName["YouTube"].Service != "" || byName["YouTube"].Source != "mosdns" || byName["HTTP协议"].ApplicationID != "" || byName["HTTP协议"].Service != "HTTP协议" || byName["HTTP协议"].Source != "" || byName["HTTP协议"].Connections != 2 {
		t.Fatalf("unexpected application/service aggregate: %#v", protocols)
	}
}

func TestAggregateProtocolsKeepsSameNameApplicationsSeparateByID(t *testing.T) {
	protocols := aggregateProtocols(map[string]model.TerminalDetail{
		"terminal": {Connections: []model.TerminalConnection{
			{ApplicationID: "youtube", Application: "Same Name", Protocol: "tcp", ApplicationSource: "mosdns", UploadBps: 100},
			{ApplicationID: "netflix", Application: "Same Name", Protocol: "tcp", ApplicationSource: "mosdns", UploadBps: 50},
			{Service: "Same Name", Protocol: "tcp", UploadBps: 25},
		}},
	})
	if len(protocols) != 3 {
		t.Fatalf("same-name applications were merged: %#v", protocols)
	}
	byID := make(map[string]model.ProtocolStat, len(protocols))
	for _, protocol := range protocols {
		byID[protocol.ApplicationID] = protocol
	}
	if byID["youtube"].Connections != 1 || byID["youtube"].Name != "Same Name" || byID["youtube"].Service != "" {
		t.Fatalf("first application aggregate is wrong: %#v", protocols)
	}
	if byID["netflix"].Connections != 1 || byID["netflix"].Name != "Same Name" || byID["netflix"].Service != "" {
		t.Fatalf("second application aggregate is wrong: %#v", protocols)
	}
	if service := byID[""]; service.ApplicationID != "" || service.Service != "Same Name" || service.Connections != 1 {
		t.Fatalf("service aggregate is wrong: %#v", protocols)
	}
}
