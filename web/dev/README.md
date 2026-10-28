# Native Container simulation

Run in two terminals from the repository root:

```sh
ROSBOARD_CONTAINER_PREVIEW=1 go test ./internal/api -run '^TestContainerPreview$' -v -timeout 0
ROSBOARD_DEV_PROXY=http://127.0.0.1:8099 npm --prefix web run dev -- --host 127.0.0.1
```

Open http://127.0.0.1:5173/container-preview.html?ui=aurora or `?ui=compact`.
By default the server binds localhost and loads fake devices only, without
configuration, SQLite or RouterOS credentials. Optional real container-read preview
credentials are described below. Restarting resets all data. All mock writes live
in a Go `_test.go` file; the preview HTML is outside Vite's production entry.

Manual acceptance: review desktop 1440px and mobile 390px, both themes and UIs.
Create with blank optional values and explicitly filled image/VETH/bridge/IPv4/
gateway. Check all seven sections remain visible. Verify direct container-IP
access guidance and add special-character env values and read-only mounts. Exercise stop/start/restart, adoption and shared
VETH locking, logs, update and delete. Select failure and unknown-result scenarios;
recover the latter by read-back without repeating creation. Switch devices with
an open form/job; no data or pending job should carry into the other device.
Load the large fixture; search/sort/filter/paginate and inspect long image names.
Shared objects and storage data are retained on deletion. This is simulation
acceptance only; independent RouterOS tests are required for future real writes.

Local images: select **本地上传** and choose one Linux Docker-save `.tar`
archive (max 1 GiB). Use `docker save -o image.tar repository:tag`, or Podman
`save --format docker-archive`. The simulator checks manifest/config metadata,
referenced layers, architecture, size and SHA-256 without extracting the archive.
It discards staged bytes and keeps device-scoped metadata only; the displayed
RouterOS archive destination is simulated. An accepted archive is projected to
`file`, not `remote-image`. The mock import phase replaces the registry download.
OCI-only, compressed archives, docker-export and multi-image archives are not yet
supported. Select rootfs separately from persistent mount sources.

Directory browsing: click **容器运行目录（root-dir）** or press ArrowDown in
that input to open Files immediately below it; the path remains directly editable.
**浏览挂载源** opens the mount picker.
Click folders/breadcrumbs, optionally create a child folder, then choose it.
Files are read-only context; a mount source may also select an existing file.
The virtual Files root and whole disk cannot be used as container rootfs.
New defaults are `/disk/rosboard/containers/name/rootfs`; keep persistent config
and data outside rootfs, e.g. `volumes/config` and `volumes/data` siblings.
The field is labelled **容器运行目录（root-dir）**. Runtime and persistent
directories may share an application parent, for example `nginx/rootdir` and
`nginx/data/config`; the mount source must remain outside the runtime directory.
Folder names are user choices; `rootfs` is the generated default name only.
Existing container paths are preserved. New folders/storage remain on deletion.

An optional real test device can be added with process-only environment values:
`ROSBOARD_CONTAINER_TEST_URL`, `ROSBOARD_CONTAINER_TEST_USER`, and
`ROSBOARD_CONTAINER_TEST_PASSWORD`. Set these privately, then start the same
preview command. **测试 RouterOS（容器只读）** appears in the device selector; its
snapshot and resolution use the real Container read API. Directory browsing and
folder CRUD use the production Files handlers and modify the independent test
RouterOS. Real image uploads, container actions and fixture resets are forbidden. No credentials reach
the browser. Never use production credentials or commit a credential file.
An opt-in read contract can be run with these values and
`go test ./internal/containers -run '^TestContainerIndependentRouterOSRead$' -v`.

Additional manual acceptance: in both UIs/themes at 1440px and 390px, choose
local upload; reject a wrong extension and incompatible architecture; upload a
matching Docker-save archive and observe its reference, size and destination.
Browse/create/select a rootfs folder and independently select a mount source;
verify the container target/read-only flag and all seven sections stay intact.
Switch to the real read-only test device and browse actual Files. Switching
devices during an upload/directory read must cancel outgoing work.

The table has IP/VETH and Bridge columns with no port-mapping column. The flat
editor has seven sections and describes container-IP:application-port access.
No router-IP forwarding or NAT rules are generated or claimed by the simulator.
For manual review, check this on both UIs at desktop/mobile widths and in light
and dark themes. Network reachability still uses existing routes/firewall.

Interaction acceptance: advanced network starts collapsed; expand with the arrow
to edit IPv6/MAC, collapse and reopen without losing values. Invalid advanced
fields expand on submission. Click or keyboard-open the runtime path picker,
type a path while it is open, close it and confirm the value remains; selecting
a folder updates only root-dir. Delete an env row with its right-hand trash icon
and confirm other names and multiline values remain. Check both UIs/themes at
1440px and 390px. Full-panel entry is **主机设置 → 容器管理** in each UI.

Startup acceptance: start/log/restart controls appear before a separate advanced
arrow; CMD, ENTRYPOINT, user and workdir are hidden by default. Existing overrides
show an indicator and survive collapse/reopen and submit. Health checks default
to image inheritance; custom mode enables command/timing fields, missing command
keeps the draft with an inline error, and switching back retains entered values
without projecting overrides. Read the service-response example and timing hints
in both themes; the probe runs inside the container and requires the named tool.


Bound Files acceptance: the input/right arrow opens a compact picker at the current
existing path. An invalid/missing path falls back to `/` without changing the input.
Breadcrumb ancestors remain clickable and scroll horizontally at narrow widths.
Folders appear first; files retain names/sizes with muted styling. Files cannot
be runtime roots; existing file mount sources remain selectable.

Click **＋ 新建文件夹**: only a local row appears, with its default name selected.
Type the final name and press Enter or blur to save; Escape cancels without a
RouterOS write. Creation stays in the parent. Folder **⋯** offers inline rename
and delete; failed rename restores the original row and leaves a readable error.
Delete requires a confirmation showing the exact name/path and stating that all
files/subdirectories will be removed. Root, disks and container-used root/mount
paths cannot be renamed/deleted. Rename updates matching draft paths; successful
delete clears matching selected paths without changing mount destinations.
Use only disposable test folders for these **real** operations. Loading stays
inside the picker; repeated commits cannot repeat a write. Unknown results block
further writes and offer **刷新确认结果**, which only reads back the prior request.
Unresolved operation records survive picker remounts, but are currently in memory
and do not survive application restarts.

Health inheritance hides all custom command/timing fields; switching to custom
shows them and switching back preserves entered values. Resource limits default
off and hide details; enabled shows memory/CPU fields. Turning off uses inherited
values; reopening retains input. Existing explicit limits initialize enabled.
Verify both behaviors at desktop 1440px/mobile 390px and both UI/themes.

Theme alignment acceptance: open the full Aurora and Compact container routes,
compare statistics/list/editor cards with adjacent native pages, then switch
light/dark at desktop 1440px and mobile 390px. Inputs, directory picker, status
badges and confirmation dialogs should follow the selected shell palette;
Aurora cards retain translucent surfaces and Compact cards retain solid surfaces.
The standalone preview loads only the chosen shell stylesheet for the same colours.
