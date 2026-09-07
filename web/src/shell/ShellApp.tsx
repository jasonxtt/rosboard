import { lazy, Suspense, type ComponentType, type LazyExoticComponent } from 'react'
import { Skeleton, ToastHost } from '../ui'
import { MobileNav } from './MobileNav'
import { ShellProvider } from './ShellProvider'
import { TopNav } from './TopNav'
import { useShell } from './useShell'
import { WarningBar } from './WarningBar'
import { DEVICE_SCOPED_VIEWS, type View } from './views'

/**
 * Page registry — one lazy chunk per feature view. Real pages land in
 * slices 1–4 by replacing the stubs under web/src/pages/.
 */
const pageComponents: Record<View, LazyExoticComponent<ComponentType>> = {
  fleet: lazy(() => import('../pages/FleetPage')),
  overview: lazy(() => import('../pages/OverviewPage')),
  interfaces: lazy(() => import('../pages/InterfacesPage')),
  terminals: lazy(() => import('../pages/TerminalsPage')),
  protocols: lazy(() => import('../pages/ProtocolsPage')),
  policies: lazy(() => import('../pages/PoliciesPage')),
  dhcp: lazy(() => import('../pages/DhcpPage')),
  routes: lazy(() => import('../pages/RoutesPage')),
  resource: lazy(() => import('../pages/ResourcePage')),
  load: lazy(() => import('../pages/LoadPage')),
  'target-library': lazy(() => import('../pages/TargetLibraryPage')),
  'policy-routing': lazy(() => import('../pages/PolicyRoutingPage')),
  'access-control': lazy(() => import('../pages/AccessControlPage')),
  recognition: lazy(() => import('../pages/RecognitionPage')),
  settings: lazy(() => import('../pages/SettingsPage')),
}

function PageFallback() {
  return (
    <div className="page">
      <Skeleton height={22} width="30%" />
      <Skeleton height={140} className="glass-skeleton" />
    </div>
  )
}

function ShellBody() {
  const { view, selectedDeviceId } = useShell()
  const Page = pageComponents[view]
  // Device-scoped views remount on device switch, resetting all page state (§12).
  const pageKey = DEVICE_SCOPED_VIEWS.has(view) ? `${view}:${selectedDeviceId}` : view
  return (
    <main className="shell-main">
      <Suspense fallback={<PageFallback />}>
        <Page key={pageKey} />
      </Suspense>
    </main>
  )
}

export function ShellApp() {
  return (
    <ShellProvider>
      <div className="shell">
        <TopNav />
        <WarningBar />
        <ShellBody />
        <MobileNav />
      </div>
      <ToastHost />
    </ShellProvider>
  )
}
