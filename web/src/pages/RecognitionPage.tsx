import { Card, Skeleton } from '../ui'
import { EmptyDevicePanel, RecognitionCard, RestartingBanner, useRestartingAction } from '../features/settings'
import { useShell } from '../shell/useShell'
import './recognition.css'

/**
 * 识别设置 (device-scoped): per-device card with 协议分析 toggle, MosDNS
 * 归因 settings and runtime stats. Remounts on device switch via the shell
 * page key, so all state below belongs to the selected device.
 */
export default function RecognitionPage() {
  const { selectedDeviceId, devices, devicesLoading, navigate } = useShell()
  const restartGate = useRestartingAction()
  const device = devices.find((item) => item.id === selectedDeviceId)

  return (
    <div className="page recognition-page">
      <header className="page-head">
        <h1>识别设置</h1>
        <span className="page-sub">{device ? device.name : '协议分析与 MosDNS 域名识别'}</span>
      </header>

      <RestartingBanner gate={restartGate} />

      {devicesLoading && !device ? (
        <Card>
          <Skeleton lines={5} height={14} />
        </Card>
      ) : null}

      {!devicesLoading && !device ? <EmptyDevicePanel onAdd={() => navigate('settings')} /> : null}

      {device ? (
        <Card title={`识别设置 · ${device.name}`} sub="协议分析与应用归因按设备独立配置，保存后自动重启识别服务">
          <RecognitionCard deviceId={device.id} deviceName={device.name} restartGate={restartGate} />
        </Card>
      ) : null}
    </div>
  )
}
