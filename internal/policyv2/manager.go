package policyv2

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"encoding/json"

	"github.com/google/uuid"

	"rosboard/internal/routeros"
)

type PolicyReader interface {
	PolicyList(ctx context.Context, menu routeros.ReadMenu, proplist []string) ([]routeros.RouterOSObject, error)
}

type PolicyMutation interface {
	List(context.Context, routeros.MutationMenu, routeros.MutationQuery) ([]routeros.RouterOSObject, error)
	Create(context.Context, routeros.MutationMenu, routeros.RouterOSFields) (routeros.RouterOSObject, error)
	Patch(context.Context, routeros.MutationMenu, string, routeros.RouterOSFields) (routeros.RouterOSObject, error)
	Delete(context.Context, routeros.MutationMenu, string) error
	Move(context.Context, routeros.MutationMenu, routeros.MoveRequest) (routeros.MutationResponse, error)
	SetDNSSettings(context.Context, routeros.RouterOSFields) error
	FlushDNSCache(context.Context) error
}

type Applier struct {
	Mutation PolicyMutation
	Reader   PolicyReader
	Repo     Repository
	Refresh  SourceRefresher
}

type SourceRefresher func(context.Context, Source) (SourceRefresh, error)

type Manager struct {
	logger   *log.Logger
	mu       sync.RWMutex
	appliers map[string]*Applier
	plans    map[string]cachedPlan
	running  map[string]bool
}

func NewManager(logger *log.Logger) *Manager {
	return &Manager{logger: logger, appliers: make(map[string]*Applier), plans: make(map[string]cachedPlan), running: make(map[string]bool)}
}

func (m *Manager) Enabled() bool { return m != nil }

func (m *Manager) RegisterApplier(deviceID string, applier *Applier) error {
	deviceID = strings.TrimSpace(deviceID)
	if m == nil || deviceID == "" || applier == nil || applier.Reader == nil || applier.Mutation == nil || applier.Repo == nil {
		return errors.New("policy v2 applier registration is incomplete")
	}
	m.mu.Lock()
	m.appliers[deviceID] = applier
	m.mu.Unlock()
	if state, err := applier.Repo.GetDeviceState(context.Background()); err == nil && state.Job.ID != "" && !state.Job.Terminal() {
		state.Job.State = "failed"
		state.Job.Phase = "failed"
		state.Job.Error = "rosboard restarted before the apply finished; generate a new plan and retry"
		state.Job.FinishedAt = time.Now().UTC()
		_ = applier.Repo.SaveApplyJob(context.Background(), state.Job)
	}
	return nil
}

func (m *Manager) ApplierFor(deviceID string) *Applier {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	applier := m.appliers[strings.TrimSpace(deviceID)]
	m.mu.RUnlock()
	return applier
}

var (
	ErrPlanNotFound = errors.New("policy plan not found")
	ErrPlanExpired  = errors.New("policy plan expired")
	ErrPlanStale    = errors.New("policy plan is stale")
	ErrPlanBlocked  = errors.New("policy plan is blocked")
	ErrDeviceBusy   = errors.New("policy device already has an active apply")
)

func (m *Manager) GeneratePlan(ctx context.Context, deviceID, kind string) (PlanEnvelope, error) {
	applier := m.ApplierFor(deviceID)
	if applier == nil {
		return PlanEnvelope{}, errors.New("policy runtime is unavailable")
	}
	desired, err := BuildDesired(ctx, applier.Repo, applier.Reader)
	if err != nil {
		return PlanEnvelope{}, err
	}
	aliasBlockers, err := ValidateFakeAliases(ctx, applier.Reader, applier.Repo)
	if err != nil {
		return PlanEnvelope{}, err
	}
	desired.Blockers = append(desired.Blockers, aliasBlockers...)
	tableBlockers, tableWarnings, err := ValidateRouteTables(ctx, applier.Reader, applier.Repo)
	if err != nil {
		return PlanEnvelope{}, err
	}
	desired.Blockers = append(desired.Blockers, tableBlockers...)
	desired.Warnings = append(desired.Warnings, tableWarnings...)
	ingressBlockers, err := ValidateTrafficIngress(ctx, applier.Reader, applier.Repo)
	if err != nil {
		return PlanEnvelope{}, err
	}
	desired.Blockers = append(desired.Blockers, ingressBlockers...)
	actual, fingerprint, err := ScanManaged(ctx, applier.Mutation, applier.Repo, desired.Objects)
	if err != nil {
		return PlanEnvelope{}, err
	}
	operations, diffBlockers := DiffDesired(desired.Objects, actual)
	blockers := append(append([]PlanIssue{}, desired.Blockers...), diffBlockers...)
	now := time.Now().UTC()
	plan := Plan{
		PlanID: uuid.NewString(), DeviceID: deviceID, Kind: firstNonEmptyString(kind, "structural"), Lifecycle: "interactive",
		CreatedAt: now, ExpiresAt: now.Add(15 * time.Minute), DesiredRevision: desired.Revision, DesiredHash: desired.Hash,
		ActualFingerprint: fingerprint, Blockers: blockers, FamilyBlockers: []PlanIssue{}, Warnings: desired.Warnings,
		Acknowledgements: []PlanAcknowledgement{}, OwnershipStrict: true, Operations: operations,
	}
	plan.Summary = planSummary(operations, blockers, desired.Warnings)
	if len(blockers) > 0 {
		plan.State = "blocked"
	} else {
		plan.State = "ready"
	}
	hashPayload, err := json.Marshal(struct {
		DeviceID          string
		DesiredRevision   int64
		DesiredHash       string
		ActualFingerprint string
		Operations        []PlanOperation
	}{deviceID, desired.Revision, desired.Hash, fingerprint, operations})
	if err != nil {
		return PlanEnvelope{}, err
	}
	plan.PlanHash = shortHash(string(hashPayload), 64)
	m.mu.Lock()
	m.plans[plan.PlanID] = cachedPlan{Plan: plan, Desired: desired.Objects, Actual: actual}
	for id, cached := range m.plans {
		if now.After(cached.Plan.ExpiresAt) {
			delete(m.plans, id)
		}
	}
	m.mu.Unlock()
	return PlanEnvelope{Plan: plan, PlanID: plan.PlanID, PlanHash: plan.PlanHash}, nil
}

