import { SubjectSelector } from './Selectors'
import { PolicyNotice } from '../policy-routing/components'
import { parseSourcePrefixLines, type RoutingInterfaceSource } from './source'
import type { PolicyDiscovery, PolicySourceSelectors, PolicyTerminal, RoutingSourceKind, Subject, TrafficIngressScope } from './canonical'

export type RoutingSourceSelectionKind = RoutingSourceKind | 'legacy'

type Props = {
  sourceKind: RoutingSourceSelectionKind
  interfaceSource: RoutingInterfaceSource
  excludeText: string
  ipText: string
  sourceSelectors: PolicySourceSelectors | null
  sourceSelectorError: string | null
  discovery: PolicyDiscovery | null
  trafficIngress: TrafficIngressScope
  subject: Subject
  terminals: PolicyTerminal[]
  busy: boolean
  onKindChange: (kind: RoutingSourceSelectionKind) => void
  onInterfaceSource: (next: RoutingInterfaceSource) => void
  onExcludeText: (text: string) => void
  onIpText: (text: string) => void
  onIngress: (candidate: PolicyDiscovery['trafficIngress'][number]) => void
  onSubject: (subject: Subject) => void
}

const labels: Partial<Record<RoutingSourceSelectionKind, string>> = { interface: '指定接口', device: '指定终端', ip: '指定 IP 范围' }

export function RoutingSourcePicker({ sourceKind, interfaceSource, excludeText, ipText, sourceSelectors, sourceSelectorError, discovery, trafficIngress, subject, terminals, busy, onKindChange, onInterfaceSource, onExcludeText, onIpText, onIngress, onSubject }: Props) {
  const selectedLists = new Set(trafficIngress.interfaceLists)
  const factNotice = sourceSelectors && !sourceSelectors.available
    ? sourceSelectors.reason || '无法读取 RouterOS 接口事实；请刷新后重试。'
    : sourceSelectors?.factStatus === 'partial'
      ? '部分 RouterOS selector 事实暂不可用；已返回的事实仍可选择，apply 时会再次 fresh preflight。'
      : null
  return <section className="policy-section-card">
    <h4 className="policy-section-card-title">来源范围</h4>
    {sourceKind === 'legacy' ? <>
      <p className="policy-hint">这是旧规则兼容编辑模式，仅保留原有 Subject + TrafficIngress matcher 语义；它不是新规则的来源资格门。</p>
      <LegacyIngress discovery={discovery} trafficIngress={trafficIngress} selectedLists={selectedLists} subject={subject} busy={busy} onIngress={onIngress} />
      <SubjectSelector terminals={terminals} value={subject} allowExcluded excludedDisabled={!hasIngress(trafficIngress)} requireObservedAddress onChange={onSubject} />
    </> : <>
      <p className="policy-hint">来源范围三选一：指定接口（可多选并排除个别地址）、指定终端或指定 IP 范围；推荐分析不决定选择资格。</p>
      <div className="policy-choice-list" role="radiogroup" aria-label="来源范围类型">{(['interface', 'device', 'ip'] as const).map((kind) => <label key={kind} className={`policy-choice${sourceKind === kind ? ' active' : ''}`}><input type="radio" checked={sourceKind === kind} disabled={busy} onChange={() => onKindChange(kind)} /><span><strong>{labels[kind]}</strong><small>{description(kind)}</small></span></label>)}</div>
      {sourceSelectorError ? <PolicyNotice tone="warn" title="RouterOS 来源事实读取失败">{sourceSelectorError}</PolicyNotice> : null}
      {factNotice ? <PolicyNotice tone="warn" title="来源事实状态">{factNotice}</PolicyNotice> : null}
      {sourceSelectors?.warnings.length ? <p className="policy-hint">推荐分析：{sourceSelectors.warnings.slice(0, 2).join('；')}</p> : null}
      {sourceKind === 'interface' ? <>
        <SelectorCheckList title="RouterOS 接口" selected={interfaceSource.interfaces} items={sourceSelectors?.interfaces ?? []} busy={busy} onToggle={(name) => onInterfaceSource({ ...interfaceSource, interfaces: toggleName(interfaceSource.interfaces, name) })} />
        <SelectorCheckList title="RouterOS 接口列表" selected={interfaceSource.interfaceLists} items={sourceSelectors?.interfaceLists ?? []} busy={busy} onToggle={(name) => onInterfaceSource({ ...interfaceSource, interfaceLists: toggleName(interfaceSource.interfaceLists, name) })} />
        <label className="policy-field canonical-prefix-field"><span>排除以下来源 IP / CIDR（可选）</span><textarea className="settings-input policy-textarea" rows={3} value={excludeText} disabled={busy} onChange={(event) => onExcludeText(event.target.value)} placeholder={'10.0.0.10\n10.0.0.0/24\nfd86::/64'} /><small>仅「指定接口」可用；每行一个 IPv4、IPv6、IPv4 CIDR 或 IPv6 CIDR。命中所选接口但来源地址在排除列表中的流量不匹配本规则。</small></label>
      </> : null}
      {sourceKind === 'device' ? <SubjectSelector terminals={terminals} value={subject} selectedOnly requireObservedAddress onChange={onSubject} /> : null}
      {sourceKind === 'ip' ? <label className="policy-field canonical-prefix-field"><span>来源 IP / CIDR</span><textarea className="settings-input policy-textarea" rows={4} value={ipText} disabled={busy} onChange={(event) => onIpText(event.target.value)} placeholder={'10.0.0.10\n10.0.0.0/24\nfd86::/64'} /><small>每行一个 IPv4、IPv6、IPv4 CIDR 或 IPv6 CIDR，可回车换行；后端会按地址族严格校验。当前共 {parseSourcePrefixLines(ipText).length} 条。</small></label> : null}
    </>}
  </section>
}

