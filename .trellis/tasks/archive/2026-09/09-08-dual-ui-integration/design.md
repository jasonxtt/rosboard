# Design

## Branches and integration
Pinned baseline 7a7d463da028519661878070a0027cae50b69e0c; Aurora ab3b7bb5424fc779bef30ead05a589b284ad3345; Compact 1dafe1750a547eabcb53415eee0d8f0559384f1c. Remote main c1bcdefb1ce6a7d56552e1c791e44d44e434f5ca is already an ancestor of the policy baseline. Work in /Users/tom/github/rosboard-dual-ui on codex/dual-ui-integration starting at Aurora. Merge Compact without committing until acceptance; keep Aurora root frontend and import the entire Compact source into src/compact. Both parent histories remain represented in the eventual merge commit. Do not modify the original checkout or either PR branch.

## Frontend boundaries
A neutral bootstrap dynamically imports only the selected UI entrypoint. No UI CSS is imported by bootstrap. A full document navigation on switch removes the outgoing styles, polling, subscriptions, dialogs and chart runtime. Reuse one React/Vite build and embedded assets directory; include both ECharts and uPlot but load only the relevant variant graph. Existing root source is Aurora; src/compact is Compact. Keep styling local to each independent module graph.

Shared browser preference helper validates a compact/aurora choice. URL ui parameter provides explicit override and recovery even when storage is denied; localStorage remembers preference for ordinary visits. Default Aurora (user amendment on 2026-09-08). Switch targets the counterpart settings UI section; keep same origin/auth cookies and device key. Normalize shared preferences without overwriting unrelated fields, respecting legacy Compact panel-preferences and Aurora theme/refresh keys. Unknown values fail to sensible defaults. Each settings form retains its own visual controls.

## API compatibility
Go changes from the two PRs affect disjoint files relative to baseline. Retain both, review all callsites. Compact terminal name changes remove remark edits; align Aurora terminal editor/API/parser with customName-only and auto-name reset. Keep Aurora target library deletion/recovery enhancements available in both UIs.

## Delivery
Use disposable 10.0.0.60 with dedicated config/data. Test migrations against synthetic legacy fixtures only. Build a linux amd64 embedded executable. Before live replacement verify mounted writable NAS, retain <=10 timestamped backups, capture binary/config/all SQLite files/unit consistently, then replace and verify systemd/health/auth/API/assets. Rollback uses NAS copy. Stop for explicit production acceptance before committing or merging main.
