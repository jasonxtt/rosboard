# RouterOS Quick Provisioning — PRD

## Overview

Add a "Quick Provisioning" flow to rosboard's Device Management that generates random credentials and a RouterOS script for users, enabling a simplified setup experience while reusing the existing verification, topology preview, and device saving machinery.

## User Experience

1. User clicks "Add Device" in Device Management.
2. Default mode is "Quick Provisioning (Recommended)".
3. User only fills:
   - Device name
   - RouterOS IP/hostname
4. Default connection parameters (protocol: http, port: 80) are hidden under "Advanced Settings" (collapsed by default).
5. User clicks "Generate Script".
6. Backend generates:
   - Random RouterOS username
   - Random RouterOS group name
   - Random strong password
   - Copyable RouterOS script
   - One-time provisioning session valid for 15 minutes
7. User pastes script into RouterOS Terminal and executes it.
8. User returns to rosboard and clicks "I've executed the script, start provisioning".
9. Backend connects to RouterOS using the generated credentials.
10. On success, reuses existing:
    - RouterOS identity verification
    - Required capability verification
    - Automatic ISP traffic interface detection
    - Automatic LAN interface detection
    - Automatic IPv4/IPv6 local terminal subnet detection
    - verificationTicket creation
    - Device saving logic (config.yaml)
    - Collection restart → Panel
11. Existing "Manual Add" flow remains untouched.

## Product Boundaries (v1)

1. Default: HTTP / port 80.
2. Page must show security notice about HTTP transmitting credentials in plaintext.
3. Script only creates a dedicated user group and user account.
4. Script does NOT:
   - Enable /ip service www
   - Modify www port or address
   - Modify firewall
   - Modify certificates
   - Enable www-ssl
   - Change any existing RouterOS configuration
5. Error messages must be clear when REST API is unavailable.
6. No automatic address restriction on the new user.
7. No credential encryption changes — continues using config.yaml permission 0600.
8. No automatic cleanup on device deletion.
9. No QR code, WebSocket pairing, cloud registration.
10. No new database tables — provisioning sessions are in-memory only.
11. No change to multi-device architecture.
12. No large-scale refactoring.
13. No new frontend framework.

## API Endpoints

### POST /api/device-onboarding/sessions
Create a provisioning session, generate random credentials and script.

Request: `{ name, host, scheme?, port? }`

Response: `{ sessionId, script, expiresAt, connection: { scheme, host, port }, username }`

Requires admin authentication, same-origin write check, allowed_cidrs. Not a public API. Available in needs_routeros and ready phases.

### POST /api/device-onboarding/sessions/{sessionId}/complete
Complete provisioning using stored credentials.

Request: `{ completeOnboarding: boolean }`

Reuses existing Verify, PreviewScopes, verificationTicket, prepareDevice, saveSettings logic.

## Frontend Changes

- Add mode toggle to DeviceSettingsPanel when adding a new device: "Quick Provisioning (Recommended)" / "Manual Add"
- Quick provisioning flow: name + host fields, advanced settings (protocol/port), generate script, script display area with copy button, completion button
- Reuse existing save/restart machinery
- Accessibility: labels, roles, responsive, no modals

## Acceptance Criteria

1. Quick provisioning default: name and IP only
2. HTTP/80 default
3. Protocol/port in advanced settings (collapsed)
4. Backend generates cryptographically random credentials
5. Script is idempotent
6. Permissions strictly read,test,rest-api
7. Script does not modify RouterOS services/firewall
8. Provisioning session: 15 min, in-memory, one-time use
9. Complete provisioning reuses all existing verification/save machinery
10. Manual add flow preserved intact
11. Auth permissions correct (admin, allowed_cidrs, same-origin, phase)
12. Password/script not leaked to logs, URL, browser storage, or settings response
13. Backend tests pass
14. Frontend lint/build pass
15. Deployed to 10.0.0.6 with full backup
16. User manual acceptance before commit
