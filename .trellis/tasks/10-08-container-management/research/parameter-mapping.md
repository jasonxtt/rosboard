# RouterOS compatibility research

Sources: https://manual.mikrotik.com/docs/containers/ ; https://manual.mikrotik.com/docs/cli-reference/container/ ; https://manual.mikrotik.com/docs/containers/veth/ ; https://manual.mikrotik.com/docs/developer-guides/rest-api/ ; https://docs.portainer.io/user/docker/containers/advanced

Read-only observation on RouterOS 7.23.5 confirmed native container flags (`running`, `stopped`), `envlists`, `mountlists`, `cpu-usage`, `memory-high/max`, image defaults and native restart/update commands. Current online docs describe newer versions; version alone cannot guarantee capability. Do not collect registry credentials or config-json into API responses.

| UI | RouterOS projection | Notes |
|---|---|---|
| Image | remote-image | tag latest only if no tag/digest |
| Name | name | stable image slug + short draft ID |
| Network | interface; /interface/veth address,gateway,gateway6,mac-address; bridge port | new one-to-one; existing shared VETH preserved |
| Root directory | root-dir | largest writable disk; no free disk => validation error |
| Service access | container IP and application port; no additional RouterOS field | direct bridge/routed access; no port mappings |
| Environment | /container/envs list,key,value + envlists | values kept verbatim; independent ownership |
| Mounts | /container/mounts list,src,dst,mode + mountlists | rw or ro; shared mounts preserved |
| Command/entrypoint/user/workdir | cmd,entrypoint,user,workdir | blanks inherit; editing must preserve overrides |
| Resources | memory-high,max,cpu-list | blank global memory/default CPU; unsupported fields flagged |
| Startup | start-on-boot; start command | new true; edit preserve |
| Restart/logs | restart-policy; logging | no/always/on-failure; default no/true |
| Health | healthcheck-* | inherit unless explicit override; stop-on-unhealthy is separate and not projected |

RouterOS allows shared VETH; user deliberately requires dedicated VETH for new containers. Reading topology must join exact interface names rather than assume a veth naming convention. BSL Dockhand is visual inspiration only; implementation is original.

## Runtime read verification (2026-10-08)

All 13 projected GET menus succeeded on the current RouterOS 7.23.5 device.
The mounted ext4 `sata1` was the suitable disk; an unmounted log slot was excluded.
Native container rows expose `stopped` and `cpu-usage`, but no `memory-usage`.
Logs currently contain no records. Registry credentials/config-json were never
requested, and environment values were never printed or stored in research.
The read service caches snapshots for five seconds to bound default-preview load.

## Local archive and Files follow-up (2026-10-08)

Official references:
- https://manual.mikrotik.com/docs/containers/ (local PC image import and mount examples)
- https://manual.mikrotik.com/docs/system-information-and-utilities/files/ (metadata, directory creation and contents limits)
- https://manual.mikrotik.com/docs/developer-guides/rest-api/ (REST scalar/command behavior)

The independent test RouterOS reports 7.23.5 long-term, x86_64, Container enabled,
zero containers, one usable ext4 disk with about 3.9 GiB free. Snapshot and actual
Files reads passed through the production typed reader. The API/SSH account and
connection values were passed in process memory only, not written here.

Scoped capability verification created one unique temporary child under the
existing disk's docker directory. `/file/add` with `type=directory` and exact
metadata read-back passed. SFTP via the already enabled SSH service uploaded and
downloaded a binary archive larger than 1 MiB; SHA-256 equality passed. Temporary
archive and directory were removed, and zero matching entries remained. FTP was
disabled and was not enabled. These checks verify directory/binary transport
capability only, not container extraction/start/update behavior.

Important compatibility detail: REST DELETE of opaque `/file` IDs returned 400
on this version; typed POST `/file/remove` with the exact `numbers` ID succeeded.
Future file cleanup must read back exact owned names/IDs and verify disappearance,
not assume resource DELETE behaves like configuration menus. Large image bytes
should use a streaming binary transport (SFTP demonstrated here), not REST text
`contents`. Production transport still needs verified host keys, device-scoped
write locking, resumability/unknown-result handling, disk headroom and cleanup.

This checkpoint supports Docker-save single-image tar metadata validation only.
The simulator discards bytes after inspection and stores opaque metadata scoped
to one device. Real upload and mkdir handlers remain blocked. Optional test
preview reads actual directories; simulations remain isolated from it.

Use sibling `rootfs` and `volumes` folders under a per-container parent. Rootfs
holds the image/runtime filesystem; mounts hold persistent config/data. Never
place new persistent mount sources inside rootfs. Preserve existing relative
RouterOS file paths on edit. Services use the container IP and application port
directly. Existing routes/firewall permission still govern reachability. Port
mapping and NAT reads/ownership are removed from this feature; no automatic
hairpin NAT, source NAT or accept-rule edits belong to it.

## Direct-IP networking clarification (2026-10-08)

MikroTik documents VETH attachment to a bridge, including direct Layer2 LAN
attachment and dedicated container subnets. This is not a Docker macvlan/ipvlan
driver implementation, although independently addressed containers support the
user's direct-IP workflow. Same-LAN bridge access needs no destination NAT;
a separate subnet also supports direct access when routing and firewall permit.
Destination NAT is an optional router-address forwarding pattern, not a required
container setting. Remove it from the form, table, shared contract and simulator.
Existing firewall/NAT rules remain untouched.

Sources: https://manual.mikrotik.com/docs/containers/ ;
https://docs.docker.com/engine/network/drivers/macvlan/ ;
https://docs.docker.com/engine/network/drivers/ipvlan/

## Health-check explanation review (2026-10-08)

The official Container Healthcheck section documents support from 7.23, probing
inside the container via an application command, exit code zero as healthy and
nonzero as failure. Interval, timeout, retries and startup preparation control
when failures count. The official command example passes `curl -f ...` directly;
Dockerfile's `CMD-SHELL` marker is not a RouterOS command example. The executable
must exist in the image and the local port must match the application.

The current editor retains image inheritance by default; an image without a
check does not acquire an automatically invented probe. Custom mode projects
only the entered healthcheck fields. `stop-on-unhealthy` and notifications are
separate options, not implied by this form or its restart policy.

Reference: https://manual.mikrotik.com/docs/containers/#healthcheck
