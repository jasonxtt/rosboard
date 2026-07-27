# Dashboard traffic time ranges

## Goal

Let users inspect home-page traffic over `5min`, `1h`, `6h`, and `24h` without leaving the dashboard.

## Requirements

- Query persisted interface samples for the selected device and time range.
- Bound response size through deterministic server-side downsampling.
- Preserve chronological order and upload/download semantics.
- Retain the selected range for the browser session.

## Dependencies

- Depends on the multi-device child to provide the device-scoped store/API contract before integration.
- Reuses the existing SQLite interface sample history; it does not introduce new persistence.

## Acceptance Criteria

- [ ] All four ranges are available on the home chart.
- [ ] Each range returns an appropriate persisted time window with bounded point count.
- [ ] Empty and partially populated windows render without errors.
- [ ] Existing five-minute live behavior remains available.
