# Technical design

## Boundary

Keep the immutable plan and job executor for exceptional workflows such as
drift recovery and takeover. Change only ordinary CRUD and scheduled source
refresh behavior: once desired state is valid, the server starts the existing
`GenerateAndApply` job automatically.

## Save flow

`POST/PUT /sources` persists the source and pending version first. If the
source is enabled, assigned to an enabled egress, and the request is not a
wizard-deferred save, the handler starts one `GenerateAndApply` job and returns
`202 {source, job, jobId}`. Unassigned or disabled-egress sources keep the
existing draft response. The frontend waits for an automatic job started by a
source editor, then reloads the source so the active version is shown.

The policy wizard marks its intermediate source rebind saves as deferred and
generates/applies one plan after all egress, ingress, and source changes are
persisted. If the generated plan has blockers, pending review, or required
acknowledgements, the existing plan view remains available as an exception.

Scheduled URL refreshes batch all due source changes per device, then start one
automatic job after persistence. This avoids a revision race when multiple
sources become due together. Unassigned or disabled-egress sources remain
pending until a later eligible save.

## Compatibility and safety

The source response parser accepts both the existing bare source object and the
new accepted wrapper. Existing explicit plan, adoption, and drift endpoints are
unchanged. Automatic synchronization still uses the existing planner's
blockers, exact ownership matching, revision checks, RouterOS verification, and
failure retention rules.
