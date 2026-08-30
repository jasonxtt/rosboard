<!-- TRELLIS:START -->
# Trellis Instructions

These instructions are for AI assistants working in this project.

This project is managed by Trellis. The working knowledge you need lives under `.trellis/`:

- `.trellis/workflow.md` — development phases, when to create tasks, skill routing
- `.trellis/spec/` — package- and layer-scoped coding guidelines (read before writing code in a given layer)
- `.trellis/workspace/` — per-developer journals and session traces
- `.trellis/tasks/` — active and archived tasks (PRDs, research, jsonl context)

If a Trellis command is available on your platform (e.g. `/trellis:finish-work`, `/trellis:continue`), prefer it over manual steps. Not every platform exposes every command.

If you're using Codex or another agent-capable tool, additional project-scoped helpers may live in:
- `.agents/skills/` — reusable Trellis skills
- `.codex/agents/` — optional custom subagents

Managed by Trellis. Edits outside this block are preserved; edits inside may be overwritten by a future `trellis update`.

<!-- TRELLIS:END -->

## Machines: test vs production delivery

- **Test machine `10.0.0.60`** — disposable LXC Debian 12 (x86_64, systemd). SSH: `ssh rosboard-test` (alias in `~/.ssh/config`, key auth configured; fallback `sshpass -p '1234567a' ssh root@10.0.0.60`). The user may freely install, break, reset, and redeploy anything here: experimental builds, destructive tests, and scratch data need **no backup and no acceptance gate**. Never point it at production RouterOS credentials or the production SQLite data; give it its own config/data directory.
- **Production delivery machine `10.0.0.6`** (`ssh net`) — the user's live instance under `/opt/rosboard`. Every deployment there must follow the acceptance gate below.

## Deployment acceptance gate (production `10.0.0.6` only)

For every change that modifies the runnable program and is meant for the live instance:

1. Finish automated checks and local/runtime verification; exercise risky or destructive changes on `10.0.0.60` first.
2. Before replacing the build on `10.0.0.6`, preserve a timestamped backup of the existing binary, configuration, SQLite data, and service unit under the local NAS path `/Users/tom/nas/wyp/github/rosboard/backups/<timestamp>-<label>/`. Confirm that the NAS path is mounted and writable first; do not store development rollback backups on `10.0.0.6` (including `/opt/rosboard/backups`). Keep at most 10 backup directories; before creating the 11th, remove only the oldest timestamped backup directory.
3. Verify the remote systemd service, health endpoint, affected API contracts, and embedded frontend assets.
4. Wait for the user to manually inspect the deployed instance and explicitly approve it.
5. Only after that approval, create the work commit and continue with Trellis task archival/session recording.

Do not commit program changes before the remote manual-acceptance gate. Documentation-only or planning-only changes do not require deployment. Test-machine deployments (`10.0.0.60`) do not require backups or manual acceptance, but still require the automated checks to pass first.

## Mac development directory backup

To sync the local GitHub development directory for remote development, use `/Users/tom/github/backup-github-to-nas.sh`. It copies `/Users/tom/github/` to the NAS path `/Users/tom/nas/wyp/github/` over SSH, so the NAS contains directly usable project directories rather than an archive. Rebuildable `node_modules/`, `.venv/`, `target/`, and `__pycache__/` directories are excluded. It does not delete NAS-only files, so work created remotely is not removed by a later Mac-to-NAS sync. The rosboard rollback directory and old-net archive are also outside the sync scope and remain reserved for backup retention.
