import type {
  PolicyAddressFamily,
  PolicyEgress,
  PolicyEgressDraft,
  PolicyEgressFamily,
  PolicyDiscovery,
  PolicySource,
  PolicySourceDraft,
} from './types'

export function defaultEgressDraft(): PolicyEgressDraft {
  return {
    id: '',
    name: '',
    priority: 100,
    listMode: 'dedicated',
    listName: 'manual_proxy_lab',
    dnsUpstream: '1.1.1.1',
    fakeAlias: '',
    failureMode: 'strict',
    routerOutput: false,
    enabled: true,
    revision: 0,
    families: [
      {
        family: 'ipv4',
        enabled: true,
        wanInterface: '',
        gateway: '',
        routeTable: '',
        routeMode: '',
        natMode: '',
        wanSource: '',
      },
    ],
  }
}

export function egressDraftFromEgress(egress: PolicyEgress): PolicyEgressDraft {
  return {
    id: egress.id,
    name: egress.name,
    priority: egress.priority,
    listMode: egress.listMode,
    listName: egress.listName,
    dnsUpstream: egress.dnsUpstream,
    fakeAlias: egress.fakeAlias,
    failureMode: egress.failureMode,
    routerOutput: egress.routerOutput,
    enabled: egress.enabled,
    revision: egress.revision,
    families: egress.families.length
      ? egress.families.map((f) => ({ ...f }))
      : [
          {
            family: 'ipv4',
            enabled: true,
            wanInterface: '',
            gateway: '',
            routeTable: '',
            routeMode: '',
            natMode: '',
            wanSource: '',
          },
        ],
  }
}

export function defaultSourceDraft(egressId: string): PolicySourceDraft {
  return {
    id: '',
    egressId,
    type: 'url',
    name: '',
    url: '',
    schedule: '24h',
    enabled: true,
    revision: 0,
  }
}

export function sourceDraftFromSource(source: PolicySource): PolicySourceDraft {
  return {
    id: source.id,
    egressId: source.egressId,
    type: source.type,
    name: source.name,
    url: source.url,
    schedule: source.schedule,
    enabled: source.enabled,
    revision: source.revision,
  }
}

export function egressDraftErrors(draft: PolicyEgressDraft, discovery: PolicyDiscovery | null = null): string[] {
  const errors: string[] = []
  if (!draft.name.trim()) errors.push('出口名称不能为空')
  if (draft.listMode === 'dedicated' && !draft.listName.trim()) {
    errors.push('专用列表模式下需要指定列表名称')
  }
  const enabledFamilies = draft.families.filter((f) => f.enabled)
  if (enabledFamilies.length === 0) errors.push('至少启用一个地址族')
  for (const f of enabledFamilies) {
    if (f.wanSource === 'next-hop') {
      if (f.wanInterface.trim()) errors.push(`${f.family.toUpperCase()} 的下一跳模式仍带有旧接口值，请重新选择真实接口，或重新选择“下一跳网关”后再保存`)
      if (!f.gateway.trim()) errors.push(`${f.family.toUpperCase()} 选择“下一跳网关”后必须填写网关 IP`)
    } else if (!f.wanInterface.trim()) {
      errors.push(`${f.family.toUpperCase()} 必须选择目标 WAN 接口`)
    } else {
      const wan = discovery?.wans.find((candidate) => candidate.interface === f.wanInterface)
      if (!wan?.pointToPoint && !f.gateway.trim()) {
        errors.push(`${f.family.toUpperCase()} 为普通接口且未发现唯一网关，必须填写下一跳网关 IP`)
      }
    }
  }
  return errors
}

export function ensureFamily(draft: PolicyEgressDraft, family: PolicyAddressFamily): PolicyEgressFamily {
  const existing = draft.families.find((f) => f.family === family)
  if (existing) return existing
  const newFamily: PolicyEgressFamily = {
    family,
    enabled: false,
    wanInterface: '',
    gateway: '',
    routeTable: '',
    routeMode: '',
    natMode: '',
    wanSource: '',
  }
  draft.families.push(newFamily)
  return newFamily
}

export function toggleFamily(draft: PolicyEgressDraft, family: PolicyAddressFamily): void {
  const f = ensureFamily(draft, family)
  f.enabled = !f.enabled
}
