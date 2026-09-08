/**
 * Editable draft shapes and pure converters for the device add/edit flows.
 * Kept pure so wizards and the verification dialog share one mapping
 * (type-safety spec: draft validation lives outside components).
 */

import type { SettingsDevice, TerminalScopeConfig, TrafficScopeConfig } from './api'

export type ScopeOverrideDraft = {
  trafficIncludeInterfaces: string
  trafficExcludeInterfaces: string
  includeInterfaces: string
  excludeInterfaces: string
  includeCidrs: string
  excludeCidrs: string
}

export const emptyScopeOverrideDraft: ScopeOverrideDraft = {
  trafficIncludeInterfaces: '',
  trafficExcludeInterfaces: '',
  includeInterfaces: '',
  excludeInterfaces: '',
  includeCidrs: '',
  excludeCidrs: '',
}

/** 「每行一项 / 逗号分隔」textarea text → trimmed list. */
export function parseSettingList(value: string): string[] {
  return value
    .split(/[\n,]/)
    .map((item) => item.trim())
    .filter(Boolean)
}

export function scopeConfigsFromOverrides(draft: ScopeOverrideDraft): {
  trafficScope: TrafficScopeConfig
  terminalScope: TerminalScopeConfig
} {
  return {
    trafficScope: {
      mode: 'auto',
      include_interfaces: parseSettingList(draft.trafficIncludeInterfaces),
      exclude_interfaces: parseSettingList(draft.trafficExcludeInterfaces),
    },
    terminalScope: {
      mode: 'auto',
      include_interfaces: parseSettingList(draft.includeInterfaces),
      exclude_interfaces: parseSettingList(draft.excludeInterfaces),
      include_cidrs: parseSettingList(draft.includeCidrs),
      exclude_cidrs: parseSettingList(draft.excludeCidrs),
    },
  }
}

export function scopeOverrideDraftFromDevice(device: SettingsDevice): ScopeOverrideDraft {
  return {
    trafficIncludeInterfaces: (device.trafficScope.include_interfaces ?? []).join('\n'),
    trafficExcludeInterfaces: (device.trafficScope.exclude_interfaces ?? []).join('\n'),
    includeInterfaces: (device.terminalScope.include_interfaces ?? []).join('\n'),
    excludeInterfaces: (device.terminalScope.exclude_interfaces ?? []).join('\n'),
    includeCidrs: (device.terminalScope.include_cidrs ?? []).join('\n'),
    excludeCidrs: (device.terminalScope.exclude_cidrs ?? []).join('\n'),
  }
}

export type DeviceFormDraft = {
  name: string
  scheme: 'http' | 'https'
  host: string
  port: number
  username: string
  password: string
}

export function deviceFormDraft(device?: SettingsDevice): DeviceFormDraft {
  return {
    name: device?.name ?? '',
    scheme: device?.scheme === 'https' ? 'https' : 'http',
    host: device?.host ?? '',
    port: device?.port ?? 80,
    username: device?.username ?? '',
    // Passwords are write-only: an edit draft always starts blank and the
    // backend keeps the stored secret when the field arrives empty.
    password: '',
  }
}
