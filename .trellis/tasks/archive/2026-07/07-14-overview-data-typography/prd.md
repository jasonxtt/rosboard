# Improve overview data typography

## Goal

Improve the readability of concrete values and supporting data on the system
overview at 100% browser zoom without enlarging its headings or changing the
dashboard information architecture.

## Requirements

- Keep the existing overview titles, primary metric values, panels, and grid
  structure unchanged.
- Increase overview-only secondary metric text, live traffic values, chart
  labels and tooltips, system-status data, interface-table data, and alert data
  to a readable 11-13px scale.
- Preserve tabular numerals and the existing visual hierarchy.
- Avoid overflow or clipping at the existing desktop and mobile breakpoints.

## Acceptance Criteria

- [x] Supporting text in metric cards is at least 12px.
- [x] Live traffic values are 13px and chart axes are 12px.
- [x] System-status labels and values are 12px, with status text at least 11px.
- [x] Overview interface table headers are 11px and values are 12px.
- [x] Overview alert data and summary text are at least 11px.
- [x] The production frontend build, lint, and dependency audit pass.
- [x] The overview remains usable without page-level horizontal overflow at
      desktop and mobile widths.

## Notes

- This is a lightweight, CSS-focused task. ECharts font-size options are the
  only component-code change expected.
