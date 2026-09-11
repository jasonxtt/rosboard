package service

import (
	"context"
	"strings"
	"sync"
	"time"

	"rosboard/internal/applicationpreset"
	mosclient "rosboard/internal/mosdns"
	"rosboard/internal/store"
)

// dnsFeatureMaxAge bounds how long a learned (client, answer IP) → domain
// fingerprint stays usable for attribution. Pull-based sync lags far behind
// answer TTLs, so TTLs are display-only; this single recency gate is the
// staleness bound instead.
const dnsFeatureMaxAge = 7 * 24 * time.Hour

// ApplicationSourceMosDNS marks attribution backed by a DNS observation inside
// the configured match window; ApplicationSourceMosDNSLearned marks attribution
// from an older learned fingerprint.
const (
	ApplicationSourceMosDNS        = "mosdns"
	ApplicationSourceMosDNSLearned = "mosdns-learned"
)

type dnsEvidence struct {
	domain   string
	lastSeen time.Time
}

type ApplicationResolver struct {
	storage       *store.Store
	registry      *applicationpreset.Registry
	matchWindow   time.Duration
	cacheDuration time.Duration

	mu            sync.RWMutex
	refreshMu     sync.Mutex
	cacheLoadedAt time.Time
	evidence      map[string]dnsEvidence
	entries       []applicationpreset.DomainEntry
}

// NewApplicationResolver attributes connections with learned DNS fingerprints
// and materialized curated preset TargetLists.
func NewApplicationResolver(storage *store.Store, mosEnabled bool, matchWindowMinutes int) *ApplicationResolver {
	return NewApplicationResolverWithRegistry(storage, applicationpreset.Default(), mosEnabled, matchWindowMinutes)
}

func NewApplicationResolverWithRegistry(storage *store.Store, registry *applicationpreset.Registry, mosEnabled bool, matchWindowMinutes int) *ApplicationResolver {
	if storage == nil || !mosEnabled || matchWindowMinutes <= 0 {
		return nil
	}
	if registry == nil {
		registry = applicationpreset.Default()
	}
	return &ApplicationResolver{
		storage:       storage,
		registry:      registry,
		matchWindow:   time.Duration(matchWindowMinutes) * time.Minute,
		cacheDuration: 30 * time.Second,
		evidence:      make(map[string]dnsEvidence),
	}
}

// Resolve returns the preset attribution for a connection. ok is false when
// the matched domain is ambiguous or unknown to presets; domain is still
// returned for display. source distinguishes in-window evidence
// (ApplicationSourceMosDNS) from older learned fingerprints
// (ApplicationSourceMosDNSLearned) and is only meaningful when ok is true.
func (r *ApplicationResolver) Resolve(ctx context.Context, clientIP, answerIP string, at time.Time) (applicationID, application, domain, source string, ok bool) {
	if r == nil {
		return "", "", "", "", false
	}
	clientIP = mosclient.NormalizeClientIP(clientIP)
	answerIP = mosclient.NormalizeAnswerIP(answerIP)
	if clientIP == "" || answerIP == "" {
		return "", "", "", "", false
	}
	if at.IsZero() {
		at = time.Now().UTC()
	} else {
		at = at.UTC()
	}
	if err := r.refresh(ctx, at); err != nil {
		return "", "", "", "", false
	}
	r.mu.RLock()
	candidate, found := r.evidence[clientIP+"\x00"+answerIP]
	entries := append([]applicationpreset.DomainEntry(nil), r.entries...)
	r.mu.RUnlock()
	if !found || candidate.lastSeen.IsZero() || at.Sub(candidate.lastSeen) > dnsFeatureMaxAge {
		return "", "", "", "", false
	}
	match := r.registry.MatchDomain(candidate.domain, entries)
	if match.Ambiguous || match.Preset.ID == "" {
		return "", "", candidate.domain, "", false
	}
	source = ApplicationSourceMosDNSLearned
	if at.Sub(candidate.lastSeen) <= r.matchWindow {
		source = ApplicationSourceMosDNS
	}
	return match.Preset.ID, match.Preset.Name, candidate.domain, source, true
}

func (r *ApplicationResolver) refresh(ctx context.Context, at time.Time) error {
	if at.IsZero() {
		at = time.Now().UTC()
	} else {
		at = at.UTC()
	}
	r.mu.RLock()
	fresh := r.cacheIsFresh(time.Now().UTC())
	r.mu.RUnlock()
	if fresh {
		return nil
	}
	r.refreshMu.Lock()
	defer r.refreshMu.Unlock()
	r.mu.RLock()
	fresh = r.cacheIsFresh(time.Now().UTC())
	r.mu.RUnlock()
	if fresh {
		return nil
	}
	features, err := r.storage.DNSFeaturesForMatch(ctx, at.Add(-dnsFeatureMaxAge))
	if err != nil {
		return err
	}
	entries, err := r.storage.PolicyRepository().ListPresetDomainEntries(ctx)
	if err != nil {
		return err
	}
	evidence := make(map[string]dnsEvidence, len(features))
	for _, feature := range features {
		clientIP := mosclient.NormalizeClientIP(feature.ClientIP)
		answerIP := mosclient.NormalizeAnswerIP(feature.AnswerIP)
		domain := strings.TrimSpace(feature.Domain)
		if clientIP == "" || answerIP == "" || domain == "" || feature.LastSeen.IsZero() {
			continue
		}
		key := clientIP + "\x00" + answerIP
		lastSeen := feature.LastSeen.UTC()
		if current, ok := evidence[key]; !ok || lastSeen.After(current.lastSeen) {
			evidence[key] = dnsEvidence{domain: domain, lastSeen: lastSeen}
		}
	}
	r.mu.Lock()
	r.evidence = evidence
	r.entries = entries
	r.cacheLoadedAt = time.Now().UTC()
	r.mu.Unlock()
	return nil
}

func (r *ApplicationResolver) cacheIsFresh(now time.Time) bool {
	return !r.cacheLoadedAt.IsZero() && now.Sub(r.cacheLoadedAt) < r.cacheDuration
}
