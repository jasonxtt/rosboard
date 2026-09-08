/**
 * Settings feature barrel. `EmptyDevicePanel` is exported for the shell team:
 * ShellApp renders it for device-scoped views when no enabled device exists.
 */
import './settings.css'

export { EmptyDevicePanel } from './EmptyDevicePanel'
export { DevicesSection, RestartingBanner } from './DevicesSection'
export { CollectionForm } from './CollectionForm'
export { UiPrefsForm } from './UiPrefsForm'
export { AccountSecurityForm } from './AccountSecurityForm'
export { MaintenanceSection } from './MaintenanceSection'
export { RecognitionCard } from './RecognitionCard'
export { useSettings, useRestartingAction } from './hooks'
export type { SettingsResponse, SettingsDevice, MutationResult } from './api'
