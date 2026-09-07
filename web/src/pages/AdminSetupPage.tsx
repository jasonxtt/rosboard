import { SetupPlaceholder } from './SetupPlaceholder'

export default function AdminSetupPage({ onComplete }: { onComplete: () => void }) {
  return <SetupPlaceholder title="创建管理员" description="第一步：设置用于持续登录 rosboard 的管理员账号。" phaseLabel="needs_admin" onComplete={onComplete} />
}
