# Technical Design

## Scope and boundaries

The bug is in TargetList edit hydration. Keep the change on
`feat/policy-access-rebuild` and Draft PR #4. Do not change the RoutingRule
wizard state machine, policy plan generation, apply API, or canonical routing
commit behavior.

## Data flow

```text
TargetList summary
    ↓ user opens edit
GET /api/target-lists/{id}
    ↓ detail-only editableContent
TargetListModal.text
    ↓ user changes content
manual preview
    ↓ exact previewId
TargetList save/version update
```

The list and routing-context responses remain summary-shaped. The existing
single-target GET response may add `editableContent` for manual targets only.
It is derived from the current active version, or the pending version when no
active version exists. It must never use an in-memory preview or proposal row.

## Backend detail representation

Use the existing stored version and rule data. Convert the selected committed
version into deterministic one-rule-per-line text (`DOMAIN,<domain>` or
`DOMAIN-SUFFIX,<domain>` for domains; the corresponding IP rule type and
address for IP lists). The serialized text is semantically equivalent to the
stored rules; byte-for-byte preservation of the original pasted formatting is
out of scope.

If the stored version cannot be read or decoded, return a normal read error;
do not return an empty editable value that could be mistaken for user input.

## Frontend state

Keep the edit content local to `TargetListModal`:

- New target: empty text, no detail fetch.
- Existing manual target: `contentLoading` until detail arrives; then set both
  the textarea and its loaded baseline.
- A failed detail fetch leaves saving and preview disabled.
- Metadata-only edits do not require a preview.
- A manual content change clears the previous preview and requires a new one
  before save. Existing URL/upload behavior should not regress.

Use the existing API parsing boundary and add a detail-specific type/parser;
do not place large content on the shared `TargetList` summary model.

## Verification and rollout

Add a backend API regression covering create/detail, metadata-only edit, an
unsaved preview not leaking into detail, and content replacement. Run the
existing frontend and Go quality gates, rebuild `internal/ui/dist`, commit only
the focused task files and generated assets, push the same branch, and request
root re-review. After explicit `APPROVED`, deploy the pushed build to the
disposable test machine and hand off manual UI verification to the user.
