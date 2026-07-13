# Refine connection table filters

## Goal

Make the connection-detail table filters faster to use and prevent table actions from obscuring the final column heading.

## Requirements

- Keep each filter arrow visually adjacent to its column label while preserving separate sort and filter hit targets.
- Keep the clear and global-search icon buttons at the right edge of the table header without overlapping the connection-status heading.
- For IP version, application, protocol, outbound line, and flags, opening the column filter must immediately show all available choices; choosing an option applies it without requiring a second native-select click.
- Generate application, protocol, and outbound-line choices from the current scoped connection rows. Keep the existing text filters for local and destination endpoints and the existing status filter behavior.
- Preserve current sorting, combined-filter behavior, accessible labels/focus states, mobile touch targets, and panel anchoring.
- On mobile, compact the terminal-monitor toolbar into fewer, evenly sized rows without removing any controls.
- On mobile connection details, move clear and global-search actions from the table header to the connection-detail tab row. Hide them on every other detail tab.
- Match the second-level terminal, traffic, and runtime monitor menu typography to the line-monitor item.

## Acceptance Criteria

- [x] On desktop, the visible gap between a filterable column label and its arrow is compact and consistent.
- [x] The clear/search actions do not cover or collide with the connection-status column label.
- [x] IP version opens choices for All, IPv4, and IPv6 in one click; application, protocol, outbound line, and flags likewise show direct option lists in one click.
- [x] Selecting a direct option applies the filter, marks the column filter active, and closes the panel.
- [x] Clear resets direct-option filters, search, and sorting as before.
- [x] Production lint/build, Go tests, and browser verification at desktop and mobile sizes pass without console errors or document-level horizontal overflow.
- [x] At 375px, the terminal toolbar uses three coordinated rows with consistent 44px control heights.
- [x] At 375px, clear/search actions appear at the right of the tab row only while connection details are active, and no longer overlap table headings.
- [x] The second-level monitor menu labels share the same font size and visual weight.

## Out of Scope

- Backend/API changes.
- Redesigning table columns or changing connection data semantics.
