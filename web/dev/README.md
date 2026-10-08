# Native Container simulation

Run in two terminals from the repository root:

```sh
ROSBOARD_CONTAINER_PREVIEW=1 go test ./internal/api -run '^TestContainerPreview$' -v -timeout 0
ROSBOARD_DEV_PROXY=http://127.0.0.1:8099 npm --prefix web run dev -- --host 127.0.0.1
```

Open http://127.0.0.1:5173/container-preview.html?ui=aurora or `?ui=compact`.
The server binds localhost, loads fake devices only, and never uses configuration,
SQLite or RouterOS credentials. Restarting resets all data. All mock writes live
in a Go `_test.go` file; the preview HTML is outside Vite's production entry.

Manual acceptance: review desktop 1440px and mobile 390px, both themes and UIs.
Create with blank optional values and explicitly filled image/VETH/bridge/IPv4/
gateway. Check all eight sections remain visible. Add TCP/UDP, special-character
env values and read-only mounts. Exercise stop/start/restart, adoption and shared
VETH locking, logs, update and delete. Select failure and unknown-result scenarios;
recover the latter by read-back without repeating creation. Switch devices with
an open form/job; no data or pending job should carry into the other device.
Load the large fixture; search/sort/filter/paginate and inspect long image names.
Shared objects and storage data are retained on deletion. This is simulation
acceptance only; independent RouterOS tests are required for future real writes.
