import type { Egress, EgressFamily, PolicyDiscovery } from '../canonical'

export const NEXT_HOP_VALUE = '__rosboard_next_hop__'

export function defaultEgressFamily(family: 'ipv4' | 'ipv6'): EgressFamily {
  return { family, enabled: family === 'ipv4', wanInterface: '', gateway: '', routeTable: '', routeMode: '', natMode: '', wanSource: '' }
}

export function defaultEgressDraft(): Egress {
  return {
    id: '',
    name: '',
    priority: 100,
    listMode: 'shared',
    listName: '',
    dnsUpstream: '1.1.1.1',
    fakeAlias: '',
    failureMode: 'strict',
    routerOutput: false,
    enabled: true,
    pendingDeletion: false,
    revision: 0,
    applied: false,
    families: [defaultEgressFamily('ipv4')],
  }
}

export function egressDraftFrom(egress: Egress): Egress {
  return { ...egress, families: egress.families.length ? egress.families.map((family) => ({ ...family })) : [defaultEgressFamily('ipv4')] }
}

/** Client-side egress validation mirrored from the old wizard (server re-validates). */
export function egressDraftErrors(draft: Egress, discovery: PolicyDiscovery | null, requireName: boolean): string[] {
  const errors: string[] = []
  if (requireName && !draft.name.trim()) errors.push('出口名称不能为空')
  const enabledFamilies = draft.families.filter((family) => family.enabled)
  if (!enabledFamilies.length) errors.push('至少启用一个协议族')
  for (const family of enabledFamilies) {
    const label = family.family.toUpperCase()
    if (family.wanSource === 'next-hop') {
      if (!family.gateway.trim()) errors.push(`${label} 下一跳模式必须填写网关 IP`)
      continue
    }
    if (!family.wanInterface.trim()) {
      if (!family.gateway.trim()) errors.push(`${label} 必须选择已发现的 WAN 接口或填写网关`)
      continue
    }
    const wan = discovery?.wans.find((candidate) => candidate.interface === family.wanInterface)
    if (discovery?.available && !wan) errors.push(`${label} 的 WAN 接口未被设备发现`)
    else if (wan && !wan.pointToPoint && !family.gateway.trim()) errors.push(`${label} 未发现唯一下一跳，请填写网关 IP`)
  }
  return errors
}
