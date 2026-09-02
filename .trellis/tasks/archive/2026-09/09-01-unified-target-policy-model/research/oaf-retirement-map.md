# OAF retirement map for Slice 2

Slice 2 establishes `RoutingRule` as the authority for policy-routing ownership
and keeps the existing OAF application catalog outside that migration boundary.

## Preserve during Slice 2

- `internal/applicationcatalog/` and its catalog data remain the source of
  application identity and normalization.
- `ApplicationIDs` and `TargetScopeApplications` remain part of the existing
  access-control/application model.
- `access_rule_applications`, `ApplicationResolver`, `rb_app_*`, and
  `dns:application:*` objects remain unchanged.
- Existing application-related API and frontend behavior remains unchanged.

## Later migration boundary

- Slice 3 may introduce target-list presets and migrate explicit
  `ApplicationIDs` into those presets. That migration must preserve the
  existing application rules and must not silently delete them.
- Slice 4 may remove OAF enforcement, attribution, frontend, and obsolete
  structures only after their replacement projection and compatibility path
  are implemented and verified.

## Explicitly out of scope here

Slice 2 does not add `ApplicationPreset`, migrate access rules, change
application attribution, change the application resolver, or clean up OAF
frontend/enforcement code. The current OAF files are inventory inputs for the
later migration, not Slice 2 implementation targets.