func (m *Manager) ApplyPlan(ctx context.Context, deviceID, planID string) (ApplyJob, error) {
	applier := m.ApplierFor(deviceID)
	if applier == nil {
		return ApplyJob{}, errors.New("policy runtime is unavailable")
	}
	m.mu.RLock()
	cached, ok := m.plans[planID]
	m.mu.RUnlock()
	if !ok || cached.Plan.DeviceID != deviceID {
		return ApplyJob{}, ErrPlanNotFound
	}
	if time.Now().After(cached.Plan.ExpiresAt) {
		return ApplyJob{}, ErrPlanExpired
	}
	if len(cached.Plan.Blockers) > 0 {
		return ApplyJob{}, ErrPlanBlocked
	}
	desired, err := BuildDesired(ctx, applier.Repo, applier.Reader)
	if err != nil {
		return ApplyJob{}, err
	}
	if desired.Revision != cached.Plan.DesiredRevision || desired.Hash != cached.Plan.DesiredHash {
		return ApplyJob{}, ErrPlanStale
	}
	if len(desired.Blockers) > 0 {
		return ApplyJob{}, ErrPlanStale
	}
	aliasBlockers, err := ValidateFakeAliases(ctx, applier.Reader, applier.Repo)
	if err != nil {
		return ApplyJob{}, err
	}
	tableBlockers, _, err := ValidateRouteTables(ctx, applier.Reader, applier.Repo)
	if err != nil {
		return ApplyJob{}, err
	}
	ingressBlockers, err := ValidateTrafficIngress(ctx, applier.Reader, applier.Repo)
	if err != nil {
		return ApplyJob{}, err
	}
	if len(aliasBlockers) > 0 || len(tableBlockers) > 0 || len(ingressBlockers) > 0 {
		return ApplyJob{}, ErrPlanStale
	}
	_, fingerprint, err := ScanManaged(ctx, applier.Mutation, applier.Repo, desired.Objects)
	if err != nil {
		return ApplyJob{}, err
	}
	if fingerprint != cached.Plan.ActualFingerprint {
		return ApplyJob{}, ErrPlanStale
	}
	m.mu.Lock()
	if m.running[deviceID] {
		m.mu.Unlock()
		return ApplyJob{}, ErrDeviceBusy
	}
	m.running[deviceID] = true
	delete(m.plans, planID)
	m.mu.Unlock()

	now := time.Now().UTC()
	job := ApplyJob{ID: uuid.NewString(), PlanID: planID, State: "queued", Phase: "queued", CreatedAt: now}
	if err := applier.Repo.SaveApplyJob(ctx, job); err != nil {
		m.mu.Lock()
		delete(m.running, deviceID)
		m.mu.Unlock()
		return ApplyJob{}, err
	}
	go m.runApply(deviceID, applier, cached, job)
	return job, nil
}

func (m *Manager) GenerateAndApply(ctx context.Context, deviceID, kind string) (ApplyJob, error) {
	plan, err := m.GeneratePlan(ctx, deviceID, kind)
	if err != nil {
		return ApplyJob{}, err
	}
	return m.ApplyPlan(ctx, deviceID, plan.PlanID)
}

