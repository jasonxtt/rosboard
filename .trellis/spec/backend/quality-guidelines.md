# Quality Guidelines

> Code quality standards for backend development.

## Overview

Backend changes should preserve the existing package boundaries and be
verified with deterministic Go tooling. The default local checks are:

```bash
gofmt -w <changed-go-files>
go build ./...
go test ./...
go vet ./...
git diff --check
```

Use targeted `go test -race` for code that coordinates concurrent monitors,
jobs, or stores. Record a known pre-existing failure instead of weakening the
test or hiding it.

## Forbidden Patterns

- Do not put secrets in source, fixtures, logs, task artifacts, or test output.
- Do not call arbitrary RouterOS paths from outside `internal/routeros`; use
  its typed, allowlisted read/mutation methods.
- Do not fall back to the owner/default SQLite database when a device-scoped
  store cannot be opened.
- Do not ignore errors from database transactions, RouterOS writes, or
  response decoding just to make a request succeed.
- Do not broaden a feature into a new registry, DSL, or parallel subsystem
  when the existing package already owns the behavior.

## Required Patterns

- Pass `context.Context` through I/O and background operations.
- Wrap returned errors with operation context using `%w`; use `errors.Is` or
  `errors.As` at classification boundaries.
- Use a transaction for related SQLite updates and commit only after every
  statement succeeds.
- Keep RouterOS desired-state changes deterministic and ownership-aware; an
  unknown mutation outcome must stop normal execution until read-back
  reconciliation.
- Add a regression test for a new API, migration, parser, ownership, or
  concurrency contract.

## Testing Requirements

- Put unit and package contract tests beside production files as `_test.go`.
- Use `httptest` for HTTP contracts and fakes or local test servers for
  RouterOS behavior; do not make normal tests depend on a live router.
- Include success, validation failure, missing-device, and relevant rollback
  or isolation cases for stateful changes.
- For parsers, use table-driven cases for normalization, duplicates, invalid
  input, and ignored rule types.
- For migrations and device isolation, assert both the selected device and a
  different device so accidental cross-scope reads fail visibly.

## Code Review Checklist

- Is the change in the package that owns it, with no unrelated cleanup?
- Are device IDs, credentials, and RouterOS object ownership preserved?
- Are errors wrapped and translated at the correct boundary?
- Are multi-row writes transactional and ambiguous RouterOS outcomes fail
  closed?
- Are tests present for the new contract and its failure path?
- Do build, test, vet, race (when relevant), and `git diff --check` pass?

## Examples

- [Transactional policy repository writes](../../../internal/store/policy.go)
  use `BeginTx`, deferred rollback, and commit after all related statements.
- [RouterOS mutation safety](../../../internal/routeros/mutation.go) validates
  menus, fields, IDs, retries, and unknown outcomes.
- [Policy manager tests](../../../internal/policyv2/manager_test.go) use
  in-memory fakes to exercise plans and job behavior without a live device.
