# Widen overview sparklines flexibly

## Goal

Make overview metric sparklines use the remaining card width while preserving the primary value area when numbers grow.

## Requirements

- The sparkline should begin immediately after the icon/value block and extend close to the card's right edge.
- The icon/value block must keep enough intrinsic width for longer primary values; the sparkline side should shrink first.
- Remove unnecessary SVG endpoint padding so the plotted line reaches the available sparkline area.
- Keep CPU detail, card footers, progress bars, and four-card layout intact.
- Build and deploy the updated embedded panel to `10.0.0.6`.

## Acceptance Criteria

- [ ] Metric card grid uses value-content sizing plus a flexible sparkline column, not a fixed percentage value column.
- [ ] Sparkline SVG uses the full plotting width without artificial endpoint gaps.
- [ ] Frontend lint/build and Go tests pass.
- [ ] Updated panel is available at `http://10.0.0.6:8080/`.

## Notes

- Keep `prd.md` focused on requirements, constraints, and acceptance criteria.
- Lightweight tasks can remain PRD-only.
- For complex tasks, add `design.md` for technical design and `implement.md` for execution planning before `task.py start`.
