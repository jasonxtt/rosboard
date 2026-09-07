import { Field, Textarea } from '../../ui'
import type { ScopeOverrideDraft } from './drafts'

type ScopeEditorProps = {
  value: ScopeOverrideDraft
  onChange: (field: keyof ScopeOverrideDraft, value: string) => void
  disabled?: boolean
}

/**
 * Advanced TrafficScope/TerminalScope override editors (流量采集覆盖 /
 * 终端范围覆盖). 留空即使用自动识别；每行一项。Used by the manual-add
 * wizard, the edit form, and the verification dialog.
 */
export function ScopeEditor({ value, onChange, disabled = false }: ScopeEditorProps) {
  const field = (name: keyof ScopeOverrideDraft, label: string, placeholder?: string) => (
    <Field key={name} label={label}>
      <Textarea
        rows={2}
        value={value[name]}
        placeholder={placeholder}
        disabled={disabled}
        ariaLabel={label}
        onChange={(next) => onChange(name, next)}
      />
    </Field>
  )
  return (
    <div className="scope-editor">
      <p className="scope-editor-help">仅在自动识别结果不符合实际拓扑时填写；每行一项，留空使用自动识别。</p>
      <section className="scope-editor-section" aria-label="流量采集覆盖">
        <h4>流量采集覆盖</h4>
        <div className="scope-editor-grid">
          {field('trafficIncludeInterfaces', '强制纳入采集接口', 'ether1')}
          {field('trafficExcludeInterfaces', '强制排除采集接口')}
        </div>
      </section>
      <section className="scope-editor-section" aria-label="终端范围覆盖">
        <h4>终端范围覆盖</h4>
        <div className="scope-editor-grid">
          {field('includeInterfaces', '强制纳入 LAN 接口', 'bridge')}
          {field('excludeInterfaces', '强制排除接口')}
          {field('includeCidrs', '额外纳入 CIDR', '10.0.0.0/24')}
          {field('excludeCidrs', '排除 CIDR')}
        </div>
      </section>
    </div>
  )
}
