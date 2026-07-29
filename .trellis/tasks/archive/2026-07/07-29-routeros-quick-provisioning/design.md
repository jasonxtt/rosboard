# RouterOS Quick Provisioning — Design

## Architecture

### Backend: provisioningSessions (in-memory)

New file `internal/api/provisioning.go`:

```
provisioningSessionLifetime = 15 * time.Minute

type provisioningSession struct {
    deviceName string
    baseURL    string
    scheme     string
    host       string
    port       int
    username   string
    groupName  string
    password   string
    expiresAt  time.Time
}

type provisioningSessions struct {
    mu     sync.Mutex
    now    func() time.Time
    random io.Reader
    items  map[[32]byte]provisioningSession
}
```

Session ID: 32 bytes from crypto/rand, returned as base64.RawURLEncoding. Map key is SHA-256 hash. Expired items pruned on every create/get/consume.

### Backend: Random Credential Generator

- 8 random bytes → 16-char lowercase hex suffix
- Username: `rosboard_<suffix>`
- Group name: `rosboard_g_<suffix>`
- Password: 32 chars from charset `ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789` (no 0/O/1/l/I), each char selected via crypto/rand

### Backend: RouterOS Script Template

Rendered server-side. Only username, group name, password inserted. Device name NOT in script.

```routeros
:local rbGroup "rosboard_g_<suffix>"
:local rbUser "rosboard_<suffix>"
:local rbPassword "<password>"

:local rbGroupId [/user group find where name=$rbGroup]
:if ([:len $rbGroupId] = 0) do={
    /user group add name=$rbGroup policy=read,test,rest-api
} else={
    /user group set $rbGroupId policy=read,test,rest-api
}

:local rbUserId [/user find where name=$rbUser]
:if ([:len $rbUserId] = 0) do={
    /user add name=$rbUser password=$rbPassword group=$rbGroup comment="Managed by rosboard"
} else={
    /user set $rbUserId password=$rbPassword group=$rbGroup disabled=no comment="Managed by rosboard"
}

:put ("rosboard account ready: " . $rbUser)
```

### Backend: Server Integration

- Add `provisioning *provisioningSessions` field to `Server` struct
- Initialize in all constructors: NewServer, NewServerWithRestart, NewServerWithManager, NewServerWithAuth
- Route provisioning API in `serveAPI` mux
- Clear on full reset alongside tickets.clear()

### Backend: verifyConnection extraction

Minimal extraction from verification.go to avoid duplicating Verify + PreviewScopes + issueWithTopology logic. Extract a `verifyConnection` method on Server.

### Frontend: App.tsx

- Add provisioning types to types.ts
- Add provisioning UI state to DeviceSettingsPanel
- Mode toggle: quick vs manual (only when `draft.id` is empty, i.e. adding new device)
- Quick provisioning form: name, host, advanced settings (scheme/port), generate script button
- Step display after generation: script readonly textarea, copy button, completion button
- On complete: call POST /api/device-onboarding/sessions/{sessionId}/complete
- On restart: reuse existing waitForPanelRestart

### Frontend: CSS (index.css)

- `.provisioning-mode-toggle` — mode switch pills
- `.provisioning-step` — step cards
- `.provisioning-script-area` — textarea/pre for script display
- `.provisioning-advanced` — details disclosure for scheme/port
- `.provisioning-expiry` — expiry display
- `.settings-inline-note-http` — HTTP security notice

## Data Flow

1. User fills name + host → POST /api/device-onboarding/sessions
2. Backend validates, generates random credentials + script, stores session, returns sessionId + script
3. User copies script, pastes to RouterOS, executes
4. User clicks complete → POST /api/device-onboarding/sessions/{sessionId}/complete
5. Backend reads session, creates routeros.Client with stored credentials, calls Verify
6. On Verify success, creates PreviewScopes (auto), creates verificationTicket, calls prepareDevice, saveSettings
7. On save success, consumes verification ticket + provisioning session, completes onboarding if needed, schedules restart

## Error Handling

- Session expired/missing: HTTP 410, `provisioning_expired`
- Connection failure: HTTP 502, `routeros_connection`
- Auth failure: HTTP 502, `routeros_authentication`
- REST API unavailable: clear error mentioning RouterOS 7.9+ / www service
- No ISP traffic identified: clear error suggesting manual add
- Session already consumed: HTTP 410
- Errors NEVER contain password, full script, Authorization, or config content

## Security

- crypto/rand for all random generation
- Session token stored only as SHA-256 hash
- Password never in logs, settings response, URL, browser storage
- API responses use Cache-Control: no-store
- Admin authentication required (not public API)
- Same-origin write check enforced
- allowed_cidrs enforced
- JSON body size limited via existing decodeJSONBody
- Script/credentials never accepted from user input
