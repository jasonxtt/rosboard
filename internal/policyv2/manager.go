package policyv2

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"encoding/json"

	"github.com/google/uuid"

	"rosboard/internal/accesscontrol"
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

// PolicyBatchMutation is optional so existing test and integration fakes can
// keep the small CRUD contract. The real RouterOS client implements these
// methods for the high-volume DNS and activation paths.
type PolicyBatchMutation interface {
	CreateBatch(context.Context, routeros.MutationMenu, []routeros.RouterOSFields) error
	SetDisabledBatch(context.Context, routeros.MutationMenu, []string, bool) error
}

// AccessCapabilityVerifier is implemented by the real RouterOS mutation
// client. It proves the exact filter features required by an access-control
// plan before any plan mutation starts.
type AccessCapabilityVerifier interface {
	VerifyAccessControlCapabilities(context.Context, []routeros.MutationMenu) error
}

type Applier struct {
	Mutation  PolicyMutation
	Reader    PolicyReader
	Repo      Repository
	Access    accesscontrol.Repository
	Terminals TerminalResolver
	Scope     ScopeResolver
	Refresh   SourceRefresher
}

type PlanOptions struct {
	InternetEgresses map[string][]string
}

type SourceRefresher func(context.Context, Source) (SourceRefresh, error)

type Manager struct {
	logger   *log.Logger
	mu       sync.RWMutex
	appliers map[string]*Applier
	plans    map[string]cachedPlan
	gate     *routeros.DeviceWriteGate
}

func NewManager(logger *log.Logger) *Manager {
	return &Manager{logger: logger, appliers: make(map[string]*Applier), plans: make(map[string]cachedPlan), gate: routeros.NewDeviceWriteGate()}
}

func (m *Manager) Enabled() bool { return m != nil }

func (m *Manager) WriteGate() *routeros.DeviceWriteGate {
	if m == nil {
		return nil
	}
	return m.gate
}

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
	return m.GeneratePlanWithOptions(ctx, deviceID, kind, PlanOptions{})
}

