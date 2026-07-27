# Overview metric detail layout

## Goal

Refine the overview metric card layout and terminal menu behavior based on the browser review feedback.

## Requirements

- Overview metric cards must give sparklines more horizontal width and reduce unused left/right whitespace.
- Metric card detail text must not be visually squeezed or ellipsized:
  - Memory detail renders as two lines: used memory and unused memory.
  - Online terminals detail renders as two lines: offline count and inactive count.
  - Active connections detail renders as two lines: TCP count and UDP count.
- Existing metric primary values, progress bars, averages, and peaks remain visible and aligned.
- Clicking the second-level `终端监控` sidebar item must show the right-side terminal list with `全部终端` selected.
- Build and publish the updated embedded panel to `10.0.0.6`.

## Acceptance Criteria

- [ ] The four overview metric sparklines occupy more of each card width without overlapping detail text.
- [ ] Memory, online terminal, and active connection detail text render as two compact lines.
- [ ] Clicking `终端监控` activates `terminals` and sets family to `all`.
- [ ] Frontend lint/build pass and the updated panel is deployed to `http://10.0.0.6:8080/`.

## Notes

- Keep `prd.md` focused on requirements, constraints, and acceptance criteria.
- Lightweight tasks can remain PRD-only.
- For complex tasks, add `design.md` for technical design and `implement.md` for execution planning before `task.py start`.
