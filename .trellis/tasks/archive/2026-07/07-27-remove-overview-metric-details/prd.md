# Remove overview metric details

## Goal

Try a cleaner overview card layout by removing the small detail text from the memory, online-terminal, and active-connection metric cards.

## Requirements

- Keep the CPU card detail text unchanged.
- Remove the small detail text under the primary value for the memory, online-terminal, and active-connection cards.
- Keep card title, icon, primary value, sparkline, progress bar where applicable, and footer average/peak values.
- Build and deploy the updated embedded panel to `10.0.0.6`.

## Acceptance Criteria

- [ ] Memory, online-terminal, and active-connection cards no longer render detail text under the primary value.
- [ ] CPU card still renders `当前负载`.
- [ ] Frontend lint/build pass.
- [ ] Updated panel is available at `http://10.0.0.6:8080/`.

## Notes

- Keep `prd.md` focused on requirements, constraints, and acceptance criteria.
- Lightweight tasks can remain PRD-only.
- For complex tasks, add `design.md` for technical design and `implement.md` for execution planning before `task.py start`.