func (m *Manager) GeneratePlanWithOptions(ctx context.Context, deviceID, kind string, options PlanOptions) (PlanEnvelope, error) {
	applier := m.ApplierFor(deviceID)
	if applier == nil {
		return PlanEnvelope{}, errors.New("policy runtime is unavailable")
	}
	accessOnlyPlan := isAccessOnlyPlanKind(kind)
	desired, err := BuildDesiredWithAccessOptions(ctx, applier.Repo, applier.Reader, applier.Access, applier.Terminals, applier.Scope, options.InternetEgresses)
	if err != nil {
		return PlanEnvelope{}, err
	}
	if accessOnlyPlan {
		policyState, stateErr := applier.Repo.GetDeviceState(ctx)
		if stateErr != nil {
			return PlanEnvelope{}, stateErr
		}
		if policyState.DesiredRevision != policyState.AppliedRevision {
			desired.Blockers = append(desired.Blockers, PlanIssue{
				Code:   "policy_changes_pending",
				Status: "blocker",
				Reason: "策略路由仍有待应用变更，访问控制同步不会替代策略路由应用。",
			})
		}
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
	preflightRelease, acquired := m.gate.TryAcquire(deviceID)
	if !acquired {
		return PlanEnvelope{}, ErrDeviceBusy
	}
	capabilityBlockers, err := accessCapabilityBlockers(ctx, applier.Mutation, desired.Objects)
	preflightRelease()
	if err != nil {
		return PlanEnvelope{}, err
	}
	desired.Blockers = append(desired.Blockers, capabilityBlockers...)
	actual, fingerprint, err := ScanManaged(ctx, applier.Mutation, applier.Repo, desired.Objects)
	if err != nil {
		return PlanEnvelope{}, err
	}
	operations, diffBlockers := DiffDesired(desired.Objects, actual)
	accessOrderOperations, err := planAccessJumpsFirst(ctx, applier.Mutation, desired.Objects, actual)
	if err != nil {
		return PlanEnvelope{}, err
	}
	operations = append(operations, accessOrderOperations...)
	sortPlanOperations(operations)
	if accessOnlyPlan {
		for _, operation := range operations {
			if isAccessPlanOperation(operation, desired.AccessSourceIDs) {
				continue
			}
			diffBlockers = append(diffBlockers, PlanIssue{
				Code:      "policy_drift_requires_policy_apply",
				Status:    "blocker",
				LogicalID: operation.LogicalID,
				Reason:    "访问控制同步发现 RouterOS 上有未纳入当前策略配置的策略对象。请先登录 RouterOS 手动确认并清理旧版 rosboard 对象，完成后再重新应用访问控制；若对象属于当前策略配置，再从策略路由页面生成并应用完整计划。",
			})
			break
		}
	}
	blockers := append(append([]PlanIssue{}, desired.Blockers...), diffBlockers...)
	now := time.Now().UTC()
	plan := Plan{
		PlanID: uuid.NewString(), DeviceID: deviceID, Kind: firstNonEmptyString(kind, "structural"), Lifecycle: "interactive",
		CreatedAt: now, ExpiresAt: now.Add(15 * time.Minute), DesiredRevision: desired.Revision, AccessRevision: desired.AccessRevision, AccessResolutionCount: len(desired.AccessResolutions), DesiredHash: desired.Hash,
		InternetEgressCandidates: desired.InternetEgressCandidates,
		ActualFingerprint:        fingerprint, Blockers: blockers, FamilyBlockers: []PlanIssue{}, Warnings: desired.Warnings,
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
		AccessRevision    int64
		Operations        []PlanOperation
	}{deviceID, desired.Revision, desired.Hash, fingerprint, desired.AccessRevision, operations})
	if err != nil {
		return PlanEnvelope{}, err
	}
	plan.PlanHash = shortHash(string(hashPayload), 64)
	m.mu.Lock()
	m.plans[plan.PlanID] = cachedPlan{Plan: plan, Desired: desired.Objects, Actual: actual, AccessResolutions: desired.AccessResolutions, InternetEgresses: cloneInternetEgresses(options.InternetEgresses)}
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
	desired, err := BuildDesiredWithAccessOptions(ctx, applier.Repo, applier.Reader, applier.Access, applier.Terminals, applier.Scope, cached.InternetEgresses)
	if err != nil {
		return ApplyJob{}, err
	}
	if desired.Revision != cached.Plan.DesiredRevision || desired.AccessRevision != cached.Plan.AccessRevision || desired.Hash != cached.Plan.DesiredHash {
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
	release, acquired := m.gate.TryAcquire(deviceID)
	if !acquired {
		return ApplyJob{}, ErrDeviceBusy
	}
	// The desired graph was built before acquiring the RouterOS gate. Desired
	// writes do not use that gate, so take one final snapshot while this apply
	// owns the device slot; otherwise a save that lands while ApplyPlan waits
	// could still stage the old graph before CommitApply rejects it.
	latestDesired, latestErr := BuildDesiredWithAccessOptions(ctx, applier.Repo, applier.Reader, applier.Access, applier.Terminals, applier.Scope, cached.InternetEgresses)
	if latestErr != nil {
		release()
		return ApplyJob{}, latestErr
	}
	if latestDesired.Revision != cached.Plan.DesiredRevision || latestDesired.AccessRevision != cached.Plan.AccessRevision || latestDesired.Hash != cached.Plan.DesiredHash || len(latestDesired.Blockers) > 0 {
		release()
		return ApplyJob{}, ErrPlanStale
	}
	desired = latestDesired
	aliasBlockers, err = ValidateFakeAliases(ctx, applier.Reader, applier.Repo)
	if err != nil {
		release()
		return ApplyJob{}, err
	}
	tableBlockers, _, err = ValidateRouteTables(ctx, applier.Reader, applier.Repo)
	if err != nil {
		release()
		return ApplyJob{}, err
	}
	ingressBlockers, err = ValidateTrafficIngress(ctx, applier.Reader, applier.Repo)
	if err != nil {
		release()
		return ApplyJob{}, err
	}
	if len(aliasBlockers) > 0 || len(tableBlockers) > 0 || len(ingressBlockers) > 0 {
		release()
		return ApplyJob{}, ErrPlanStale
	}
	if capabilityBlockers, capabilityErr := accessCapabilityBlockers(ctx, applier.Mutation, desired.Objects); capabilityErr != nil {
		release()
		return ApplyJob{}, capabilityErr
	} else if len(capabilityBlockers) > 0 {
		release()
		return ApplyJob{}, ErrPlanStale
	}
	_, fingerprint, err := ScanManaged(ctx, applier.Mutation, applier.Repo, desired.Objects)
	if err != nil {
		release()
		return ApplyJob{}, err
	}
	if fingerprint != cached.Plan.ActualFingerprint {
		release()
		return ApplyJob{}, ErrPlanStale
	}
	m.mu.Lock()
	delete(m.plans, planID)
	m.mu.Unlock()

	now := time.Now().UTC()
	job := ApplyJob{ID: uuid.NewString(), PlanID: planID, State: "queued", Phase: "queued", CreatedAt: now}
	if err := applier.Repo.SaveApplyJob(ctx, job); err != nil {
		release()
		return ApplyJob{}, err
	}
	go m.runApply(deviceID, applier, cached, job, release)
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
	_ = m.ReconcileAccess(ctx)
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			_ = m.RefreshDue(ctx, now.UTC())
			_ = m.ReconcileAccess(ctx)
		}
	}
}

