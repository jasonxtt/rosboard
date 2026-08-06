# Add MosDNS DNS feature ingestion

## Goal

Add a read-only MosDNS ingestion and application-recognition path that lets rosboard build a local, time-scoped DNS answer library and use it to enrich RouterOS connection rows without changing RouterOS or MosDNS.

## Requirements

- Add MosDNS configuration with a default local endpoint and synchronization interval of 30 minutes, plus an explicit disable switch.
- Read the existing MosDNS v2 audit log API only; do not write to MosDNS, restart it, or use SSH at runtime.
- Import new audit records incrementally and persist a watermark so a restart does not require reprocessing the whole ring buffer.
- Deduplicate repeated delivery of the same audit record by `trace_id`, with a deterministic content fingerprint fallback when `trace_id` is absent.
- Preserve distinct queries made at different times; deduplication must not collapse legitimate repeated DNS activity.
- Persist answer observations with client IP, normalized domain, answer IP, query type, query time, TTL, effective tag, source trace ID, and ingestion time.
- Keep IP/domain observations time-scoped and prune them according to existing local retention policy; do not treat a CDN address as a permanent global domain mapping.
- Maintain a separate long-term learned IP feature table that is continuously upserted from new observations and is not removed when DNS TTL or raw-observation retention expires; retain the client-IP dimension to reduce shared-CDN misattribution.
- Expose read-only synchronization status and recent observation data through rosboard for diagnostics and future application attribution.
- Match current RouterOS connections to recent client-IP/answer-IP DNS observations, classify matched domains with a public domain feature library, and surface the result in terminal connection details and protocol statistics.
- Make MosDNS integration and feature-library enablement, source, refresh interval, and match window configurable through rosboard settings.
- Keep the existing RouterOS monitoring behavior unchanged and degrade gracefully when MosDNS is unavailable.

## Acceptance Criteria

- [x] A configuration with MosDNS enabled defaults to a 30-minute sync interval and validates invalid values.
- [x] A successful sync reads paginated audit records, saves new answer observations, advances the watermark, and reports counts.
- [x] Replaying the same page or restarting rosboard does not create duplicate observations.
- [x] Repeated queries at different timestamps remain separate observations or update only their aggregate counters as designed.
- [x] IPv4-mapped IPv6 client addresses, trailing-dot domains, case differences, and duplicate answers are normalized consistently.
- [x] MosDNS HTTP errors leave existing data and the previous watermark intact and do not stop RouterOS monitoring.
- [x] Automated tests cover API decoding, pagination/watermark behavior, deduplication, normalization, configuration, and SQLite persistence.
- [x] Local runtime verification confirms the live MosDNS endpoint is read only and no RouterOS/MosDNS configuration changes are made.
- [x] A matched DNS domain changes the terminal connection application and the aggregate protocol statistic, while an unmatched connection retains the port-estimated fallback.
- [x] Settings can disable MosDNS or the feature library independently and persist the source/refresh/match settings across restart.
- [x] Existing DNS observations are backfilled into a durable learned IP feature table, and later raw-log pruning leaves those learned facts available for matching.

## Constraints

- Do not modify RouterOS configuration.
- Do not modify MosDNS configuration or install a MosDNS plugin/sidecar.
- Do not make MosDNS a high-frequency polling source; 30 minutes is the initial cadence.
- Preserve unrelated working-tree changes already present in the repository.
