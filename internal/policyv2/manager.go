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
	"rosboard/internal/ownership"
	"rosboard/internal/routeros"
)

// applicationListPrefix is retained only so reconciliation can recognize and
// retire the pre-Slice-3 OAF access projection. Canonical desired state never
// creates this identity.
const applicationListPrefix = "rb_app_"

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
	Proposal         *PolicyProposal
	AccessProposal   *AccessProposal
	Domain           PolicyDomain
	// TargetListIDs limits pending-version promotion and content selection to
	// the target mutation that triggered a normal target-list reconcile.
	TargetListIDs []string
}

type SourceRefresher func(context.Context, Source) (SourceRefresh, error)

type Manager struct {
	logger        *log.Logger
	mu            sync.RWMutex
	appliers      map[string]*Applier
	plans         map[string]cachedPlan
	followUpPlans map[string][]string
	gate          *routeros.DeviceWriteGate
}

func NewManager(logger *log.Logger) *Manager {
	return &Manager{logger: logger, appliers: make(map[string]*Applier), plans: make(map[string]cachedPlan), followUpPlans: make(map[string][]string), gate: routeros.NewDeviceWriteGate()}
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
	ErrPlanNotFound            = errors.New("policy plan not found")
	ErrPlanExpired             = errors.New("policy plan expired")
	ErrPlanStale               = errors.New("policy plan is stale")
	ErrPlanBlocked             = errors.New("policy plan is blocked")
	ErrDeviceBusy              = errors.New("policy device already has an active apply")
	ErrTargetListSplitRequired = errors.New("target list is consumed by both policy domains and requires separate applies")
)

func (m *Manager) GeneratePlan(ctx context.Context, deviceID, kind string) (PlanEnvelope, error) {
	return m.GeneratePlanWithOptions(ctx, deviceID, kind, PlanOptions{})
}

func (m *Manager) GeneratePlanWithOptions(ctx context.Context, deviceID, kind string, options PlanOptions) (PlanEnvelope, error) {
	applier := m.ApplierFor(deviceID)
	if applier == nil {
		return PlanEnvelope{}, errors.New("policy runtime is unavailable")
	}
	domain := options.Domain
	var err error
	if domain == "" {
		domain, err = resolvePolicyDomain(ctx, applier, kind)
	}
	if err != nil {
		return PlanEnvelope{}, err
	}
	accessOnlyPlan := domain == PolicyDomainAccess
	planningRepository := applier.Repo
	baseDesiredRevision := int64(-1)
	proposalHash := ""
	proposal := options.Proposal
	accessProposal := options.AccessProposal
	if proposal != nil && accessProposal != nil {
		return PlanEnvelope{}, errors.New("routing and access proposals cannot be combined")
	}
	if proposal != nil {
		// A proposal is the routing-policy bundle. Keep it out of the legacy
		// combined compatibility path even when an older caller uses a generic
		// plan kind.
		domain = PolicyDomainRouting
		proposal = clonePolicyProposal(proposal)
		if proposal == nil {
			return PlanEnvelope{}, errors.New("policy proposal could not be cloned")
		}
		if accessOnlyPlan || proposal.Empty() {
			return PlanEnvelope{}, errors.New("a policy proposal is not valid for this plan")
		}
		state, err := applier.Repo.GetDeviceState(ctx)
		if err != nil {
			return PlanEnvelope{}, err
		}
		baseDesiredRevision = state.DesiredRevision
		if err := CaptureProposalDependencies(ctx, applier.Repo, proposal); err != nil {
			return PlanEnvelope{}, err
		}
		planningRepository, err = newProposalRepository(ctx, applier.Repo, *proposal)
		if err != nil {
			return PlanEnvelope{}, err
		}
		proposalHash, err = ProposalHash(*proposal)
		if err != nil {
			return PlanEnvelope{}, err
		}
	} else if accessProposal != nil {
		domain = PolicyDomainAccess
		accessProposal = cloneAccessProposal(accessProposal)
		if accessProposal == nil || accessProposal.Empty() {
			return PlanEnvelope{}, errors.New("an access proposal is not valid for this plan")
		}
		if applier.Access == nil {
			return PlanEnvelope{}, errors.New("access repository is unavailable")
		}
		if migrator, ok := applier.Access.(CanonicalAccessMigrator); ok {
			if err := migrator.EnsureCanonicalAccessMigrated(ctx); err != nil {
				return PlanEnvelope{}, err
			}
		}
		state, err := applier.Access.GetState(ctx)
		if err != nil {
			return PlanEnvelope{}, err
		}
		baseDesiredRevision = state.DesiredRevision
		if err := CaptureAccessProposalDependencies(ctx, applier.Repo, accessProposal); err != nil {
			return PlanEnvelope{}, err
		}
		planningRepository, err = newProposalRepository(ctx, applier.Repo, PolicyProposal{TargetLists: accessProposal.TargetLists})
		if err != nil {
			return PlanEnvelope{}, err
		}
		proposalHash, err = AccessProposalHash(*accessProposal)
		if err != nil {
			return PlanEnvelope{}, err
		}
	}
	planningAccessRepository := applier.Access
	if accessProposal != nil {
		planningAccessRepository, err = newAccessProposalRepository(ctx, applier.Access, *accessProposal)
		if err != nil {
			return PlanEnvelope{}, err
		}
	}
	targetScope := targetScopeMap(options.TargetListIDs)
	desired, err := buildPlanDesiredWithAccessRepository(ctx, applier, planningRepository, planningAccessRepository, domain, options.InternetEgresses, targetScope)
	if err != nil {
		return PlanEnvelope{}, err
	}
	if domain == PolicyDomainRouting || domain == PolicyDomainCombined {
		if err := appendRoutingValidation(ctx, applier.Reader, planningRepository, &desired); err != nil {
			return PlanEnvelope{}, err
		}
	}
	actual, fingerprint, err := ScanManagedForDomain(ctx, applier.Mutation, applier.Repo, desired.Objects, domain)
	if err != nil {
		return PlanEnvelope{}, err
	}
	operations, diffBlockers := DiffDesired(desired.Objects, actual)
	if domain == PolicyDomainAccess {
		accessOrderOperations, orderErr := planAccessJumpsFirst(ctx, applier.Mutation, desired.Objects, actual)
		if orderErr != nil {
			return PlanEnvelope{}, orderErr
		}
		operations = append(operations, accessOrderOperations...)
	}
	sortPlanOperations(operations)
	if len(desired.Blockers) == 0 && len(operations) > 0 {
		preflightRelease, acquired := m.gate.TryAcquire(deviceID)
		if !acquired {
			return PlanEnvelope{}, ErrDeviceBusy
		}
		capabilityBlockers, capabilityErr := accessCapabilityBlockers(ctx, applier.Mutation, desired.Objects)
		preflightRelease()
		if capabilityErr != nil {
			return PlanEnvelope{}, capabilityErr
		}
		desired.Blockers = append(desired.Blockers, capabilityBlockers...)
	}
	blockers := append(append([]PlanIssue{}, desired.Blockers...), diffBlockers...)
	now := time.Now().UTC()
	planDesiredRevision := desired.Revision
	if domain == PolicyDomainAccess {
		planDesiredRevision = desired.AccessRevision
	}
	if proposal != nil {
		// CommitPolicyProposal bumps the canonical revision once for the whole
		// bundle. The plan exposes the post-approval revision while the cache
		// retains the pre-approval revision for the stale check.
		planDesiredRevision = baseDesiredRevision + 1
	}
	if accessProposal != nil {
		planDesiredRevision = baseDesiredRevision + 1
	}
	plan := Plan{
		PlanID: uuid.NewString(), DeviceID: deviceID, Kind: firstNonEmptyString(kind, "structural"), Domain: domain, Lifecycle: "interactive",
		CreatedAt: now, ExpiresAt: now.Add(15 * time.Minute), BaseDesiredRevision: baseDesiredRevision, DesiredRevision: planDesiredRevision, AccessRevision: desired.AccessRevision, AccessResolutionCount: len(desired.AccessResolutions), DesiredHash: desired.Hash, ProposalHash: proposalHash,
		InternetEgressCandidates: desired.InternetEgressCandidates,
		TargetPromotions:         append([]TargetVersionPromotion(nil), desired.TargetPromotions...),
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
		ProposalHash      string
		ActualFingerprint string
		AccessRevision    int64
		Domain            PolicyDomain
		Operations        []PlanOperation
	}{deviceID, planDesiredRevision, desired.Hash, proposalHash, fingerprint, desired.AccessRevision, domain, operations})
	if err != nil {
		return PlanEnvelope{}, err
	}
	plan.PlanHash = shortHash(string(hashPayload), 64)
	m.mu.Lock()
	m.plans[plan.PlanID] = cachedPlan{Plan: plan, Desired: desired.Objects, Actual: actual, AccessResolutions: desired.AccessResolutions, InternetEgresses: cloneInternetEgresses(options.InternetEgresses), Proposal: clonePolicyProposal(proposal), AccessProposal: cloneAccessProposal(accessProposal), BaseDesiredRevision: baseDesiredRevision, TargetPromotions: append([]TargetVersionPromotion(nil), desired.TargetPromotions...), TargetScope: cloneTargetScope(targetScope)}
	for id, cached := range m.plans {
		if now.After(cached.Plan.ExpiresAt) {
			delete(m.plans, id)
		}
	}
	m.mu.Unlock()
	return PlanEnvelope{Plan: plan, PlanID: plan.PlanID, PlanHash: plan.PlanHash}, nil
}

