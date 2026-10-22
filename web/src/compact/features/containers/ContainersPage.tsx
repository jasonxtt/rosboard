import type { ComponentProps } from 'react'
import { ContainerPage } from '../../../features/containers/ContainerPage'
import { ContainerPresentationProvider } from '../../../features/containers/ContainerUI'
import { compactContainerPresentation } from './presentation'
import './containers.css'

export default function ContainersPage(props: ComponentProps<typeof ContainerPage>) {
  return <ContainerPresentationProvider value={compactContainerPresentation}><ContainerPage {...props} /></ContainerPresentationProvider>
}
