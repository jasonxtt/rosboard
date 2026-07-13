# Refine mobile terminal controls

## Goal

Make the terminal-monitor controls and terminal-detail tabs compact, coordinated, and fully readable on phone-sized screens.

## Requirements

- At phone widths, show the complete terminal-monitor control set in exactly two rows.
- Keep every terminal-monitor input, select, and button the same height; the online-state control and manual-refresh button must not differ visually.
- Preserve search, state, interface, inactive-device toggle, result count, refresh-period selection, and manual refresh behavior.
- Prevent the terminal-detail tab row from horizontal scrolling.
- Keep all four available detail tabs visible when the all-terminal scope includes history.
- Make the mobile clear and search icon controls smaller and position them without covering the history tab.
- Show clear/search only while the connection-detail tab is active and preserve their existing behavior and accessible labels.
- Do not change desktop layout or backend behavior.

## Acceptance Criteria

- [x] At 375px, the terminal-monitor toolbar occupies exactly two visual rows.
- [x] All interactive terminal-monitor controls have the same computed height.
- [x] No control label is covered and the document has no horizontal overflow.
- [x] At 375px, all detail tabs are visible without scrolling and the tab row has no horizontal scrollbar.
- [x] Clear/search do not overlap the history label, remain usable, and disappear on other detail tabs.
- [x] Frontend lint/build, Go tests, desktop/mobile browser checks, and console-log checks pass.

## Out of Scope

- Backend/API changes.
- Changes to terminal data or filter semantics.
- A broader redesign of desktop navigation or tables.
