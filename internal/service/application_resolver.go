package service

import (
	"context"
	"strings"
	"sync"
	"time"

	mosclient "rosboard/internal/mosdns"
	"rosboard/internal/store"
)

type ApplicationResolver struct {
	storage       *store.Store
	feature       *FeatureLibrarySynchronizer
	matchWindow   time.Duration
	cacheDuration time.Duration

	mu            sync.RWMutex
	refreshMu     sync.Mutex
	cacheLoadedAt time.Time
	domains       map[string]string
}

func NewApplicationResolver(storage *store.Store, feature *FeatureLibrarySynchronizer, mosEnabled bool, matchWindowMinutes int) *ApplicationResolver {
	if storage == nil || feature == nil || !mosEnabled || matchWindowMinutes <= 0 {
		return nil
	}
	return &ApplicationResolver{
		storage:       storage,
		feature:       feature,
		matchWindow:   time.Duration(matchWindowMinutes) * time.Minute,
		cacheDuration: 30 * time.Second,
		domains:       make(map[string]string),
	}
}

func (r *ApplicationResolver) Resolve(ctx context.Context, clientIP, answerIP string, at time.Time) (string, string, bool) {
	if r == nil {
		return "", "", false
	}
	clientIP = mosclient.NormalizeClientIP(clientIP)
	answerIP = mosclient.NormalizeAnswerIP(answerIP)
	if clientIP == "" || answerIP == "" {
		return "", "", false
	}
	if err := r.refresh(ctx, at); err != nil {
		return "", "", false
	}
	r.mu.RLock()
	domain := r.domains[clientIP+"\x00"+answerIP]
	r.mu.RUnlock()
	if domain == "" {
		return "", "", false
	}
	application, ok := r.feature.Lookup(domain)
	if !ok {
		return "", domain, false
	}
	return application, domain, true
}

func (r *ApplicationResolver) refresh(ctx context.Context, at time.Time) error {
	now := time.Now().UTC()
	r.mu.RLock()
	fresh := !r.cacheLoadedAt.IsZero() && now.Sub(r.cacheLoadedAt) < r.cacheDuration
	r.mu.RUnlock()
	if fresh {
		return nil
	}
	r.refreshMu.Lock()
	defer r.refreshMu.Unlock()
	r.mu.RLock()
	fresh = !r.cacheLoadedAt.IsZero() && time.Now().UTC().Sub(r.cacheLoadedAt) < r.cacheDuration
	r.mu.RUnlock()
	if fresh {
		return nil
	}
	if at.IsZero() {
		at = now
	}
	observations, err := r.storage.DNSObservationsForMatch(ctx, at.Add(-r.matchWindow), at.Add(2*time.Minute))
	if err != nil {
		return err
	}
	features, err := r.storage.DNSFeaturesForMatch(ctx)
	if err != nil {
		return err
	}
	domains := make(map[string]string, len(observations))
	for _, observation := range observations {
		clientIP := mosclient.NormalizeClientIP(observation.ClientIP)
		answerIP := mosclient.NormalizeAnswerIP(observation.AnswerIP)
		if clientIP == "" || answerIP == "" || strings.TrimSpace(observation.Domain) == "" {
			continue
		}
		key := clientIP + "\x00" + answerIP
		if _, exists := domains[key]; !exists {
			domains[key] = observation.Domain
		}
	}
	for _, feature := range features {
		clientIP := mosclient.NormalizeClientIP(feature.ClientIP)
		answerIP := mosclient.NormalizeAnswerIP(feature.AnswerIP)
		if clientIP == "" || answerIP == "" || strings.TrimSpace(feature.Domain) == "" {
			continue
		}
		key := clientIP + "\x00" + answerIP
		if _, exists := domains[key]; !exists {
			domains[key] = feature.Domain
		}
	}
	r.mu.Lock()
	r.domains = domains
	r.cacheLoadedAt = now
	r.mu.Unlock()
	return nil
}
