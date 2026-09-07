import { SetupPlaceholder } from './SetupPlaceholder'

export default function LoginPage({ onComplete }: { onComplete: () => void }) {
  return <SetupPlaceholder title="登录 rosboard" description="使用管理员账号登录后进入监控面板。" phaseLabel="needs_login" onComplete={onComplete} />
}
