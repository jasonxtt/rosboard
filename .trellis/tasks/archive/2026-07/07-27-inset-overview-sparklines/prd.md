# Inset overview sparklines

## Goal

Slightly shorten overview metric sparklines so they no longer touch the available chart area's edges.

## Requirements

- Add balanced left/right inset to the mini sparkline area, roughly two primary-value digits on each side.
- Preserve the current content-sized value column and flexible chart column so longer values still display first and the chart shrinks.
- Keep four-card layout, progress bars, CPU detail, and footer values unchanged.
- Build and deploy the updated embedded panel to `10.0.0.6`.

## Acceptance Criteria

- [ ] Mini sparklines are visually shorter with equal left/right inset.
- [ ] Value text remains content-sized and is not clipped by the chart.
- [ ] Frontend lint/build and Go tests pass.
- [ ] Updated panel is available at `http://10.0.0.6:8080/`.

## Notes

- Keep `prd.md` focused on requirements, constraints, and acceptance criteria.
- Lightweight tasks can remain PRD-only.
- For complex tasks, add `design.md` for technical design and `implement.md` for execution planning before `task.py start`.
