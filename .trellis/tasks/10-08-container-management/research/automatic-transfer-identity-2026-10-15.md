# Automatic transfer identity, 2026-10-15

User explicitly rejects manual fingerprint lookup, entry and trust prompts. This
supersedes the earlier manual independent-verification UX requirement. Keep SSH
identity checks in the server and use trust on first use for explicit operations.

First registry create/update or archive upload with no stored pin authenticates
REST using the saved device connection, observes the SSH key at that same host
without sending credentials, then authenticates and reads SFTP with the exact key.
Persist only after success and a fresh unchanged endpoint/credentials/write-authority
check. Existing pins are never auto-replaced; mismatches remain connection failures.
GET/resolve/capability checks remain read-only and never enroll trust. Dedicated
transfer credentials remain separate. No new grants or RouterOS write probes exist.
The transfer panel no longer exposes a manual fingerprint input; normal operations
need no extra settings step. Explicit settings save can also initialize the key;
TestOnly does not save it. Connection authority remains private.

Before this amendment, the user requested production write testing for the filled
unicom draft. Its new generated account had cleared private write opt-in and SSH
trust. Enabled container_writes for unicom only after a consistent verified NAS
backup at /home/tom/nas/wyp/github/rosboard/backups/20261015T071035Z-unicom-write-test/.
Verified all other config fields unchanged; the original form retains all values
and now offers Create. No create action was submitted by the agent.

Validation in progress; production rollout and manual/root acceptance pending.

Validation completed: full Go tests and vet pass;100 frontend tests, TypeScript/
Vite build, lint (four pre-existing FastTrack key warnings) and dual-UI isolation
pass. Added real SSH/SFTP fixture tests for first-use discovery, bad credentials
and mismatched pins, plus persistence tests for credential drift and existing-pin
protection. Actual independent RouterOS10.0.2.5 verified REST authentication,
automatic key observation, pinned SFTP metadata read and automatic persistence,
without RouterOS writes. Test service10.0.0.60 runs dev-auto-transfer-1015 and
all79 embedded file hashes match the final build.

Production rollout was blocked by automatic approval review before execution.
Reason returned: replacing the production binary and restarting service requires
explicit approval for this version. No production automatic-identity deployment
or new backup was executed; production remains dev-container-feedback-1015.
The earlier unicom private write opt-in remains enabled. Pending explicit user
permission to back up and deploy this version; keep task in_progress and PR Draft.
