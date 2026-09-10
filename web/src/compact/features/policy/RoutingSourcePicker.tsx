import { SubjectSelector } from './Selectors'
import { PolicyNotice } from '../policy-routing/components'
import type { PolicyDiscovery, PolicySourceSelectors, PolicyTerminal, RoutingSourceKind, Subject, TrafficIngressScope } from './canonical'

export type RoutingSourceSelectionKind = RoutingSourceKind | 'legacy'

type Props = {
  sourceKind: RoutingSourceSelectionKind
  sourceName: string
  sourceSelectors: PolicySourceSelectors | null
  sourceSelectorError: string | null
  discovery: PolicyDiscovery | null
  trafficIngress: TrafficIngressScope
  subject: Subject
  terminals: PolicyTerminal[]
  busy: boolean
  onKindChange: (kind: RoutingSourceSelectionKind) => void
  onNameChange: (name: string) => void
  onIngress: (candidate: PolicyDiscovery['trafficIngress'][number]) => void
  onSubject: (subject: Subject) => void
}

const labels: Record<string, string> = { device: 'Device · 指定终端', ip: 'IP / CIDR', interface: 'Interface · 接口', 'interface-list': 'InterfaceList · 接口列表', all: 'All · 全部来源' }

export function RoutingSourcePicker({ sourceKind, sourceName, sourceSelectors, sourceSelectorError, discovery, trafficIngress, subject, terminals, busy, onKindChange, onNameChange, onIngress, onSubject }: Props) {
  const selectedLists = new Set(trafficIngress.interfaceLists)
  const factNotice = sourceSelectors && !sourceSelectors.available
    ? sourceSelectors.reason || '无法读取 RouterOS 接口事实；请刷新后重试。'
    : sourceSelectors?.factStatus === 'partial'
      ? '部分 RouterOS selector 事实暂不可用；已返回的事实仍可选择，apply 时会再次 fresh preflight。'
      : null
  return <section className="policy-section-card">
    <h4 className="policy-section-card-title">来源范围</h4>
    {sourceKind === 'legacy' ? <>
      <p className="policy-hint">这是旧规则兼容编辑模式，保留原有 Subject + TrafficIngress 语义。</p>
      <LegacyIngress discovery={discovery} trafficIngress={trafficIngress} selectedLists={selectedLists} subject={subject} busy={busy} onIngress={onIngress} />
      <SubjectSelector terminals={terminals} value={subject} allowExcluded excludedDisabled={!hasIngress(trafficIngress)} requireObservedAddress onChange={onSubject} />
    </> : <>
      <p className="policy-hint">来源范围直接表达为 Device、IP/CIDR、Interface 或 InterfaceList；推荐分析不决定选择资格。</p>
      <div className="policy-choice-list" role="radiogroup" aria-label="来源范围类型">{(['device', 'ip', 'interface', 'interface-list', 'all'] as const).map((kind) => <label key={kind} className={`policy-choice${sourceKind === kind ? ' active' : ''}`}><input type="radio" checked={sourceKind === kind} disabled={busy} onChange={() => onKindChange(kind)} /><span><strong>{labels[kind]}</strong><small>{description(kind)}</small></span></label>)}</div>
      {sourceSelectorError ? <PolicyNotice tone="warn" title="RouterOS 来源事实读取失败">{sourceSelectorError}</PolicyNotice> : null}
      {factNotice ? <PolicyNotice tone="warn" title="来源事实状态">{factNotice}</PolicyNotice> : null}
      {sourceSelectors?.warnings.length ? <p className="policy-hint">推荐分析：{sourceSelectors.warnings.slice(0, 2).join('；')}</p> : null}
      {sourceKind === 'device' ? <SubjectSelector terminals={terminals} value={subject} selectedOnly requireObservedAddress onChange={onSubject} /> : null}
      {sourceKind === 'ip' ? <label className="policy-field canonical-prefix-field"><span>来源 IP / CIDR</span><textarea className="settings-input policy-textarea" rows={4} value={subject.prefixes.join('\n')} disabled={busy} onChange={(event) => onSubject({ mode: 'selected', members: [], prefixes: event.target.value.split(/\r?\n/).map((item) => item.trim()).filter(Boolean) })} placeholder={'10.0.0.10\n10.0.0.0/24\nfd86::/64'} /><small>每行一个 IPv4、IPv6、IPv4 CIDR 或 IPv6 CIDR；后端会按地址族严格校验。</small></label> : null}
      {sourceKind === 'interface' ? <SelectorList title="RouterOS 接口" selected={sourceName} items={sourceSelectors?.interfaces ?? []} busy={busy} onSelect={onNameChange} /> : null}
      {sourceKind === 'interface-list' ? <SelectorList title="RouterOS 接口列表" selected={sourceName} items={sourceSelectors?.interfaceLists ?? []} busy={busy} onSelect={onNameChange} /> : null}
      {sourceKind === 'all' ? <PolicyNotice tone="warn" title="All 当前为安全延后项">当前 prerouting/output、loop prevention 与管理流量边界尚未完成安全证明；计划会明确 blocker，不会写入 broad matcher。</PolicyNotice> : null}
    </>}
  </section>
}

