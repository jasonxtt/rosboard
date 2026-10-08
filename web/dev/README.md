# Native Container simulation

Run in two terminals from the repository root:

```sh
ROSBOARD_CONTAINER_PREVIEW=1 go test ./internal/api -run '^TestContainerPreview$' -v -timeout 0
ROSBOARD_DEV_PROXY=http://127.0.0.1:8099 npm --prefix web run dev -- --host 127.0.0.1
```

Open http://127.0.0.1:5173/container-preview.html?ui=aurora or `?ui=compact`.
By default the server binds localhost and loads fake devices only, without
configuration, SQLite or RouterOS credentials. Optional real read-only preview
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

Directory browsing: **浏览根目录** or **浏览挂载源** opens the Files picker.
Click folders/breadcrumbs, optionally create a child folder, then choose it.
Files are read-only context; a mount source may also select an existing file.
The virtual Files root and whole disk cannot be used as container rootfs.
New defaults are `/disk/rosboard/containers/name/rootfs`; keep persistent config
and data outside rootfs, e.g. `volumes/config` and `volumes/data` siblings.
Existing container paths are preserved. New folders/storage remain on deletion.

An optional real test device can be added with process-only environment values:
`ROSBOARD_CONTAINER_TEST_URL`, `ROSBOARD_CONTAINER_TEST_USER`, and
`ROSBOARD_CONTAINER_TEST_PASSWORD`. Set these privately, then start the same
preview command. **测试 RouterOS（只读）** appears in the device selector; its
snapshot, resolution and directory browsing use the real read-only API. Real
uploads, mkdir, actions and fixture resets are forbidden. No credentials reach
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
