import { ContainerPage } from '../features/containers/ContainerPage'
import { useShell } from '../shell/useShell'
import './ContainersPage.css'
export default function ContainersPage() {
  const { selectedDeviceId, reloadNonce, refreshMs } = useShell()
  return (
    <ContainerPage
      deviceId={selectedDeviceId}
      refreshNonce={reloadNonce}
      refreshMs={refreshMs}
    />
  )
}
