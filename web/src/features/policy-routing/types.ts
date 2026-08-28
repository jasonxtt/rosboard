// Policy routing domain types — reconstructed from dist bundle and backend API contract.

export type PolicySetupState = 'write_access_required' | 'manager_unavailable' | 'runtime_unavailable' | 'ready'

export type PolicyAddressFamily = 'ipv4' | 'ipv6'

export type PolicyListMode = 'shared' | 'dedicated'

export type PolicyFailureMode = 'strict' | 'fallback' | 'existing'

export type PolicySourceType = 'url' | 'upload'

export type PolicyRuleType = 'DOMAIN' | 'DOMAIN-SUFFIX'

export type PolicyWANSource = '' | 'next-hop'

export type PolicyRouteMode = '' | 'strict' | 'fallback'

export type PolicyNATMode = '' | 'none'

export type PolicyOwnershipState = 'owned' | 'reused' | 'foreign' | 'manual_candidate'

export type PolicyCapabilityStatus = 'supported' | 'unsupported' | 'unknown' | 'unavailable'

export type PolicyPlanKind = 'initial' | 'structural' | 'domain_delta' | 'source_migration' | 'disable_delete' | 'adoption'

export type PolicyPlanState = 'ready' | 'approved' | 'preview' | 'partial' | 'pending_review' | 'blocked'

export type PolicyPlanLifecycle = 'interactive' | 'scheduled'

export type PolicyOperationAction =
  | 'create' | 'patch' | 'delete' | 'move' | 'disable' | 'enable'
  | 'reference_add' | 'reference_remove' | 'reuse' | 'adopt' | 'dns_cache_flush'

export type PolicyJobState =
  | 'queued' | 'reconciling' | 'backing_up' | 'staging' | 'ordering'
  | 'activating' | 'flushing_cache' | 'verifying' | 'committed'
  | 'committed_partial' | 'rolling_back' | 'rolled_back' | 'rollback_failed'
  | 'needs_decision' | 'cancelled_before_write' | 'failed'

export type PolicyProofStatus = 'safe' | 'warning' | 'blocker' | 'indeterminate'

export type PolicyScanIssueKind = 'blocker' | 'warning' | 'unavailable'

export type PolicyAckCode =
  | 'fallback_main_table' | 'main_table_reuse' | 'firewall_high_risk_exception'
  | 'reuse_user_list' | 'adoption' | 'force_adoption' | 'managed_field_delta'
  | 'large_change' | 'source_shrink_review'

export type PolicySection = 'settings' | 'sources'

// ---- Egress family ----

export type PolicyEgressFamily = {
  family: PolicyAddressFamily
  enabled: boolean
  wanInterface: string
  gateway: string
  routeTable: string
  routeMode: PolicyRouteMode | string
  natMode: PolicyNATMode | string
  wanSource: PolicyWANSource | string
}

// ---- Egress (policy route) ----

export type PolicyEgressSourceSummary = {
  id: string
  name: string
  type: PolicySourceType | string
  enabled: boolean
  ruleCount: number
}

export type PolicyEgress = {
  id: string
  name: string
  priority: number
  listMode: PolicyListMode | string
  listName: string
  dnsUpstream: string
  fakeAlias: string
  failureMode: PolicyFailureMode | string
  routerOutput: boolean
  enabled: boolean
  revision: number
  pendingDeletion: boolean
  applied: boolean
  families: PolicyEgressFamily[]
  // The overview endpoint returns the complete source object here in the
  // original bundle.  Keep the summary shape assignable for older handlers
  // that still omit version metadata.
  sources: Array<PolicySource | PolicyEgressSourceSummary>
  objects?: PolicyObjectReference[]
}

// ---- Source (domain list) ----

export type PolicySourceVersion = {
  id: string
  sourceId: string
  sha256: string
  state: string
  error: string
  httpStatus: number
  createdAt: string
  counts: Record<string, number>
  diff: Record<string, unknown>
}

export type PolicySource = {
  id: string
  egressId: string
  type: PolicySourceType | string
  name: string
  url: string
  schedule: string
  enabled: boolean
  activeVersionId: string
  lastGoodVersionId: string
  etag: string
  lastModified: string
  nextRunAt: string
  revision: number
  pendingDeletion: boolean
  versions: PolicySourceVersion[]
  counts?: Record<string, number>
}

// ---- Preview ----

export type PolicyPreviewRule = {
  type: PolicyRuleType | string
  domain: string
}

export type PolicyPreview = {
  previewId?: string
  url?: string
  filename?: string
  statusCode?: number
  contentType?: string
  etag?: string
  lastModified?: string
  notModified?: boolean
  size?: number
  sha256?: string
  validRules: number
  ignored: Record<string, number>
  errorSamples: string[]
  rules: PolicyPreviewRule[]
}

// ---- Rules page ----

