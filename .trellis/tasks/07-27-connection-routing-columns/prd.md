# Simplify connection routing columns

## Goal

Make terminal connection details easier to scan by removing redundant legacy
fields and separating route attribution into two operationally meaningful
columns.

## Background

- The existing `出口线路` value is always `未知`; the newer route attribution
  data supersedes its original purpose.
- `标志` and `连接状态` both derive from `seenReply` and `assured`, so showing
  both is redundant.
- A gateway value such as `10.0.2.1%wan-xray` represents the next-hop gateway
  `10.0.2.1` qualified by the egress interface `wan-xray`. The standard column
  label is `下一跳网关`.

## Requirements

- Remove the `出口线路` column and its filter and sort controls from terminal
  connection details.
- Remove the `标志` column and its filter and sort controls.
- Hide the `连接状态` column and its filter and sort controls for ordinary
  terminals. Preserve the compact connection status for the `routeros:self`
  connection-tracking detail, where reply state remains diagnostically useful.
- Replace the composite `命中路由` presentation with two columns:
  - `路由表`, displaying `routeTable`.
  - `下一跳网关`, displaying `routeGateways` and preserving RouterOS-qualified
    values such as `10.0.2.1%wan-xray`.
- Do not display the matched destination prefix or matched-rule text in either
  routing column.
- Add independent header sorting and filtering to both routing columns,
  consistent with the existing connection-table controls.
- A route-table filter matches the displayed routing table. A gateway filter
  matches an individual gateway, including any member of an ECMP gateway list.
- Rows with unavailable route-table or gateway attribution remain visibly
  identifiable and filterable as `无法判断`.
- Include route-table and gateway values in global connection search; remove
  the obsolete outbound-line value from that search.
- Keep backend route-attribution and reply-state fields intact; this task only
  changes how the existing data is presented and controlled in the frontend.

## Acceptance Criteria

- [x] Ordinary terminal connection tables no longer show `出口线路`, `标志`, or
      `连接状态`.
- [x] RouterOS self connection tracking retains `连接状态` but no longer shows
      `出口线路` or `标志`.
- [x] Every connection row shows separate `路由表` and `下一跳网关` cells, with
      no target-prefix or matched-rule secondary line.
- [x] Clicking either new column label sorts ascending, then descending.
- [x] Each new column has a header filter; route-table choices match exact
      tables and gateway choices match individual gateways, including ECMP
      members and `无法判断`.
- [x] Clearing table state resets the two new filters and their active header
      indicators.
- [x] Global search finds rows by route table or next-hop gateway.
- [x] Empty-result rows span the correct number of visible columns for both
      ordinary terminals and `routeros:self`.
- [x] Frontend lint and production build pass.
- [x] Desktop and mobile browser verification confirms sorting, filtering,
      horizontal table scrolling, and clear/search controls work without
      overlap or console errors.

## Out of Scope

- Removing legacy backend JSON fields such as `line`, `status`, `seenReply`, or
  `assured`.
- Changing the route-attribution algorithm or claiming inferred attribution is
  a RouterOS-recorded packet hit.
