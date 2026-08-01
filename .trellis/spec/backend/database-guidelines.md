# Database Guidelines

## Scenario: Device-scoped monitoring persistence

### Contracts

- Every monitoring table includes `device_id` in its primary or unique ownership key.
- `Store.OpenDevice(id)` owns an isolated SQLite database for each non-default device; every read, write, prune, merge, and metadata update remains inside that device ID. `Store.ForDevice(id)` preserves the helper API and routes non-default IDs through the same owner-managed store.
- The versioned migration rebuilds legacy tables in one transaction and assigns every old row to `default`.
- The unique terminal MAC key is `(device_id, mac)`; identical MAC addresses on different routers are valid and never merge.
- Archive performs no database deletion. Permanent purge deletes all tables for one device in one transaction.
- Migration failure rolls back, and an old binary must be restored together with the pre-migration database.
- Per-device files use a collision-resistant filename derived from the device ID; sanitization must never make two IDs share a database.
- `load_samples.connection_count` stores the RouterOS IPv4+IPv6 conntrack total in the same minute bucket as CPU, memory, terminal count, and traffic. Legacy rows migrate with `-1` to mean unavailable; never present an invented zero as historical evidence.

### Tests Required

- Open a legacy database containing terminal totals and metadata; verify all values survive under `default` only.
- Insert the same MAC in two device scopes and verify metadata, totals, pruning, and purge cannot cross scopes.
- Save and load a connection count in one device scope, verify another device cannot read it, and verify legacy load rows retain the `-1` unavailable marker.

## Scenario: Merge a temporary terminal identity into a stable identity

### 1. Scope / Trigger

- Trigger: changes to terminal identity correlation or `Store.MergeTerminal` that move persisted data from an `addr:` identity to a `mac:` or `routeros:self` identity.
- SQLite writes remain local panel state; they never write RouterOS configuration.

### 2. Signatures

- Store method: `MergeTerminal(ctx context.Context, fromID, toID string) error`.
- Address key: `PRIMARY KEY (terminal_id, family, address)` in `terminal_addresses`.
- Related rows: `terminal_totals`, `terminal_addresses`, `connection_state`, and `terminals` are updated in one transaction.

### 3. Contracts

- Empty IDs and identical IDs are no-ops.
- A source and target may already contain the same `(family, address)`; this is normal when neighbor discovery and MAC correlation overlap.
- Address moves use insert/upsert into the target followed by deletion from the source. On conflict, retain `max(existing.last_seen, incoming.last_seen)`.
- Do not delete source addresses until all target upserts succeed.
- Totals, addresses, connection ownership, and source-terminal deletion commit atomically; any error rolls back the full merge.
- A merge failure prevents `Monitor.refresh` from publishing its newly collected snapshot, so persistent failures appear in the UI as a frozen `updatedAt` and unchanged metrics.

### 4. Validation & Error Matrix

- `fromID == ""`, `toID == ""`, or IDs equal -> return nil without writes.
- Source address absent from target -> insert it under target and remove it from source.
- Source address already exists under target -> keep one target row with the greater `last_seen`, then remove the source row.
- Any target address upsert fails -> roll back totals and all identity changes.
- Dashboard `updatedAt` stops advancing -> inspect monitor logs for storage errors before changing frontend polling.

### 5. Good/Base/Bad Cases

- Good: an IPv6 `addr:fc00::20` identity merges into a known MAC that already owns `fc00::20`; one target row survives with the newest timestamp.
- Base: a source identity has only new addresses; all move to the target.
- Bad: directly update `terminal_id` when the target can already own the same address; SQLite raises a unique-key error and every later monitor snapshot remains unpublished.
- Bad: repair a merge conflict by deleting the database; this discards history and hides the identity-correlation defect.

### 6. Tests Required

- Store unit: source and target share an address; merge succeeds and exactly one target row remains.
- Store unit: merged duplicate uses the greater `last_seen` and a source-only address also moves.
- Store unit: source address rows are empty after merge.
- Live regression: use the existing database, verify monitor logs contain no merge constraint errors, and verify `/api/dashboard.overview.updatedAt` advances across at least two poll intervals.

### 7. Wrong vs Correct

#### Wrong

```sql
UPDATE terminal_addresses
SET terminal_id = :to_id
WHERE terminal_id = :from_id;
```

#### Correct

```sql
INSERT INTO terminal_addresses (terminal_id, family, address, last_seen)
SELECT :to_id, family, address, last_seen
FROM terminal_addresses
WHERE terminal_id = :from_id
ON CONFLICT(terminal_id, family, address) DO UPDATE SET
  last_seen = max(terminal_addresses.last_seen, excluded.last_seen);

DELETE FROM terminal_addresses WHERE terminal_id = :from_id;
```
