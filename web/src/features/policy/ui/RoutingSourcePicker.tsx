import { Textarea } from '../../../ui/inputs'
import { Notice } from './Notice'
import { SubjectSelector } from './SubjectSelector'
import type { PolicyDiscovery, PolicySourceSelectors, PolicyTerminal, RoutingSourceKind, Subject, TrafficIngressScope } from '../canonical'

export type RoutingSourceSelectionKind = RoutingSourceKind | 'legacy'

type RoutingSourcePickerProps = {
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

const kindLabels: Record<string, string> = {
  device: 'Device · 指定终端',
  ip: 'IP / CIDR',
  interface: 'Interface · 接口',
  'interface-list': 'InterfaceList · 接口列表',
  all: 'All · 全部来源',
}

function factWarning(warnings: string[]): string | null {
  return warnings.length ? warnings.slice(0, 2).join('；') : null
}

export function RoutingSourcePicker({ sourceKind, sourceName, sourceSelectors, sourceSelectorError, discovery, trafficIngress, subject, terminals, busy, onKindChange, onNameChange, onIngress, onSubject }: RoutingSourcePickerProps) {
  const selectedLists = new Set(trafficIngress.interfaceLists)
  const selectedInterface = sourceKind === 'interface' ? sourceName : ''
  const selectedInterfaceList = sourceKind === 'interface-list' ? sourceName : ''
  const sourceFactNotice = sourceSelectors && !sourceSelectors.available
    ? sourceSelectors.reason || '无法读取 RouterOS 接口事实；请刷新后重试。'
    : sourceSelectors && sourceSelectors.factStatus === 'partial'
      ? '部分 RouterOS selector 事实暂不可用；已返回的事实仍可选择，apply 时会再次 fresh preflight。'
      : null

  return <section className="pol-section">
    <h4 className="pol-section-title">来源范围</h4>
    {sourceKind === 'legacy' ? <>
      <p className="pol-hint">这是旧规则的兼容编辑模式，仅保留原有 Subject + TrafficIngress matcher 语义；它不是新规则的来源资格门。新规则请使用下面的 typed source scope。</p>
      <LegacyIngressPicker discovery={discovery} trafficIngress={trafficIngress} selectedLists={selectedLists} subject={subject} busy={busy} onIngress={onIngress} />
      <SubjectSelector terminals={terminals} value={subject} allowExcluded excludedDisabled={!hasTrafficIngress(trafficIngress)} requireObservedAddress onChange={onSubject} />
    </> : <>
      <p className="pol-hint">来源范围直接表达为 Device、IP/CIDR、Interface 或 InterfaceList；TrafficIngress discovery 只提供事实与推荐，不决定选择资格。</p>
      <div className="pol-choice-list pol-choice-row" role="radiogroup" aria-label="来源范围类型">
        {(['device', 'ip', 'interface', 'interface-list', 'all'] as const).map((kind) => <label key={kind} className={`pol-choice${sourceKind === kind ? ' pol-choice-active' : ''}`}>
          <input type="radio" checked={sourceKind === kind} disabled={busy} onChange={() => onKindChange(kind)} />
          <span><strong>{kindLabels[kind]}</strong><small>{sourceKindDescription(kind)}</small></span>
        </label>)}
      </div>
      {sourceSelectorError ? <Notice tone="warn" title="RouterOS 来源事实读取失败">{sourceSelectorError}</Notice> : null}
      {sourceFactNotice ? <Notice tone="warn" title="来源事实状态">{sourceFactNotice}</Notice> : null}
      {sourceSelectors?.warnings.length ? <p className="pol-hint">推荐分析：{factWarning(sourceSelectors.warnings)}</p> : null}
      {sourceKind === 'device' ? <SubjectSelector terminals={terminals} value={subject} selectedOnly requireObservedAddress onChange={onSubject} /> : null}
      {sourceKind === 'ip' ? <label className="pol-prefix-field">
        <span className="field-label">来源 IP / CIDR</span>
        <Textarea rows={4} value={subject.prefixes.join('\n')} disabled={busy} onChange={(text) => onSubject({ mode: 'selected', members: [], prefixes: text.split(/\r?\n/).map((item) => item.trim()).filter(Boolean) })} placeholder={'10.0.0.10\n10.0.0.0/24\nfd86::/64'} ariaLabel="来源 IP 或 CIDR" />
        <small className="faint">每行一个 IPv4、IPv6、IPv4 CIDR 或 IPv6 CIDR；后端会按地址族严格校验。</small>
      </label> : null}
      {sourceKind === 'interface' ? <SelectorFacts title="RouterOS 接口" selectedName={selectedInterface} items={sourceSelectors?.interfaces ?? []} busy={busy} onSelect={onNameChange} /> : null}
      {sourceKind === 'interface-list' ? <SelectorListFacts title="RouterOS 接口列表" selectedName={selectedInterfaceList} items={sourceSelectors?.interfaceLists ?? []} busy={busy} onSelect={onNameChange} /> : null}
      {sourceKind === 'all' ? <Notice tone="warn" title="All 当前为安全延后项">来源事实不附加 source matcher，但当前 RouterOS prerouting/output、loop prevention 与管理流量边界尚未完成安全证明；计划会明确 blocker，不会写入 broad matcher。</Notice> : null}
    </>}
  </section>
}

function sourceKindDescription(kind: RoutingSourceKind): string {
  switch (kind) {
    case 'device': return '复用 rosboard 已识别终端与现有 identity/IP binding。'
    case 'ip': return '直接匹配来源地址或网段，不依赖 TrafficIngress。'
    case 'interface': return '直接匹配 RouterOS in-interface；推荐只是提示。'
    case 'interface-list': return '直接匹配 RouterOS in-interface-list；事实列表不隐藏。'
    case 'all': return '一期保留模型入口，但 apply 安全门仍延后。'
  }
}

function hasTrafficIngress(scope: TrafficIngressScope) {
  return scope.interfaceLists.length + scope.interfaces.length > 0
}

function LegacyIngressPicker({ discovery, trafficIngress, selectedLists, subject, busy, onIngress }: { discovery: PolicyDiscovery | null; trafficIngress: TrafficIngressScope; selectedLists: Set<string>; subject: Subject; busy: boolean; onIngress: (candidate: PolicyDiscovery['trafficIngress'][number]) => void }) {
  const kindLabel: Record<string, string> = { 'interface-list': '接口列表', bridge: 'Bridge', vlan: 'VLAN', wireguard: 'WireGuard', vpn: 'VPN', tunnel: '隧道', physical: '物理接口' }
  return <>
    <p className="pol-hint">旧规则入口兼容层：仅用于保留已有 Subject + ingress 语义；推荐分析缺失不会改变已保存的旧 matcher。</p>
    <div className="pol-choice-list">
      {(discovery?.trafficIngress ?? []).map((candidate) => {
        const selected = candidate.kind === 'interface-list' ? trafficIngress.interfaceLists.includes(candidate.name) : trafficIngress.interfaces.includes(candidate.name)
        const covered = candidate.kind !== 'interface-list' && candidate.coveredBy.some((name) => selectedLists.has(name))
        return <label key={`${candidate.kind}:${candidate.name}`} className={`pol-choice${selected || covered ? ' pol-choice-active' : ''}${covered ? ' pol-choice-disabled' : ''}`}>
          <input type="checkbox" checked={selected || covered} disabled={covered || busy} onChange={() => onIngress(candidate)} />
          <span><strong>{candidate.name}</strong><small>{kindLabel[candidate.kind] ?? candidate.kind}{candidate.addresses.length ? ` · ${candidate.addresses.join(', ')}` : ''}{candidate.reason ? ` · ${candidate.reason}` : ''}</small></span>
        </label>
      })}
      {discovery && !discovery.trafficIngress.length ? <p className="pol-hint">没有发现旧版入口推荐；已保存的旧入口仍按原有 matcher 语义保留，创建新规则请使用 typed source。</p> : null}
    </div>
    {(subject.mode === 'all' || subject.mode === 'excluded') && !hasTrafficIngress(trafficIngress) ? <Notice tone="warn">旧规则的全部/排除来源仍需要保留有效 TrafficIngress。</Notice> : null}
  </>
}

function SelectorFacts({ title, selectedName, items, busy, onSelect }: { title: string; selectedName: string; items: PolicySourceSelectors['interfaces']; busy: boolean; onSelect: (name: string) => void }) {
  return <div className="pol-choice-list"><h5 className="pol-section-subtitle">{title}</h5>{items.map((item) => <label key={item.name} className={`pol-choice${selectedName === item.name ? ' pol-choice-active' : ''}`}>
    <input type="radio" name="routing-source-interface" checked={selectedName === item.name} disabled={busy} onChange={() => onSelect(item.name)} />
    <span><strong>{item.name}</strong><small>{item.kind || item.type || 'interface'}{item.recommended ? ' · 推荐' : ' · 非推荐但可选'}{item.reason ? ` · ${item.reason}` : ''}</small>{item.warnings.map((warning) => <small key={warning} className="faint">⚠ {warning}</small>)}</span>
  </label>)}{!items.length ? <p className="pol-hint">暂无 RouterOS 接口事实；请刷新后重试。</p> : null}</div>
}

function SelectorListFacts({ title, selectedName, items, busy, onSelect }: { title: string; selectedName: string; items: PolicySourceSelectors['interfaceLists']; busy: boolean; onSelect: (name: string) => void }) {
  return <div className="pol-choice-list"><h5 className="pol-section-subtitle">{title}</h5>{items.map((item) => <label key={item.name} className={`pol-choice${selectedName === item.name ? ' pol-choice-active' : ''}`}>
    <input type="radio" name="routing-source-interface-list" checked={selectedName === item.name} disabled={busy} onChange={() => onSelect(item.name)} />
    <span><strong>{item.name}</strong><small>{item.recommended ? '推荐' : '非推荐但可选'}{item.reason ? ` · ${item.reason}` : ''}</small>{item.warnings.map((warning) => <small key={warning} className="faint">⚠ {warning}</small>)}</span>
  </label>)}{!items.length ? <p className="pol-hint">暂无 RouterOS 接口列表事实；请刷新后重试。</p> : null}</div>
}
