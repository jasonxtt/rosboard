import { ContainerPage } from '../features/containers/ContainerPage'
import { ContainerPresentationProvider } from '../features/containers/ContainerUI'
import { useShell } from '../shell/useShell'
import { auroraContainerPresentation } from './ContainerPresentation'
import './ContainersPage.css'

export default function ContainersPage() {
  const { selectedDeviceId, reloadNonce, refreshMs } = useShell()
  return <ContainerPresentationProvider value={auroraContainerPresentation}>
    <ContainerPage key={selectedDeviceId} deviceId={selectedDeviceId} refreshNonce={reloadNonce} refreshMs={refreshMs} />
  </ContainerPresentationProvider>
}
