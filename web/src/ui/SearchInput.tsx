type SearchInputProps = {
  value: string
  onChange: (value: string) => void
  placeholder?: string
  ariaLabel?: string
  width?: number | string
}

/** Search field with icon and one-click clear. */
export function SearchInput({ value, onChange, placeholder = '搜索', ariaLabel = '搜索', width }: SearchInputProps) {
  return (
    <span className="search-input" style={width ? { width } : undefined}>
      <span className="search-icon" aria-hidden="true">
        ⌕
      </span>
      <input
        className="input"
        type="search"
        value={value}
        placeholder={placeholder}
        aria-label={ariaLabel}
        onChange={(event) => onChange(event.target.value)}
      />
      {value ? (
        <button type="button" className="search-clear" aria-label="清除搜索" onClick={() => onChange('')}>
          ✕
        </button>
      ) : null}
    </span>
  )
}
