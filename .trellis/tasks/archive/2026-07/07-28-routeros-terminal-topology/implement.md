# Implementation plan

1. Add RouterOS topology types/client calls and optional error handling, verified against recorded live fields.
2. Add pure topology model/resolver/roles/prefixes/overrides with table-driven tests.
3. Add config validation and device/API runtime projection while preserving legacy/masked/multi-device contracts.
4. Integrate immutable scope publication, DHCP-server provenance, ARP/neighbor/link-local filters, and scope-aware conntrack; remove neighbor `/128` and empty-is-all behavior.
5. Update frontend shared types and device settings: read-only automatic summary, legacy notice, collapsed advanced overrides, responsive/theme/device-switch validation.
6. Run `gofmt`, focused and full Go tests, `npm run lint`, `npm run build`, executable build, and ignored-local-config runtime/browser checks.
7. Inspect remote deployment read-only; build, timestamp-backup binary/config/SQLite, deploy through existing systemd workflow, verify health/API/assets/terminal scope/browser, then stop for user acceptance.

## Risk focus

- Preserve `routeros:self`, terminal merge/history, monitor locking, `allowed_cidrs`, and password masking.
- Optional topology failures must not hide current required poll failures.
- Generated `internal/ui/dist` comes only from frontend build; final Go executable must embed it.
- No commit, push, archive, or spec-finalization before remote manual acceptance.
