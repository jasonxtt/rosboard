# Terminal scope summaries implementation plan

1. Add failing backend aggregation tests.
   - Cover online dual-stack, IPv4-only, IPv6-only, RouterOS self,
     inactive/offline, selected-traffic-interface/WAN, and empty inputs.
   - Assert all connection/rate/active-byte values equal IPv4 plus IPv6 while
     all device count deduplicates dual-stack terminals.

2. Add the typed dashboard summary contract.
   - Define `TerminalScopeSummary` and the `terminalScopeSummaries` dashboard
     field in the Go and TypeScript models.
   - Extract/reuse one online-LAN eligibility predicate from
     `connectedLANDeviceCount`.
   - Populate all three summaries during terminal/full snapshot refresh without
     adding RouterOS reads or database writes.

3. Add the terminal topbar summary.
   - Pass the selected scope summary to a small presentational component.
   - Render six fields with existing rate/byte formatters and tabular numerals.
   - Keep the existing filtered result count and terminal toolbar behavior
     unchanged.

4. Implement responsive layout.
   - Desktop: title / summary / global-controls topbar regions.
   - Mobile: title and controls on the first row; six summary values in a
     full-width two-by-three second row.
   - Preserve the existing 44px mobile controls and prevent document-level
     horizontal overflow.

5. Rebuild and verify.
   - Run `gofmt`, `go test ./...`, `npm --prefix web run build`,
     `npm --prefix web run lint`, `npm --prefix web audit`, and
     `git diff --check`.
   - Rebuild the Go binary, restart the local service, and verify HTTP 200 for
     `/` and `/api/dashboard`.
   - At 1440px and 375px, verify all six values, correct family switching,
     filter independence, no overlap, and no page-level overflow.
   - Compare live API values: all connection/rate/active bytes equal IPv4 plus
     IPv6; all device count is deduplicated and RouterOS self is excluded.

## Risk and rollback points

- Do not reuse `Terminal.TotalUploadBytes` / `TotalDownloadBytes` in the new
  summary; those are persisted lifetime totals and break the additive family
  invariant.
- Do not aggregate from `filteredTerminals`; it already contains search and
  family filtering.
- If layout pressure appears, wrap the summary within its topbar region; do not
  move or compress existing refresh controls.
- No migration rollback is necessary because this plan changes no schema.
