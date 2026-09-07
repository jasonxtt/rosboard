import type { ReactNode } from 'react'

type FieldProps = {
  label: ReactNode
  children: ReactNode
  hint?: string
  className?: string
}

/** Label-above-control form field (§7 inputs). */
export function Field({ label, children, hint, className = '' }: FieldProps) {
  return (
    <div className={`field ${className}`.trim()}>
      <span className="field-label">{label}</span>
      {children}
      {hint ? <small className="faint">{hint}</small> : null}
    </div>
  )
}

type InputProps = {
  value: string
  onChange: (value: string) => void
  type?: 'text' | 'password' | 'number' | 'url' | 'email'
  placeholder?: string
  disabled?: boolean
  required?: boolean
  minLength?: number
  maxLength?: number
  min?: number
  max?: number
  step?: number
  autoFocus?: boolean
  autoComplete?: string
  name?: string
  ariaLabel?: string
}

export function Input({
  value,
  onChange,
  type = 'text',
  placeholder,
  disabled = false,
  required = false,
  minLength,
  maxLength,
  min,
  max,
  step,
  autoFocus = false,
  autoComplete,
  name,
  ariaLabel,
}: InputProps) {
  return (
    <input
      className="input"
      value={value}
      onChange={(event) => onChange(event.target.value)}
      type={type}
      placeholder={placeholder}
      disabled={disabled}
      required={required}
      minLength={minLength}
      maxLength={maxLength}
      min={min}
      max={max}
      step={step}
      autoFocus={autoFocus}
      autoComplete={autoComplete}
      name={name}
      aria-label={ariaLabel}
    />
  )
}

export type SelectOption = { value: string; label: string }

type SelectProps = {
  value: string
  onChange: (value: string) => void
  options: SelectOption[]
  disabled?: boolean
  ariaLabel?: string
}

export function Select({ value, onChange, options, disabled = false, ariaLabel }: SelectProps) {
  return (
    <select className="select" value={value} onChange={(event) => onChange(event.target.value)} disabled={disabled} aria-label={ariaLabel}>
      {options.map((option) => (
        <option key={option.value} value={option.value}>
          {option.label}
        </option>
      ))}
    </select>
  )
}

type TextareaProps = {
  value: string
  onChange: (value: string) => void
  placeholder?: string
  rows?: number
  disabled?: boolean
  ariaLabel?: string
}

export function Textarea({ value, onChange, placeholder, rows = 4, disabled = false, ariaLabel }: TextareaProps) {
  return (
    <textarea
      className="textarea"
      value={value}
      onChange={(event) => onChange(event.target.value)}
      placeholder={placeholder}
      rows={rows}
      disabled={disabled}
      aria-label={ariaLabel}
    />
  )
}
