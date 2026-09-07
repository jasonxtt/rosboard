import { Card } from '../ui/Card'
import { Skeleton } from '../ui/Skeleton'

type PageStubProps = {
  title: string
  description: string
  icon?: string
}

/**
 * Slice-0 placeholder: glass card + title + one-line description.
 * Slices 1–4 replace the body with the real feature page.
 */
export function PageStub({ title, description, icon = '◍' }: PageStubProps) {
  return (
    <div className="page">
      <header className="page-head">
        <h1>{title}</h1>
        <span className="page-sub">{description}</span>
      </header>
      <Card>
        <div className="stub-body">
          <span className="stub-icon" aria-hidden="true">{icon}</span>
          <strong>{title}将在后续迭代中开放</strong>
          <p className="faint">{description}。当前为基础框架版本，页面骨架与导航已就绪。</p>
          <Skeleton lines={3} height={14} />
        </div>
      </Card>
    </div>
  )
}
