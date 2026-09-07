type ToggleProps = {
  checked: boolean
  onChange?: (checked: boolean) => void
  disabled?: boolean
  label?: string
}

/** 38×22 switch: --grad-ok when on, glass when off (§7 toggle). */
export function Toggle({ checked, onChange, disabled = false, label }: ToggleProps) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={label ?? (checked ? '已开启' : '已关闭')}
      className={`toggle${checked ? '' : ' toggle-off'}`}
      disabled={disabled}
      onClick={() => onChange?.(!checked)}
    />
  )
}