function description(kind: RoutingSourceKind) {
  if (kind === 'device') return '复用已识别终端与 identity/IP binding。'
  if (kind === 'ip') return '直接匹配来源地址或网段，不依赖 TrafficIngress。'
  if (kind === 'interface') return '直接匹配 RouterOS in-interface；推荐只是提示。'
  if (kind === 'interface-list') return '直接匹配 RouterOS in-interface-list；事实列表不隐藏。'
  return '一期保留模型入口，但 apply 安全门仍延后。'
}

function hasIngress(scope: TrafficIngressScope) { return scope.interfaceLists.length + scope.interfaces.length > 0 }

function LegacyIngress({ discovery, trafficIngress, selectedLists, subject, busy, onIngress }: { discovery: PolicyDiscovery | null; trafficIngress: TrafficIngressScope; selectedLists: Set<string>; subject: Subject; busy: boolean; onIngress: (candidate: PolicyDiscovery['trafficIngress'][number]) => void }) {
  const kindLabel: Record<string, string> = { 'interface-list': '接口列表', bridge: 'Bridge', vlan: 'VLAN', wireguard: 'WireGuard', vpn: 'VPN', tunnel: '隧道', physical: '物理接口' }
  return <><p className="policy-hint">旧规则入口兼容层：仅用于保留已有 Subject + ingress 语义。</p><div className="policy-choice-list">{(discovery?.trafficIngress ?? []).map((candidate) => { const selected = candidate.kind === 'interface-list' ? trafficIngress.interfaceLists.includes(candidate.name) : trafficIngress.interfaces.includes(candidate.name); const covered = candidate.kind !== 'interface-list' && candidate.coveredBy.some((name) => selectedLists.has(name)); return <label key={`${candidate.kind}:${candidate.name}`} className={`policy-choice${selected || covered ? ' active' : ''}${covered ? ' policy-choice-disabled' : ''}`}><input type="checkbox" checked={selected || covered} disabled={covered || busy} onChange={() => onIngress(candidate)} /><span><strong>{candidate.name}</strong><small>{kindLabel[candidate.kind] ?? candidate.kind}{candidate.addresses.length ? ` · ${candidate.addresses.join(', ')}` : ''}{candidate.reason ? ` · ${candidate.reason}` : ''}</small></span></label> })}</div>{(subject.mode === 'all' || subject.mode === 'excluded') && !hasIngress(trafficIngress) ? <PolicyNotice tone="warn">旧规则的全部/排除来源仍需要保留有效 TrafficIngress。</PolicyNotice> : null}</>
}

function SelectorList({ title, selected, items, busy, onSelect }: { title: string; selected: string; items: PolicySourceSelectors['interfaces'] | PolicySourceSelectors['interfaceLists']; busy: boolean; onSelect: (name: string) => void }) {
  return <div className="policy-choice-list"><h5>{title}</h5>{items.map((item) => <label key={item.name} className={`policy-choice${selected === item.name ? ' active' : ''}`}><input type="radio" name={`routing-source-${title}`} checked={selected === item.name} disabled={busy} onChange={() => onSelect(item.name)} /><span><strong>{item.name}</strong><small>{item.recommended ? '推荐' : '非推荐但可选'}{item.reason ? ` · ${item.reason}` : ''}</small>{item.warnings.map((warning) => <small key={warning}>⚠ {warning}</small>)}</span></label>)}{!items.length ? <p className="policy-hint">暂无 RouterOS 事实；请刷新后重试。</p> : null}</div>
}
