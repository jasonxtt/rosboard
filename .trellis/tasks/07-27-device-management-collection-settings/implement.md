# Implementation Plan

## Checklist

- [x] Read pre-development guidance for frontend/backend before editing.
- [x] Adjust backend collection settings request/response so global collection saves no longer include or persist traffic interfaces and terminal CIDRs.
- [x] Add/adjust API tests for global collection save preserving each device's interface/CIDR values.
- [x] Refactor settings UI labels: `连接设置` -> `设备管理`, `流量接口` -> `采集接口`.
- [x] Move picker-style interface selection from collection settings into the device editor while retaining configured missing interfaces.
- [x] Add terminal CIDR picker/manual-entry support in the device editor using current selected device interface addresses as suggestions.
- [x] Remove global interface/CIDR fields from `采集设置`.
- [x] Replace fixed reloads in device/archive actions with the existing health/assets restart wait flow.
- [x] Suppress expected fetch errors while an intentional restart is pending.
- [x] Update README/config docs if user-facing settings names or API field ownership need clarification.
- [x] Run Go tests for config/API/service areas touched.
- [x] Run frontend lint/build.

## Validation Commands

- `go test ./internal/config ./internal/api ./internal/service`
- `npm --prefix web run build`
- `npm --prefix web run lint`

## Risk Points

- Do not mix selected-device live interface hints into another device's editor.
- Do not remove manual terminal CIDR entry; automatic hints are convenience, not the source of truth.
- Do not accidentally drop existing per-device YAML values when saving global collection settings.
- Restart wait must cover devices and archive actions, not only settings/collection/manual restart.
