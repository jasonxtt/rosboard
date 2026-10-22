# Delete journal-bound update roots — 2026-10-16

## Diagnosis and scope

The user observed a retained ddns-go root after deleting its updated container.
The private journal proves that its successful update renamed the old runtime;
subsequent deletion removed the current runtime but did not traverse update history.
The user explicitly requests a code correction and independent real testing.
Continue on the existing container branch/Draft PR #30, separate from the later
Compose/native-UI/policy-UI branches. This amendment does not deploy production or
sweep any already-orphaned user directory.

## Implementation

- Bind the confirmed Files ID after retain-root rename, including unknown-result
  reconciliation. RouterOS may change opaque IDs during rename. An unattempted
  rename cannot claim a pre-existing target solely from satisfied path state.
- Delete follows completed update cutovers backward from the current exact
  ID/root/interface/owner. Names may be edited. An ID reused for another root or
  owner cannot acquire old history; unrelated/malformed histories are isolated.
- Add exact ID/path/type historical cleanup to the existing durable delete plan.
  Validate every linked root before admitting deletion; re-read roots and all mount
  sources immediately before each directory removal. Same-path replacements,
  shared roots, mount overlaps, ambiguous/cyclic history and unreadable metadata
  fail closed. Missing historical paths are already clean.
- Legacy records lacking a matching post-rename ID fail clearly before container
  deletion; no name-only migration claims an arbitrary current directory. Existing
  pending delete journals keep their original intent and recovery contract.
- Current root deletion and independent persistent data behavior are preserved;
  update rollback roots remain available until explicit container deletion.

## Deterministic verification

Full Go tests, go vet and embedded-program build passed. Focused contracts cover
multiple updates followed by restart/delete, changed rename IDs, edited names,
foreign similarly named folders, mount-data preservation, same-path replacement,
late references, reused container IDs, different owners, legacy missing ID,
malformed/unrelated history, and pending historical-removal read-back without
replaying delete. The former one-update test now requires zero retained roots.
Frontend source/assets are unchanged by this backend-only amendment.

## Actual independent RouterOS validation

Compiled the package test binary and ran the opt-in host-image lab on the isolated
test machine against its independent RouterOS 7.23.7. A unique generated parent,
names, VETHs and independent addresses isolate the fixtures. The existing saved
proxy and pinned SSH identity were reused privately; no credentials entered Git.

The 70.18-second run passed Docker Hub Alpine and GHCR import/start/stop/restart,
two Alpine updates, reconstruction of the service from its persisted journal,
then explicit delete. Fresh Files reads proved that the current root and both
historical roots were absent while the same container's sibling mount source
remained. Delete-before-first-start still cleaned its generated image. All fixture
containers/network objects and their unique parent were cleaned afterward.

The original MSM ID/root/interface/ownership/running state matched before/after.
The private temporary configuration and uploaded test executable were removed.
Existing production/test panel services and the user's old retained directory were
not replaced or modified. Production delivery/backup/manual acceptance remain
separate; keep PR Draft and task in_progress, with no merge/release/archive.
