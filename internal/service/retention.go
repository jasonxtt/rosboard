package service

import (
	"context"
	"time"
)

const (
	retentionInterval    = 5 * time.Minute
	retentionPassTimeout = 30 * time.Second
)

func (m *Monitor) startRetentionMaintenance(ctx context.Context) {
	// Start before the initial RouterOS refresh, which may fail and be retried
	// with the same monitor and lifecycle context by MonitorManager.
	m.retentionOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(retentionInterval)
			defer ticker.Stop()
			m.runRetentionMaintenance(ctx, ticker.C)
		}()
	})
}

func (m *Monitor) runRetentionMaintenance(ctx context.Context, ticks <-chan time.Time) {
	for {
		if ctx.Err() != nil {
			return
		}
		now := time.Now().UTC()
		passCtx, cancel := context.WithTimeout(ctx, retentionPassTimeout)
		err := m.store.PruneMonitoringState(passCtx,
			now.Add(-time.Duration(m.cfg.SampleRetentionHours)*time.Hour),
			now.Add(-2*time.Hour), now.Add(-35*24*time.Hour))
		cancel()
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			// Maintenance failure does not describe the health of RouterOS collection.
			m.logger.Printf("device %s retention maintenance failed: %v", m.store.DeviceID(), err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticks:
		}
	}
}