export type PolicyRuleEntry = {
  type: PolicyRuleType | string
  domain: string
}

export type PolicyRulesPage = {
  sourceId: string
  versionId: string
  rules: PolicyRuleEntry[]
  nextCursor: string
}

// ---- Device account ----

export type PolicyAccount = {
  username: string
  group?: string
  policies: string[]
  permission: 'write' | 'read_only' | 'unknown' | string
  writeAccess: boolean
  error?: string
}

// ---- Discovery ----

export type PolicyWANRoute = {
  id: string
  family: PolicyAddressFamily | string
  destination: string
  gateway: string
  immediateGateway: string
  table: string
  source: string
  distance: number
  active: boolean
  proven: boolean
}

export type PolicyTrafficIngressCandidate = {
  name: string
  kind: string
  include: string[]
  exclude: string[]
  staticMembers: string[]
  dynamicMembers: boolean
  frozen: boolean
  addresses: string[]
  reason: string
  coveredBy: string[]
  default: boolean
  dynamic: boolean
  running: boolean
}

export type PolicyTrafficIngressScope = {
  interfaceLists: string[]
  interfaces: string[]
}

export type PolicyObjectReference = {
  logicalId: string
  menu: string
  routerId: string
  ownership: PolicyOwnershipState | string
}

export type PolicyExistingPolicy = PolicyObjectReference & {
  reason: string
  foreignManager: boolean
}

export type PolicyDiscovery = {
  device?: { id: string; name: string }
  available: boolean
  reason?: string
  warnings: string[]
  snapshot?: {
    fingerprint: string
    deviceIdentity: Record<string, string>
    capabilities: Record<string, PolicyCapabilityEvidence>
  }
  wans: Array<{
    interface: string
    type: string
    running: boolean
    pointToPoint: boolean
    proven: boolean
    routes: PolicyWANRoute[]
  }>
  trafficIngress: PolicyTrafficIngressCandidate[]
  builtins?: PolicyObjectReference[]
  existingPolicy?: PolicyExistingPolicy[]
  adoptionCandidates?: PolicyExistingPolicy[]
}

export type PolicyCapabilityEvidence = {
  capability: string
  status: PolicyCapabilityStatus | string
  reason: string
  evidence: string[]
}

// ---- Runtime capability ----

export type PolicyRuntimeCapability = {
  state: PolicySetupState | string
  reason: string
  version: string
  entries: Record<string, PolicyCapabilityEvidence>
}

// ---- Health / drift ----

export type PolicyHealth = {
  state: string
  driftState: string
  mutationPaused: boolean
  manualInterventionRequired: boolean
  pauseReason: string
  pauseJobId: string
}

export type PolicyDriftItem = {
  code?: string
  status?: string
  family?: string
  egressID?: string
  logicalID?: string
  reason: string
}

export type PolicyDrift = {
  state: string
  items: PolicyDriftItem[]
}

// ---- Device ----

export type PolicyDevice = {
  id: string
  name: string
  enabled: boolean
  archived?: boolean
}

// ---- Overview (main state) ----

export type PolicyOverview = {
  device: PolicyDevice
  account: PolicyAccount
  setup: { state: PolicySetupState; managerAvailable?: boolean }
  capability?: PolicyRuntimeCapability
  trafficIngress: PolicyTrafficIngressScope
  health?: PolicyHealth
  drift?: PolicyDrift
  egresses: PolicyEgress[]
  sources: PolicySource[]
  activeJobs: PolicyJob[]
  pendingJobs: PolicyJob[]
  applied?: boolean
}

// ---- Plan ----

export type PolicyPlanIssue = {
  code: string
  status: PolicyProofStatus | string
  family?: string
  egressID?: string
  logicalID?: string
  reason: string
}

export type PolicyPlanAcknowledgement = {
  code: PolicyAckCode | string
  required: boolean
  accepted: boolean
}

export type PolicyPlanSummary = {
  create: number
  patch: number
  delete: number
  move: number
  disable: number
  enable: number
  referenceAdd: number
  referenceRemove: number
  reuse: number
  adopt: number
  blockers: number
  warnings: number
  familyBlockers: number
}

export type PolicyResourceEstimate = {
  validDomains: number
  activeDNSStatic: number
  resourceWarning: boolean
  sourceLimit: number
  deviceLimit: number
  largestSource: number
  scheduledShrink: boolean
  removedDomains: number
}

export type PolicyAnchorDescriptor = {
  logicalID?: string
  routerID?: string
  relation?: string
  neighborID?: string
  menu?: string
  family?: string
}

export type PolicyVerificationDescriptor = {
  action: string
  menu?: string
  logicalID?: string
  routerID?: string
}

export type PolicyCompensationDescriptor = {
  action: string
  menu?: string
  reason?: string
}

