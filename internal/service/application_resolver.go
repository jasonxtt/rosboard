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

type dnsEvidence struct {
	domain    string
	queryTime time.Time
	expiresAt time.Time
}

type ApplicationResolver struct {
	storage       *store.Store
	registry      *applicationpreset.Registry
	matchWindow   time.Duration
	cacheDuration time.Duration

	mu            sync.RWMutex
	refreshMu     sync.Mutex
	cacheLoadedAt time.Time
	cacheSince    time.Time
	cacheUntil    time.Time
	evidence      map[string][]dnsEvidence
	entries       []applicationpreset.DomainEntry
}

// NewApplicationResolver uses only materialized curated preset TargetLists
// for attribution.
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
		evidence:      make(map[string][]dnsEvidence),
	}
}

func (r *ApplicationResolver) Resolve(ctx context.Context, clientIP, answerIP string, at time.Time) (string, string, string, bool) {
	if r == nil {
		return "", "", "", false
	}
	clientIP = mosclient.NormalizeClientIP(clientIP)
	answerIP = mosclient.NormalizeAnswerIP(answerIP)
	if clientIP == "" || answerIP == "" {
		return "", "", "", false
	}
	if at.IsZero() {
		at = time.Now().UTC()
	} else {
		at = at.UTC()
	}
	if err := r.refresh(ctx, at); err != nil {
		return "", "", "", false
	}
	r.mu.RLock()
	evidence := append([]dnsEvidence(nil), r.evidence[clientIP+"\x00"+answerIP]...)
	entries := append([]applicationpreset.DomainEntry(nil), r.entries...)
	r.mu.RUnlock()
	for _, candidate := range evidence {
		if !candidate.validAt(at, r.matchWindow) {
			continue
		}
		match := r.registry.MatchDomain(candidate.domain, entries)
		if match.Ambiguous || match.Preset.ID == "" {
			return "", "", candidate.domain, false
		}
		return match.Preset.ID, match.Preset.Name, candidate.domain, true
	}
	return "", "", "", false
}

func (r *ApplicationResolver) refresh(ctx context.Context, at time.Time) error {
	if at.IsZero() {
		at = time.Now().UTC()
	} else {
		at = at.UTC()
	}
	since := at.Add(-r.matchWindow)
	until := at.Add(2 * time.Minute)
	now := time.Now().UTC()
	r.mu.RLock()
	fresh := r.cacheIsFresh(now, at)
	r.mu.RUnlock()
	if fresh {
		return nil
	}
	r.refreshMu.Lock()
	defer r.refreshMu.Unlock()
	r.mu.RLock()
	fresh = r.cacheIsFresh(time.Now().UTC(), at)
	r.mu.RUnlock()
	if fresh {
		return nil
	}
	observations, err := r.storage.DNSObservationsForMatch(ctx, since, until)
	if err != nil {
		return err
	}
	entries, err := r.storage.PolicyRepository().ListPresetDomainEntries(ctx)
	if err != nil {
		return err
	}
	evidence := make(map[string][]dnsEvidence, len(observations))
	for _, observation := range observations {
		clientIP := mosclient.NormalizeClientIP(observation.ClientIP)
		answerIP := mosclient.NormalizeAnswerIP(observation.AnswerIP)
		domain := strings.TrimSpace(observation.Domain)
		if clientIP == "" || answerIP == "" || domain == "" || observation.QueryTime.IsZero() || observation.TTL <= 0 {
			continue
		}
		key := clientIP + "\x00" + answerIP
		queryTime := observation.QueryTime.UTC()
		evidence[key] = append(evidence[key], dnsEvidence{
			domain:    domain,
			queryTime: queryTime,
			expiresAt: queryTime.Add(time.Duration(observation.TTL) * time.Second),
		})
	}
	r.mu.Lock()
	r.evidence = evidence
	r.entries = entries
	r.cacheLoadedAt = now
	r.cacheSince = since
	r.cacheUntil = until
	r.mu.Unlock()
	return nil
}

func (r *ApplicationResolver) cacheIsFresh(now, at time.Time) bool {
	return !r.cacheLoadedAt.IsZero() && now.Sub(r.cacheLoadedAt) < r.cacheDuration &&
		!at.Before(r.cacheSince) && !at.After(r.cacheUntil)
}

func (evidence dnsEvidence) validAt(at time.Time, matchWindow time.Duration) bool {
	return !evidence.queryTime.After(at) && at.Sub(evidence.queryTime) <= matchWindow && at.Before(evidence.expiresAt)
}