func policyDomainForPlanKind(kind string) PolicyDomain {
	if isAccessOnlyPlanKind(kind) {
		return PolicyDomainAccess
	}
	switch {
	case strings.HasPrefix(strings.ToLower(strings.TrimSpace(kind)), "routing-"),
		strings.HasPrefix(strings.ToLower(strings.TrimSpace(kind)), "source-"),
		strings.HasPrefix(strings.ToLower(strings.TrimSpace(kind)), "egress-"),
		strings.HasPrefix(strings.ToLower(strings.TrimSpace(kind)), "target-list-"):
		return PolicyDomainRouting
	default:
		// Untyped legacy jobs are routing jobs. Combined remains available to
		// direct compatibility builders, but is never inferred by the manager.
		return PolicyDomainRouting
	}
}

func targetScopeMap(targetIDs []string) map[string]bool {
	if len(targetIDs) == 0 {
		return nil
	}
	result := make(map[string]bool, len(targetIDs))
	for _, targetID := range targetIDs {
		targetID = strings.TrimSpace(targetID)
		if targetID != "" {
			result[targetID] = true
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func cloneTargetScope(scope map[string]bool) map[string]bool {
	if scope == nil {
		return nil
	}
	result := make(map[string]bool, len(scope))
	for targetID, selected := range scope {
		result[targetID] = selected
	}
	return result
}

func resolvePolicyDomain(ctx context.Context, applier *Applier, kind string) (PolicyDomain, error) {
	domain := policyDomainForPlanKind(kind)
	if applier == nil || applier.Access == nil || domain != PolicyDomainRouting {
		return domain, nil
	}
	kindLower := strings.ToLower(strings.TrimSpace(kind))
	if !strings.HasPrefix(kindLower, "source-") && !strings.HasPrefix(kindLower, "target-list-") {
		return domain, nil
	}
	targetID := planTargetID(kind)
	if targetID == "" {
		// Normal target mutations carry the affected target ID and use the
		// explicit target-domain helpers. An unqualified legacy source kind is
		// routing-compatible only; it must never inspect unrelated targets to
		// choose a domain.
		return domain, nil
	}
	consumerRepository, ok := applier.Repo.(TargetConsumerDomainRepository)
	if !ok {
		return domain, nil
	}
	consumers, err := consumerRepository.TargetConsumerDomains(ctx, targetID)
	if err != nil {
		return "", err
	}
	switch {
	case consumers.Access && consumers.Routing:
		return "", ErrTargetListSplitRequired
	case consumers.Access:
		return PolicyDomainAccess, nil
	default:
		return PolicyDomainRouting, nil
	}
}

func planTargetID(kind string) string {
	kind = strings.TrimSpace(kind)
	lower := strings.ToLower(kind)
	if !strings.HasPrefix(lower, "source-") && !strings.HasPrefix(lower, "target-list-") {
		return ""
	}
	separator := strings.IndexByte(kind, ':')
	if separator < 0 {
		return ""
	}
	return strings.TrimSpace(kind[separator+1:])
}

func buildPlanDesired(ctx context.Context, applier *Applier, repository Repository, domain PolicyDomain, internetEgresses map[string][]string) (DesiredResult, error) {
	return buildPlanDesiredWithAccessRepository(ctx, applier, repository, applier.Access, domain, internetEgresses, nil)
}

func buildPlanDesiredWithAccessRepository(ctx context.Context, applier *Applier, repository Repository, accessRepository accesscontrol.Repository, domain PolicyDomain, internetEgresses map[string][]string, targetScope map[string]bool) (DesiredResult, error) {
	var desired DesiredResult
	var err error
	if domain == PolicyDomainAccess {
		desired, err = buildDesiredForDomainWithTargetScope(ctx, repository, applier.Reader, accessRepository, applier.Terminals, applier.Scope, internetEgresses, PolicyDomainAccess, targetScope)
	} else if domain == PolicyDomainCombined {
		desired, err = buildDesiredForDomainWithTargetScope(ctx, repository, applier.Reader, accessRepository, applier.Terminals, applier.Scope, internetEgresses, PolicyDomainCombined, targetScope)
	} else {
		desired, err = buildDesiredForDomainWithTargetScope(ctx, repository, applier.Reader, accessRepository, applier.Terminals, applier.Scope, internetEgresses, PolicyDomainRouting, targetScope)
	}
	if err != nil {
		return DesiredResult{}, err
	}
	if err := appendCrossDomainProjectionBlockers(ctx, repository, accessRepository, targetScope, &desired); err != nil {
		return DesiredResult{}, err
	}
	return desired, nil
}

func appendRoutingValidation(ctx context.Context, reader PolicyReader, repository Repository, desired *DesiredResult) error {
	aliasBlockers, err := ValidateFakeAliases(ctx, reader, repository)
	if err != nil {
		return err
	}
	desired.Blockers = append(desired.Blockers, aliasBlockers...)
	tableBlockers, tableWarnings, err := ValidateRouteTables(ctx, reader, repository)
	if err != nil {
		return err
	}
	desired.Blockers = append(desired.Blockers, tableBlockers...)
	desired.Warnings = append(desired.Warnings, tableWarnings...)
	ingressBlockers, err := ValidateTrafficIngress(ctx, reader, repository)
	if err != nil {
		return err
	}
	desired.Blockers = append(desired.Blockers, ingressBlockers...)
	return nil
}

func desiredMatchesPlan(desired DesiredResult, plan Plan, domain PolicyDomain) bool {
	if desired.Hash != plan.DesiredHash {
		return false
	}
	if domain == PolicyDomainAccess {
		return desired.AccessRevision == plan.AccessRevision && desired.AccessRevision == plan.DesiredRevision
	}
	if domain == PolicyDomainCombined {
		return desired.Revision == plan.DesiredRevision && desired.AccessRevision == plan.AccessRevision
	}
	return desired.Revision == plan.DesiredRevision
}

func (m *Manager) ApplyPlan(ctx context.Context, deviceID, planID string) (ApplyJob, error) {
	return m.applyPlanWithHash(ctx, deviceID, planID, "")
}

func (m *Manager) ApplyPlanWithHash(ctx context.Context, deviceID, planID, planHash string) (ApplyJob, error) {
	return m.applyPlanWithHash(ctx, deviceID, planID, planHash)
}

func (m *Manager) applyPlanWithHash(ctx context.Context, deviceID, planID, planHash string) (ApplyJob, error) {
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
	if (cached.Proposal != nil || cached.AccessProposal != nil) && strings.TrimSpace(planHash) == "" {
		return ApplyJob{}, ErrPlanStale
	}
	if planHash != "" && planHash != cached.Plan.PlanHash {
		return ApplyJob{}, ErrPlanStale
	}
	if time.Now().After(cached.Plan.ExpiresAt) {
		return ApplyJob{}, ErrPlanExpired
	}
	if len(cached.Plan.Blockers) > 0 {
		return ApplyJob{}, ErrPlanBlocked
	}
	if cached.Proposal != nil {
		return m.applyProposedPlan(ctx, deviceID, planID, applier, cached)
	}
	if cached.AccessProposal != nil {
		return m.applyAccessProposedPlan(ctx, deviceID, planID, applier, cached)
	}
	domain := cached.Plan.Domain
	if domain == "" {
		domain = policyDomainForPlanKind(cached.Plan.Kind)
	}
	desired, err := buildPlanDesiredWithAccessRepository(ctx, applier, applier.Repo, applier.Access, domain, cached.InternetEgresses, cached.TargetScope)
	if err != nil {
		return ApplyJob{}, err
	}
	if !desiredMatchesPlan(desired, cached.Plan, domain) {
		return ApplyJob{}, ErrPlanStale
	}
	if len(desired.Blockers) > 0 {
		return ApplyJob{}, ErrPlanStale
	}
	if domain == PolicyDomainRouting || domain == PolicyDomainCombined {
		validation := DesiredResult{}
		if err := appendRoutingValidation(ctx, applier.Reader, applier.Repo, &validation); err != nil {
			return ApplyJob{}, err
		}
		if len(validation.Blockers) > 0 {
			return ApplyJob{}, ErrPlanStale
		}
	}
	release, acquired := m.gate.TryAcquire(deviceID)
	if !acquired {
		return ApplyJob{}, ErrDeviceBusy
	}
	// The desired graph was built before acquiring the RouterOS gate. Desired
	// writes do not use that gate, so take one final snapshot while this apply
	// owns the device slot; otherwise a save that lands while ApplyPlan waits
	// could still stage the old graph before CommitApply rejects it.
	latestDesired, latestErr := buildPlanDesiredWithAccessRepository(ctx, applier, applier.Repo, applier.Access, domain, cached.InternetEgresses, cached.TargetScope)
	if latestErr != nil {
		release()
		return ApplyJob{}, latestErr
	}
	if !desiredMatchesPlan(latestDesired, cached.Plan, domain) || len(latestDesired.Blockers) > 0 {
		release()
		return ApplyJob{}, ErrPlanStale
	}
	desired = latestDesired
	if domain == PolicyDomainRouting || domain == PolicyDomainCombined {
		validation := DesiredResult{}
		if err := appendRoutingValidation(ctx, applier.Reader, applier.Repo, &validation); err != nil {
			release()
			return ApplyJob{}, err
		}
		if len(validation.Blockers) > 0 {
			release()
			return ApplyJob{}, ErrPlanStale
		}
	}
	if len(cached.Plan.Operations) > 0 {
		if capabilityBlockers, capabilityErr := accessCapabilityBlockers(ctx, applier.Mutation, desired.Objects); capabilityErr != nil {
			release()
			return ApplyJob{}, capabilityErr
		} else if len(capabilityBlockers) > 0 {
			release()
			return ApplyJob{}, ErrPlanStale
		}
	}
	_, fingerprint, err := ScanManagedForDomain(ctx, applier.Mutation, applier.Repo, desired.Objects, domain)
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

func (m *Manager) applyProposedPlan(ctx context.Context, deviceID, planID string, applier *Applier, cached cachedPlan) (ApplyJob, error) {
	proposal := cached.Proposal
	if proposal == nil || cached.BaseDesiredRevision < 0 {
		return ApplyJob{}, ErrPlanStale
	}
	proposalHash, err := ProposalHash(*proposal)
	if err != nil {
		return ApplyJob{}, err
	}
	if proposalHash != cached.Plan.ProposalHash {
		return ApplyJob{}, ErrPlanStale
	}
	baseState, err := applier.Repo.GetDeviceState(ctx)
	if err != nil {
		return ApplyJob{}, err
	}
	if baseState.DesiredRevision != cached.BaseDesiredRevision {
		return ApplyJob{}, ErrPlanStale
	}
	if err := ValidateProposalDependencies(ctx, applier.Repo, *proposal); err != nil {
		return ApplyJob{}, ErrPlanStale
	}
	planningRepository, err := newProposalRepository(ctx, applier.Repo, *proposal)
	if err != nil {
		return ApplyJob{}, err
	}
	desired, err := BuildRoutingDesired(ctx, planningRepository, applier.Reader, applier.Terminals)
	if err != nil {
		return ApplyJob{}, err
	}
	if desired.Revision != cached.BaseDesiredRevision || desired.Hash != cached.Plan.DesiredHash || len(desired.Blockers) > 0 {
		return ApplyJob{}, ErrPlanStale
	}
	if err := validatePlanRepository(ctx, applier, planningRepository, PolicyDomainRouting); err != nil {
		return ApplyJob{}, err
	}
	release, acquired := m.gate.TryAcquire(deviceID)
	if !acquired {
		return ApplyJob{}, ErrDeviceBusy
	}
	latestState, err := applier.Repo.GetDeviceState(ctx)
	if err != nil {
		release()
		return ApplyJob{}, err
	}
	if latestState.DesiredRevision != cached.BaseDesiredRevision {
		release()
		return ApplyJob{}, ErrPlanStale
	}
	if err := ValidateProposalDependencies(ctx, applier.Repo, *proposal); err != nil {
		release()
		return ApplyJob{}, ErrPlanStale
	}
	// Rebuild the overlay after acquiring the device slot. The repository
	// commit below repeats this same base-revision check in its transaction for
	// writes such as source refreshes that do not use the RouterOS gate.
	planningRepository, err = newProposalRepository(ctx, applier.Repo, *proposal)
	if err != nil {
		release()
		return ApplyJob{}, err
	}
	desired, err = BuildRoutingDesired(ctx, planningRepository, applier.Reader, applier.Terminals)
	if err != nil {
		release()
		return ApplyJob{}, err
	}
	if desired.Revision != cached.BaseDesiredRevision || desired.Hash != cached.Plan.DesiredHash || len(desired.Blockers) > 0 {
		release()
		return ApplyJob{}, ErrPlanStale
	}
	if err := validatePlanRepository(ctx, applier, planningRepository, PolicyDomainRouting); err != nil {
		release()
		return ApplyJob{}, err
	}
	if len(cached.Plan.Operations) > 0 {
		if capabilityBlockers, capabilityErr := accessCapabilityBlockers(ctx, applier.Mutation, desired.Objects); capabilityErr != nil {
			release()
			return ApplyJob{}, capabilityErr
		} else if len(capabilityBlockers) > 0 {
			release()
			return ApplyJob{}, ErrPlanStale
		}
	}
	_, fingerprint, err := ScanManagedForDomain(ctx, applier.Mutation, applier.Repo, desired.Objects, PolicyDomainRouting)
	if err != nil {
		release()
		return ApplyJob{}, err
	}
	if fingerprint != cached.Plan.ActualFingerprint {
		release()
		return ApplyJob{}, ErrPlanStale
	}
	committer, ok := applier.Repo.(ProposalCommitter)
	if !ok {
		release()
		return ApplyJob{}, errors.New("policy repository does not support atomic proposal commit")
	}
	committedRevision, err := committer.CommitPolicyProposal(ctx, *proposal, cached.BaseDesiredRevision)
	if errors.Is(err, ErrRevisionStale) {
		release()
		return ApplyJob{}, ErrPlanStale
	}
	if err != nil {
		release()
		return ApplyJob{}, err
	}
	if committedRevision != cached.Plan.DesiredRevision {
		release()
		return ApplyJob{}, ErrPlanStale
	}
	committedDesired, err := BuildRoutingDesired(ctx, applier.Repo, applier.Reader, applier.Terminals)
	if err != nil {
		release()
		return ApplyJob{}, err
	}
	if committedDesired.Revision != committedRevision || committedDesired.Hash != cached.Plan.DesiredHash || len(committedDesired.Blockers) > 0 {
		release()
		return ApplyJob{}, ErrPlanStale
	}
	cached.Proposal = nil
	cached.BaseDesiredRevision = committedRevision
	cached.Desired = committedDesired.Objects
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

func (m *Manager) applyAccessProposedPlan(ctx context.Context, deviceID, planID string, applier *Applier, cached cachedPlan) (ApplyJob, error) {
	proposal := cached.AccessProposal
	if proposal == nil || cached.BaseDesiredRevision < 0 || applier.Access == nil {
		return ApplyJob{}, ErrPlanStale
	}
	proposalHash, err := AccessProposalHash(*proposal)
	if err != nil {
		return ApplyJob{}, err
	}
	if proposalHash != cached.Plan.ProposalHash {
		return ApplyJob{}, ErrPlanStale
	}
	state, err := applier.Access.GetState(ctx)
	if err != nil {
		return ApplyJob{}, err
	}
	if state.DesiredRevision != cached.BaseDesiredRevision {
		return ApplyJob{}, ErrPlanStale
	}
	if err := ValidateAccessProposalDependencies(ctx, applier.Repo, *proposal); err != nil {
		return ApplyJob{}, ErrPlanStale
	}
	planningRepository, err := newProposalRepository(ctx, applier.Repo, PolicyProposal{TargetLists: proposal.TargetLists})
	if err != nil {
		return ApplyJob{}, err
	}
	planningAccessRepository, err := newAccessProposalRepository(ctx, applier.Access, *proposal)
	if err != nil {
		return ApplyJob{}, err
	}
	desired, err := buildPlanDesiredWithAccessRepository(ctx, applier, planningRepository, planningAccessRepository, PolicyDomainAccess, cached.InternetEgresses, nil)
	if err != nil {
		return ApplyJob{}, err
	}
	if desired.AccessRevision != cached.BaseDesiredRevision || desired.Hash != cached.Plan.DesiredHash || len(desired.Blockers) > 0 {
		return ApplyJob{}, ErrPlanStale
	}
	release, acquired := m.gate.TryAcquire(deviceID)
	if !acquired {
		return ApplyJob{}, ErrDeviceBusy
	}
	latestState, err := applier.Access.GetState(ctx)
	if err != nil {
		release()
		return ApplyJob{}, err
	}
	if latestState.DesiredRevision != cached.BaseDesiredRevision {
		release()
		return ApplyJob{}, ErrPlanStale
	}
	if err := ValidateAccessProposalDependencies(ctx, applier.Repo, *proposal); err != nil {
		release()
		return ApplyJob{}, ErrPlanStale
	}
	planningRepository, err = newProposalRepository(ctx, applier.Repo, PolicyProposal{TargetLists: proposal.TargetLists})
	if err != nil {
		release()
		return ApplyJob{}, err
	}
	planningAccessRepository, err = newAccessProposalRepository(ctx, applier.Access, *proposal)
	if err != nil {
		release()
		return ApplyJob{}, err
	}
	desired, err = buildPlanDesiredWithAccessRepository(ctx, applier, planningRepository, planningAccessRepository, PolicyDomainAccess, cached.InternetEgresses, nil)
	if err != nil {
		release()
		return ApplyJob{}, err
	}
	if desired.AccessRevision != cached.BaseDesiredRevision || desired.Hash != cached.Plan.DesiredHash || len(desired.Blockers) > 0 {
		release()
		return ApplyJob{}, ErrPlanStale
	}
	if len(cached.Plan.Operations) > 0 {
		if capabilityBlockers, capabilityErr := accessCapabilityBlockers(ctx, applier.Mutation, desired.Objects); capabilityErr != nil {
			release()
			return ApplyJob{}, capabilityErr
		} else if len(capabilityBlockers) > 0 {
			release()
			return ApplyJob{}, ErrPlanStale
		}
	}
	_, fingerprint, err := ScanManagedForDomain(ctx, applier.Mutation, applier.Repo, desired.Objects, PolicyDomainAccess)
	if err != nil {
		release()
		return ApplyJob{}, err
	}
	if fingerprint != cached.Plan.ActualFingerprint {
		release()
		return ApplyJob{}, ErrPlanStale
	}
	committer, ok := applier.Repo.(AccessProposalCommitter)
	if !ok {
		release()
		return ApplyJob{}, errors.New("policy repository does not support atomic access proposal commit")
	}
	committedRevision, err := committer.CommitAccessProposal(ctx, *proposal, cached.BaseDesiredRevision, "policy-plan")
	if errors.Is(err, ErrRevisionStale) || errors.Is(err, accesscontrol.ErrRevisionStale) {
		release()
		return ApplyJob{}, ErrPlanStale
	}
	if err != nil {
		release()
		return ApplyJob{}, err
	}
	committedDesired, err := BuildAccessDesiredWithOptions(ctx, applier.Repo, applier.Reader, applier.Access, applier.Terminals, applier.Scope, cached.InternetEgresses)
	if err != nil {
		release()
		return ApplyJob{}, err
	}
	if committedDesired.AccessRevision != committedRevision || committedDesired.Hash != cached.Plan.DesiredHash || len(committedDesired.Blockers) > 0 {
		release()
		return ApplyJob{}, ErrPlanStale
	}
	cached.AccessProposal = nil
	cached.BaseDesiredRevision = committedRevision
	cached.Plan.DesiredRevision = committedRevision
	cached.Plan.AccessRevision = committedRevision
	cached.Desired = committedDesired.Objects
	cached.AccessResolutions = committedDesired.AccessResolutions
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

func validatePlanRepository(ctx context.Context, applier *Applier, repository Repository, domain PolicyDomain) error {
	if domain == PolicyDomainAccess {
		return nil
	}
	aliasBlockers, err := ValidateFakeAliases(ctx, applier.Reader, repository)
	if err != nil {
		return err
	}
	tableBlockers, _, err := ValidateRouteTables(ctx, applier.Reader, repository)
	if err != nil {
		return err
	}
	ingressBlockers, err := ValidateTrafficIngress(ctx, applier.Reader, repository)
	if err != nil {
		return err
	}
	if len(aliasBlockers) > 0 || len(tableBlockers) > 0 || len(ingressBlockers) > 0 {
		return ErrPlanStale
	}
	return nil
}

func (m *Manager) GenerateAndApply(ctx context.Context, deviceID, kind string) (ApplyJob, error) {
	plan, err := m.GeneratePlan(ctx, deviceID, kind)
	if err != nil {
		return ApplyJob{}, err
	}
	return m.ApplyPlan(ctx, deviceID, plan.PlanID)
}

// GenerateAndApplyTarget reconciles only the domains that consume one target
// list. A shared target is deliberately applied as routing first and access
// second, with two independently built plans and two domain-scoped commits.
func (m *Manager) GenerateAndApplyTarget(ctx context.Context, deviceID, kind, targetID string) (ApplyJob, error) {
	applier := m.ApplierFor(deviceID)
	if applier == nil {
		return ApplyJob{}, errors.New("policy runtime is unavailable")
	}
	domains := TargetConsumerDomains{Routing: true}
	if consumerRepository, ok := applier.Repo.(TargetConsumerDomainRepository); ok {
		var err error
		domains, err = consumerRepository.TargetConsumerDomains(ctx, strings.TrimSpace(targetID))
		if err != nil {
			return ApplyJob{}, err
		}
	}
	return m.generateAndApplyTargetDomains(ctx, deviceID, kind, []string{targetID}, domains)
}

// GenerateAndApplyTargetDomains is used by the scheduler to batch changed
// target lists into one routing reconcile and one access reconcile.
func (m *Manager) GenerateAndApplyTargetDomains(ctx context.Context, deviceID, kind string, targetIDs []string, domains TargetConsumerDomains) (ApplyJob, error) {
	return m.generateAndApplyTargetDomains(ctx, deviceID, kind, targetIDs, domains)
}

func (m *Manager) generateAndApplyTargetDomains(ctx context.Context, deviceID, kind string, targetIDs []string, domains TargetConsumerDomains) (ApplyJob, error) {
	targetIDs = uniqueTargetIDs(targetIDs)
	if len(targetIDs) == 0 || (!domains.Routing && !domains.Access) {
		return ApplyJob{}, nil
	}
	planIDs := make([]string, 0, 2)
	options := PlanOptions{TargetListIDs: targetIDs}
	if domains.Routing {
		options.Domain = PolicyDomainRouting
		plan, err := m.GeneratePlanWithOptions(ctx, deviceID, kind, options)
		if err != nil {
			return ApplyJob{}, err
		}
		planIDs = append(planIDs, plan.PlanID)
	}
	if domains.Access {
		options.Domain = PolicyDomainAccess
		plan, err := m.GeneratePlanWithOptions(ctx, deviceID, kind, options)
		if err != nil {
			m.deleteCachedPlans(planIDs)
			return ApplyJob{}, err
		}
		planIDs = append(planIDs, plan.PlanID)
	}
	if len(planIDs) == 0 {
		return ApplyJob{}, nil
	}
	if len(planIDs) > 1 {
		m.mu.Lock()
		m.followUpPlans[planIDs[0]] = append([]string(nil), planIDs[1:]...)
		m.mu.Unlock()
	}
	job, err := m.ApplyPlan(ctx, deviceID, planIDs[0])
	if err != nil {
		m.mu.Lock()
		delete(m.followUpPlans, planIDs[0])
		m.mu.Unlock()
		m.deleteCachedPlans(planIDs[1:])
		return ApplyJob{}, err
	}
	return job, nil
}

func (m *Manager) deleteCachedPlans(planIDs []string) {
	if len(planIDs) == 0 {
		return
	}
	m.mu.Lock()
	for _, planID := range planIDs {
		delete(m.plans, planID)
	}
	m.mu.Unlock()
}

func uniqueTargetIDs(targetIDs []string) []string {
	seen := make(map[string]bool, len(targetIDs))
	result := make([]string, 0, len(targetIDs))
	for _, targetID := range targetIDs {
		targetID = strings.TrimSpace(targetID)
		if targetID == "" || seen[targetID] {
			continue
		}
		seen[targetID] = true
		result = append(result, targetID)
	}
	sort.Strings(result)
	return result
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
		needRoutingApply := false
		needAccessApply := false
		changedTargetIDs := make([]string, 0)
		for _, source := range sources {
			interval, ok := sourceScheduleInterval(source.Schedule)
			if !ok || (source.Type != TargetSourceTypeURL && source.Type != TargetSourceTypePreset) || source.PendingDeletion || source.PendingVersionID != "" {
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
				changedTargetIDs = append(changedTargetIDs, source.ID)
				if consumerRepository, ok := applier.Repo.(TargetConsumerDomainRepository); ok {
					consumers, consumerErr := consumerRepository.TargetConsumerDomains(ctx, source.ID)
					if consumerErr != nil {
						return consumerErr
					}
					needRoutingApply = needRoutingApply || consumers.Routing
					needAccessApply = needAccessApply || consumers.Access
				} else {
					needRoutingApply = true
				}
			}
		}
		if len(changedTargetIDs) > 0 && (needRoutingApply || needAccessApply) {
			if _, err := m.GenerateAndApplyTargetDomains(ctx, applier.Repo.DeviceID(), "source-refresh", changedTargetIDs, TargetConsumerDomains{Routing: needRoutingApply, Access: needAccessApply}); err != nil {
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
	routingAuthoritative := false
	if routingRepository, ok := repository.(RoutingRuleRepository); ok {
		if err := routingRepository.EnsureRoutingRulesMigrated(ctx); err == nil {
			if authority, authorityErr := routingRepository.RoutingAuthority(ctx); authorityErr == nil && authority == RoutingRuleAuthorityV1 {
				routingAuthoritative = true
				if rules, rulesErr := routingRepository.ListRoutingRules(ctx); rulesErr == nil {
					for _, rule := range rules {
						for _, targetID := range rule.TargetListIDs {
							if targetID != source.ID {
								continue
							}
							egress, egressErr := repository.GetEgress(ctx, rule.EgressID)
							if egressErr == nil && egress.Enabled && !egress.PendingDeletion {
								return true
							}
						}
					}
				}
			}
		}
	}
	if !routingAuthoritative && source.EgressID != "" {
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
			if !rule.Enabled {
				continue
			}
			switch rule.TargetScope {
			case accesscontrol.TargetScopeTargets:
				for _, targetID := range rule.TargetListIDs {
					if targetID == source.ID {
						return true
					}
				}
			case accesscontrol.TargetScopeSources:
				for _, sourceID := range rule.SourceIDs {
					if sourceID == source.ID {
						return true
					}
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
	for {
		var completed bool
		job, completed = m.runApplyDomain(ctx, deviceID, applier, cached, job)
		if !completed {
			m.deleteFollowUpPlans(cached.Plan.PlanID)
			return
		}

		m.mu.Lock()
		followUps := m.followUpPlans[cached.Plan.PlanID]
		delete(m.followUpPlans, cached.Plan.PlanID)
		var next cachedPlan
		nextPlanID := ""
		haveNext := false
		if len(followUps) > 0 {
			nextPlanID = followUps[0]
			next, haveNext = m.plans[nextPlanID]
			delete(m.plans, nextPlanID)
			for _, orphanID := range followUps[1:] {
				delete(m.plans, orphanID)
			}
		}
		m.mu.Unlock()
		if nextPlanID == "" || !haveNext {
			return
		}

		job.PlanID = next.Plan.PlanID
		job.State = "queued"
		job.Phase = "queued"
		job.Progress = 0
		job.Error = ""
		job.FinishedAt = time.Time{}
		if err := applier.Repo.SaveApplyJob(ctx, job); err != nil {
			m.failJob(ctx, applier.Repo, &job, "save-follow-up", err)
			m.deleteFollowUpPlans(next.Plan.PlanID)
			return
		}
		cached = next
	}
}

func (m *Manager) deleteFollowUpPlans(planID string) {
	m.mu.Lock()
	remaining := m.followUpPlans[planID]
	delete(m.followUpPlans, planID)
	for _, followUpID := range remaining {
		delete(m.plans, followUpID)
	}
	m.mu.Unlock()
}

func (m *Manager) runApplyDomain(ctx context.Context, deviceID string, applier *Applier, cached cachedPlan, job ApplyJob) (ApplyJob, bool) {
	domain := cached.Plan.Domain
	if domain == "" {
		domain = policyDomainForPlanKind(cached.Plan.Kind)
	}
	job.State = "staging"
	job.Phase = "staging"
	job.StartedAt = time.Now().UTC()
	if err := applier.Repo.SaveApplyJob(ctx, job); err != nil {
		m.failJob(ctx, applier.Repo, &job, "save-start", err)
		return job, false
	}
	planned, err := buildPlanDesiredWithAccessRepository(ctx, applier, applier.Repo, applier.Access, domain, cached.InternetEgresses, cached.TargetScope)
	if err != nil {
		m.failJob(ctx, applier.Repo, &job, "desired-state-check", err)
		return job, false
	}
	if !desiredMatchesPlan(planned, cached.Plan, domain) || len(planned.Blockers) > 0 {
		m.failJob(ctx, applier.Repo, &job, "desired-state-changed", ErrPlanStale)
		return job, false
	}
	if _, fingerprint, err := ScanManagedForDomain(ctx, applier.Mutation, applier.Repo, planned.Objects, domain); err != nil {
		m.failJob(ctx, applier.Repo, &job, "actual-state-check", err)
		return job, false
	} else if fingerprint != cached.Plan.ActualFingerprint {
		m.failJob(ctx, applier.Repo, &job, "actual-state-changed", ErrPlanStale)
		return job, false
	}
	if domain == PolicyDomainRouting || domain == PolicyDomainCombined {
		validation := DesiredResult{}
		if err := appendRoutingValidation(ctx, applier.Reader, applier.Repo, &validation); err != nil {
			m.failJob(ctx, applier.Repo, &job, "desired-state-validation", err)
			return job, false
		}
		if len(validation.Blockers) > 0 {
			m.failJob(ctx, applier.Repo, &job, "desired-state-validation", ErrPlanStale)
			return job, false
		}
	}
	needsDNSMutation := domain == PolicyDomainRouting || hasDesiredMenu(cached.Desired, routeros.MenuIPDNSStatic)
	if needsDNSMutation {
		if err := ensureDefaultDNSCache(ctx, applier); err != nil {
			m.failJob(ctx, applier.Repo, &job, "dns-cache-size", err)
			return job, false
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
				return job, false
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
				return job, false
			}
		case isEnableOnlyPatch(operation):
			// Active objects are enabled together after every create/update has
			// been staged. This avoids exposing a partially built policy.
		case isDisableOnlyPatch(operation):
			if err := applyStagedOperation(ctx, applier.Mutation, operation, createdRouterIDs); err != nil {
				m.failJob(ctx, applier.Repo, &job, operation.LogicalID, err)
				return job, false
			}
		default:
			if err := applyStagedOperation(ctx, applier.Mutation, operation, createdRouterIDs); err != nil {
				m.failJob(ctx, applier.Repo, &job, operation.LogicalID, err)
				return job, false
			}
		}
		index = nextIndex
		job.Progress = index
		if err := applier.Repo.SaveApplyJob(ctx, job); err != nil {
			m.failJob(ctx, applier.Repo, &job, "save-progress", err)
			return job, false
		}
	}
	if domain == PolicyDomainAccess || domain == PolicyDomainCombined {
		if err := ensureAccessJumpsFirst(ctx, applier.Mutation, cached.Desired); err != nil {
			m.failJob(ctx, applier.Repo, &job, "access-filter-order", err)
			return job, false
		}
	}
	job.Phase = "activation"
	if err := applier.Repo.SaveApplyJob(ctx, job); err != nil {
		m.failJob(ctx, applier.Repo, &job, "save-activation", err)
		return job, false
	}
	if err := activateDesiredObjectsForDomain(ctx, applier, cached.Desired, domain); err != nil {
		m.failJob(ctx, applier.Repo, &job, "activation", err)
		return job, false
	}
	if needsDNSMutation {
		if err := applier.Mutation.FlushDNSCache(ctx); err != nil {
			m.failJob(ctx, applier.Repo, &job, "dns-cache", err)
			return job, false
		}
	}
	job.State = "verifying"
	job.Phase = "verifying"
	if err := applier.Repo.SaveApplyJob(ctx, job); err != nil {
		m.failJob(ctx, applier.Repo, &job, "save-verifying", err)
		return job, false
	}
	remaining, blockers, err := verifyDesiredWithRetryForDomain(ctx, applier.Mutation, applier.Repo, cached.Desired, domain)
	if err != nil {
		m.failJob(ctx, applier.Repo, &job, "verify-scan", err)
		return job, false
	}
	if len(remaining) > 0 || len(blockers) > 0 {
		m.failJob(ctx, applier.Repo, &job, "verify-diff", fmt.Errorf("RouterOS still differs after apply: operations=%d blockers=%d", len(remaining), len(blockers)))
		return job, false
	}
	// Monitor-driven auto addresses do not bump the persistent access revision.
	// Re-read the desired graph immediately before committing so a terminal
	// change during the RouterOS mutation cannot be recorded as an applied
	// trusted projection for an older snapshot.
	latest, err := buildPlanDesiredWithAccessRepository(ctx, applier, applier.Repo, applier.Access, domain, cached.InternetEgresses, cached.TargetScope)
	if err != nil {
		m.failJob(ctx, applier.Repo, &job, "desired-state-check", err)
		return job, false
	}
	if !desiredMatchesPlan(latest, cached.Plan, domain) {
		m.failJob(ctx, applier.Repo, &job, "desired-state-changed", ErrPlanStale)
		return job, false
	}
	job.Progress = len(cached.Plan.Operations)
	var commitErr error
	if domain == PolicyDomainCombined {
		commitErr = applier.Repo.CommitApply(ctx, cached.Plan.DesiredRevision, cached.Plan.AccessRevision, cached.Plan.DesiredHash, job, cached.AccessResolutions, true)
	} else if committer, ok := applier.Repo.(DomainApplyRepository); ok {
		if domain == PolicyDomainAccess {
			commitErr = committer.CommitAccessApply(ctx, cached.Plan.AccessRevision, cached.Plan.DesiredHash, job, cached.AccessResolutions, cached.TargetPromotions)
		} else {
			commitErr = committer.CommitRoutingApply(ctx, cached.Plan.DesiredRevision, cached.Plan.DesiredHash, job, cached.TargetPromotions)
		}
	} else {
		// The old broad commit is a compatibility seam for explicit Combined
		// callers only. Normal manager paths require the domain-scoped contract.
		commitErr = errors.New("policy repository does not support domain-scoped apply")
	}
	if commitErr != nil {
		m.failJob(ctx, applier.Repo, &job, "commit", commitErr)
		return job, false
	}
	job.State = "committed"
	job.Phase = "committed"
	job.FinishedAt = time.Now().UTC()
	return job, true
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

func verifyDesiredWithRetryForDomain(ctx context.Context, mutation PolicyMutation, repository Repository, desired []DesiredObject, domain PolicyDomain) ([]PlanOperation, []PlanIssue, error) {
	const maxAttempts = 5
	var remaining []PlanOperation
	var blockers []PlanIssue
	for attempt := 0; attempt < maxAttempts; attempt++ {
		actual, _, err := ScanManagedForDomain(ctx, mutation, repository, desired, domain)
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
	return activateDesiredObjectsForDomain(ctx, applier, desired, PolicyDomainCombined)
}

func activateDesiredObjectsForDomain(ctx context.Context, applier *Applier, desired []DesiredObject, domain PolicyDomain) error {
	activeKeys := make(map[string]struct{})
	for _, object := range desired {
		key := object.Menu + "\x00" + object.LogicalID
		if strings.EqualFold(strings.TrimSpace(object.Fields["disabled"]), "no") {
			activeKeys[key] = struct{}{}
		}
	}
	actual, err := scanActivationActual(ctx, applier, desired, domain)
	if err != nil {
		return err
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

func scanActivationActual(ctx context.Context, applier *Applier, desired []DesiredObject, domain PolicyDomain) ([]ActualObject, error) {
	const maxAttempts = 5
	for attempt := 0; attempt < maxAttempts; attempt++ {
		actual, _, err := ScanManagedForDomain(ctx, applier.Mutation, applier.Repo, desired, domain)
		if err != nil {
			return nil, fmt.Errorf("scan RouterOS before activation: %w", err)
		}
		if missing := missingActiveObject(desired, actual); missing == "" {
			return actual, nil
		} else if attempt == maxAttempts-1 {
			return nil, fmt.Errorf("managed object %s is missing during activation", missing)
		}
		delay := time.Duration(attempt+1) * time.Second
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	return nil, errors.New("activation scan exhausted")
}

func missingActiveObject(desired []DesiredObject, actual []ActualObject) string {
	actualKeys := make(map[string]struct{}, len(actual))
	for _, object := range actual {
		if object.Ownership != "owned" {
			continue
		}
		actualKeys[object.Menu+"\x00"+object.LogicalID] = struct{}{}
	}
	for _, object := range desired {
		if !strings.EqualFold(strings.TrimSpace(object.Fields["disabled"]), "no") {
			continue
		}
		key := object.Menu + "\x00" + object.LogicalID
		if _, ok := actualKeys[key]; !ok {
			return object.LogicalID
		}
	}
	return ""
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
	kind = strings.ToLower(strings.TrimSpace(kind))
	return strings.HasPrefix(kind, "access-") || strings.HasPrefix(kind, "canonical-target-")
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

func isAccessPlanOperation(operation PlanOperation, accessTargetIDs []string) bool {
	logicalID := operation.LogicalID
	if isStaleApplicationOperation(operation) ||
		logicalID == "access-forwarder" ||
		strings.HasPrefix(logicalID, "access:") ||
		strings.HasPrefix(logicalID, "access-member:") ||
		strings.HasPrefix(logicalID, "access-target:") ||
		strings.HasPrefix(logicalID, "access-target-dns:") ||
		strings.HasPrefix(logicalID, "access-internet-egress:") ||
		strings.HasPrefix(logicalID, "access-local:") ||
		strings.HasPrefix(logicalID, "dns:access:") ||
		strings.HasPrefix(logicalID, "dns:application:") {
		return true
	}
	if !strings.HasPrefix(logicalID, "source-addr:") {
		return false
	}
	parts := strings.SplitN(logicalID, ":", 3)
	if len(parts) != 3 {
		return false
	}
	for _, targetID := range accessTargetIDs {
		if targetID == parts[1] {
			return true
		}
	}
	return false
}

func isStaleApplicationOperation(operation PlanOperation) bool {
	if operation.Action != "delete" || operation.Ownership != "owned" || !strings.HasPrefix(operation.LogicalID, "stale:") {
		return false
	}
	switch operation.Menu {
	case string(routeros.MenuIPDNSStatic), string(routeros.MenuIPFirewallFilter), string(routeros.MenuIPv6FirewallFilter):
	default:
		return false
	}
	for _, field := range []string{"address-list", "src-address-list", "dst-address-list"} {
		value := strings.TrimSpace(operation.Before[field])
		if strings.HasPrefix(value, applicationListPrefix) || strings.HasPrefix(value, "rb_ac_") || ownership.IsNamespace(value) {
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
