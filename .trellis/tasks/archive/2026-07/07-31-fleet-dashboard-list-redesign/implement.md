# Implementation plan

1. Reshape `FleetDashboardPage` and its fleet row components into one list with
   a shared seven-column header/template; place live metrics in the required
   order and preserve offline and click-through behavior.
   - Verify by source inspection and TypeScript compilation.
2. Replace fleet-only card/grid styling with a dense reference-matching list,
   including responsive and dark-theme rules.
   - Verify desktop column alignment, offline/no-result states, 375px reflow,
     focus visibility, and lack of document-level overflow through deterministic
     layout/source checks and the required user visual handoff.
3. Remove remaining legacy fleet-view types, preferences, controls, renderers,
   and selectors that are no longer reachable.
   - Verify with `rg` and lint.
4. Run frontend lint/build, relevant Go tests/vet, and rebuild/verify the
   embedded UI assets.
5. Inspect the remote deployment paths read-only, create one timestamped backup
   of binary/configuration/SQLite data and sidecars/unit as applicable, deploy
   the verified Linux amd64 binary to `10.0.0.6`, and verify systemd, health,
   fleet/device APIs, and embedded frontend asset hashes.
6. Stop and ask the user to inspect `http://10.0.0.6:8080/`. Only after explicit
   approval may the work be committed and the Trellis task archived/journaled.
