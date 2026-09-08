# Overview reference amendment — 2026-09-07

## User decision and limits

The user approves the overall plan, especially the new-routing-policy selectors and layout in the 8791 study. Adopt exactly two visual references from the other agent’s UI: the four overview header components and the quick-link module. Put the study’s device-information module below quick links. All other design directions stay unchanged. This request authorizes updating the plan only; implementation remains paused.

## Located and pinned source

The reference branch remains checked out at `/Users/tom/github/rosboard`, branch `feat/ui-ikuai-restyle`. Implementation is separate at `/Users/tom/github/rosboard-ui-compact`, branch `codex/ui-compact-rebuild`, based directly on `feat/policy-access-rebuild`. Extract the approved modules selectively; do not import the whole UI or branch history. Both local HEAD and the GitHub branch resolved to `74ac43aa6a6d3ca8b3fde33f8002b912d0f70757` when checked. That commit only changes planning artifacts relative to `1049314`; the referenced application UI is unchanged.

Use the immutable reference rather than whichever UI a future branch HEAD contains:

- [Overview source at the pinned commit](https://github.com/jasonxtt/rosboard/blob/74ac43aa6a6d3ca8b3fde33f8002b912d0f70757/web/src/App.tsx#L2873): `OverviewPage`, header at line 2915, quick links at 2985.
- [Styles at the pinned commit](https://github.com/jasonxtt/rosboard/blob/74ac43aa6a6d3ca8b3fde33f8002b912d0f70757/web/src/index.css#L830): `.overview-top-grid`, `.device-strip-card`, `.overview-stat-card`, `.osc-*`, `.quick-link-grid`, `.quick-link`; inspect referenced tokens and responsive/dark rules in the same file.
- Preserve `MiniSparkline` and `OverviewCompositionBar` behavior and their style dependencies; do not copy only the JSX and then let new global tokens silently change their appearance.

This is source verification, not a new runtime screenshot comparison. The branch source is the requested module reference; root `preview-ikuai/` is still not the approved general design.

## Header: direct module reuse

| Component | Preserve from source |
|---|---|
| System information / device identity | Router icon, name, version tag, model/address line, uptime, white independent card. Source displays the device name rather than a literal “系统信息” title; do not borrow the unrelated system-resource page’s similarly named component. |
| Terminal count | Purple card, online/inactive/offline legend, main number, sparkline, three-part composition bar and average/peak footer. |
| Resource usage | Blue card, CPU and memory progress bars/percentages, average CPU/memory and peak footer. This source component uses progress bars, not a new invented sparkline. |
| Active connections | Orange card, TCP/UDP/other legend, main number, sparkline, composition bar and average/peak footer. |

Retain source desktop order: identity, terminals, resources, connections. There are four separate cards with 12 px gaps; the first is 1.1fr and the remaining three are 1fr. Stat-card padding is 16px 20px 12px, radius 8px, subtle shadow and 30px main metric. Adapt to available width without shrinking away legends or values.

Local exception colors: terminal background `#F5F4FD`, composition/line accents `#A5A0F8`, `#C6C3F7`, `#E3E2FA`; connection background `#FFF8EC`, accents `#F5A623`, `#F9C568`, `#FCE3B3`; resource background `#F0F6FD`, accent `#4794EB`; card border `#EAEEF2`, shadow `0 1px 2px rgba(0,0,0,.03)`. Preserve corresponding dark-mode distinctions and readability against the study’s dark shell. Scope these exceptions to the adopted modules; they must not change the routing wizard, general navigation or traffic-chart palette.

## Left rail: required order

1. **Interface information**: keep the study’s design direction and real-data mapping from the main design plan.
2. **Quick links**: copy the source’s titled panel and three-column icon-over-label grid, 4px grid gap, 19px icons, approximately 13px 4px 11px button padding and hover feedback. Keep all nine destinations: 策略路由、终端监控、目标库、流量监控、访问控制、识别设置、网络服务、系统运行、面板设置. Preserve their real callbacks; this is not a custom shortcut editor.
3. **Device information**: use the study’s existing panel design, with real device/system fields. Do not remove it because the adopted header also contains device identity.

The right-hand overview region and all other pages follow the existing plan. No functionality, alert, field or action is removed by this amendment.

## Handoff verification

Compare the header and quick links against the pinned branch source, and everything else against the frozen study. In particular, verify that newly adopted colors have not spread to policy selectors or other pages. The original preview files and 8791 service are unchanged: they do not yet render this combined design. A future implementation/updated preview must visibly show the amended layout before claiming visual acceptance.