func (m *Manager) ReconcileAccess(ctx context.Context) error {
	m.mu.RLock()
	deviceIDs := make([]string, 0, len(m.appliers))
	for deviceID, applier := range m.appliers {
		if applier.Access != nil {
			deviceIDs = append(deviceIDs, deviceID)
		}
	}
	m.mu.RUnlock()
	sort.Strings(deviceIDs)
	for _, deviceID := range deviceIDs {
		applier := m.ApplierFor(deviceID)
		if applier == nil || applier.Access == nil {
			continue
		}
		plan, err := m.GeneratePlan(ctx, deviceID, "access-terminal-refresh")
		if err != nil {
			return err
		}
		if len(plan.Plan.Blockers) > 0 || (len(plan.Plan.Operations) == 0 && plan.Plan.AccessResolutionCount == 0) {
			continue
		}
		if _, err := m.ApplyPlan(ctx, deviceID, plan.PlanID); err != nil && !errors.Is(err, ErrDeviceBusy) {
			return err
		}
	}
	return nil
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
			if saveErr == nil && refresh.Version != nil && refresh.Version.State != "failed" && SourceAutoApplyEligible(ctx, applier.Repo, source, applier.Access) {
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
// in RouterOS immediately. A disable transition is relevant too: it must
// remove or block the source's existing projection rather than leave RouterOS
// stale.
func SourceAutoApplyEligible(ctx context.Context, repository Repository, source Source, accessRepositories ...accesscontrol.Repository) bool {
	if repository == nil || source.PendingDeletion {
		return false
	}
	if source.EgressID != "" {
		egress, err := repository.GetEgress(ctx, source.EgressID)
		if err == nil && egress.Enabled && !egress.PendingDeletion {
			return true
		}
	}
	for _, accessRepository := range accessRepositories {
		if accessRepository == nil {
			continue
		}
		rules, err := accessRepository.ListRules(ctx)
		if err != nil {
			continue
		}
		for _, rule := range rules {
			if !rule.Enabled || rule.TargetScope != accesscontrol.TargetScopeSources {
				continue
			}
			for _, sourceID := range rule.SourceIDs {
				if sourceID == source.ID {
					return true
				}
			}
		}
	}
	return false
}

func sourceScheduleInterval(value string) (time.Duration, bool) {
	switch strings.TrimSpace(value) {
	case "1h":
		return time.Hour, true
	case "6h":
		return 6 * time.Hour, true
	case "12h":
		return 12 * time.Hour, true
	case "":
		return 7 * 24 * time.Hour, true
	case "24h":
		return 24 * time.Hour, true
	case "7d":
		return 7 * 24 * time.Hour, true
	case "30d":
		return 30 * 24 * time.Hour, true
	default:
		return 0, false
	}
}

func (m *Manager) runApply(deviceID string, applier *Applier, cached cachedPlan, job ApplyJob, release func()) {
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	job.State = "staging"
	job.Phase = "staging"
	job.StartedAt = time.Now().UTC()
	if err := applier.Repo.SaveApplyJob(ctx, job); err != nil {
		m.failJob(ctx, applier.Repo, &job, "save-start", err)
		return
	}
	planned, err := BuildDesiredWithAccessOptions(ctx, applier.Repo, applier.Reader, applier.Access, applier.Terminals, applier.Scope, cached.InternetEgresses)
	if err != nil {
		m.failJob(ctx, applier.Repo, &job, "desired-state-check", err)
		return
	}
	if planned.Revision != cached.Plan.DesiredRevision || planned.AccessRevision != cached.Plan.AccessRevision || planned.Hash != cached.Plan.DesiredHash || len(planned.Blockers) > 0 {
		m.failJob(ctx, applier.Repo, &job, "desired-state-changed", ErrPlanStale)
		return
	}
	aliasBlockers, err := ValidateFakeAliases(ctx, applier.Reader, applier.Repo)
	if err != nil {
		m.failJob(ctx, applier.Repo, &job, "validate-fake-aliases", err)
		return
	}
	tableBlockers, _, err := ValidateRouteTables(ctx, applier.Reader, applier.Repo)
	if err != nil {
		m.failJob(ctx, applier.Repo, &job, "validate-route-tables", err)
		return
	}
	ingressBlockers, err := ValidateTrafficIngress(ctx, applier.Reader, applier.Repo)
	if err != nil {
		m.failJob(ctx, applier.Repo, &job, "validate-traffic-ingress", err)
		return
	}
	if len(aliasBlockers) > 0 || len(tableBlockers) > 0 || len(ingressBlockers) > 0 {
		m.failJob(ctx, applier.Repo, &job, "desired-state-validation", ErrPlanStale)
		return
	}
	needsDNSMutation := !isAccessOnlyPlanKind(cached.Plan.Kind) || hasDesiredMenu(cached.Desired, routeros.MenuIPDNSStatic)
	if needsDNSMutation {
		if err := ensureDefaultDNSCache(ctx, applier); err != nil {
			m.failJob(ctx, applier.Repo, &job, "dns-cache-size", err)
			return
		}
	}
	createdRouterIDs := make(map[string]string)
	batchMutation, hasBatchMutation := applier.Mutation.(PolicyBatchMutation)
	for index := 0; index < len(cached.Plan.Operations); {
		operation := cached.Plan.Operations[index]
		nextIndex := index + 1
		switch {
		case hasBatchMutation && operation.Action == "create" && operation.Menu == string(routeros.MenuIPDNSStatic):
			entries := make([]routeros.RouterOSFields, 0)
			nextIndex = index
			for nextIndex < len(cached.Plan.Operations) {
				candidate := cached.Plan.Operations[nextIndex]
				if candidate.Action != "create" || candidate.Menu != string(routeros.MenuIPDNSStatic) {
					break
				}
				entries = append(entries, operationFields(candidate, true))
				nextIndex++
			}
			if err := batchMutation.CreateBatch(ctx, routeros.MenuIPDNSStatic, entries); err != nil {
				m.failJob(ctx, applier.Repo, &job, operation.LogicalID, err)
				return
			}
		case hasBatchMutation && isDisableOnlyPatch(operation):
			ids := make([]string, 0)
			nextIndex = index
			for nextIndex < len(cached.Plan.Operations) {
				candidate := cached.Plan.Operations[nextIndex]
				if !isDisableOnlyPatch(candidate) || candidate.Menu != operation.Menu {
					break
				}
				ids = append(ids, candidate.RouterID)
				nextIndex++
			}
			if err := batchMutation.SetDisabledBatch(ctx, routeros.MutationMenu(operation.Menu), ids, true); err != nil {
				m.failJob(ctx, applier.Repo, &job, operation.LogicalID, err)
				return
			}
		case isEnableOnlyPatch(operation):
			// Active objects are enabled together after every create/update has
			// been staged. This avoids exposing a partially built policy.
		case isDisableOnlyPatch(operation):
			if err := applyStagedOperation(ctx, applier.Mutation, operation, createdRouterIDs); err != nil {
				m.failJob(ctx, applier.Repo, &job, operation.LogicalID, err)
				return
			}
		default:
			if err := applyStagedOperation(ctx, applier.Mutation, operation, createdRouterIDs); err != nil {
				m.failJob(ctx, applier.Repo, &job, operation.LogicalID, err)
				return
			}
		}
		index = nextIndex
		job.Progress = index
		if err := applier.Repo.SaveApplyJob(ctx, job); err != nil {
			m.failJob(ctx, applier.Repo, &job, "save-progress", err)
			return
		}
	}
	if err := ensureAccessJumpsFirst(ctx, applier.Mutation, cached.Desired); err != nil {
		m.failJob(ctx, applier.Repo, &job, "access-filter-order", err)
		return
	}
	job.Phase = "activation"
	if err := applier.Repo.SaveApplyJob(ctx, job); err != nil {
		m.failJob(ctx, applier.Repo, &job, "save-activation", err)
		return
	}
	if err := activateDesiredObjects(ctx, applier, cached.Desired); err != nil {
		m.failJob(ctx, applier.Repo, &job, "activation", err)
		return
	}
	if needsDNSMutation {
		if err := applier.Mutation.FlushDNSCache(ctx); err != nil {
			m.failJob(ctx, applier.Repo, &job, "dns-cache", err)
			return
		}
	}
	job.State = "verifying"
	job.Phase = "verifying"
	if err := applier.Repo.SaveApplyJob(ctx, job); err != nil {
		m.failJob(ctx, applier.Repo, &job, "save-verifying", err)
		return
	}
	remaining, blockers, err := verifyDesiredWithRetry(ctx, applier.Mutation, applier.Repo, cached.Desired)
	if err != nil {
		m.failJob(ctx, applier.Repo, &job, "verify-scan", err)
		return
	}
	if len(remaining) > 0 || len(blockers) > 0 {
		m.failJob(ctx, applier.Repo, &job, "verify-diff", fmt.Errorf("RouterOS still differs after apply: operations=%d blockers=%d", len(remaining), len(blockers)))
		return
	}
	// Monitor-driven auto addresses do not bump the persistent access revision.
	// Re-read the desired graph immediately before committing so a terminal
	// change during the RouterOS mutation cannot be recorded as an applied
	// trusted projection for an older snapshot.
	latest, err := BuildDesiredWithAccessOptions(ctx, applier.Repo, applier.Reader, applier.Access, applier.Terminals, applier.Scope, cached.InternetEgresses)
	if err != nil {
		m.failJob(ctx, applier.Repo, &job, "desired-state-check", err)
		return
	}
	if latest.Revision != cached.Plan.DesiredRevision || latest.AccessRevision != cached.Plan.AccessRevision || latest.Hash != cached.Plan.DesiredHash {
		m.failJob(ctx, applier.Repo, &job, "desired-state-changed", ErrPlanStale)
		return
	}
	job.Progress = len(cached.Plan.Operations)
	if err := applier.Repo.CommitApply(ctx, cached.Plan.DesiredRevision, cached.Plan.AccessRevision, cached.Plan.DesiredHash, job, cached.AccessResolutions, !isAccessOnlyPlanKind(cached.Plan.Kind)); err != nil {
		m.failJob(ctx, applier.Repo, &job, "commit", err)
	}
}

func verifyDesiredWithRetry(ctx context.Context, mutation PolicyMutation, repository Repository, desired []DesiredObject) ([]PlanOperation, []PlanIssue, error) {
	const maxAttempts = 5
	var remaining []PlanOperation
	var blockers []PlanIssue
	for attempt := 0; attempt < maxAttempts; attempt++ {
		actual, _, err := ScanManaged(ctx, mutation, repository, desired)
		if err != nil {
			return nil, nil, err
		}
		remaining, blockers = DiffDesired(desired, actual)
		if len(remaining) == 0 || len(blockers) > 0 || attempt == maxAttempts-1 {
			return remaining, blockers, nil
		}
		delay := time.Duration(attempt+1) * time.Second
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, nil, ctx.Err()
		case <-timer.C:
		}
	}
	return remaining, blockers, nil
}

func activateDesiredObjects(ctx context.Context, applier *Applier, desired []DesiredObject) error {
	actual, _, err := ScanManaged(ctx, applier.Mutation, applier.Repo, desired)
	if err != nil {
		return fmt.Errorf("scan RouterOS before activation: %w", err)
	}
	desiredByKey := make(map[string]DesiredObject, len(desired))
	activeKeys := make(map[string]struct{})
	for _, object := range desired {
		key := object.Menu + "\x00" + object.LogicalID
		desiredByKey[key] = object
		if strings.EqualFold(strings.TrimSpace(object.Fields["disabled"]), "no") {
			activeKeys[key] = struct{}{}
		}
	}
	idsByMenu := make(map[routeros.MutationMenu][]string)
	seen := make(map[string]bool, len(activeKeys))
	for _, object := range actual {
		if object.Ownership != "owned" {
			continue
		}
		key := object.Menu + "\x00" + object.LogicalID
		if _, ok := activeKeys[key]; !ok {
			continue
		}
		if seen[key] {
			return fmt.Errorf("duplicate managed object %s during activation", object.LogicalID)
		}
		seen[key] = true
		if object.RouterID == "" {
			return fmt.Errorf("managed object %s has no RouterOS ID during activation", object.LogicalID)
		}
		disabled, declared := object.Fields["disabled"]
		if !declared || strings.TrimSpace(disabled) == "" {
			continue
		}
		isDisabled, parseErr := routeros.ParseRouterOSBool(disabled)
		if parseErr != nil {
			return fmt.Errorf("parse disabled state for %s: %w", object.LogicalID, parseErr)
		}
		if isDisabled {
			menu := routeros.MutationMenu(object.Menu)
			idsByMenu[menu] = append(idsByMenu[menu], object.RouterID)
		}
	}
	for key, object := range desiredByKey {
		if _, active := activeKeys[key]; active && !seen[key] {
			return fmt.Errorf("managed object %s is missing during activation", object.LogicalID)
		}
	}
	menus := make([]routeros.MutationMenu, 0, len(idsByMenu))
	for menu := range idsByMenu {
		menus = append(menus, menu)
		sort.Strings(idsByMenu[menu])
	}
	sort.Slice(menus, func(i, j int) bool {
		left, right := activationMenuRank(menus[i]), activationMenuRank(menus[j])
		if left != right {
			return left < right
		}
		return menus[i] < menus[j]
	})
	batchMutation, hasBatchMutation := applier.Mutation.(PolicyBatchMutation)
	for _, menu := range menus {
		ids := idsByMenu[menu]
		if hasBatchMutation {
			if err := batchMutation.SetDisabledBatch(ctx, menu, ids, false); err != nil {
				return err
			}
			continue
		}
		for _, id := range ids {
			if _, err := applier.Mutation.Patch(ctx, menu, id, routeros.RouterOSFields{"disabled": false}); err != nil {
				return fmt.Errorf("enable %s %s: %w", menu, id, err)
			}
		}
	}
	return nil
}

func activationMenuRank(menu routeros.MutationMenu) int {
	switch menu {
	case routeros.MenuIPRoute, routeros.MenuIPv6Route, routeros.MenuRoutingRule:
		return 1
	case routeros.MenuIPFirewallAddressList, routeros.MenuIPv6FirewallAddressList:
		return 2
	case routeros.MenuIPFirewallFilter, routeros.MenuIPv6FirewallFilter:
		return 3
	case routeros.MenuIPFirewallMangle, routeros.MenuIPv6FirewallMangle:
		return 4
	case routeros.MenuIPFirewallNAT, routeros.MenuIPv6FirewallNAT:
		return 5
	case routeros.MenuIPDNSStatic:
		return 6
	default:
		return 7
	}
}

func isEnableOnlyPatch(operation PlanOperation) bool {
	return operation.Action == "patch" && len(operation.After) == 1 && strings.EqualFold(strings.TrimSpace(operation.After["disabled"]), "no")
}

func isDisableOnlyPatch(operation PlanOperation) bool {
	return operation.Action == "patch" && len(operation.After) == 1 && strings.EqualFold(strings.TrimSpace(operation.After["disabled"]), "yes")
}

func operationFields(operation PlanOperation, staging bool) routeros.RouterOSFields {
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
	if staging && (operation.Action == "create" || operation.Action == "patch") && strings.EqualFold(strings.TrimSpace(operation.After["disabled"]), "no") {
		fields["disabled"] = true
	}
	return fields
}

func applyStagedOperation(ctx context.Context, mutation PolicyMutation, operation PlanOperation, createdRouterIDs map[string]string) error {
	return applyOperationWithFields(ctx, mutation, operation, createdRouterIDs, operationFields(operation, true))
}

func applyOperation(ctx context.Context, mutation PolicyMutation, operation PlanOperation, createdRouterIDs map[string]string) error {
	return applyOperationWithFields(ctx, mutation, operation, createdRouterIDs, operationFields(operation, false))
}

func applyOperationWithFields(ctx context.Context, mutation PolicyMutation, operation PlanOperation, createdRouterIDs map[string]string, fields routeros.RouterOSFields) error {
	menu := routeros.MutationMenu(operation.Menu)
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

func (m *Manager) failJob(_ context.Context, repository Repository, job *ApplyJob, target string, cause error) {
	job.State = "failed"
	job.Phase = "failed"
	job.Error = target + ": " + cause.Error()
	job.FinishedAt = time.Now().UTC()
	saveContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := repository.SaveApplyJob(saveContext, *job); err != nil {
		m.logApplyError(repository.DeviceID(), job.ID, err)
	}
}

func (m *Manager) logApplyError(deviceID, jobID string, err error) {
	if m != nil && m.logger != nil {
		m.logger.Printf("policy v2 device=%s job=%s: %v", deviceID, jobID, err)
	}
}

func isAccessOnlyPlanKind(kind string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(kind)), "access-")
}

