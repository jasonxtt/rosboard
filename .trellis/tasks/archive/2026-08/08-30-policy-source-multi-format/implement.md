# Policy source multi-format parsing implementation plan

## Phase 1 — plan and baseline

- [x] Review this PRD/design and relevant backend/frontend specs.
- [x] Inspect the current worktree and preserve unrelated dirty files.
- [x] Run focused parser/upload tests before editing to establish the baseline.
- [x] Activate the task after planning review.

## Phase 2 — implementation

### Parser and source content

- [x] Skip full-line comments and blank lines in domain/IP line parsers.
- [x] Add content-based Clash YAML → line-list fallback for URL/upload
      preparation, preserving kind-specific extraction and existing YAML
      safety limits.
- [x] Preserve original content hashing/compression and pending-version
      behavior.

### Upload and frontend

- [x] Allow `.yaml`, `.yml`, `.txt`, and `.list` uploads while rejecting other
      extensions.
- [x] Update parameterized source-editor copy and input filtering for generic
      YAML/text/list content; keep domain wording unchanged where applicable.
- [x] Change the new URL source default refresh interval to 7 days in the
      frontend, API, and scheduler fallback.

### Tests

- [x] Add parser tests for both kinds and all accepted formats.
- [x] Add upload tests for new extensions and rejection cases.
- [x] Add API tests for URL/upload selected-kind filtering if existing test
      helpers make that boundary practical.

## Phase 3 — verification and finish

- [x] Run `gofmt` on changed Go files.
- [x] Run targeted and full Go tests, `go vet ./...`, and `git diff --check`.
- [x] Run `npm run build` and `npm run lint` in `web`.
- [x] Inspect the diff for scope creep and verify embedded frontend assets.
- [x] Complete local runtime/browser checks for URL and upload workflows.
- [x] Complete the NAS backup, deployment, health/API/assets verification.
- [x] Receive explicit user acceptance of the deployed instance before
      committing program changes.
- [x] No stable project spec update is needed; the new behavior remains
      scoped to this task's source and parser implementation.
- [x] Commit and archive the task only after explicit deployment acceptance.
