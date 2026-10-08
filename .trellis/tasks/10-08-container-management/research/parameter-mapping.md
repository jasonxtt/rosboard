# RouterOS compatibility research

Sources: https://manual.mikrotik.com/docs/containers/ ; https://manual.mikrotik.com/docs/cli-reference/container/ ; https://manual.mikrotik.com/docs/containers/veth/ ; https://manual.mikrotik.com/docs/developer-guides/rest-api/ ; https://docs.portainer.io/user/docker/containers/advanced

Read-only observation on RouterOS 7.23.5 confirmed native container flags (`running`, `stopped`), `envlists`, `mountlists`, `cpu-usage`, `memory-high/max`, image defaults and native restart/update commands. Current online docs describe newer versions; version alone cannot guarantee capability. Do not collect registry credentials or config-json into API responses.

| UI | RouterOS projection | Notes |
|---|---|---|
| Image | remote-image | tag latest only if no tag/digest |
| Name | name | stable image slug + short draft ID |
| Network | interface; /interface/veth address,gateway,gateway6,mac-address; bridge port | new one-to-one; existing shared VETH preserved |
| Root directory | root-dir | largest writable disk; no free disk => validation error |
| Ports | /ip/firewall/nat dstnat protocol,dst-port,to-addresses,to-ports | explicit only; no egress changes |
| Environment | /container/envs list,key,value + envlists | values kept verbatim; independent ownership |
| Mounts | /container/mounts list,src,dst,mode + mountlists | rw or ro; shared mounts preserved |
| Command/entrypoint/user/workdir | cmd,entrypoint,user,workdir | blanks inherit; editing must preserve overrides |
| Resources | memory-high,max,cpu-list | blank global memory/default CPU; unsupported fields flagged |
| Startup | start-on-boot; start command | new true; edit preserve |
| Restart/logs | restart-policy; logging | no/always/on-failure; default no/true |
| Health | healthcheck-*; stop-on-unhealthy | inherit unless explicit override |

RouterOS allows shared VETH; user deliberately requires dedicated VETH for new containers. Reading topology must join exact interface names rather than assume a veth naming convention. BSL Dockhand is visual inspiration only; implementation is original.

## Runtime read verification (2026-10-08)

All 13 projected GET menus succeeded on the current RouterOS 7.23.5 device.
The mounted ext4 `sata1` was the suitable disk; an unmounted log slot was excluded.
Native container rows expose `stopped` and `cpu-usage`, but no `memory-usage`.
Logs currently contain no records. Registry credentials/config-json were never
requested, and environment values were never printed or stored in research.
The read service caches snapshots for five seconds to bound default-preview load.
