# RouterOS monitoring parity and UI overhaul implementation plan

## Ordered Checklist

1. Establish normalized state and storage foundations
   - add poller freshness/error contracts
   - add additive SQLite migrations for load/protocol buckets and terminal sessions
   - add batch connection accounting and stale-state pruning
   - verify existing cumulative totals survive migration

2. Correct terminal identity and state semantics
   - numeric address sorting and primary-address selection
   - evidence-based online/offline/idle state, online sessions, and last-seen handling
   - remove LAN-interface-as-egress-line behavior and implement evidence-based attribution
   - add focused service/store tests

3. Rebuild terminal list and detail UI
   - introduce monitoring navigation hierarchy and route state
   - compact terminal list toolbar, sortable headers, filters, pagination, refresh controls
   - default IPv4 numeric ascending order
   - compact terminal detail, remove remark editing there, add live detail refresh and connection filters
   - verify the table starts near the summary/toolbar and no duplicate content remains

4. Complete line monitoring
   - extend RouterOS client/types for available interface properties and monitoring commands
   - collect rates for all monitorable interfaces
   - add interface list/detail API and history query
   - build compact line list and per-interface detail chart

5. Add load history
   - persist CPU, memory, storage if available, terminal count, rates, and packet metrics
   - add bounded bucket aggregation and 1h/1d/1w/1m queries
   - build load-history charts with ranges, axes/tooltips, current and maximum values

6. Add native protocol monitoring
   - aggregate connection tracking by L4 protocol and estimated application category
   - persist bounded time buckets and 30-minute distribution
   - build protocol summary/trend page with explicit estimated/native labelling

7. Add native policy and routing/split monitoring
   - read existing queue, queue-tree, firewall/mangle counter, connection-mark, routing-table/rule, and active-route data where available
   - normalize empty/unsupported/readable states independently
   - build policy and routing/split pages without write actions

8. Harden refresh and API behavior
   - isolate optional poller failures
   - expose freshness/error state
   - keep last-good data during partial failures
   - ensure focused endpoints avoid oversized dashboard payloads

9. Full verification and cleanup
   - run backend tests and build
   - run frontend lint, type-check, and production build
   - inspect desktop UI in the local browser at list/detail/chart pages
   - verify embedded assets through the Go server
   - verify git diff contains only task-scoped changes

## Validation Commands

- `go test ./...`
- `go build ./...`
- `cd web && npm run lint`
- `cd web && npm run build`
- API smoke requests against the local running application
- browser checks at 1280px and a wider desktop viewport

## Review Gates

- Gate A after steps 1-3: terminal semantics and UI acceptance criteria pass.
- Gate B after steps 4-5: line and load monitoring are complete and storage remains bounded.
- Gate C after steps 6-8: native protocol/policy/routing pages are honest, resilient, and read-only.
- Final gate: full regression suite and live browser verification.

## Risky Files and Areas

- `internal/service/monitor.go`: identity correlation, connection orientation, poller split
- `internal/store/sqlite.go`: additive migrations, batch accounting, retention
- `internal/routeros/*`: optional fields and endpoint compatibility
- `internal/api/server.go`: focused route parsing and compatibility
- `web/src/App.tsx`: current monolith must be split surgically without losing working views
- embedded frontend assets: source build and served build must stay synchronized

## Rollback Points

- Preserve existing tables and `/api/dashboard` compatibility before UI replacement.
- Complete and verify terminal changes before expanding RouterOS endpoint coverage.
- Treat each optional RouterOS monitoring surface as independently disableable when unavailable.

## Pre-Start Review

- Scope excludes every RouterOS write and all external-device monitoring.
- The user explicitly approved implementation after reviewing the analysis and scope.
- The task is ready to start once the PRD/design/implementation artifacts pass the convergence check.
