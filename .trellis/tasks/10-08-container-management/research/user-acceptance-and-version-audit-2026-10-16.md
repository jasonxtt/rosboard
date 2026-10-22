# User acceptance and GitHub/version audit — 2026-10-16

## User decision and preservation boundary

The user explicitly accepted the container-management work and simple UI changes
from the last two days, requested saving them to GitHub, then asked for an audit
of PR integration and differences between main, release and latest development.
Record this as user acceptance; do not infer a request to merge, publish a release,
replace a production build, or complete/archive tasks from this audit request.

All implementation checkpoints were already published on their work branches:

| PR | Source checkpoint audited | Implemented scope | GitHub state at audit |
| --- | --- | --- | --- |
| #30 | 5a887a46e6b6310347b0c1de6f20933493cb2a15 | Native lifecycle/Files, host image download/SFTP, shared proxy, onboarding permissions, cleanup/memory fixes, latest update-history cleanup | Open Draft; unmerged |
| #32 | b3bd8864afb51decc9af8d4420bb78b7a18691c3 | Compose/Docker run imports and both native container presentations | Open Draft; unmerged |
| #33 | 4c95befa8fc04cda1d7680312d06daf5faf3a118 | Supplied Aurora policy-routing UI integration with theme/page isolation fixes | Open Draft; unmerged |
| #31 | c8570266905ba83f104261d83059d94f56c26943 | Existing RouterOS policy adoption planning only, implementation on hold | Open Draft; unmerged; outside this acceptance scope |

Acceptance-only documentation checkpoints may advance these PR heads afterward;
the source checkpoints above identify the audited functional differences.

The production and isolated test panels still identify as dev-policy-ui-1016,
which includes the earlier container baseline plus #32/#33. The newest historical
root cleanup in 5a887a4 passed independent real RouterOS validation but was not
installed into either panel service. User acceptance does not retroactively make
that a production deployment or authorize sweeping an already-orphaned folder.
Task completion/merge/release and any further production delivery retain their
separate workflow gates. The user acceptance portion is now satisfied.

## Formal release and main

Official GitHub latest-release API reports v0.2.7, published 2026-10-04T16:01:30Z,
not a draft or prerelease. Its target/tag commit is
d48f5e7770f46761c945fdfe7608989272403927.
Current remote main is 0d8c67059bb9f9650f1b698cd6621f43c5dc6e0c.

The complete v0.2.7 -> main diff contains only README.md, docs/docker.md and the
two Docker Compose example comments (five changed lines in each direction).
There are no program/frontend/dependency/workflow/VERSION differences. Main's
only additional change is merged PR #29 correcting RouterOS-address documentation.
Existing monitoring, routing/access, both UI variants, online update and Docker
support are present in the release/main baseline; the new container feature is not.

## Earlier PR audit

The official pulls API returned all 33 PRs: 26 directly merged, three closed without
a direct PR merge (#5/#7/#18), and four open Draft PRs (#30/#31/#32/#33).

- #7's exact head ab3b7bb is an ancestor of main: Aurora entered through the later
  dual-UI integration (#8), despite #7 itself being closed without a direct merge.
- #18's exact head c0e1a46 is an ancestor of main: FastTrack work entered through
  later integration (ce30141); the closed PR does not mean its code is missing.
- #5 is the closed unmerged old iKuai-style proposal, not an outstanding current
  feature release. Its head object was unavailable locally; do not infer inclusion.
- #25 (Docker support), #27 (entrypoint recovery), release PRs #26/#28 and #29 are
  directly merged. #31 remains a planning-only hold, not an implemented feature.

## Latest code is split across related heads

#32 descends from the older container checkpoint 92c35f7; #33 descends from #32.
The new 5a887a4 container-history cleanup is a separate descendant of 92c35f7 and
is not an ancestor of #32, #33 or main. Thus none of these three heads contains the complete
union of the accepted workstreams. Their code is safely saved in GitHub,
but a later integration must carry the #30 cleanup forward while preserving
#32 imports/native container UI and #33 policy-routing UI. Do not merge an older
feature head and assume the newest fix is already included.

Relative to release/main, the unmerged implemented functionality comprises:

1. Native container lifecycle, directories, verification/recovery and telemetry.
2. Host registry image downloads, HTTP/SOCKS proxy use for images and updater,
   verified archive/SFTP import and image staging/cleanup semantics.
3. Transfer-account onboarding/guarded authority checks and first-use identity.
4. Compose and Docker run form import with explicit unsupported-field handling.
5. Native container UI refinements in both shells and the Aurora routing UI refresh.
6. Root deletion, memory-unit/validation/error feedback fixes and the newest
   journal-bound historical-runtime cleanup (the last item is test-verified only).

Evidence: fresh GitHub PR/release API reads, refreshed remote refs/tags, ancestry
checks and exact Git diffs. This is a documentation-only audit; no runtime tests
need repeating, and no deployment, main merge or release occurred.
