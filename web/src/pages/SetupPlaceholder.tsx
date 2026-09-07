import { Button } from '../ui/Button'

type SetupPlaceholderProps = {
  title: string
  description: string
  phaseLabel: string
  onComplete: () => void
}

/** Shared slice-0 placeholder for the bootstrap phases; slice 1 designs the real forms. */
export function SetupPlaceholder({ title, description, phaseLabel, onComplete }: SetupPlaceholderProps) {
  return (
    <main className="startup-shell">
      <section className="glass startup-card">
        <div className="setup-brand">
          <span className="logo" aria-hidden="true">R</span>
          <div>
            <h1>{title}</h1>
            <p className="muted">{description}</p>
          </div>
        </div>
        <p className="faint">
          初始化阶段：<code>{phaseLabel}</code> —— 设置流程将在下一个迭代开放。
        </p>
        <Button variant="primary" onClick={onComplete}>重新检查状态</Button>
      </section>
    </main>
  )
}
