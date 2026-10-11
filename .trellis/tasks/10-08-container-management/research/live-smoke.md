# First authorized live Container smoke test

The user authorized a simple real container on the independent lab RouterOS,
after accepting the current UI provisionally. The lab runs RouterOS 7.23.7
long-term on x86_64 with Container mode enabled. Credentials, actual network
addresses, device IDs and private operation receipts stay outside the repository.

## Verified operations

- Resolve a dedicated Nginx Alpine draft with the deployed rosboard API, then
  apply its Container fields to the isolated lab. Dedicated VETH/bridge-port
  objects were created with an identifying smoke-test comment.
- Registry creation was accepted, but the router failed direct Docker Hub
  access and its fallback registry DNS resolution. This is a network/download
  failure, not successful image installation. Read logs before proceeding.
- Download the official linux/amd64 image on the disposable rosboard test host
  using skopeo, produce a single-image Docker archive, and transfer it through
  SFTP. Re-read the remote archive and compare its SHA-256 with the source.
- Remove only the exact failed smoke container after matching its name, ID,
  interface, root path and comment; preserve its dedicated VETH and bridge port.
  Import the verified archive using `file` instead of `remote-image`.
- RouterOS accepted the resolved absolute `/disk/application/rootdir` path.
  Import finished with `stopped=true`; explicit start launched the inherited
  Nginx entrypoint and command. A localhost HTTP probe returned 200 and healthy.
- Explicit stop exited gracefully with code zero. Start and native restart
  retained the root path and returned to healthy, with repeated HTTP 200 logs.
- `/container/start`, `/container/stop`, and `/container/remove` accepted
  `numbers` with the exact object ID. On this version, `/container/restart`
  rejected `numbers` and accepted singular `number`. Do not guess one argument
  shape for every lifecycle command.

## Read-model fixes revealed by real data

- Download flags are `downloading/extracting` and `download/extract failed`.
  Failure takes precedence over a stopped flag. URL-encode the `.proplist`
  parameter because a requested property contains a space.
- A healthy container may return `healthy=true` without `running=true`.
  `starting-with-healthcheck` maps to starting; healthy/unhealthy maps to running
  only after explicit stopped/error/transitional flags have been considered.
- Per-container memory is `memory-current`; retain `memory-usage` as an older
  fallback without inventing telemetry when neither property is present.

## Boundary and remaining validation

This was an operator-controlled live smoke test, not a production Container
writer acceptance. The panel Container lifecycle endpoints still fail closed;
real Files directory operations remain separate. The running smoke container
and its owned artifacts remain in the lab for user inspection.

The selected existing bridge originally had no LAN member. The user explicitly
authorized lab bridge preparation and console recovery. Adding the lab LAN
interface exposed a virtual-switch dependency: the bridge retained its VETH
MAC, whereas the ESXi port group rejected MAC changes and forged transmits.
The lab management connection dropped before address migration could execute.

Recovery through the named lab VM console fixed the bridge administrative MAC
to the original LAN NIC MAC and moved the existing management address to the
bridge. REST access and the deployed rosboard read model recovered. The lab
router fetched the container IP directly and received HTTP 200 with the Nginx
welcome page. External clients still could not reach it.

Read-only ESXi and upstream RouterOS inspection confirmed that the lab shares
an untagged standard-switch network with existing VMs. Its port group rejects
promiscuous mode and forged transmits, preventing a bridged container from
using an additional MAC through the VM NIC. Do not relax the shared production
port group as a silent container-creation side effect. A proposed separate lab
port group on the same switch would scope the two required exceptions to the
lab NIC; this still permits the lab to receive more traffic on that shared
broadcast domain and requires explicit authorization.

Inspection also found a pre-existing duplicate lab WAN address on another VM.
Alternate management destinations must be identified before authenticating;
an open SSH port does not establish that the endpoint is the intended router.
Private topology and recovery receipts remain outside the public repository.
No bridge VLAN, NAT, upstream RouterOS, or shared ESXi policy changes were
applied in this checkpoint.

Reference: https://manual.mikrotik.com/docs/containers/

Virtual-switch reference: https://knowledge.broadcom.com/external/article/440372/virtual-switch-portgroup-configuration-f.html
