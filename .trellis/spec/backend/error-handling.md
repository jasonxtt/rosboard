# Error Handling

> How errors are handled in this project.

## Overview

Errors are returned up the call chain with operation context. Packages use
standard Go errors, sentinel errors, and typed errors; HTTP handlers translate
those errors into the public response contract. A failure must not be hidden
by returning a plausible empty success result, especially for device-scoped
storage or RouterOS writes.

## Error Types

- Use `errors.New` for stable package-level conditions and expose them as
  `Err...` values when callers need `errors.Is`.
- Use `fmt.Errorf("operation: %w", err)` to add context while preserving the
  underlying error.
- Use a typed error when the API needs structured classification. For example,
  `routeros.VerificationError` carries `Kind`, `Message`, and `Cause`, while
  `routeros.MutationOutcomeUnknownError` prevents an ambiguous write from
  being retried blindly.
- Treat `context.Canceled` and `context.DeadlineExceeded` as control-flow
  outcomes; do not replace them with unrelated success values.

## Error Handling Patterns

Lower layers return errors and do not write HTTP responses. Callers decide
whether an error is recoverable and add context:

```go
interfaces, err := client.Interfaces(ctx)
if err != nil {
    return config.DeviceConfig{}, false,
        fmt.Errorf("refresh RouterOS interfaces: %w", err)
}
```

Use `errors.Is` and `errors.As` instead of comparing error strings. Multi-step
SQLite operations begin a transaction, `defer tx.Rollback()`, and return the
commit error; RouterOS mutation code must stop and reconcile when the outcome
is unknown.

Do not panic for bad user input, a missing device, a failed RouterOS request,
or a background refresh failure. Startup-only failures that make the process
unable to serve are reported by the command entrypoint with `log.Fatalf`.

## API Error Responses

HTTP handlers use the shared helpers in `internal/api`:

- `writeJSON` sets the JSON content type and status.
- Legacy/general endpoints use `writeError`, returning `{ "error": "..." }`.
- Auth and validation paths use `writeAPIError`, returning `{ "code": "...", "error": "..." }`.
- Policy-routing endpoints use `writePolicyError`, which also includes a
  non-null `details` object.
- Invalid methods set the `Allow` header through `methodNotAllowed`.
- JSON request bodies are bounded by `decodeJSONBody` (64 KiB) and malformed
  input returns the stable `invalid_json` code.

Expose controlled validation messages to the user, but map infrastructure or
credential failures to a safe public message. Never include passwords,
Authorization headers, raw config, or full untrusted source content in an API
error.

## Examples

```go
if errors.Is(err, auth.ErrInvalidSession) {
    writeAPIError(writer, http.StatusUnauthorized,
        "authentication_required", "authentication required")
    return
}
writeAPIError(writer, http.StatusInternalServerError,
    "login_failed", "failed to create login session")
```

See [auth.go](../../../internal/api/auth.go) for authentication mapping,
[device_validation.go](../../../internal/api/device_validation.go) for
verification mapping, and [server.go](../../../internal/api/server.go) for the
response helpers.

## Common Mistakes

- Returning `200` with an empty object after a failed device-store lookup can
  make the UI treat missing data as real state.
- Retrying `MutationOutcomeUnknownError` can duplicate or corrupt a RouterOS
  change; read and reconcile first.
- Returning `err.Error()` for an internal network or SQL error can disclose
  implementation details or secrets.
- Using `http.Error` or inventing a one-off JSON shape breaks the frontend API
  contract.