func (m *Manager) GetJob(ctx context.Context, deviceID, jobID string) (ApplyJob, error) {
	applier := m.ApplierFor(deviceID)
	if applier == nil {
		return ApplyJob{}, errors.New("policy runtime is unavailable")
	}
	return applier.Repo.GetApplyJob(ctx, jobID)
}

func (m *Manager) Start(ctx context.Context) {
	if m == nil {
		return
	}
	_ = m.RefreshDue(ctx, time.Now().UTC())
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			_ = m.RefreshDue(ctx, now.UTC())
		}
	}
}

func (m *Manager) RefreshDue(ctx context.Context, now time.Time) error {
	m.mu.RLock()
	appliers := make([]*Applier, 0, len(m.appliers))
	for _, applier := range m.appliers {
		appliers = append(appliers, applier)
	}
	m.mu.RUnlock()
	for _, applier := range appliers {
		if applier.Refresh == nil {
			continue
		}
		sources, err := applier.Repo.ListSources(ctx, "")
		if err != nil {
			return err
		}
		needsApply := false
		for _, source := range sources {
			interval, ok := sourceScheduleInterval(source.Schedule)
			if !ok || source.Type != "url" || !source.Enabled || source.PendingDeletion || source.PendingVersionID != "" {
				continue
			}
			if !source.NextRunAt.IsZero() && source.NextRunAt.After(now) {
				continue
			}
			next := now.Add(interval)
			refresh, refreshErr := applier.Refresh(ctx, source)
			if refreshErr != nil {
				refresh = SourceRefresh{Version: &SourceVersion{ID: uuid.NewString(), SourceID: source.ID, State: "failed", Error: refreshErr.Error(), CreatedAt: now}}
			}
			saveErr := applier.Repo.SaveSourceRefresh(ctx, source, refresh, next)
			if saveErr != nil && !errors.Is(saveErr, ErrRevisionStale) {
				return saveErr
			}
			if saveErr == nil && refresh.Version != nil && refresh.Version.State != "failed" && SourceAutoApplyEligible(ctx, applier.Repo, source) {
				needsApply = true
			}
		}
		if needsApply {
			if _, err := m.GenerateAndApply(ctx, applier.Repo.DeviceID(), "source-refresh"); err != nil {
				return err
			}
		}
	}
	return nil
}

// SourceAutoApplyEligible reports whether a source change should be reflected
// in RouterOS immediately. Unassigned and disabled policy sources remain
// drafts until a later binding or enable operation makes them relevant.
func SourceAutoApplyEligible(ctx context.Context, repository Repository, source Source) bool {
	if repository == nil || source.EgressID == "" || !source.Enabled || source.PendingDeletion {
		return false
	}
	egress, err := repository.GetEgress(ctx, source.EgressID)
	return err == nil && egress.Enabled && !egress.PendingDeletion
}

func sourceScheduleInterval(value string) (time.Duration, bool) {
	switch strings.TrimSpace(value) {
	case "1h":
		return time.Hour, true
	case "6h":
		return 6 * time.Hour, true
	case "12h":
		return 12 * time.Hour, true
	case "", "24h":
		return 24 * time.Hour, true
	case "7d":
		return 7 * 24 * time.Hour, true
	case "30d":
		return 30 * 24 * time.Hour, true
	default:
		return 0, false
	}
}

