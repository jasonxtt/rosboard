export type AccessTargetScope = 'internet' | 'sources'

export type AccessBinding = 'auto' | 'fixed'

export type AccessMemberState = 'resolved' | 'temporarily_unresolved' | 'conflicted'

export type AccessMemberStatus = 'applied' | 'applying' | 'pending' | 'failed' | 'degraded' | 'disabled'

export type AccessRuleMember = {
  terminalId: string
  binding: AccessBinding
  state: AccessMemberState
  ipv4: string[]
  ipv6: string[]
  reason?: string
}

export type AccessRule = {
  id: string
  name: string
  targetScope: AccessTargetScope
  sourceIds: string[]
  enabled: boolean
  revision: number
  createdAt: string
  updatedAt: string
  members: AccessRuleMember[]
  status: AccessMemberStatus
  issues: string[]
}

export type AccessTerminal = {
  id: string
  displayName: string
  ipv4: string[]
  ipv6: string[]
  autoEligible: boolean
}

export type AccessSourceVersion = {
  id: string
  state: string
}

export type AccessSource = {
  id: string
  name: string
  kind: 'domain' | 'ip'
  enabled: boolean
  pendingDeletion: boolean
  activeVersionId: string
  versions: AccessSourceVersion[]
}

export type AccessEgressCandidate = {
  interface: string
  type: string
  running: boolean
  reason?: string
}

export type AccessEgressCandidates = Record<string, AccessEgressCandidate[]>

export type AccessJob = {
  id: string
  state: string
  phase: string
  progress: number
  error?: string
}

export type AccessState = {
  deviceId: string
  desiredRevision: number
  appliedRevision: number
  appliedAt?: string
}

export type AccessOverview = {
  device: { id: string; name: string; enabled: boolean }
  rules: AccessRule[]
  terminals: AccessTerminal[]
  sources: AccessSource[]
  state: AccessState
  job: AccessJob
  boundary: string
}

export type AccessRuleMemberInput = {
  terminalId: string
  binding: AccessBinding
  pinnedIpv4: string[]
  pinnedIpv6: string[]
}

export type AccessRuleInput = {
  id: string
  name: string
  targetScope: AccessTargetScope
  sourceIds: string[]
  enabled: boolean
  revision: number
  members: AccessRuleMemberInput[]
}

export type AccessApplyResult = {
  rule?: AccessRule
  deleted?: boolean
  desiredSaved?: boolean
  job?: AccessJob
  jobId?: string
}
