# Review and integrate supplied Aurora policy routing UI

## Goal and authorization

The user supplied replacement TSX/CSS and requested review, replacement when sound,
and a compiled preview. Continue the existing test-first production-preview flow.
This is an independent UI workstream based on the current production source.

## Requirements

- Integrate the supplied Aurora header, rule count, primary create action, table
  spacing/column widths, loading state and duplicate egress-line suppression.
- Correct incompatible theme tokens and scope the new styles to policy routing;
  loading the route must not restyle access-control tables or Compact.
- Preserve backend/API contracts, write guards, wizard/review/delete behavior,
  desired/apply recovery feedback, existing container features and production data.
- Do not apply a RouterOS configuration change as part of preview verification.

## Acceptance criteria

- [x] Inspect both supplied files against current source; record findings/fixes.
- [x] Existing frontend tests, lint, TypeScript/build and UI graph checks pass.
- [ ] Compile the embedded static binary; verify the same binary on the isolated
  test service before production replacement.
- [ ] Before production replacement, create/verify a consistent timestamped NAS
  backup of binary/configuration/SQLite/service files with retention at most ten.
- [ ] Production service, health, authenticated read APIs and embedded asset hashes
  pass; configuration/supervisor/unit stay unchanged.
- [ ] Provide exact Aurora URL and desktop/mobile/light/dark manual QA steps.
  Keep Draft/in_progress until fresh user acceptance and root APPROVED.

## Review findings

The supplied CSS references undefined text/surface/border tokens with light-only
fallback colours. Map them to Aurora ink/glass/stroke tokens. Its pol-rule-table
selectors also match access control once the CSS chunk is loaded; scope all new
selectors under a dedicated pol-routing-page root. The TSX changes leave mutation
handlers/contracts intact; preserve useful recovery comments removed by the draft.

## Branch dependency

codex/policy-routing-ui-refresh starts at b3bd886 (PR #32), the exact current
production source lineage. Keep the container features; comparison to main also
includes the pending PR #30/#32 baseline. No unrelated policy-adoption work.
