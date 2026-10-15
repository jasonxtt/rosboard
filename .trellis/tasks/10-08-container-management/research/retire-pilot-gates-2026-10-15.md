# Retire container pilot gates, 2026-10-15

User reports new test connections unexpectedly read-only, then requests a thorough
check and removal of obsolete testing restrictions. Earlier functional tests were
manually accepted as basically passing; UI optimization remains next. No main
merge is authorized.

Audit found one obsolete per-device container_writes switch shared by API mutation
admission, native journal service, Files CRUD/capabilities, automatic transfer
identity initialization/persistence, startup journal restoration and image cleanup
workers. Removed the field and all runtime references. Existing legacy YAML true
and false values load normally, are ignored, and disappear on the next save.
New devices and credential changes no longer require operator file edits. Changed
connections still invalidate transfer credentials and SSH identity. No additional
production rollout/env flag exists in the container/image/Files workflow; opt-in
lab environment variables run integration tests only. Normal account permission,
device enabled/archive, package/API/storage, SSH mismatch, ownership/shared root,
write serialization and unknown-outcome recovery guards remain.

Validation: full Go tests and vet pass; focused API tests pass after final
transfer enabled-device guard reorder. Added legacy true/false load-save coverage,
normal new-device capabilities/Files writes, credential-change identity handling,
and disabled-device no-I/O denial coverage. Independent real RouterOS Docker Hub /
GHCR create/start/update/stop/restart/delete and sibling mount preservation lab
passes (56.70s), using no enrollment field. Test service dev-no-pilot-1015 is
healthy. Read-only local API harness on the test machine confirms supported=true,
writes=true, directoryWrites=true, mode=native against its real RouterOS. Harness
uses temporary state, emits no credentials, and is removed afterward. Frontend
assets are unchanged. Targeted race tooling cannot run locally because no C
compiler is installed; no concurrency algorithm changed.

Production deployment was rejected before execution by automatic approval review:
removing private opt-in expands operation capabilities to every enabled device;
review requires explicit user authorization of that scope. No new production
backup/replacement/restart was performed. Production remains dev-memory-mb-1015;
no native operation or task replay was performed. Code is ready for approval and
stays on the same Draft PR with task in_progress.
