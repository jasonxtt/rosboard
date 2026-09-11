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
