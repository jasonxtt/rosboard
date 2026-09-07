type SegTabsProps<T extends string> = {
  options: Array<{ value: T; label: string }>
  value: T
  onChange: (value: T) => void
  ariaLabel: string
  className?: string
}

/** Pill segmented control — window pickers, in-page view tabs (§6/§8). */
export function SegTabs<T extends string>({ options, value, onChange, ariaLabel, className = '' }: SegTabsProps<T>) {
  return (
    <div className={`seg ${className}`.trim()} role="tablist" aria-label={ariaLabel}>
      {options.map((option) => (
        <button
          key={option.value}
          type="button"
          role="tab"
          aria-selected={option.value === value}
          className={option.value === value ? 'seg-active' : undefined}
          onClick={() => onChange(option.value)}
        >
          {option.label}
        </button>
      ))}
    </div>
  )
}