func cloneInternetEgresses(values map[string][]string) map[string][]string {
	if values == nil {
		return nil
	}
	result := make(map[string][]string, len(values))
	for family, interfaces := range values {
		result[family] = append([]string(nil), interfaces...)
	}
	return result
}

func hasDesiredMenu(desired []DesiredObject, menu routeros.MutationMenu) bool {
	for _, object := range desired {
		if object.Menu == string(menu) {
			return true
		}
	}
	return false
}

func isAccessPlanOperation(operation PlanOperation, accessSourceIDs []string) bool {
	logicalID := operation.LogicalID
	if logicalID == "access-forwarder" ||
		strings.HasPrefix(logicalID, "access:") ||
		strings.HasPrefix(logicalID, "access-member:") ||
		strings.HasPrefix(logicalID, "access-internet-egress:") ||
		strings.HasPrefix(logicalID, "access-local:") ||
		strings.HasPrefix(logicalID, "dns:access:") {
		return true
	}
	if !strings.HasPrefix(logicalID, "source-addr:") {
		return false
	}
	parts := strings.SplitN(logicalID, ":", 3)
	if len(parts) != 3 {
		return false
	}
	for _, sourceID := range accessSourceIDs {
		if sourceID == parts[1] {
			return true
		}
	}
	return false
}

