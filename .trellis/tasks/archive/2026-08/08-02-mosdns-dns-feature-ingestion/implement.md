# Implementation Plan

1. [x] Add `config.MosDNSConfig`, defaults, environment overrides, validation, and settings serialization. Verify with config unit tests.
2. [x] Add `internal/mosdns` API types, HTTP client, pagination, normalization, and deduplication helpers. Verify with httptest fixtures and focused unit tests.
3. [x] Add additive SQLite schema, watermark/observation persistence, durable learned IP features, pruning, and read methods. Verify insert-or-ignore behavior, restart replay, backfill, and retention tests.
4. [x] Add the process-scoped synchronizer and start it from `cmd/rosboard` without changing RouterOS monitor startup. Verify cancellation and failure isolation.
5. [x] Add read-only diagnostics endpoints and response types. Verify API method/status/auth behavior and recent observation limits.
6. [x] Run `gofmt`, `go test ./...`, and a local runtime smoke test against a fixture server. Inspect the diff for unrelated changes.
7. [x] Integrate DNS/domain matches into connection rows and aggregate protocol statistics; verify matched and fallback cases.
8. [x] Add feature-library and MosDNS settings UI/API, then run frontend/backend checks.
9. [x] Run deployment and remote acceptance only after the user confirms the local result, following the repository deployment gate.
