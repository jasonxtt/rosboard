import { SetupPlaceholder } from './SetupPlaceholder'

export default function RouterOSSetupPage({ onComplete }: { onComplete: () => void }) {
  return <SetupPlaceholder title="添加 RouterOS" description="连接第一台 RouterOS 设备并开始采集。" phaseLabel="needs_routeros" onComplete={onComplete} />
}
