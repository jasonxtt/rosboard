# Technical Design

## Scope

This task adds the ingestion, domain classification, and connection-attribution layer. RouterOS remains the source of connection counters; MosDNS and the public domain list only enrich the application label when a time-scoped match is available.

## Data flow

1. `cmd/rosboard` starts an optional process-scoped MosDNS synchronizer when `config.MosDNS` is configured.
2. The synchronizer calls `GET /api/v2/audit/logs` at the configured interval, starting from page one and walking older pages until it reaches its persisted watermark.
3. The client decodes the custom v2 response, normalizes client addresses/domains/answer addresses, and expands A/AAAA answers into observations.
4. The store inserts observations with `INSERT OR IGNORE` on a deterministic deduplication key and updates the watermark in the same SQLite transaction.
5. The store prunes observations older than the existing sample retention period. A failed fetch never advances the watermark.
6. Each newly inserted observation also upserts a durable `dns_features` fact keyed by client IP, domain, and answer IP, updating first/last seen and hit count. This table is never pruned by TTL or raw-log retention; existing observations are backfilled when the schema is first upgraded.
7. The connection builder matches recent DNS observations by normalized client IP and answer IP first, then falls back to the durable learned feature fact; unmatched rows retain the existing port-based estimate.
8. Read-only API endpoints and settings expose sync/classification status and configuration, including the learned feature count and last learning time.

## Ownership and storage

MosDNS is global to the rosboard process rather than owned by an individual RouterOS monitor. Observations therefore live in the owner SQLite database, not in per-device databases. The observation carries the client IP; later flow attribution can associate that address with the selected RouterOS device.

The resolver loads recent observations plus the durable learned feature table into an in-memory index and refreshes that index periodically, avoiding one SQLite query per connection. Recent observations take precedence; the learned fallback is still keyed by client IP and answer IP so shared CDN addresses are not treated as one permanent global owner.

The schema stores one row per DNS answer. Query records without A/AAAA answers are not useful for IP attribution and are intentionally not expanded into the answer table. The deduplication key includes the trace ID when available and a content fingerprint fallback otherwise. The answer IP is part of the key so one query returning multiple addresses remains lossless.

The watermark stores the newest boundary encountered during a completed sync as `query_time` plus `trace_id`. Because the current API is page-based rather than cursor-based, the synchronizer fetches newest pages until it sees a record older than that boundary. It keeps a bounded page count and returns an explicit overflow error if the ring buffer is too active to reach the watermark.

## Configuration

```yaml
mosdns:
  enabled: true
  base_url: http://10.0.0.3
  sync_interval_minutes: 30
```

Synchronization is enabled by default for the local MosDNS instance at `10.0.0.3`; set `enabled: false` to disable it. The interval must be positive and defaults to 30 minutes when omitted. No MosDNS credentials are added in this increment because the observed API is local read-only access.

```yaml
feature_library:
  enabled: true
  source_url: https://github.com/v2fly/domain-list-community/releases/latest/download/dlc.dat_plain.yml
  refresh_interval_hours: 168
  match_window_minutes: 30
```

The default source is the public V2Fly plain geosite release; only curated application lists are used for labels. Feature-library failure leaves the DNS store and port-estimated fallback usable.

## API

- `GET /api/mosdns`: current configuration, enabled state, last attempt/success, imported/duplicate counts, watermark, and last error.
- `GET /api/mosdns/observations?limit=N`: newest normalized DNS answer observations.
- `GET /api/recognition`: combined MosDNS and feature-library runtime status.
- `POST /api/settings/recognition`: persist the independent MosDNS/feature-library switches and their source/interval/window settings, then restart rosboard.

`GET /api/mosdns` and the settings response also report the durable learned-feature count and its latest observation time.

These endpoints are read-only and use the existing rosboard authentication/allowed-CIDR gates.

## Failure and compatibility behavior

- MosDNS being unreachable logs a warning and leaves RouterOS monitors running.
- Malformed individual records are skipped with a count; malformed top-level responses fail the sync without advancing the watermark.
- Existing configs without a `mosdns` section remain valid and use the local default, unless synchronization is explicitly disabled.
- Existing database files receive additive tables only.
