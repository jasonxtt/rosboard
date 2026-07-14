# Plan terminal scope summaries

## Goal

Add a compact, glanceable summary to the terminal-list header for the three
terminal scopes: all terminals, IPv4, and IPv6.

## Requirements

- Show six summary fields: device count, connection count, current upload
  rate, current download rate, cumulative upload, and cumulative download.
- The all-terminals scope combines IPv4 and IPv6 traffic and connection data.
- Device count includes only online LAN terminals and excludes
  `routeros:self`, inactive terminals, and offline terminals. The all scope
  deduplicates dual-stack devices; IPv4 counts only devices with IPv4 and IPv6
  counts only devices with IPv6.
- The IPv4 scope shows IPv4-only connection and traffic values; the IPv6 scope
  shows IPv6-only connection and traffic values.
- IPv4 list rows require at least one IPv4 address; IPv6 list rows require at
  least one IPv6 address. A device without the selected family never appears
  in that family table, even if it has the other family.
- All three scopes use active-conntrack cumulative upload/download. The all
  scope sums IPv4 and IPv6 active bytes; no persisted lifetime total appears in
  this header summary.
- Reuse the compact inline visual language from the terminal detail header,
  while keeping the terminal list toolbar and table readable.
- Header summaries are computed from the complete dashboard terminal snapshot
  for the selected family scope. Search, state filter, interface filter,
  pagination, and page size do not change them.
- The existing toolbar result count continues to describe filtered table rows.
- All six fields exclude `routeros:self`. Connection, rate, and active-byte
  aggregation uses the same online LAN client set as device count.
- Plan responsive behavior for desktop and mobile before implementation.
- On desktop, place the compact summary in the terminal topbar between the page
  title block and the global status/refresh controls.
- On narrow/mobile layouts, move the same six values to a full-width second
  topbar row arranged as two rows of three values. Do not add them to the
  terminal filter toolbar.
- Use the terminal detail header's muted 11-12px inline style, tabular numerals,
  and existing arrow notation without adding a new card or heavy border.

## Acceptance Criteria

- [ ] Each of the three terminal scopes displays all six summary fields.
- [ ] All-scope device count counts each online LAN terminal once; IPv4/IPv6
      device counts include only devices eligible for that family.
- [ ] All-scope connection, rates, and active cumulative values equal the IPv4
      plus IPv6 family summaries from the same dashboard snapshot.
- [ ] IPv4 and IPv6 scopes never display the opposite family's connection or
      traffic values.
- [ ] IPv4 rows all have an IPv4 address and IPv6 rows all have an IPv6
      address; family-ineligible devices are absent.
- [ ] Values use the existing rate and byte formatters.
- [ ] Summary placement does not overlap filters, refresh controls, table
      headers, or mobile navigation.
- [ ] Desktop summary occupies the topbar middle region; mobile renders all six
      values in a two-by-three summary grid without horizontal overflow.
- [ ] Changing search, state, interface, sort, page, or page size leaves the
      header summary unchanged while the toolbar result count still updates.
- [ ] Automated tests cover mixed-family, single-family, and empty data.
- [ ] RouterOS self, inactive/offline terminals, and WAN/selected-traffic-
      interface terminals contribute to none of the six summary values.

## Notes

- The user supplied the current terminal-list and terminal-detail header as
  layout references.
- This is planning-only until the user reviews the final PRD, design, and
  implementation plan.

## Confirmed Technical Facts

- `/api/dashboard` already returns one deduplicated `Terminal` per device and
  `familyStats.ipv4` / `familyStats.ipv6` for current connection count, upload,
  download, and bytes held by currently active conntrack rows.
- Combined terminal totals are locally persisted across conntrack expiry, but
  the SQLite total does not record IPv4 and IPv6 separately.
- Existing connection snapshots do not persist their family. Historical
  combined bytes therefore cannot be backfilled accurately into IPv4/IPv6.
- Current terminal rows intentionally label family-specific totals as
  `活动累计上行/下行`, while all-scope rows use persisted `累计上行/下行`.
- The existing detail header already provides the intended compact inline
  styling pattern and updates on the live dashboard refresh cadence.
- Existing backend code already owns the authoritative online-LAN predicate in
  `connectedLANDeviceCount`; family summary aggregation should reuse that
  boundary instead of duplicating WAN exclusions in React.
