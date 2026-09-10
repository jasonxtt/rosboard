import { useMemo, useState } from 'react'
import { SearchInput } from '../../../ui/SearchInput'
import { Select } from '../../../ui/inputs'
import { Textarea } from '../../../ui/inputs'
import type { PolicyTerminal, Subject, SubjectBinding } from '../canonical'

const emptySubject: Subject = { mode: 'selected', members: [], prefixes: [] }

function terminalAddresses(terminal: PolicyTerminal, routingOnly: boolean) {
  return routingOnly ? { ipv4: terminal.routingIpv4, ipv6: terminal.routingIpv6 } : { ipv4: terminal.ipv4, ipv6: terminal.ipv6 }
}

type SubjectSelectorProps = {
  terminals: PolicyTerminal[]
  value?: Subject
  onChange: (value: Subject) => void
  /** 策略路由 allows 排除 mode; 访问控制 only 全部/指定 */
  allowExcluded?: boolean
  excludedDisabled?: boolean
  /** routing rules only see terminals with routing-usable addresses */
  requireObservedAddress?: boolean
  /** Typed Device sources only allow an explicit terminal selection. */
  selectedOnly?: boolean
}

/** 来源 picker: 全部终端 / 指定终端 (multi-select + binding + pinned IPs) / 排除 + manual prefixes. */
export function SubjectSelector({ terminals, value = emptySubject, onChange, allowExcluded = false, excludedDisabled = false, requireObservedAddress = false, selectedOnly = false }: SubjectSelectorProps) {
  const [query, setQuery] = useState('')
  const selected = new Map(value.members.map((member) => [member.terminalId, member]))
  const visibleTerminals = useMemo(() => {
    const candidates = terminals.filter((terminal) => {
      // RouterOS 自身的 conntrack 追踪项（routeros:self）不参与规则来源选择
      if (terminal.id === 'routeros:self') return false
      const addresses = terminalAddresses(terminal, requireObservedAddress)
      return !requireObservedAddress || addresses.ipv4.length + addresses.ipv6.length > 0
    })
    const keyword = query.trim().toLowerCase()
    if (!keyword) return candidates
    return candidates.filter((terminal) => {
      const addresses = terminalAddresses(terminal, requireObservedAddress)
      return [terminal.displayName, terminal.id, terminal.macAddress, ...addresses.ipv4, ...addresses.ipv6].join(' ').toLowerCase().includes(keyword)
    })
  }, [query, requireObservedAddress, terminals])

  const update = (next: Partial<Subject>) => onChange({ ...value, ...next, members: next.members ?? value.members, prefixes: next.prefixes ?? value.prefixes })
  const toggleTerminal = (terminal: PolicyTerminal) => {
    const members = [...value.members]
    const index = members.findIndex((member) => member.terminalId === terminal.id)
    if (index >= 0) members.splice(index, 1)
    else {
      const addresses = terminalAddresses(terminal, requireObservedAddress)
      const binding: SubjectBinding = terminal.autoEligible ? 'auto' : 'fixed'
      members.push({ terminalId: terminal.id, binding, pinnedIpv4: binding === 'fixed' ? [...addresses.ipv4] : [], pinnedIpv6: binding === 'fixed' ? [...addresses.ipv6] : [] })
    }
    update({ members })
  }
  const changeBinding = (terminal: PolicyTerminal, binding: SubjectBinding) => {
    const addresses = terminalAddresses(terminal, requireObservedAddress)
    const members = value.members.map((member) =>
      member.terminalId === terminal.id
        ? { ...member, binding, pinnedIpv4: binding === 'fixed' ? [...addresses.ipv4] : [], pinnedIpv6: binding === 'fixed' ? [...addresses.ipv6] : [] }
        : member,
    )
    update({ members })
  }

  const modeOptions = selectedOnly ? null : (
    <div className="pol-choice-list pol-choice-row" role="radiogroup" aria-label="来源范围">
      <label className={`pol-choice${value.mode === 'all' ? ' pol-choice-active' : ''}`}>
        <input type="radio" checked={value.mode === 'all'} onChange={() => update({ mode: 'all', members: [], prefixes: [] })} />
        <span>
          <strong>全部终端</strong>
          <small>{allowExcluded ? '由策略入口限定范围' : '设备上的所有终端'}</small>
        </span>
      </label>
      <label className={`pol-choice${value.mode === 'selected' ? ' pol-choice-active' : ''}`}>
        <input type="radio" checked={value.mode === 'selected'} onChange={() => update({ mode: 'selected' })} />
        <span>
          <strong>指定终端 / 地址</strong>
          <small>只匹配下面勾选的终端和手动地址</small>
        </span>
      </label>
      {allowExcluded ? (
        <label className={`pol-choice${value.mode === 'excluded' ? ' pol-choice-active' : ''}${excludedDisabled ? ' pol-choice-disabled' : ''}`}>
          <input type="radio" checked={value.mode === 'excluded'} disabled={excludedDisabled} onChange={() => update({ mode: 'excluded' })} />
          <span>
            <strong>排除终端 / 地址</strong>
            <small>{excludedDisabled ? '需要先选择有效的策略入口' : '先匹配策略入口，再排除下面的终端和地址'}</small>
          </span>
        </label>
      ) : null}
    </div>
  )

  return (
    <div className="pol-selector">
      {modeOptions}
      {value.mode === 'selected' || value.mode === 'excluded' ? (
        <>
          <SearchInput value={query} onChange={setQuery} placeholder="搜索名称、IP 或 MAC" ariaLabel="搜索终端" />
          <div className="pol-terminal-list">
            {visibleTerminals.map((terminal) => {
              const member = selected.get(terminal.id)
              const addresses = terminalAddresses(terminal, requireObservedAddress)
              return (
                <div key={terminal.id} className={`pol-terminal-option${member ? ' pol-terminal-selected' : ''}`}>
                  <div className="pol-terminal-main">
                    <label>
                      <input type="checkbox" checked={Boolean(member)} onChange={() => toggleTerminal(terminal)} />
                      <span>
                        <strong>{terminal.displayName || terminal.id}</strong>
                        <small>{addresses.ipv4.join(', ') || '无 IPv4'} · {addresses.ipv6.join(', ') || '无 IPv6'}</small>
                        <small>{terminal.macAddress || '无 MAC'}</small>
                      </span>
                    </label>
                    {member ? (
                      <Select
                        className="pol-binding-select"
                        value={member.binding}
                        onChange={(binding) => changeBinding(terminal, binding as SubjectBinding)}
                        options={[
                          { value: 'auto', label: '自动跟随' },
                          { value: 'fixed', label: '固定当前地址' },
                        ]}
                        ariaLabel="地址绑定方式"
                      />
                    ) : null}
                  </div>
                  {member && requireObservedAddress && !terminal.autoEligible ? <small className="pol-hint">未获取到可靠 MAC，无法自动跟随 IP 变化，已固定使用当前地址。</small> : null}
                </div>
              )
            })}
            {!visibleTerminals.length ? <p className="pol-hint">没有匹配的终端。</p> : null}
          </div>
          {!selectedOnly ? (
            <label className="pol-prefix-field">
              <span className="field-label">{value.mode === 'excluded' ? '排除地址 / CIDR' : '手动地址 / CIDR'}</span>
              <Textarea
                rows={3}
                value={value.prefixes.join('\n')}
                onChange={(text) => update({ prefixes: text.split(/\r?\n/).map((item) => item.trim()).filter(Boolean) })}
                placeholder={'192.168.1.50\n192.168.1.0/24\n2001:db8::/64'}
                ariaLabel="手动地址或 CIDR"
              />
              <small className="faint">每行一个 IPv4、IPv6、IPv4 CIDR 或 IPv6 CIDR；保存时后端会执行严格校验。</small>
            </label>
          ) : null}
        </>
      ) : null}
    </div>
  )
}