export type PolicyPlanOperation = {
  seq: number
  operationID?: string
  groupID?: string
  egressID?: string
  family?: PolicyAddressFamily | string
  phase: string
  action: PolicyOperationAction | string
  menu?: string
  logicalID?: string
  routerID?: string
  ownership?: PolicyOwnershipState | string
  before?: Record<string, unknown>
  after?: Record<string, unknown>
  anchor?: PolicyAnchorDescriptor
  verification?: PolicyVerificationDescriptor
  compensation?: PolicyCompensationDescriptor
}

export type PolicyExecutionGroup = {
  id: string
  role: string
  egressID?: string
  family?: string
  operationSeqs: number[]
}

export type PolicyPlan = {
  planID: string
  deviceID: string
  kind: PolicyPlanKind | string
  lifecycle: PolicyPlanLifecycle | string
  createdAt: string
  expiresAt?: string
  desiredRevision: number
  desiredHash: string
  actualFingerprint: string
  capabilities: Record<string, string>
  blockers: PolicyPlanIssue[]
  familyBlockers?: PolicyPlanIssue[]
  warnings: PolicyPlanIssue[]
  acknowledgements: PolicyPlanAcknowledgement[]
  ownershipStrict: boolean
  summary: PolicyPlanSummary
  resourceEstimate: PolicyResourceEstimate
  pendingReview: boolean
  operations: PolicyPlanOperation[]
  executionGroups: PolicyExecutionGroup[]
  planHash: string
  state: PolicyPlanState | string
}

export type PolicyPlanEnvelope = {
  plan: PolicyPlan
  planId: string
  planHash: string
  readOnly: boolean
}

// ---- Job ----

export type PolicyJobStep = {
  sequence: number
  action: string
  target: string
  routerId: string
  status: string
  attempt: number
  error: string
  updatedAt: string
}

export type PolicyPartialResult = {
  planId: string
  planHash: string
  configuredFamilies: string[]
  blockedFamilies: string[]
  entries: Array<{
    egressId: string
    family: string
    groupId: string
    status: string
    reason: string
    failedOperation: string
  }>
  createdAt: string
}

export type PolicyJob = {
  id: string
  planId: string
  acknowledgements: string[]
  stepUpApproved: boolean
  state: PolicyJobState | string
  phase: string
  progress: number
  cancelRequested: boolean
  error: string
  primaryError: string
  rollbackError: string
  failedOperation: string
  createdAt: string
  startedAt: string
  finishedAt: string
  steps: PolicyJobStep[]
  recoveryChoices: string[]
  partialResult?: PolicyPartialResult
}

// ---- Audit ----

export type PolicyAuditEntry = {
  id: string
  actor: string
  remoteIp: string
  action: string
  objectId: string
  planId: string
  planHash: string
  jobId: string
  versionId: string
  result: string
  summary: string
  createdAt: string
}

export type PolicyAuditPage = {
  entries: PolicyAuditEntry[]
  nextCursor: string
}

// ---- Apply result ----

export type PolicyApplyResult = {
  job?: PolicyJob
  jobId?: string
  [key: string]: unknown
}

// ---- Drafts ----

export type PolicyEgressDraft = {
  id: string
  name: string
  priority: number
  listMode: PolicyListMode | string
  listName: string
  dnsUpstream: string
  fakeAlias: string
  failureMode: PolicyFailureMode | string
  routerOutput: boolean
  enabled: boolean
  revision: number
  families: PolicyEgressFamily[]
}

export type PolicySourceDraft = {
  id: string
  egressId: string
  type: PolicySourceType | string
  name: string
  url: string
  schedule: string
  enabled: boolean
  revision: number
}

// ---- API error ----

export type PolicyApiErrorDetails = Record<string, unknown>

export class PolicyApiError extends Error {
  status: number
  code?: string
  details: PolicyApiErrorDetails
  constructor(message: string, status: number, code?: string, details?: PolicyApiErrorDetails) {
    super(message)
    this.name = 'PolicyApiError'
    this.status = status
    this.code = code
    this.details = details ?? {}
  }
  get retryable(): boolean {
    return this.details.retryable === true
  }
  detailStrings(key: string): string[] {
    const value = this.details[key]
    if (Array.isArray(value)) return value.filter((v): v is string => typeof v === 'string')
    if (typeof value === 'string') return [value]
    return []
  }
  planIssues(key: string): PolicyPlanIssue[] {
    const value = this.details[key]
    if (!Array.isArray(value)) return []
    return value.map((item) => {
      const obj = (item ?? {}) as Record<string, string>
      return {
        code: obj.code ?? '',
        status: obj.status ?? '',
        family: obj.family ?? '',
        egressID: obj.egressID ?? obj.egressId ?? '',
        logicalID: obj.logicalID ?? obj.logicalId ?? '',
        reason: obj.reason ?? '',
      }
    })
  }
}
