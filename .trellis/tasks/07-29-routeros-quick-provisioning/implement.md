# RouterOS Quick Provisioning — Implementation Plan

## Steps (ordered)

### Step 1: Backend — provisioning.go (new file)
Create `internal/api/provisioning.go` with:
- `provisioningSessionLifetime` constant
- `provisioningSession` struct
- `provisioningSessions` struct with create, get, consume, clear, pruneLocked methods
- Random credential generator functions
- RouterOS script template renderer
- `newProvisioningSessions()` constructor

### Step 2: Backend — verifyConnection extraction
Extract shared logic from `serveDeviceConnectionTest` and the new complete-provisioning handler into a `verifyConnection` method on Server:
- Creates client
- Calls Verify with 25s timeout
- Calls canonicalTrafficScope/canonicalTerminalScope
- Calls validateTrafficScopeInterfaces
- Calls PreviewScopes
- Calls issueWithTopology
- Returns unified result

Refactor `serveDeviceConnectionTest` to use the new method. All existing tests must still pass.

### Step 3: Backend — server.go modifications
- Add `provisioning *provisioningSessions` to Server struct
- Initialize in all constructors: NewServer, NewServerWithRestart, NewServerWithManager, NewServerWithAuth
- Route provisioning API endpoints in `serveAPI`:
  - POST /api/device-onboarding/sessions → `serveCreateProvisioningSession`
  - POST /api/device-onboarding/sessions/{sessionId}/complete → `serveCompleteProvisioning`
- Clear provisioning sessions in `serveFullReset`

### Step 4: Backend — auth/phase allowlist
- Add provisioning API paths to `phaseAllows` in auth.go so they're available in needs_routeros phase
- Do NOT add to publicAPI

### Step 5: Backend — provisioning_test.go (new file)
Write tests covering:
- Create session (defaults, HTTPS defaults, input validation, random values, script content)
- Session lifecycle (invalid/expired/consumed session → 410, re-tryable before success)
- Complete provisioning (success, connection failure, auth failure, duplicate endpoint, onboarding flag)
- Security (unauthenticated, cross-origin, phase restrictions)
- Full reset clears sessions
- Concurrent map safety (lock)

### Step 6: Frontend — types.ts
Add:
- `ProvisioningSessionResponse` type
- `ProvisioningCompleteResponse` type

### Step 7: Frontend — App.tsx DeviceSettingsPanel
- Add `provisioningMode: 'quick' | 'manual'` state (default 'quick' for new devices)
- Add provisioning UI states: `provisioningSession`, `provisioningStep: 'form' | 'script' | 'completing'`
- Mode toggle UI (only when `draft.id` is empty)
- Quick mode form: name, host, advanced settings (scheme/port with HTTP security notice)
- "Generate Script" button
- Step display after generation: script readonly textarea, copy button, "I've executed the script, start provisioning" button, "Re-generate script" link
- Keep the script collapsed by default and stack all three steps vertically.
- On complete: POST to provisioning session endpoint
- Save each new device with deferred restart; refresh the device list and allow another device to be added.
- Reuse `waitForPanelRestart` only for the final onboarding completion or explicit ready-phase apply action.
- Expired session handling: show re-generate prompt, return to form
- Error handling: preserve script on failure, allow retry

### Step 8: Frontend — App.tsx RouterOSSetupPage
- Pass provisioning-aware props so onboarding page can use quick provisioning too
- Ensure "Complete Setup" button works with quick provisioning flow

### Step 9: Frontend — index.css
Add CSS classes for:
- Mode toggle pills
- Provisioning step cards
- Script display area
- Copy button with success state
- Loading/disabled states for completion flow

### Step 10: Frontend — build verification
```bash
cd web && npm run lint && npm run build && cd ..
```

### Step 11: Backend — test verification
```bash
go test ./...
```

### Step 12: README.md update
Add "RouterOS Quick Provisioning" section.

### Step 13: Local build (production binary)
```bash
cd web && npm ci && npm run build && cd ..
go build -o rosboard ./cmd/rosboard
```

### Step 14: Deploy to 10.0.0.6
- SSH, check current systemd service
- Create backup with timestamp
- Upload new binary
- Atomic replacement
- Restart, verify health
- Report deployment status

### Step 15: Present for user acceptance
- Provide test steps from specification
- Wait for user approval before commit

## Validation Commands

```bash
# Backend
go vet ./...
go test ./... -count=1

# Frontend
cd web && npm run lint && npm run build && cd ..

# Build
go build -o rosboard ./cmd/rosboard

# Git check
git status --short
git diff --stat
```

## Rollback Points

1. If backend tests fail → fix tests or implementation
2. If frontend build fails → fix TypeScript/CSS issues
3. If remote health check fails → restore backup binary