func ensureAccessJumpsFirst(ctx context.Context, mutation PolicyMutation, desired []DesiredObject) error {
	for _, menu := range []routeros.MutationMenu{routeros.MenuIPFirewallFilter, routeros.MenuIPv6FirewallFilter} {
		identities := make([]string, 0)
		for _, object := range desired {
			if object.Menu != string(menu) || !isAccessFilterFields(object.Menu, object.Fields) {
				continue
			}
			identities = append(identities, managedCommentIdentity(object.Fields["comment"]))
		}
		if len(identities) == 0 {
			continue
		}
		objects, err := mutation.List(ctx, menu, routeros.MutationQuery{})
		if err != nil {
			return fmt.Errorf("read %s before access-control ordering: %w", menu, err)
		}
		order := make([]string, 0, len(objects))
		idByIdentity := make(map[string]string, len(identities))
		for _, object := range objects {
			id := object.ID()
			if id == "" {
				continue
			}
			order = append(order, id)
			identity := managedCommentIdentity(object["comment"])
			for _, desiredIdentity := range identities {
				if identity == desiredIdentity {
					if idByIdentity[identity] != "" {
						return fmt.Errorf("duplicate managed access-control rule %s", identity)
					}
					idByIdentity[identity] = id
				}
			}
		}
		if len(order) == 0 {
			return fmt.Errorf("%s has no rules after access-control apply", menu)
		}
		correct := len(order) >= len(identities)
		for index, identity := range identities {
			if !correct || managedCommentIdentity(objects[index]["comment"]) != identity {
				correct = false
				break
			}
		}
		if correct {
			continue
		}
		for index := len(identities) - 1; index >= 0; index-- {
			sourceID := idByIdentity[identities[index]]
			if sourceID == "" {
				return fmt.Errorf("managed access-control jump %s is missing", identities[index])
			}
			if order[0] == sourceID {
				continue
			}
			anchorID := order[0]
			if _, err := mutation.Move(ctx, menu, routeros.MoveRequest{ID: sourceID, BeforeID: anchorID}); err != nil {
				return fmt.Errorf("move access-control rule %s to top of %s: %w", identities[index], menu, err)
			}
			order = moveRouterIDBefore(order, sourceID, anchorID)
		}
		objects, err = mutation.List(ctx, menu, routeros.MutationQuery{})
		if err != nil {
			return fmt.Errorf("verify %s access-control ordering: %w", menu, err)
		}
		if len(objects) < len(identities) {
			return fmt.Errorf("%s returned fewer rules than managed access-control rules", menu)
		}
		for index, identity := range identities {
			if managedCommentIdentity(objects[index]["comment"]) != identity {
				return fmt.Errorf("managed access-control rule %s is not at position %d of %s", identity, index, menu)
			}
		}
	}
	return nil
}

