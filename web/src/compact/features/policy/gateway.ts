import type { PolicyDiscovery } from './canonical'

type DiscoveryWAN = PolicyDiscovery['wans'][number]
type DiscoveryRoute = DiscoveryWAN['routes'][number]

function isMainPolicyRoute(route: DiscoveryRoute) {
  return !route.table.trim() || route.table.trim().toLowerCase() === 'main'
}

function policyRouteGateway(route: DiscoveryRoute, family: 'ipv4' | 'ipv6') {
  for (const raw of [route.gateway, route.immediateGateway]) {
    const value = raw.trim()
    const address = value.includes('%') ? value.slice(0, value.indexOf('%')) : value
    if (family === 'ipv4' ? address.includes('.') : address.includes(':')) return family === 'ipv6' ? value : address
  }
  return ''
}

export function gatewayCandidatesForWAN(wan: DiscoveryWAN | undefined, family: 'ipv4' | 'ipv6') {
  if (!wan) return []
  return wan.routes
    .filter((route) => route.family === family && route.active && route.proven && isMainPolicyRoute(route))
    .map((route) => policyRouteGateway(route, family))
    .filter(Boolean)
    .filter((gateway, index, values) => values.indexOf(gateway) === index)
}

export function suggestedGatewayForWAN(wan: DiscoveryWAN | undefined, family: 'ipv4' | 'ipv6') {
  if (!wan?.pointToPoint) {
    const candidates = gatewayCandidatesForWAN(wan, family)
    if (candidates.length === 1) return candidates[0]
  }
  return ''
}
