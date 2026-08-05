package service

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"rosboard/internal/config"
	"rosboard/internal/model"
	mosclient "rosboard/internal/mosdns"
	"rosboard/internal/store"
)

const (
	mosDNSPageSize = 200
	mosDNSMaxPages = 600
)

type MosDNSStatus struct {
	Enabled                bool      `json:"enabled"`
	BaseURL                string    `json:"baseUrl"`
	SyncIntervalMinutes    int       `json:"syncIntervalMinutes"`
	LastAttempt            time.Time `json:"lastAttempt,omitempty"`
	LastSuccess            time.Time `json:"lastSuccess,omitempty"`
	LastImported           int       `json:"lastImported"`
	LastDuplicates         int       `json:"lastDuplicates"`
	LastSkipped            int       `json:"lastSkipped"`
	Watermark              time.Time `json:"watermark,omitempty"`
	LearnedFeatureCount    int       `json:"learnedFeatureCount"`
	LearnedFeatureLastSeen time.Time `json:"learnedFeatureLastSeen,omitempty"`
	LastError              string    `json:"lastError,omitempty"`
}

type MosDNSSynchronizer struct {
	client    *mosclient.Client
	storage   *store.Store
	logger    *log.Logger
	interval  time.Duration
	retention time.Duration

	mu     sync.RWMutex
	status MosDNSStatus
}

func NewMosDNSSynchronizer(cfg config.MosDNSConfig, storage *store.Store, logger *log.Logger, retentionHours int) (*MosDNSSynchronizer, error) {
	if !cfg.Configured() {
		return nil, nil
	}
	if storage == nil {
		return nil, fmt.Errorf("MosDNS requires the owner store")
	}
	client, err := mosclient.NewClient(cfg.BaseURL, nil)
	if err != nil {
		return nil, err
	}
	if cfg.SyncIntervalMinutes <= 0 {
		return nil, fmt.Errorf("MosDNS sync interval must be positive")
	}
	return &MosDNSSynchronizer{
		client:    client,
		storage:   storage,
		logger:    logger,
		interval:  time.Duration(cfg.SyncIntervalMinutes) * time.Minute,
		retention: time.Duration(retentionHours) * time.Hour,
		status: MosDNSStatus{
			Enabled:             true,
			BaseURL:             cfg.BaseURL,
			SyncIntervalMinutes: cfg.SyncIntervalMinutes,
		},
	}, nil
}

func (s *MosDNSSynchronizer) Start(ctx context.Context) {
	if s == nil {
		return
	}
	if ctx.Err() == nil {
		_ = s.SyncOnce(ctx)
	}
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = s.SyncOnce(ctx)
		}
	}
}

func (s *MosDNSSynchronizer) SyncOnce(ctx context.Context) error {
	if s == nil {
		return nil
	}
	now := time.Now().UTC()
	s.setAttempt(now)
	watermark, hasWatermark, err := s.storage.LoadDNSWatermark(ctx)
	if err != nil {
		return s.fail(err)
	}
	nextWatermark := watermark
	observations := make([]model.DNSObservation, 0)
	skipped := 0
	for page := 1; page <= mosDNSMaxPages; page++ {
		response, err := s.client.AuditLogs(ctx, page, mosDNSPageSize)
		if err != nil {
			return s.fail(err)
		}
		if len(response.Logs) == 0 {
			break
		}
		reachedWatermark := false
		for _, record := range response.Logs {
			if record.QueryTime.IsZero() {
				skipped++
				continue
			}
			if hasWatermark && record.QueryTime.Before(watermark.QueryTime) {
				reachedWatermark = true
				break
			}
			if record.QueryTime.After(nextWatermark.QueryTime) || (record.QueryTime.Equal(nextWatermark.QueryTime) && record.TraceID > nextWatermark.TraceID) {
				nextWatermark = store.DNSWatermark{QueryTime: record.QueryTime.UTC(), TraceID: record.TraceID}
			}
			expanded, invalid := mosclient.ExpandAuditLog(record, now)
			observations = append(observations, expanded...)
			skipped += invalid
		}
		if reachedWatermark || len(response.Logs) < mosDNSPageSize || (response.Pagination.TotalPages > 0 && page >= response.Pagination.TotalPages) {
			break
		}
		if page == mosDNSMaxPages {
			return s.fail(fmt.Errorf("MosDNS audit log watermark was not reached after %d pages", mosDNSMaxPages))
		}
	}

	inserted, err := s.storage.SaveDNSObservations(ctx, observations, nextWatermark)
	if err != nil {
		return s.fail(err)
	}
	if s.retention > 0 {
		if err := s.storage.PruneDNSObservations(ctx, now.Add(-s.retention)); err != nil {
			return s.fail(err)
		}
	}
	featureCount, featureLastSeen, err := s.storage.DNSFeatureSummary(ctx)
	if err != nil {
		return s.fail(err)
	}
	s.mu.Lock()
	s.status.LastSuccess = now
	s.status.LastImported = inserted
	s.status.LastDuplicates = len(observations) - inserted
	s.status.LastSkipped = skipped
	s.status.Watermark = nextWatermark.QueryTime
	s.status.LearnedFeatureCount = featureCount
	s.status.LearnedFeatureLastSeen = featureLastSeen
	s.status.LastError = ""
	s.mu.Unlock()
	return nil
}

func (s *MosDNSSynchronizer) Status() MosDNSStatus {
	if s == nil {
		return MosDNSStatus{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status
}

func (s *MosDNSSynchronizer) setAttempt(at time.Time) {
	s.mu.Lock()
	s.status.LastAttempt = at
	s.mu.Unlock()
}

func (s *MosDNSSynchronizer) fail(err error) error {
	s.mu.Lock()
	s.status.LastError = err.Error()
	s.mu.Unlock()
	if s.logger != nil {
		s.logger.Printf("MosDNS sync failed: %v", err)
	}
	return err
}
