# Logging Guidelines

> How logging is done in this project.

## Overview

The backend uses Go's standard `log.Logger`. The process entrypoint creates a
logger and injects it into long-lived services; packages do not create their
own global logger. Logging is operational context for operators, not a second
error-return channel.

## Log Levels

The current logger exposes the standard `Print*` methods rather than a
structured level API. Use the following convention:

- `log.Fatalf` only for unrecoverable command/startup failures in
  `cmd/rosboard/main.go`.
- `logger.Printf` for recoverable background failures and important lifecycle
  events. Include an explicit phrase such as `refresh failed` or `job failed`
  when the event is not fatal.
- Return errors to the caller as well; do not log and silently continue at a
  lower layer unless the operation is intentionally best-effort.

## Structured Context

There is no JSON logger or request logging middleware. Use a stable one-line
message with key context in the text. Background policy messages should include
the device ID and job ID when available; service messages should include the
device ID and operation. Avoid dumping whole request, response, or RouterOS
object payloads.

```go
if m != nil && m.logger != nil {
    m.logger.Printf("policy v2 device=%s job=%s: %v", deviceID, jobID, err)
}
```

## What to Log

- Process startup and shutdown-relevant failures.
- Device monitor, source refresh, policy job, and RouterOS verification
  failures with the affected device and operation.
- State transitions that help an operator understand why work paused or was
  rejected, without recording the underlying secret-bearing payload.

## What NOT to Log

- RouterOS passwords, administrator passwords, session tokens, cookies, or
  `Authorization` headers.
- Full YAML/source contents, uploaded files, raw request bodies, or SQLite
  credentials.
- Large RouterOS object lists when a count, ID, menu, and operation is enough.
- Sensitive data merely to make a failed test easier to debug.

## Common Mistakes

- Creating a package-level logger makes tests and device-specific context
  difficult to control.
- Logging an error and then returning it repeatedly creates noisy duplicate
  entries; log at the boundary that can act on the failure.
- Including a raw HTTP request in a RouterOS failure can leak credentials even
  when the error message looks harmless.