func (m *Manager) runApply(deviceID string, applier *Applier, cached cachedPlan, job ApplyJob) {
	defer func() {
		m.mu.Lock()
		delete(m.running, deviceID)
		m.mu.Unlock()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	job.State = "staging"
	job.Phase = "staging"
	job.StartedAt = time.Now().UTC()
	if err := applier.Repo.SaveApplyJob(ctx, job); err != nil {
		m.logApplyError(deviceID, job.ID, err)
		return
	}
	if err := ensureDefaultDNSCache(ctx, applier); err != nil {
		m.failJob(ctx, applier.Repo, &job, "dns-cache-size", err)
		return
	}
	createdRouterIDs := make(map[string]string)
	for index, operation := range cached.Plan.Operations {
		if err := applyOperation(ctx, applier.Mutation, operation, createdRouterIDs); err != nil {
			m.failJob(ctx, applier.Repo, &job, operation.LogicalID, err)
			return
		}
		job.Progress = index + 1
		if err := applier.Repo.SaveApplyJob(ctx, job); err != nil {
			m.logApplyError(deviceID, job.ID, err)
			return
		}
	}
	if err := applier.Mutation.FlushDNSCache(ctx); err != nil {
		m.failJob(ctx, applier.Repo, &job, "dns-cache", err)
		return
	}
	job.State = "verifying"
	job.Phase = "verifying"
	if err := applier.Repo.SaveApplyJob(ctx, job); err != nil {
		m.logApplyError(deviceID, job.ID, err)
		return
	}
	actual, _, err := ScanManaged(ctx, applier.Mutation, applier.Repo, cached.Desired)
	if err != nil {
		m.failJob(ctx, applier.Repo, &job, "verify-scan", err)
		return
	}
	remaining, blockers := DiffDesired(cached.Desired, actual)
	if len(remaining) > 0 || len(blockers) > 0 {
		m.failJob(ctx, applier.Repo, &job, "verify-diff", fmt.Errorf("RouterOS still differs after apply: operations=%d blockers=%d", len(remaining), len(blockers)))
		return
	}
	job.Progress = len(cached.Plan.Operations)
	if err := applier.Repo.CommitApply(ctx, cached.Plan.DesiredRevision, cached.Plan.DesiredHash, job); err != nil {
		m.failJob(ctx, applier.Repo, &job, "commit", err)
	}
}

const defaultDNSCacheSizeKiB int64 = 32768

func ensureDefaultDNSCache(ctx context.Context, applier *Applier) error {
	settings, err := applier.Reader.PolicyList(ctx, routeros.ReadMenuIPDNS, []string{"cache-size"})
	if err != nil {
		return fmt.Errorf("读取 RouterOS DNS cache-size 失败: %w", err)
	}
	if len(settings) > 0 {
		value, ok := settings[0]["cache-size"]
		if ok {
			current, parseErr := parseDNSCacheSizeKiB(value)
			if parseErr != nil {
				return fmt.Errorf("解析 RouterOS DNS cache-size 失败: %w", parseErr)
			}
			if current >= defaultDNSCacheSizeKiB {
				return nil
			}
		}
	}
	if err := applier.Mutation.SetDNSSettings(ctx, routeros.RouterOSFields{"cache-size": "32768KiB"}); err != nil {
		return fmt.Errorf("设置 RouterOS DNS cache-size=32768KiB 失败: %w", err)
	}
	return nil
}

func parseDNSCacheSizeKiB(value string) (int64, error) {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) >= 3 && strings.EqualFold(trimmed[len(trimmed)-3:], "kib") {
		trimmed = strings.TrimSpace(trimmed[:len(trimmed)-3])
	}
	parsed, err := strconv.ParseInt(trimmed, 10, 64)
	if err != nil || parsed < 0 {
		if err == nil {
			err = errors.New("cache-size must not be negative")
		}
		return 0, err
	}
	return parsed, nil
}

func applyOperation(ctx context.Context, mutation PolicyMutation, operation PlanOperation, createdRouterIDs map[string]string) error {
	menu := routeros.MutationMenu(operation.Menu)
	fields := make(routeros.RouterOSFields, len(operation.After))
	for key, value := range operation.After {
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "yes":
			fields[key] = true
		case "no":
			fields[key] = false
		default:
			fields[key] = value
		}
	}
	switch operation.Action {
	case "create":
		object, err := mutation.Create(ctx, menu, fields)
		if err == nil && object.ID() != "" {
			createdRouterIDs[operation.LogicalID] = object.ID()
		}
		return err
	case "patch":
		_, err := mutation.Patch(ctx, menu, operation.RouterID, fields)
		return err
	case "delete":
		return mutation.Delete(ctx, menu, operation.RouterID)
	case "move":
		sourceID := operation.RouterID
		if sourceID == "" {
			sourceID = createdRouterIDs[operation.LogicalID]
		}
		if sourceID == "" || operation.Anchor == nil {
			return fmt.Errorf("move operation is missing its source or anchor")
		}
		beforeID := operation.Anchor.RouterID
		if beforeID == "" {
			beforeID = createdRouterIDs[operation.Anchor.LogicalID]
		}
		if beforeID == "" {
			return fmt.Errorf("move operation is missing its destination anchor")
		}
		_, err := mutation.Move(ctx, menu, routeros.MoveRequest{ID: sourceID, BeforeID: beforeID})
		return err
	default:
		return fmt.Errorf("unsupported policy operation %q", operation.Action)
	}
}

func (m *Manager) failJob(ctx context.Context, repository Repository, job *ApplyJob, target string, cause error) {
	job.State = "failed"
	job.Phase = "failed"
	job.Error = target + ": " + cause.Error()
	job.FinishedAt = time.Now().UTC()
	if err := repository.SaveApplyJob(ctx, *job); err != nil {
		m.logApplyError(repository.DeviceID(), job.ID, err)
	}
}

func (m *Manager) logApplyError(deviceID, jobID string, err error) {
	if m != nil && m.logger != nil {
		m.logger.Printf("policy v2 device=%s job=%s: %v", deviceID, jobID, err)
	}
}
