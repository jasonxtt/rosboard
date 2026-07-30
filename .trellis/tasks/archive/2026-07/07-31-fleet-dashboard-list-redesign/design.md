# Fleet dashboard list redesign

## Starting point

The current branch already contains an uncommitted fleet-dashboard revision.
Those changes are the implementation baseline and must not be discarded. The
existing API model (`FleetOverview` / `FleetDevice`) already provides every
value needed by the requested layout, so this task remains frontend-only apart
from rebuilding the embedded distribution.

## Structure

`FleetDashboardPage` retains local search, status, sort, and pagination state.
It renders three vertically ordered regions:

1. Four fleet summary items.
2. The search/filter/sort toolbar.
3. One unified device list followed by pagination.

The unified list owns a desktop header and device rows sharing the same CSS grid
template. The columns are `设备信息`, `CPU`, `内存`, `流量速率`, `终端`, `连接`,
and `运行时间`. Each row remains one semantic button so its existing navigation
behavior and keyboard activation are preserved.

## Row states

Online rows use compact circular CPU/memory meters, two traffic lines, terminal
and connection distribution rings with legends, then uptime and last-update
metadata. Alerting rows retain the same metrics while using an explicit text
status badge and warning accent.

Offline rows keep the device identity at the left and runtime/update metadata at
the right. The middle five live-metric columns are replaced by a centered
icon-plus-text unavailable state. This mirrors the supplied reference without
inventing unavailable values.

## Responsive behavior

At wide desktop widths, the header and rows share fixed/minmax grid columns for
strong vertical alignment. At intermediate widths, secondary distribution
details may tighten while preserving the requested order. At mobile width, the
desktop header hides and each row reflows into a stacked two-column summary;
visible metric labels preserve meaning. Search and selects wrap to 44px-high
controls, and the summary remains a two-column grid.

## Visual system

The supplied reference is authoritative over generic design-system output. Use
the repository's existing semantic tokens and icon set, with a cool light page
background, white list surface, subtle one-pixel dividers, restrained shadow,
8–10px radii, tabular numerals, blue/green/amber/red/slate semantic states, and
visible focus styling. Preserve the existing dark-theme token mappings rather
than introducing new fonts, packages, or a parallel theme system.

## Compatibility and rollback

No API, persistence schema, or backend behavior changes. Removing the obsolete
fleet-view preference is backward compatible because unknown local-storage
fields are ignored. Runtime rollback is the timestamped remote backup of the
binary, configuration, SQLite database and sidecars, and service unit where
present.