function toggleName(values: string[], name: string): string[] {
  return values.includes(name) ? values.filter((value) => value !== name) : [...values, name]
}

function description(kind: RoutingSourceKind) {
  if (kind === 'interface') return '匹配从所选接口或接口列表进入的流量，可多选；可再排除个别来源地址。'
  if (kind === 'device') return '复用已识别终端与 identity/IP binding，勾选即可多选。'
  if (kind === 'ip') return '直接匹配来源地址或网段，不依赖 TrafficIngress。'
  return ''
}

function hasIngress(scope: TrafficIngressScope) { return scope.interfaceLists.length + scope.interfaces.length > 0 }

function LegacyIngress({ discovery, trafficIngress, selectedLists, subject, busy, onIngress }: { discovery: PolicyDiscovery | null; trafficIngress: TrafficIngressScope; selectedLists: Set<string>; subject: Subject; busy: boolean; onIngress: (candidate: PolicyDiscovery['trafficIngress'][number]) => void }) {
  const kindLabel: Record<string, string> = { 'interface-list': '接口列表', bridge: 'Bridge', vlan: 'VLAN', wireguard: 'WireGuard', vpn: 'VPN', tunnel: '隧道', physical: '物理接口' }
  return <><p className="policy-hint">旧规则入口兼容层：仅用于保留已有 Subject + ingress 语义；推荐分析缺失不会改变已保存的旧 matcher。</p><div className="policy-choice-list">{(discovery?.trafficIngress ?? []).map((candidate) => { const selected = candidate.kind === 'interface-list' ? trafficIngress.interfaceLists.includes(candidate.name) : trafficIngress.interfaces.includes(candidate.name); const covered = candidate.kind !== 'interface-list' && candidate.coveredBy.some((name) => selectedLists.has(name)); return <label key={`${candidate.kind}:${candidate.name}`} className={`policy-choice${selected || covered ? ' active' : ''}${covered ? ' policy-choice-disabled' : ''}`}><input type="checkbox" checked={selected || covered} disabled={covered || busy} onChange={() => onIngress(candidate)} /><span><strong>{candidate.name}</strong><small>{kindLabel[candidate.kind] ?? candidate.kind}{candidate.addresses.length ? ` · ${candidate.addresses.join(', ')}` : ''}{candidate.reason ? ` · ${candidate.reason}` : ''}</small></span></label> })}</div>{discovery && !discovery.trafficIngress.length ? <p className="policy-hint">没有发现旧版入口推荐；已保存的旧入口仍按原有 matcher 语义保留，创建新规则请使用 typed source。</p> : null}{(subject.mode === 'all' || subject.mode === 'excluded') && !hasIngress(trafficIngress) ? <PolicyNotice tone="warn">旧规则的全部/排除来源仍需要保留有效 TrafficIngress。</PolicyNotice> : null}</>
}

function SelectorCheckList({ title, selected, items, busy, onToggle }: { title: string; selected: string[]; items: PolicySourceSelectors['interfaces'] | PolicySourceSelectors['interfaceLists']; busy: boolean; onToggle: (name: string) => void }) {
  return <div className="policy-choice-list"><h5>{title}</h5>{items.map((item) => <label key={item.name} className={`policy-choice${selected.includes(item.name) ? ' active' : ''}`}><input type="checkbox" checked={selected.includes(item.name)} disabled={busy} onChange={() => onToggle(item.name)} /><span><strong>{item.name}</strong><small>{item.recommended ? '推荐' : '非推荐但可选'}{item.reason ? ` · ${item.reason}` : ''}</small>{item.warnings.map((warning) => <small key={warning}>⚠ {warning}</small>)}</span></label>)}{!items.length ? <p className="policy-hint">暂无 RouterOS 事实；请刷新后重试。</p> : null}</div>
}
