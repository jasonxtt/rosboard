# Implementation plan

1. Reconcile the existing uncommitted mobile-header changes with the approved templates.
   - Preserve the metric-card overlap fix, old-iOS media-query compatibility, and HTML cache revalidation changes.
   - Remove or replace only obsolete mobile topbar rules.
   - Verify with `git diff --check` and focused source inspection.

2. Normalize shared topbar markup and control presentation.
   - Reuse `MonitorPageTabs` and `MonitorSummaryBar`.
   - Give theme, manual refresh, and refresh period one mobile control contract while retaining desktop labels and existing behavior.
   - Keep the status/search region flexible and the icon controls fixed at 44px.
   - Verify accessible labels, menu expansion, select values, and focus states from source and lint output.

3. Implement the responsive page mappings.
   - Terminal: tabs, six-column single-row summary, search plus actions.
   - Interfaces: tabs, six-column single-row summary, status plus actions.
   - Traffic, network services, and system runtime: tabs plus actions with no empty summary row.
   - Panel settings: active-section title plus actions.
   - Verify no page-specific control-size overrides remain and no mobile topbar row uses horizontal scrolling.

4. Run deterministic validation.
   - `npm --prefix web run lint`
   - `npm --prefix web run build`
   - `npm --prefix web audit --audit-level=high`
   - `go test ./...`
   - `go vet ./...`
   - `git diff --check`

5. Perform local runtime and asset checks.
   - Start the verified build locally.
   - Check health/bootstrap routes and embedded HTML/CSS/JS responses.
   - Inspect the built CSS for legacy-compatible media queries and the approved mobile control rules.
   - Do not treat browser automation as user visual acceptance.

6. Deploy through the required acceptance gate.
   - Before replacement, create and inspect a new timestamped backup of the remote binary, configuration, SQLite data, and systemd service unit.
   - Deploy the verified binary to `10.0.0.6`.
   - Verify systemd status, health endpoint, affected API contracts, and embedded frontend asset references.

7. Hand off manual visual acceptance.
   - Ask the user to inspect at least 375px and their reported phone width in terminal monitoring, interface monitoring, traffic monitoring, network services, system runtime, and panel settings.
   - Confirm three-row summary pages, compact single-row controls, consistent 44px control sizing, all themes, menu/select behavior, and absence of document-level horizontal overflow.
   - Do not commit until the user explicitly approves the deployed instance.

8. After approval, finish the task.
   - Review whether the established shared mobile-topbar contract belongs in the frontend spec.
   - Commit only the approved scope, record the session, and archive the Trellis task.
