# Create action feedback regression, 2026-10-15

User report: creating the filled mosdns container from the production in-app
browser appears to do nothing; no download proxy is configured.

Reproduced on the user's existing tab, preserving all form values. The action
returns HTTP409 / ssh_key_required: `请先配置并核验镜像传输账号及 SSH 指纹`.
No job or native write is admitted. The production device has no saved pinned
SSH fingerprint. This is independent of registry egress: a direct request from
production to https://registry-1.docker.io/v2/ returns the expected HTTP401
registry authentication challenge, without a proxy.

Root UI defect: ContainerPage reused one error state for both action failures and
snapshot refreshes. Every successful automatic snapshot read cleared it. It was
also rendered above the long editor, outside the viewport near the submit button.

Fix: separate snapshot read errors from operation errors. Successful refreshes
clear only read failures. Display create/edit submission failures in the editor
footer beside the submit action, preserving the draft and allowing a retry.
Both UI variants use this shared page/editor. No transfer authority, fingerprint,
proxy configuration, credentials or RouterOS resources are changed by this fix.

Validation: 98 frontend tests pass, including an integration regression that
submits a create action rejected with ssh_key_required, refreshes the snapshot and
asserts the exact error remains in the footer. TypeScript/Vite build, lint (four
existing FastTrack key warnings), dual-UI isolation and Go embedded-UI tests pass.
New embedded program built and deployed to isolated 10.0.0.60 test service as
`dev-container-feedback-1015`; health/service and all79 embedded hashes pass.

Production deployment pending: current execution environment has no mount at
/home/tom/nas; existing /mnt/nas-wyp autofs mount for //10.0.0.8/wyp fails with
No such device. Asked user to restore/provide a writable mounted NAS backup path.
Per AGENTS.md production backup gate, production was not replaced. PR stays Draft,
manual/root acceptance remains pending. Missing fingerprint must be independently
verified and explicitly saved before the requested production create can proceed.
