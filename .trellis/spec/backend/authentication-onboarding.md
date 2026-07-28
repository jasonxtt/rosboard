# Authentication and Onboarding

## Scenario: First-run administrator and verified multi-device setup

### 1. Scope / Trigger

- Trigger: changes to administrator credentials, sessions, first-run routing, RouterOS connection verification, device persistence during onboarding, or full reinitialization.
- Administrator/session/setup state belongs to SQLite; durable RouterOS devices belong to the YAML configuration. Browser state never determines the application phase.

### 2. Signatures

- SQLite tables: `admin_account` (singleton `id=1`), `auth_sessions` (SHA-256 token hash), and `app_state` (`onboarding_complete`).
- Bootstrap: `GET /api/bootstrap` -> `phase`, `authenticated`, `onboardingComplete`, and authenticated `username`.
- Setup/auth: `POST /api/setup/admin`, `POST /api/auth/login`, `POST /api/auth/logout`, `PUT /api/account`, and `POST /api/setup/complete`.
- Verification: `POST /api/devices/test-connection` -> one-use `verificationToken`, identity, interfaces, CIDR candidates, warnings, and expiry.
- Device writes: `POST /api/devices` and `PUT /api/devices/{id}` accept `verificationToken`, `completeOnboarding`, and onboarding-only `deferRestart` in addition to device fields.
- Local recovery: `rosboard admin reset-password -config <path>`.
- Destructive reset: `POST /api/settings/full-reset {"confirmed":true}`.

### 3. Contracts

- Phases are `needs_admin`, `needs_login`, `needs_routeros`, and `ready`. `GET /api/bootstrap` is the frontend's only phase source.
- Passwords contain 4-128 Unicode characters without composition rules. Argon2id hashes are stored; passwords and hashes never appear in API responses or logs.
- Sessions last seven days, roll after sustained use, persist across restarts, and are revoked on credential change or local password reset.
- Allowed CIDRs are checked before setup/auth. Authenticated write requests must be same-origin.
- RouterOS connection fields are tested before collection fields are available. Required probes must pass; optional probe failures become warnings.
- Verification tokens are memory-only, expire after 15 minutes, bind to normalized endpoint/username/password fingerprints, and are consumed only after successful YAML persistence.
- Device saves require at least one verified traffic interface and one canonical IPv4/IPv6 CIDR. Normalized endpoints are unique across active, disabled, and archived devices.
- During onboarding, `deferRestart=true` is honored only while `onboarding_complete=false`: save YAML and update the server snapshot without restarting so more devices can be added. In ready phase it cannot suppress the normal device restart.
- `completeOnboarding=true` saves the current new or existing device, sets onboarding complete, and schedules one restart. `POST /api/setup/complete {"skipRouterOS":false}` also restarts when saved devices exist; explicit empty-device skip does not need a restart.
- Full reset deletes the configured YAML plus all administrator, session, setup, device-history, and monitoring state, clears verification tickets/cookie, and restarts into `needs_admin`.

### 4. Validation & Error Matrix

- Administrator already exists -> setup-admin returns conflict and never replaces it.
- Invalid/mismatched password or invalid username -> HTTP 400; no partial account update.
- Missing/expired session -> HTTP 401; authenticated but unfinished setup on a disallowed route -> HTTP 409.
- Cross-origin authenticated write -> HTTP 403 before the business handler.
- Verification failure or changed connection fingerprint -> reject save; do not write YAML.
- Missing/unknown interface, invalid/empty CIDR, or duplicate endpoint -> reject save; do not consume the ticket.
- YAML save failure -> preserve the old configuration and onboarding state.
- Device persisted but onboarding-state write fails -> retain the device, remain in onboarding, and allow `/api/setup/complete` recovery.
- `deferRestart=true` after onboarding -> ignore the deferral and use normal restart semantics.
- Full reset without `confirmed=true` -> HTTP 400 and preserve every file/table.

### 5. Good/Base/Bad Cases

- Good: save device A with `deferRestart=true`, immediately save device B, then complete from B; only completion restarts and the next bootstrap is `ready`.
- Good: update username and password atomically, revoke all sessions, clear the current cookie, and require login with the new credentials.
- Base: explicitly skip RouterOS after creating the administrator; enter a ready empty panel and add a device later.
- Bad: restart after every onboarding save; the user returns to the setup choice and cannot efficiently add multiple devices.
- Bad: trust a frontend verification flag or return the RouterOS password from `/api/settings`.
- Bad: mark onboarding complete before the device YAML has been atomically replaced.

### 6. Tests Required

- Store/auth: singleton administrator concurrency, Argon2id verification, token hashing/expiry/renewal/revocation, atomic credential update, and full reset transaction.
- API: phase matrix, allowed-CIDR ordering, same-origin writes, login throttling, password-free responses, setup completion, and unauthenticated rejection.
- RouterOS/API: required/optional probes, ticket expiry/fingerprint/replay, endpoint uniqueness, interface sampling permission, and CIDR canonicalization.
- Restart regression: save-only onboarding emits `restarting=false` and does not call restart; direct completion of an unsaved device and completion of a saved device both persist, enter `ready`, and call restart once.
- Quality: `go test ./...`, targeted race tests, `go vet ./...`, frontend lint/build/audit, local browser verification, and deployed health/assets/polling checks.

### 7. Wrong vs Correct

#### Wrong

```go
saveDevice(payload)
scheduleRestart() // Every onboarding save interrupts multi-device setup.
```

#### Correct

```go
saveDevice(payload)
if payload.CompleteOnboarding {
    completeOnboarding()
}
if !deferRestartWhileOnboarding(payload) {
    scheduleRestart()
}
```
