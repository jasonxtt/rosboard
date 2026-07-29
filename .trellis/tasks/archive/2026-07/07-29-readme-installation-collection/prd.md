# 完善首页安装与采集说明

## Goal

Align the README with the current first-run and terminal-rate collection behavior.

## Requirements

- When `-config` is omitted, use `./config.yaml`; when it is supplied, use that path unchanged.
- Allow a missing default configuration file during first start and create it with private permissions when the onboarding flow saves RouterOS settings.
- Explain that a first installation can start without a user-created YAML file.
- Update local and systemd installation steps to preserve the YAML-free first-run path.
- Distinguish complete, overview realtime, terminal discovery, and terminal-rate collection.
- Explain the relationship between backend collection settings and browser refresh settings.
- Provide a GitHub Release workflow that is triggered only when the root `VERSION` file changes on `main`.
- Set the initial release version to `0.0.1` and document the version-bump release process.

## Acceptance Criteria

- [x] `go run ./cmd/rosboard` uses `./config.yaml`, directs the user to the first-run UI, and can persist onboarding settings there.
- [x] `-config /custom/path.yaml` continues to use `/custom/path.yaml` unchanged.
- [x] systemd setup does not require copying or editing a config file before first launch.
- [x] The README accurately documents 10-second full, 1-second overview realtime, 5-second terminal discovery, and visible-terminal one-second rate collection.
- [x] README command examples and Markdown links remain valid.
- [x] Pushing a commit to `main` that does not change `VERSION` does not run the release workflow.
- [x] Pushing a `VERSION` change to `main` builds the frontend, tests the Go project, creates `v<version>`, and uploads Linux amd64, amd64-v3, arm64, and armv7 archives with checksums.

## Notes

- Keep `prd.md` focused on requirements, constraints, and acceptance criteria.
- Lightweight tasks can remain PRD-only.
- For complex tasks, add `design.md` for technical design and `implement.md` for execution planning before `task.py start`.
