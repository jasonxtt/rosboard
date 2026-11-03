# Production acceptance checkpoint — 2026-10-28

## User decision
The user explicitly accepted this part of the delivery: “这部分验收通过 记录下 暂不合并main”. Record scoped production acceptance; do not merge main, finalize a release, or archive the task. Draft PR #34 remains open.

## Accepted scope and observed evidence
- Production rosboard on net runs `dev-family-block-10ce620`.
- On the client used for the production check, Google AAAA answers remained available.
- A fresh Google IPv6 HTTPS attempt matched the policy block connection mark; the forward DROP counter increased from 0 to 7 packets / 560 bytes, matching the Google classifier's 7 packets / 560 bytes. The client timed out as expected for silent DROP; the traffic was blocked before WAN egress instead of bypassing via main.
- The IPv4 control returned HTTP 200 in approximately 1.42 seconds.
- The new-connection-only boundary remains: existing connections are not actively disconnected or removed from conntrack.
- This checkpoint accepts the above ssjy/client production scenario. It does not claim a separate manual production acceptance for every reverse-family, source-selection or lifecycle scenario covered by automated/lab checks.

## Source and delivery identity
- Feature PR: https://github.com/jasonxtt/rosboard/pull/34
- Reviewed feature HEAD: `10ce620f066c72cadd78ad2dc98e848fff0e3c90`.
- Deployed integration HEAD: `5eccbfb9e5ab860f27888752f96db7e81a35fc5a`, combining the existing production feature baseline `086aace` with the reviewed family-blocking changes and regenerated embedded UI.
- Binary SHA-256: `1250ba0418b087d51a9cbb4aeba609ce93897351e98fbabbf8c9203363eed87c`.
- Pre-deployment NAS snapshot label: `20261027T234135Z-family-block-10ce620`. Its deployment manifest and integration bundle preserve rollback and source traceability.

## Hold state
Production scope accepted; integration into main is explicitly on hold at the user's request. Keep the task unarchived, the feature branch intact and PR #34 Draft. Do not redeploy, merge, publish a release or remove the task branch based on this acceptance alone.
