# 按参考图重塑系统概览实施计划

1. Add real-data support
   - fetch/reuse one-hour load history on overview;
   - derive protocol and interface summaries without inventing history.
2. Build reference components
   - consistent SVG icons, compact metric cards, sparklines/progress;
   - system status rows, interface summary, current events.
3. Recompose overview
   - four-card header;
   - real-time traffic + system status;
   - interface table + current events.
4. Match shell details
   - icon navigation, reference-like topbar controls, sidebar device footer;
   - tune spacing, borders, typography and chart styling.
5. Responsive and regression pass
   - 375/768/1024/1440 layouts;
   - all existing pages, terminal detail and remark modal.
6. Validate and deploy local preview
   - `npm --prefix web run lint`;
   - `npm --prefix web run build`;
   - `go test ./...`;
   - rebuild the Go binary, restart the persistent `screen` service, and verify `http://10.0.0.86:8080/`.

## Review Gates

- Gate A: 1440×900 overview visually matches the reference structure with only real data.
- Gate B: responsive and existing-page regression checks pass.
- Final: quality commands, live LAN endpoint, and browser console checks pass.

## Risk Points

- `/api/load` must not be fetched twice when switching between overview and load history.
- Current warnings are not historical alerts; UI wording must remain honest.
- The running Go binary must be rebuilt after Vite output changes or the browser will show stale embedded assets.
