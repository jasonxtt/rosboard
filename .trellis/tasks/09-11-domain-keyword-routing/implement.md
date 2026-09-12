# Implementation plan

1. Align task metadata with branch `codex/domain-keyword-routing` and PR base
   `main`; record the parser, policy-routing, cross-layer, and quality specs.
2. Add keyword parsing/normalization, per-type counts, and focused parser and
   source-content tests.
3. Add the RoutingRule field, presence-aware API/proposal handling, SQLite
   column migration, proposal persistence, and compressed-source
   re-materialization with migration tests.
4. Implement RouterOS literal escaping, separated plain/keyword desired
   projections, deterministic DNS ordering, target filtering/blockers, and
   Access precedence safety. Extend actual scanning and reconciliation fields.
5. Add backend impact/warning/ack data, plan hash coverage, stale/apply tests,
   and desired/reconcile/access regression tests.
6. Update main and compact frontend contracts, target counts, routing wizards,
   advanced toggle, impact display, and confirmation modal. Run frontend lint
   and build.
7. Run Go formatting, targeted tests, full Go test/vet, frontend validation,
   and diff checks. Inspect exact staged paths and diff for secrets/unrelated
   files.
8. Create focused commits on this branch, push it, and create a Draft PR
   targeting `main`. Keep the PR unmerged pending the repository acceptance
   gate.

## Validation commands

```text
gofmt -w <changed Go files>
go test ./internal/policy ./internal/policyv2 ./internal/store ./internal/api
go test ./...
go vet ./...
npm --prefix web run lint
npm --prefix web run build
git diff --check
```

## Checkpoints

- Parser/schema checkpoint: targeted Go tests and staged diff inspection.
- Desired/plan checkpoint: policyv2/store/API tests and staged diff inspection.
- UI checkpoint: frontend lint/build and staged diff inspection.
- Final checkpoint: full validation, push branch, Draft PR targeting `main`.
