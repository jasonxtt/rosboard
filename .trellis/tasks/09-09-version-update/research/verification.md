# Verification record

## Automated
- Full `go test ./...`, `go vet ./...`, `go build ./...` passed.
- Targeted race suite: internal/update, internal/api, cmd/rosboard passed.
- Frontend: 42 tests, lint, production build and dual-UI emitted-bootstrap/asset checks passed.
- Cross-compiled all four release targets: Linux amd64, amd64-v3, arm64, armv7.
- Optional Linux real-executable download fixture validates discovery -> install admission -> SHA256 -> extraction/ELF -> isolated metadata -> pending handoff without replacing the live executable.

## Linux process verification
Run the checked-in smoke script as unprivileged rosboard on the disposable test machine, using fixture versions 0.2.0 and 0.2.1 built from this change. No RouterOS devices/production configuration used. Cases: successful replacement, same stable supervisor, preserved session/data, failed candidate restore, interrupted subsequent download, supervisor restart with a broken installed executable, full reset clearing private recovery copies.

## Review scope / acceptance
VERSION remains 0.1.2; neither a public release nor a main merge is part of this checkpoint. Production is unchanged. Preview uses an independent service/data directory on the test machine. Visual acceptance remains user-led: inspect both UI styles at desktop/mobile widths, check version information and GitHub result, and verify confirmation/result layout using the component-tested states.

## Sources
- Existing .github/workflows/release.yml and deploy/rosboard.service.
- https://docs.github.com/en/rest/releases/releases (official release API).
- Repository backend/frontend specs listed in context manifests.

Authenticated check against the real GitHub API on the test machine passed (latest 0.1.2). All six runtime smoke assertions including full reset passed. Preview: http://10.0.0.60:8096/ (fresh isolated setup, no RouterOS devices). systemd service, health, bootstrap and embedded entry asset HTTP reads verified.
