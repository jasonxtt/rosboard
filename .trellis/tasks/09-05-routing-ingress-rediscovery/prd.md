# Second policy rule loses LAN ingress candidates after apply

## Goal

Keep policy-routing discovery idempotent after the first rule is applied. A
second rule must be able to select the same user-owned LAN interface list or
interface that was available when the first rule was created.

## Requirements

- Keep a physical LAN interface discoverable after the first policy apply adds
  a rosboard-owned custom-table default route through that interface.
- Preserve user-owned LAN/interface-list candidates after rosboard materializes
  its managed traffic-ingress aggregate list.
- Do not expose the managed aggregate as a user-selectable ingress candidate.
- Preserve the existing WAN exclusion and LAN classification behavior for
  managed ingress members and genuine WAN routes.
- Keep per-rule ingress ownership and the existing proposal, preview, apply,
  TargetList, and frontend wizard contracts unchanged.
- Add a deterministic regression test for the post-apply topology, including
  the aggregate-list `include=LAN` shape that can hide the original candidate.
- Keep the change on `feat/policy-access-rebuild` and Draft PR #4; do not merge
  or deploy to production.

## Acceptance Criteria

- [ ] Discovery before and after adding a rosboard-owned custom-table default
      route via a physical LAN interface returns that interface as a
      traffic-ingress candidate.
- [ ] A genuine unmanaged WAN route remains a WAN candidate and is not exposed
      as an ingress candidate merely because it has a private address.
- [ ] Discovery before and after adding a managed aggregate that includes a
      user LAN list returns the user LAN list as a traffic-ingress candidate.
- [ ] The managed aggregate is omitted from user-facing ingress candidates.
- [ ] A managed aggregate with direct interface members still prevents those
      interfaces from being misclassified as WAN candidates.
- [ ] Nested user interface lists remain discoverable and resolvable.
- [ ] Existing discovery, API, frontend, and full Go quality checks pass.
- [ ] The root reviewer explicitly reports `APPROVED` for the pushed HEAD.
- [ ] The approved build is deployed only to `10.0.0.60`, with health and
      embedded assets verified, for user manual re-validation.

## Constraints

- Do not change `RoutingRuleWizard`, proposal/plan/apply, canonical target
  handling, Access Control, or production `10.0.0.6`.
- Do not hard-code the test machine's RouterOS data.
