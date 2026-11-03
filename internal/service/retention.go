package service

import (
	"context"
	"time"
)

const (
	retentionInterval    = 5 * time.Minute
	retentionPassTimeout = 30 * time.Second
	// Terminal details read only the latest 30 minute buckets; a day leaves
	// ample headroom without keeping weeks of per-minute rows per terminal.
	terminalHistoryRetention = 24 * time.Hour
	compactTimeout           = 10 * time.Minute
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
		sampleBefore := now.Add(-time.Duration(m.cfg.SampleRetentionHours) * time.Hour)
		err := m.store.PruneMonitoringState(passCtx, sampleBefore,
			now.Add(-2*time.Hour), now.Add(-terminalHistoryRetention), now.Add(-35*24*time.Hour))
		if err == nil {
			err = m.store.PruneDNSState(passCtx, sampleBefore, now.Add(-dnsFeatureMaxAge))
		}
		cancel()
		if err == nil {
			// Only after a complete pass, so the freed space reflects the backlog.
			compactCtx, compactCancel := context.WithTimeout(ctx, compactTimeout)
			compacted, compactErr := m.store.CompactIfMostlyFree(compactCtx)
			compactCancel()
			if compacted {
				m.logger.Printf("device %s database compacted after retention cleanup", m.store.DeviceID())
			}
			err = compactErr
		}
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
