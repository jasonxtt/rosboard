# UI restyle: handoff entry

**2026-09-07: Overview direction approved; requested amendments and remaining-page styling implemented.**

The user approved the overview's overall appearance and explicitly requested removal of its system-status module, moving storage usage and data freshness into device information, renaming interface information to WAN信息, and a new mobile order. The same instruction authorizes extending the compact treatment to remaining pages. This supersedes the phase-1 pause below. Final visual and integrated business acceptance remain pending; no production deployment or merge is authorized.

See [the rollout checkpoint](research/rollout-checkpoint.md) for changes, verification, preservation boundaries and the updated actual application preview. The [phase-1 checkpoint](research/phase1-checkpoint.md) is historical evidence.

## Planning history (superseded by the authorization above)

The user approved the compact interactive study in this conversation, but is dissatisfied with the subsequent UI implementation by another agent. The latest instruction is to reread the repository and finish the plan without executing it. Earlier statements of acceptance or permission to begin do not authorize implementation now.

The user explicitly corrected the implementation starting point: begin from `feat/policy-access-rebuild`, not from the other agent’s UI. Only selectively take the approved overview modules from that reference. This worktree contains the accepted application baseline plus planning/reference files.

The user subsequently approved the overall plan and requested two specific overview exceptions: copy the other branch UI’s four header cards and quick-link module; place the preview’s device-information panel below quick links. This is a planning amendment, not authorization to implement. The original preview remains unchanged.

Read in order:

1. [Requirements](prd.md)
2. [Current source audit](research/current-state-audit.md)
3. [Design and migration decisions](design.md)
4. [Feature preservation checklist](research/rosboard-feature-inventory.md)
5. [Future implementation and verification](implement.md)
6. [Frozen approved preview](research/approved-preview/README.md)

Reference precedence: latest user instructions → [explicit overview exceptions](research/overview-reference-amendment.md) for the four header cards and quick links → compact approved preview for all other appearance → accepted functional baseline for behavior → this migration plan → earlier iKuai research. The prototype does not define the product's feature set.

- Implementation branch: `codex/ui-compact-rebuild`, independent worktree `/Users/tom/github/rosboard-ui-compact`. Created directly from the accepted functional baseline; none of the other agent’s UI commits are ancestors.
- Other-agent reference branch: `feat/ui-ikuai-restyle`; audited application HEAD: `1049314`. It remains untouched.
- Functional reference: `feat/policy-access-rebuild`, commit `7a7d463da028519661878070a0027cae50b69e0c`.
- Use a separate Draft PR for this independent implementation; do not update PR #5. The user explicitly requested a separate branch/worktree, superseding the previous same-workstream instruction. The accepted functional branch remains the implementation base; eventual delivery targets `main` under the repository acceptance gate.
- No runnable application files or generated assets are changed in this planning checkpoint. Task remains planning, not completed or accepted.
- Next action: wait for explicit user authorization to implement, then refresh the source audit if HEAD changed and begin phase 1 in `implement.md`.

The root `preview-ikuai/` directory is a different artifact and is **not** the visual reference approved in this conversation. Existing unrelated/untracked files must be preserved. No blanket reset, clean, or branch rollback.
