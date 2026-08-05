# Implementation Plan

1. Update the four status-monitor sidebar groups and topbar JSX, add reusable
   page-level tabs, and keep the controlled terminal search in the terminal
   topbar.
2. Add only the grouped-monitor CSS needed for desktop/tablet/mobile layout,
   focus states, and active tabs.
3. Run `npm --prefix web run lint` and `npm --prefix web run build`; inspect
   the diff for scope and stale classes.
4. Deploy the verified frontend to `10.0.0.6` after backing up the current
   binary, configuration, and SQLite data with a timestamp; verify systemd,
   `/api/health`, affected dashboard/API responses, and embedded assets.
5. Ask the user to inspect all four deployed monitor groups at desktop and
   mobile widths. Do not commit until explicit approval is received.

## Execution Record

- `npm --prefix web run lint` passed.
- `npm --prefix web run build` passed and regenerated tracked embedded assets.
- `go test ./...` passed; `git diff --check` passed.
- A temporary local instance returned healthy `/api/health`, bootstrap JSON,
  and embedded HTML asset references. Binding the local port required the
  approved temporary runtime check outside the default sandbox.
- `npm --prefix web audit --audit-level=high` could not complete because the
  npm registry audit endpoint was unavailable; the escalation was rejected
  because it would disclose dependency metadata to the public registry.
- Remote backup: `/opt/rosboard/backups/20260805-122354-terminal-family-header/`.
  The old binary and SQLite backup hashes matched their live counterparts; no
  SQLite WAL/SHM sidecars were present.
- Deployed Linux amd64 binary SHA-256:
  `11d74f0b796969e5ff3c3112f34ed300ec09d6662d09e97ff4d435542d382134`.
- Remote `rosboard.service` is active and enabled; `/api/health` returns
  `{"ok":true}`, `/api/bootstrap` returns the expected logged-out ready phase,
  unauthenticated `/api/dashboard` returns `401`, and the deployed JS/CSS
  assets return `200` and contain the new terminal-header strings.
- The first-scope deployment was superseded by the scope extension below.

## Scope Extension

The user extended the request after the first deployment: `流量监控`、`网络服务`
and `系统运行` must use the same top-of-page third-level navigation pattern as
`终端监控`. The implementation, verification, deployment, and manual
acceptance gate must therefore be repeated before any commit.

## Scope-Extension Execution Record

- `npm --prefix web run lint`, `npm --prefix web run build`, `go test ./...`,
  `go vet ./...`, and `git diff --check` passed.
- A temporary local instance returned healthy `/api/health`, the expected
  `needs_admin` bootstrap phase, embedded HTML asset references, HTTP 200 JS
  and CSS responses, and the four monitor tab labels.
- Remote backup: `/opt/rosboard/backups/20260805-124133-status-monitor-nav/`.
  The service was stopped before copying the existing binary, configuration,
  SQLite database, and any present database sidecars; backup files are present.
- The new Linux amd64 binary SHA-256 is
  `9c35d4184da3402aaac6616a0911fde7ceb4b12be71a5f870e80d1d3d4e7b53b`.
- Remote `rosboard.service` is active and enabled; `/api/health` returns
  `{"ok":true}`, `/api/bootstrap` returns the expected logged-out ready phase,
  unauthenticated `/api/dashboard` returns `401`, and the deployed JS/CSS
  assets return `200` and contain the new monitor-tab strings.
- The user manually inspected the final deployment at
  `http://10.0.0.6:8080` and approved terminal, traffic, network-service, and
  system-runtime tabs on 2026-08-05. The work-commit gate is satisfied.
