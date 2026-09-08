import type { Egress, RoutingRule } from '../canonical.ts'

/** Plain structural status (kept React-free so node --test can import it). */
export type RoutingRuleStatus = { tone: 'ok' | 'warn' | 'err' | 'neutral'; label: string }

/**
 * Routing-rule display status. `desiredMismatch` is the page-level signal
 * from /api/policy-routing/overview `applied === false`: the global desired
 * revision has not landed on RouterOS, so no rule may claim 已应用 — even
 * when its own egress is applied (e.g. a quick toggle whose auto-apply
 * failed after the desired state was saved).
 */
export function routingRuleStatus(rule: RoutingRule, egress: Egress | undefined, desiredMismatch: boolean): RoutingRuleStatus {
  if (!rule.enabled) return { tone: 'neutral', label: '已停用' }
  if (!egress) return { tone: 'err', label: '出口缺失' }
  if (egress.pendingDeletion) return { tone: 'warn', label: '出口待删除' }
  if (!egress.enabled) return { tone: 'warn', label: '出口已停用' }
  if (!egress.applied || desiredMismatch) return { tone: 'warn', label: '待应用' }
  return { tone: 'ok', label: '已应用' }
}
