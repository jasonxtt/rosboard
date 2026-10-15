# Compose paste import

## Goal
Paste a full Compose document, choose one service, and fill the existing native
container creation form with supported information. Unsupported fields are
automatically skipped with visible explanations, without per-field approval.

## Requirements
- Shared Aurora/Compact/mobile create-form entry and transient import modal.
- Parse, choose a service, review imported/skipped/manual-input information,
  then explicitly apply to the form; parsing/applying never deploys anything.
- Import common image/name/env/startup/user/workdir/resources/restart fields.
- Bind and named-volume syntax preserve target/mode; ROS sources are selected
  manually. Original Docker paths never become ROS paths automatically.
- Preserve existing ROS networking/root when applying; repeat imports replace
  old imported values. Do not add Docker permission, sysctl, NAT or orchestration.
- Do not read .env/env_file/external files or resolve host environment variables.
- Bound untrusted input and avoid raw source, secret or value disclosure in logs.

## Acceptance
Parser/API/component tests cover conversion, rejected YAML, unsupported fields,
interpolation, resource units, read-only mounts, no-I/O device-scoped parsing,
cancel/stale/device/repeat behavior. Required Go/frontend checks pass. Deploy to
the disposable test machine and exercise isolated RouterOS creation without
touching the user's running MSM. Production requires verified NAS backup,
service/health/API/assets verification and fresh user manual acceptance.
