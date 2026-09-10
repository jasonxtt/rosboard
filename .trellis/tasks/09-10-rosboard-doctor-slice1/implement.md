# Slice 1 Implementation Plan

1. Add diagnostic model, status aggregation, and quick-check source adapters.
2. Add server dependency wiring and the device-scoped GET endpoint.
3. Add backend model/aggregation/API tests, including optional-module and
   disabled/skipped semantics.
4. Add shared frontend diagnostics API/types/hook and the Aurora Settings
   section.
5. Add the matching Compact Settings section using the same API/types.
6. Run targeted tests, Go tests/vet, frontend tests/lint/build/dual-UI check,
   and `git diff --check`.
7. Inspect the exact diff and preserve all baseline untracked paths. Stop at
   Slice 1 and report for root review; do not start later slices.
