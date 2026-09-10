# SourceFetcher FakeIP and synthetic DNS compatibility

## Goal

Make `internal/policy.SourceFetcher` tolerate a DNS resolver that returns the
unusable IPv6 sentinel `::ffff` alongside a valid source address, while
preserving the existing SSRF boundary for real private, local, link-local,
ULA, and other forbidden destinations.

## Requirements

- Classify resolved addresses as usable, ignorable synthetic/unusable
  sentinels, or forbidden security-sensitive addresses.
- Explicitly allow only the rosboard FakeIP ranges `198.18.0.0/15`,
  `28.0.0.0/8`, `2001:2::/64`, and `f2b0::/18`, with strict prefix matching.
- Drop `0.0.0.0`, `::`, and `::ffff` as unusable sentinels, but canonicalize
  IPv4-mapped addresses before classification so mapped private addresses are
  still rejected.
- Keep only usable addresses for pinned dialing; return a retryable transport
  failure when no usable address remains.
- Keep HTTPS-only fetching, redirect revalidation, DNS pinning, response
  limits, content validation, and preset fallback behavior intact.

## Acceptance Criteria

- [ ] FakeIP allowlist and prefix-boundary cases are covered by tests.
- [ ] Mixed public/FakeIP plus `::ffff` succeeds and pins only usable IPs.
- [ ] Only-sentinel resolution fails without dialing and is retryable.
- [ ] Mixed public plus real forbidden and mapped-private answers still fail
      closed.
- [ ] Application preset fallback treats only-sentinel primary resolution as a
      retryable transport failure.
- [ ] Targeted, race, full, vet, formatting, and diff checks pass.
- [ ] A draft PR is pushed for review before test-machine deployment.

## Notes

- Keep `prd.md` focused on requirements, constraints, and acceptance criteria.
- Lightweight tasks can remain PRD-only.
- For complex tasks, add `design.md` for technical design and `implement.md` for execution planning before `task.py start`.
