import { useState } from 'react'
import { Badge } from '../../../ui/Badge'
import { Button } from '../../../ui/Button'
import { Modal } from '../../../ui/Modal'
import type { InternetEgressCandidates } from '../api'

type InternetEgressModalProps = {
  candidates: InternetEgressCandidates
  busy?: boolean
  onClose: () => void
  /** receives selections as { family: [interface] } for the sync request */
  onSubmit: (selection: Record<string, string[]>) => void
}

function familyLabel(family: string): string {
  return family.toLowerCase() === 'ipv6' ? 'IPv6' : 'IPv4'
}

/**
 * 「整个互联网」阻断需要运营商确认互联网出口接口 — RouterOS 没有以
 * rosboard 可证明安全的形式暴露默认路由时，由用户显式选择。
 */
export function InternetEgressModal({ candidates, busy = false, onClose, onSubmit }: InternetEgressModalProps) {
  const families = Object.keys(candidates).sort()
  const [selection, setSelection] = useState<Record<string, string>>(() => {
    const initial: Record<string, string> = {}
    for (const family of families) {
      const list = candidates[family]
      if (list.length === 1) initial[family] = list[0].interface
    }
    return initial
  })
  const complete = families.every((family) => selection[family])

  return (
    <Modal
      open
      onClose={() => {
        if (!busy) onClose()
      }}
      title="选择互联网出口接口"
      maxWidth={560}
      footer={
        <>
          <Button disabled={busy} onClick={onClose}>
            取消
          </Button>
          <Button variant="primary" loading={busy} disabled={!complete} onClick={() => onSubmit(Object.fromEntries(Object.entries(selection).map(([family, iface]) => [family, [iface]])))}>
            确认并重新同步
          </Button>
        </>
      }
    >
      <p className="pol-hint">
        RouterOS 未能以可验证的方式暴露默认路由，「整个互联网」阻断规则需要你显式确认每个协议族的互联网出口接口。确认后会重新执行同步。
      </p>
      {families.map((family) => (
        <div key={family} className="pol-egr-pick-group">
          <h4 className="pol-section-title">{familyLabel(family)} 出口接口</h4>
          <div className="pol-choice-list" role="radiogroup" aria-label={`${familyLabel(family)} 出口接口`}>
            {candidates[family].map((candidate) => (
              <label key={candidate.interface} className={`pol-choice${selection[family] === candidate.interface ? ' pol-choice-active' : ''}`}>
                <input
                  type="radio"
                  checked={selection[family] === candidate.interface}
                  disabled={busy}
                  onChange={() => setSelection((current) => ({ ...current, [family]: candidate.interface }))}
                />
                <span>
                  <strong>
                    {candidate.interface}
                    {candidate.running ? <Badge tone="ok">运行中</Badge> : <Badge tone="neutral">未运行</Badge>}
                  </strong>
                  <small>
                    {candidate.type || '未知类型'}
                    {candidate.reason ? ` · ${candidate.reason}` : ''}
                  </small>
                </span>
              </label>
            ))}
          </div>
        </div>
      ))}
    </Modal>
  )
}
