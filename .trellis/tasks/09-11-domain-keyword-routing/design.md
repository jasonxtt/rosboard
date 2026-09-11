# Technical design

## Boundaries and data flow

The implementation follows the existing source pipeline:

`source bytes -> policy parser -> SourceVersion/SourceRule + Counts ->
canonical RoutingRule -> desired projection -> RouterOS scan/diff/apply -> UI
plan review`.

The parser remains the single source of truth for keyword recognition and
normalization. The frontend receives counts and keyword impact from API plan
data; it does not inspect source text.

## Contracts

### Source rules

Introduce `DOMAIN-KEYWORD` as a first-class rule type. Line-list
`keyword:<value>` maps to that type. Normalized keywords are literal matching
tokens, not user-authored regexes. Existing unsupported forms remain ignored.

Counts retain the existing aggregate fields and add per-rule-type counts so
`DOMAIN-KEYWORD` survives previews, stored versions, target details, and
refreshes.

### Routing rule persistence and compatibility

Add `include_keyword_domains INTEGER NOT NULL DEFAULT 0` to the routing-rule
table with an idempotent upgrade for installations created before the column.
Use presence-aware JSON decoding for API/proposal payloads: a missing field is
false for a new rule and preserves the current row on update. Serialized
canonical rules always expose the boolean.

### Projection

Keep plain and keyword projections separate on each `(egress,target)`:

- plain `DOMAIN`/`DOMAIN-SUFFIX` entries retain the existing address-list
  identity and `name`/`match-subdomain` fields;
- enabled keyword rules use a distinct keyword address-list identity and DNS
  Static `regexp` field containing an escaped literal surrounded by a match
  expression;
- keyword entries are collected globally and sorted ahead of plain entries.

Mangle/output matchers reference both physical lists when both are populated,
so a keyword-only target is not silently lost. A target with no effective
projection is omitted from runtime objects and produces the explicit empty
target blocker when that makes a routing rule empty.

The existing plain-domain overlap arbitration continues to see only plain
rules. Keyword-to-keyword order is deterministic and does not use domain-set
subtraction. Keyword logical IDs include the keyword rule type and are distinct
from `DOMAIN`/`DOMAIN-SUFFIX` identities.

### Plan impact and safety

Add a backend `KeywordImpact` object to a plan. It contains the enabled flag,
available/projected counts, normalized keyword list, introduced keywords,
confirmation requirement, and a stable precedence mode. For a proposed
routing-rule write, compare the proposed keyword set with the current rule;
require acknowledgement only for first enablement, false-to-true enablement,
or newly introduced keywords. Add warning code
`routing_keyword_regexp_precedence` and required acknowledgement code through
the existing `PlanAcknowledgement` mechanism. Access precedence is evaluated
before acknowledgement handling and yields
`routing_keyword_access_precedence_unsafe`.

### Migration and verification

After routing schema initialization, run one transactional schema marker
migration that decompresses existing raw source bytes, reparses domain
content, inserts missing derived keyword rows, and updates derived counts. It
never rewrites compressed bytes or SHA. Invalid/non-gzip test fixture payloads
are left untouched; a restart retries an incomplete migration.

Add `regexp` to managed RouterOS structural fields and extend DNS matcher
identity parsing so scans and generic reconciliation treat keyword entries as
managed. Existing desired hash, actual fingerprint, stale checks, post-verify,
rollback, and cleanup then naturally include the new field/objects.

## UI design

Extend both canonical policy models and parsers. The routing wizard keeps the
toggle in Advanced settings, defaults it on only for a new rule, displays
backend-derived keyword counts and impact, and sends the boolean in every
proposal. Plan review renders a blocking confirmation modal for the keyword
acknowledgement; Continue records the existing ack code and Return opens the
Advanced section and focuses the toggle. Existing acknowledgement controls
remain the final protocol guard.

Target selectors/cards/details show the per-type keyword count where counts are
already displayed. Both main and compact UIs use the same labels and behavior.

## Rollout and rollback

The schema defaults historical rows to false, so enabling the feature is
opt-in. The source migration is additive and idempotent. If validation fails,
the branch/PR remains unmerged and can be reverted without changing source
content. No production deployment is part of this task.
