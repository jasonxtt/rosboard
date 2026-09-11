# Validation checkpoint

Draft PR: https://github.com/jasonxtt/rosboard/pull/18

Implementation is isolated on `codex/fasttrack-compatibility`, based on stable
`main`. The separate MosDNS attribution branch and its commits are preserved.

## Automated checks

- `go test ./...`
- `go build ./...`
- `go vet ./...`
- `go test -race ./internal/policyv2 ./internal/store ./internal/api ./internal/routeros -run 'FastTrack|RoutingRuleWritesRespect|UnsetFirewallConnectionMark' -count=1`
- `npm --prefix web run lint`
- `npm --prefix web test` (51 tests)
- `npm --prefix web run build`
- `npm --prefix web run check:ui-build`
- `npm --prefix web audit --audit-level=high` (zero vulnerabilities)
- `git diff --check`

Regression coverage includes plan-bound acknowledgement rejection, foreign
producer staleness, counter-insensitive fingerprints, no journal without
FastTrack, durable original/intended snapshots, lost mutation responses,
unexpected read-back, sticky external divergence, device isolation/reopen,
multiple consumers, disable, non-final deletion without acquisition, failed
apply retaining compatibility, failed cleanup preventing release, missing
filters, transient and permanent restoration failures, completed restoration
with a lost response, discovery of new filters, and concurrent first proposals.

## Remaining acceptance

The unset HTTP request and property-presence restoration are covered by the
transport fake and lifecycle tests. No isolated RouterOS 7.22.3 instance was
used, so real-device unset normalization and new-connection routing/FastTrack
counter verification remain required before production delivery. The Debian
test machine alone does not validate RouterOS firmware behavior.

No production deployment, backup rotation, merge, release, or task archival was
performed. Keep the PR Draft pending root review and runtime/user acceptance.

## P1 review correction: internal domain follow-up acknowledgement

Confirmed the reported bypass with a real shared-domain Target refresh test.
Before the fix, both complex FastTrack and foreign connection-mark producer
cases incorrectly completed Routing after Access without confirmation.

Interactive apply and internal follow-ups now share a pure acknowledgement
validator. Internal follow-ups provide no hash/accepted codes, discard a plan
requiring confirmation, and stop with `follow-up-acknowledgement-required` and
an explicit re-preview message. The successful Access commit and pending
Routing desired state remain intact. No confirmation is inferred or inherited.

The two regressions now verify no Routing objects/DNS are written, Access stays
applied, Routing stays pending, and a new interactive plan succeeds only with
its exact acknowledgement. Validator cases cover missing/stale hashes, wrong
codes, presentation flags, explicit consent, and plans requiring no consent.
