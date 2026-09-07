type WizardStepsProps = {
  steps: string[]
  current: number
  /** highest step index the user may jump to */
  maxUnlocked: number
  disabled?: boolean
  onJump: (index: number) => void
}

/** Numbered step pills for the rule wizard header. */
export function WizardSteps({ steps, current, maxUnlocked, disabled = false, onJump }: WizardStepsProps) {
  return (
    <div className="pol-wizard-steps" role="list" aria-label="向导步骤">
      {steps.map((label, index) => {
        const state = index === current ? 'current' : index < current ? 'done' : 'todo'
        const reachable = index <= maxUnlocked || index === current
        return (
          <button
            key={label}
            type="button"
            role="listitem"
            className={`pol-wizard-step pol-wizard-step-${state}`}
            disabled={disabled || !reachable}
            aria-current={index === current ? 'step' : undefined}
            onClick={() => onJump(index)}
          >
            <span className="pol-wizard-step-num" aria-hidden="true">
              {state === 'done' ? '✓' : index + 1}
            </span>
            {label}
          </button>
        )
      })}
    </div>
  )
}
