# DOMAIN-KEYWORD routing end-to-end support

## Goal

Allow policy-routing target lists to carry Clash `DOMAIN-KEYWORD` rules from
source ingestion through RouterOS DNS Static, with an explicit and reviewable
per-routing-rule opt-in.

## Requirements

- Parse `DOMAIN-KEYWORD` in Clash YAML and `keyword:` in line-list sources.
  Normalize by trimming and lower-casing, reject empty/overlong/unsafe values,
  deduplicate, and keep unsupported `regexp:`, `DOMAIN-REGEX`, and `include:`
  inputs ignored. Do not turn user input into arbitrary regular expressions.
- Add `includeKeywordDomains` to the canonical RoutingRule contract. New UI
  rules default to enabled and always send the value; existing persisted rules
  remain disabled until explicitly enabled. An omitted update field preserves
  the existing value and an omitted create field is false.
- Persist the option with an idempotent SQLite migration and re-materialize
  keyword rules from the original compressed source content without changing
  source bytes or SHA identity. The migration must be transactional,
  restart-safe, and apply to every source-version state.
- Project plain and keyword domain rules into separate deterministic logical
  target/address-list identities. Emit RouterOS DNS Static `regexp` literals
  using a RouterOS-appropriate literal escaper; keyword entries are ordered
  before plain entries, with keyword ties ordered by routing priority, rule ID,
  target ID, and keyword. Never auto-promote plain rules or subtract/shadow
  domain sets.
- Include keyword objects in desired-state fingerprints, scans, diffs, apply
  verification, cleanup, stale-plan checks, and recovery. Keep the managed
  RouterOS field set aware of `regexp`.
- Add backend-derived keyword impact data to previews/plans and require the
  existing acknowledgement protocol for first enablement, false-to-true
  enablement, and newly introduced keywords. Automatic refreshes of an already
  enabled rule must not repeatedly prompt.
- Add the exact Advanced setting label `启用关键字域名规则`, prescribed
  explanation/impact text, keyword counts, and a confirmation modal with
  Continue/Return-to-Advanced actions in both policy UIs. The UI must not parse
  YAML itself, and the toggle must remain usable when there are zero keywords.
- Include keyword rules in target counts/details. If disabling keyword
  projection leaves every target for a rule empty, block with
  `routing_target_empty_after_keyword_filter`.
- Keep Access control unaware of keyword semantics. If an active Access exact
  or suffix domain projection can coexist with an active Routing keyword
  projection, fail closed with `routing_keyword_access_precedence_unsafe`
  before the acknowledgement modal.

## Acceptance Criteria

- [ ] Parser, normalization, persistence, migration, and count tests cover
      YAML, line-list, deduplication, unsupported forms, and old compressed
      versions.
- [ ] Desired-state tests prove plain/keyword separation, literal escaping,
      global regexp-first ordering, deterministic keyword ties, empty-target
      blocking, and no plain-rule promotion or domain subtraction.
- [ ] Plan tests prove keyword impact fields, acknowledgement timing and
      deduplication behavior, stale-plan/hash coverage, and Access precedence
      blockers.
- [ ] Reconcile/verification tests prove `regexp` is scanned, diffed,
      fingerprinted, and cleaned without logical-ID collisions.
- [ ] Frontend lint/build pass and both routing-rule UIs expose the toggle,
      backend-derived counts/impact, and confirmation flow.
- [ ] `go test ./...`, `go vet ./...`, `npm --prefix web run lint`,
      `npm --prefix web run build`, and `git diff --check` pass. Changes are
      committed only on the task branch and a Draft PR targets `main`.

## Constraints

- Do not modify or commit unrelated pre-existing untracked files.
- Do not commit directly to `main`, merge the PR, deploy production, or mark
  the task accepted without the repository's explicit review/acceptance gate.
