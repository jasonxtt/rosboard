# RouterOS Monitor Panel Design

## Overview

`v1` will be a single-router, read-only monitoring application for the live RouterOS device at `10.0.0.1`. It will run as a single native Linux service, poll RouterOS REST endpoints on a schedule, persist local history/accounting, and serve a browser dashboard over HTTP inside the LAN.

The design optimizes for:

- always-on binary deployment
- low operational complexity
- useful terminal monitoring over perfect iKuai feature parity
- clear separation between "live RouterOS facts" and "panel-computed history"

## Recommended Stack

### Backend

- Go
- SQLite for local persistence
- built-in `net/http` plus small routing/middleware layer

Reasoning:

- Go is a strong fit for a single Linux binary deployment target.
- Router polling, background workers, local persistence, and embedded static assets are straightforward in Go.
- SQLite keeps storage local, durable, and simple for a single-node LAN deployment.

### Frontend

- React + TypeScript
- Vite build
- embedded built frontend assets inside the Go binary

Reasoning:

- The dashboard needs dynamic charts, dense tables, refreshable status tiles, and stateful filters.
- React + TypeScript keeps frontend state and data contracts explicit.
- Runtime deployment still stays binary-only because compiled assets are embedded into the backend binary.

## Scope Boundaries

### Included in `v1`

- system overview
- interface / line status
- unified terminal monitoring
- concise capability notes explaining unsupported or config-dependent metrics

### Excluded from `v1`

- RouterOS write operations
- panel login flow
- multi-router support
- wireless / AC / AP modules
- camera / switch / peripheral monitoring
- half-supported protocol / policy / load / split-flow pages

## Data Flow

### High-level flow

```text
RouterOS REST
  -> pollers
  -> normalization layer
  -> SQLite persistence
  -> query/service layer
  -> HTTP JSON API
  -> frontend dashboard
```

### Polling flows

1. Router snapshot poll:
   - `system/resource`
   - `system/health`
   - `interface`
   - `interface/ethernet`
   - `ip/address`
2. Interface rate poll:
   - `interface/monitor-traffic` for selected interfaces
3. Terminal identity poll:
   - `ip/dhcp-server/lease`
   - `ip/arp`
   - `ipv6/neighbor`
4. Terminal traffic / connection poll:
   - `ip/firewall/connection`
   - `ipv6/firewall/connection`

## Layer Boundaries and Contracts

### Router client boundary

Responsibility:

- authenticate to RouterOS REST
- fetch typed endpoint payloads
- convert RouterOS field naming into internal typed models
- fail loudly on unexpected payload shape

Internal contract examples:

- `RouterResourceSnapshot`
- `InterfaceSnapshot`
- `ConnectionSnapshotV4`
- `ConnectionSnapshotV6`
- `NeighborSnapshot`

Validation rule:

- raw REST JSON is decoded once at the client boundary
- downstream layers do not cast RouterOS payload fields ad hoc

### Normalization boundary

Responsibility:

- merge raw RouterOS facts into panel-owned domain views
- keep one owner for identity correlation and traffic attribution logic

Core normalized models:

- `RouterOverview`
- `InterfaceStatus`
- `TerminalIdentity`
- `TerminalLiveStats`
- `CapabilityNote`

### Persistence boundary

Responsibility:

- retain time-series samples needed for charts
- retain cumulative accounting state
- retain previous connection counters needed to compute deltas

Stored data categories:

- interface rate samples
- router overview snapshots
- terminal cumulative totals
- seen terminal identities and alias addresses
- previous connection counter cache

### API boundary

Responsibility:

- expose frontend-ready JSON
- avoid leaking RouterOS raw shapes to the UI

Planned API shape:

- `GET /api/overview`
- `GET /api/interfaces`
- `GET /api/interfaces/history?window=5m`
- `GET /api/terminals`
- `GET /api/capabilities`

Future auth hook:

- all `/api/*` routes should pass through a middleware chain that currently allows LAN-only access but can later host auth/session enforcement.

## Terminal Identity Model

### Goal

Present one row per device as much as possible, combining:

- IPv4 addresses
- IPv6 addresses
- MAC address
- host/comment names when available
- live connection/rate data
- persisted cumulative totals

### Identity strategy

Primary identity preference:

1. MAC address when known
2. stable fallback synthetic ID when MAC is unknown

Merge inputs:

- DHCP lease data for IPv4 hostnames/comments
- ARP for IPv4-to-MAC correlation
- IPv6 neighbor table for IPv6-to-MAC correlation
- connection tables for live traffic and connection counts

Practical rule:

- if v4 and v6 addresses resolve to the same MAC, they belong to one terminal row
- if an address has traffic but no MAC yet, create a provisional terminal row and merge later if a MAC becomes known

## Cumulative Traffic Design

This is the main nontrivial part of the system.

### Constraint

The live RouterOS does not currently expose the desired per-terminal cumulative totals directly through the verified REST data surfaces.

### Proposed solution

Maintain panel-side cumulative accounting from connection deltas.

Mechanism:

1. Poll v4/v6 connection tables.
2. Build a stable per-connection key from protocol + addresses + ports + direction-defining fields.
3. For each seen connection, compare current byte counters to the previously stored counters.
4. Compute non-negative deltas for origin/reply directions.
5. Attribute those deltas to the owning terminal identity.
6. Add the deltas into persisted cumulative terminal totals.
7. Persist the latest per-connection counters for the next poll.

Why this shape:

- summing current connection totals per terminal is not enough because connections expire and totals would appear to drop
- delta-based accounting survives connection churn
- persisted state allows totals to survive process restarts

Known trade-off:

- cumulative totals begin when the panel starts tracking; there is no reliable historical backfill from the current router state

## Time-Series Design

### 5-minute interface chart

- poll interface monitor traffic every 5 seconds
- store samples with timestamp + rx bps + tx bps
- query the last 5 minutes for chart rendering
- prune old high-frequency samples on a retention schedule

### Overview refresh

- overview cards can reuse the same 5-second polling cadence

## Capability Notes Design

The UI should include a compact capability section that distinguishes:

- available now
- available after RouterOS config changes
- not provided natively on the current data surface

This keeps `v1` honest and prevents fake placeholder pages.

## Deployment Shape

### Runtime layout

- one Linux binary
- one writable data directory
- one config file
- one `systemd` unit

Suggested writable contents:

- `config.yaml`
- `data/rosboard.db`
- `logs/`

### Config inputs

- RouterOS base URL
- RouterOS username
- RouterOS password
- poll interval settings
- listen address / port
- optional trusted LAN CIDRs for current auth placeholder middleware

## Compatibility and Expansion

### Planned future additions

- auth/session layer
- Docker packaging
- multi-router support
- richer protocol / policy / load / split-flow views

### Decisions that keep expansion possible

- router access isolated in a single client package
- API routes mounted behind middleware
- persistence schema separated into router facts, terminal identities, terminal totals, and time-series samples
- no frontend dependence on raw RouterOS field names

## Main Risks

- identity correlation can be imperfect when a device has traffic but no visible MAC mapping yet
- panel-side cumulative accounting depends on stable polling and careful connection-key design
- some iKuai visual concepts do not map cleanly to RouterOS native data

## Rollback / Simplification Options

- if per-terminal cumulative accounting proves too fragile in the first build, keep the schema but temporarily expose a "tracking since" label while preserving the read-only terminal table
- if embedded frontend build complexity slows iteration too much, keep the backend API contract stable and temporarily simplify the UI component layer rather than changing runtime architecture
