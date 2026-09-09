# Version and online updates

## Approved scope
User approved the simplified design on 2026-09-09 and authorized implementation.
Add a version/update card to maintenance in both UI variants. Exactly two primary actions: check manually and install a newer stable release after confirmation. Display current/latest version, platform, check time, release notes/link, update stages and only the latest update result. No reinstall, downgrade, automatic checks/install, channels, signature infrastructure or history screen.

## Acceptance
- Inject release version/commit/time/architecture into the executable. Development builds are explicitly identified.
- Fetch official GitHub Releases with timeout/cooldown; compare semantic versions, exclude draft/prerelease and require an exact supported Linux asset plus SHA256 manifest.
- Download safely without interrupting service; reject corrupt/mismatched assets. Authenticate all update endpoints and serialize update vs other writes.
- After download, stop the child, back up all data/config/binary consistently, atomically replace, verify a candidate before enabling monitoring/policy mutations, and recover on failure independently of the candidate.
- Only Linux supervised installs support online installation; others still check and explain the restriction.
- Most recent job survives restart/browser closure; only one job may run. Failures never appear as up-to-date.
- Existing production NAS-only backups/manual acceptance rules remain binding; production online installation stays disabled until an appropriate external backup destination is configured.
- Automated tests, frontend checks and isolated Linux test-machine smoke/failure checks pass. Keep Draft PR and do not publish/merge/complete before acceptance.