func planAccessJumpsFirst(ctx context.Context, mutation PolicyMutation, desired []DesiredObject, scanned ...[]ActualObject) ([]PlanOperation, error) {
	result := make([]PlanOperation, 0)
	for _, menu := range []routeros.MutationMenu{routeros.MenuIPFirewallFilter, routeros.MenuIPv6FirewallFilter} {
		jumps := make([]DesiredObject, 0)
		desiredByIdentity := make(map[string]DesiredObject)
		desiredIdentityByLogicalID := make(map[string]string)
		for _, object := range desired {
			if object.Menu != string(menu) || !isAccessFilterFields(object.Menu, object.Fields) {
				continue
			}
			identity := managedCommentIdentity(object.Fields["comment"])
			jumps = append(jumps, object)
			desiredByIdentity[identity] = object
			desiredIdentityByLogicalID[object.LogicalID] = identity
		}
		if len(jumps) == 0 {
			continue
		}
		// ScanManaged may have matched an old access comment to the desired
		// logical ID. Carry that match into this second live listing so a
		// legacy jump that needs reordering is moved by its real RouterOS ID
		// instead of being mistaken for a newly-created object.
		routerIDToDesiredIdentity := make(map[string]string)
		if len(scanned) > 0 {
			for _, object := range scanned[0] {
				if object.Menu != string(menu) || object.Ownership == "foreign" || object.RouterID == "" {
					continue
				}
				if identity := desiredIdentityByLogicalID[object.LogicalID]; identity != "" {
					routerIDToDesiredIdentity[object.RouterID] = identity
				}
			}
		}
		objects, err := mutation.List(ctx, menu, routeros.MutationQuery{})
		if err != nil {
			return nil, fmt.Errorf("read %s before planning access-control ordering: %w", menu, err)
		}
		routerIDByIdentity := make(map[string]string, len(jumps))
		seenDesiredJump := make(map[string]bool, len(jumps))
		firstJumpIndex := -1
		boundaryIndex := -1
		boundaryID := ""
		ambiguous := false
		for index, object := range objects {
			identity := managedCommentIdentity(object["comment"])
			if desiredIdentity := routerIDToDesiredIdentity[object.ID()]; desiredIdentity != "" {
				identity = desiredIdentity
			}
			if _, desiredJump := desiredByIdentity[identity]; desiredJump {
				if seenDesiredJump[identity] {
					ambiguous = true
					break
				}
				seenDesiredJump[identity] = true
				routerIDByIdentity[identity] = object.ID()
				if firstJumpIndex == -1 {
					firstJumpIndex = index
				}
				continue
			}
			if boundaryIndex == -1 {
				boundaryIndex = index
				boundaryID = object.ID()
			}
		}
		// Missing jumps will be created by the ordinary diff before the final
		// ordering verification. Do not emit a move with an empty RouterOS ID;
		// the final pass can place the newly-created block at the top safely.
		missingJump := firstJumpIndex == -1
		if !missingJump {
			for _, jump := range jumps {
				identity := managedCommentIdentity(jump.Fields["comment"])
				if routerIDByIdentity[identity] == "" {
					missingJump = true
					break
				}
			}
		}
		if ambiguous || missingJump || boundaryIndex == -1 || boundaryID == "" || (firstJumpIndex >= 0 && boundaryIndex > firstJumpIndex) {
			continue
		}

		anchor := &PlanAnchor{RouterID: boundaryID, Relation: "before", Menu: string(menu)}
		for index := len(jumps) - 1; index >= 0; index-- {
			jump := jumps[index]
			identity := managedCommentIdentity(jump.Fields["comment"])
			result = append(result, PlanOperation{
				Order: -jump.Order, Phase: jump.Phase, Action: "move", Menu: jump.Menu,
				LogicalID: jump.LogicalID, RouterID: routerIDByIdentity[identity], Ownership: "owned",
				Anchor: anchor,
			})
			anchor = &PlanAnchor{LogicalID: jump.LogicalID, RouterID: routerIDByIdentity[identity], Relation: "before", Menu: string(menu)}
		}
	}
	return result, nil
}

func moveRouterIDBefore(values []string, source, target string) []string {
	withoutSource := make([]string, 0, len(values))
	for _, value := range values {
		if value != source {
			withoutSource = append(withoutSource, value)
		}
	}
	for index, value := range withoutSource {
		if value == target {
			result := append([]string(nil), withoutSource[:index]...)
			result = append(result, source)
			return append(result, withoutSource[index:]...)
		}
	}
	return values
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
