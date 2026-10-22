# Supplied policy routing UI review and delivery — 2026-10-16

## Review and implementation

Source: 144d2e29ddec19b1ae9faad14a5a8ac78303cd8d, independent branch
codex/policy-routing-ui-refresh / Draft PR #33, based on the current production
lineage from PR #32. Existing container/import functionality remains included.

Reviewed the two user-supplied files against current source. Integrated Aurora's
header/count pill/primary create action, loading skeleton, top-aligned rule cells,
column widths and duplicate egress-line suppression. Mutation handlers, API
contracts, permission guards and recovery feedback are unchanged; useful recovery
comments removed by the supplied draft were retained.

Corrected two integration defects before replacement:

1. Supplied text/surface/border tokens were undefined and fell back to light-only
   colours. Use the existing Aurora ink/glass/stroke and type/spacing/radius tokens.
2. Unscoped pol-rule-table overrides would affect access control after navigation.
   Every page selector now requires pol-routing-page. Long names wrap and the
   table's card is constrained to its own available width.

Compact has no source/style changes. Other generated JavaScript chunks differ
only in emitted asset references. Existing specs already cover token use, page
isolation and user-led visual acceptance; no new convention was introduced.

## Automated checks

- All 108 existing frontend tests passed, including policy recovery and container
  imports. No test was added for this reversible presentation-only adjustment.
- TypeScript/Vite build, lint and emitted dual-UI/bootstrap/CSS isolation passed.
  Lint retains only four pre-existing FastTrack JSX-key warnings.
- Static inspection verified every CSS variable exists in Aurora tokens and all
  new selectors are routing-page scoped. Staged paths/diff and secret patterns
  were reviewed before publishing; no supplied private artifact was committed.
- Static linux-amd64 embedded binary compiled as dev-policy-ui-1016, source 144d2e2.
  SHA256: fe28529e5526718b19408395376dd6cf65898b672abb83d2658e0573f29829ad.

## Same-binary test and production delivery

- The isolated test service contains only its independent lab device. No native
  jobs were unsettled before replacement. Health, both UI entries and all 78
  distributable asset hashes passed. Configuration/supervisor/unit stayed intact.
- Authenticated policy overview/rules/target lists/access overview reads passed.
  Existing Compose and exact ddns-go Docker run parsing and container reads passed;
  the original MSM remained healthy. Temporary verification sessions were removed.
- Production preflight confirmed both device scopes readable with no active policy
  job; native/updater journals were settled. Confirmed the NAS mounted writable,
  removed only the oldest timestamped backup before creating the next, and stopped
  the service to capture a consistent binary/configuration/SQLite/data/supervisor/
  updater-state/systemd-unit archive directly to the NAS.
- Backup label: 20261016T080550Z-policy-routing-ui, 1,686,865,920 bytes. Fully read all
  archive members and calculated SHA256 before replacement:
  faa4f1a35ebf8ef167506a0146bf1701762e4064dd5d2bcc9d30ec3a24163c3d.
  Retention is 10 directories. Private backup data/manifest remain outside Git.
- Atomically replaced the production binary with the exact test-verified file.
  Service is active/running, Result=success, ExecMainStatus=0. Health reports
  dev-policy-ui-1016; all 78 assets and both UI entry points pass.
- Both production device scopes pass policy context reads, container/import
  regression reads and the unauthenticated parser boundary. Configuration,
  supervisor and service unit match the NAS backup byte for byte. No RouterOS
  mutation or container operation was requested by verification.

## Pending visual acceptance

Open the production Aurora policy-routing page (ui=aurora, #/policy-routing).
Inspect desktop and 390px mobile, switching light/dark. Expect the rule count and
primary Create rule action in the header, top-aligned cells and a nonwrapping
egress description; verify long names stay contained. Open/cancel the rule wizard
and inspect pending-apply feedback without submitting a configuration change.
Navigate to access control to confirm its table presentation stays independent.

The delivery is available for user inspection. Keep task in_progress and PR Draft;
manual acceptance and root APPROVED remain pending. No main merge, release or
archive is authorized by these runtime checks.
