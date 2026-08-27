// Mock API for development/testing — mimics the policy-routing backend responses.
import type {
  PolicyAccess,
  PolicyDiscovery,
  PolicyEgress,
  PolicyOverview,
  PolicyPreview,
  PolicySource,
} from '../types'

const mockAccess: PolicyAccess = {
  enabled: false,
  username: '',
  passwordSet: false,
  managed: false,
  cleanupAvailable: false,
}

const mockEgresses: PolicyEgress[] = [
  {
    id: 'mock-egress-1',
    name: '主出口',
    priority: 100,
    listMode: 'shared',
    listName: 'manual_proxy_lab',
    dnsUpstream: '1.1.1.1',
    fakeAlias: '',
    failureMode: 'strict',
    routerOutput: false,
    enabled: true,
    revision: 1,
    pendingDeletion: false,
    applied: false,
    families: [
      { family: 'ipv4', enabled: true, wanInterface: 'pppoe-out1', gateway: '', routeTable: '', routeMode: '', natMode: '', wanSource: '' },
    ],
    sources: [],
  },
]

const mockSources: PolicySource[] = []

const mockOverview: PolicyOverview = {
  device: { id: 'mock-device', name: 'Mock Device', enabled: true },
  access: mockAccess,
  setup: { state: 'access_required' },
  lanScope: {},
  health: { state: 'ok', driftState: '', mutationPaused: false, manualInterventionRequired: false, pauseReason: '', pauseJobId: '' },
  drift: { state: '', items: [] },
  egresses: mockEgresses,
  sources: mockSources,
  activeJobs: [],
  pendingJobs: [],
  applied: false,
}

const mockDiscovery: PolicyDiscovery = {
  available: false,
  reason: 'runtime not ready',
  wans: [],
  lan: [],
}

const mockPreview: PolicyPreview = {
  validRules: 0,
  ignored: {},
  errorSamples: [],
  rules: [],
}

export const mockPolicyApi = {
  async fetchOverview(_deviceID: string): Promise<PolicyOverview> {
    return mockOverview
  },
  async fetchDiscovery(_deviceID: string): Promise<PolicyDiscovery> {
    return mockDiscovery
  },
  async saveAccess(_deviceID: string, _body: { enabled: boolean; username: string; password: string }): Promise<{ access: PolicyAccess; restarting: boolean }> {
    return { access: { ...mockAccess, enabled: true, username: 'rosboard_policy_mock' }, restarting: true }
  },
  async saveEgress(_deviceID: string, draft: import('../types').PolicyEgressDraft): Promise<PolicyEgress> {
    return { ...mockEgresses[0], ...draft, id: draft.id || 'mock-egress-new', applied: false, pendingDeletion: false, sources: [] }
  },
  async saveSource(_deviceID: string, draft: import('../types').PolicySourceDraft): Promise<PolicySource> {
    return { ...mockSources[0], ...draft, id: draft.id || 'mock-source-new', versions: [], pendingDeletion: false }
  },
  async previewURL(_deviceID: string, _body: { url: string }): Promise<PolicyPreview> {
    return { ...mockPreview, url: _body.url, validRules: 42, rules: [{ type: 'DOMAIN', domain: 'example.com' }] }
  },
  async previewUpload(_deviceID: string, _formData: FormData): Promise<PolicyPreview> {
    return { ...mockPreview, filename: 'upload.yaml', validRules: 10, rules: [{ type: 'DOMAIN-SUFFIX', domain: 'test.com' }] }
  },
}
