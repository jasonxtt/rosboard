# Runtime Configuration

## Scenario: Local non-interactive YAML startup

### 1. Scope / Trigger

- Trigger: local development or review service startup that must not depend on chat history, secret extraction, or `ROSBOARD_*` injection.
- The real local RouterOS credential is machine-local state and must never enter Git.

### 2. Signatures

- Binary: `./rosboard -config ./configs/config.local.yaml`.
- Stable launcher: `./scripts/run-local.sh`.
- Public template: `configs/config.example.yaml`.
- Private config: `configs/config.local.yaml` with mode `0600` and a `.gitignore` rule.

### 3. Contracts

- Required YAML fields: `routeros.base_url`, `routeros.username`, and `routeros.password`.
- Local defaults are explicit in YAML: `listen_address: 0.0.0.0:8080`, `data_dir: ./data`, positive poll/retention values, selected traffic interfaces, and allowed CIDRs.
- Allowed CIDRs include IPv4/IPv6 loopback plus the intended LAN ranges so both local and LAN review URLs work.
- `scripts/run-local.sh` resolves the repository root, verifies that the binary and config are available, changes to the root so relative `data_dir` is stable, and then uses `exec ... -config ...`.
- The launcher contains no credentials and sets no `ROSBOARD_*` values. Environment overrides remain supported by `config.Load`, but are not required by this workflow.

### 4. Validation & Error Matrix

- Missing/non-executable binary -> launcher exits with a build command hint.
- Missing/unreadable local YAML -> launcher exits before starting the service.
- Missing required RouterOS field -> `config.Load` fails startup.
- Invalid credential -> initial RouterOS refresh fails; validate with `/rest/system/resource` without printing the secret.
- Loopback omitted from `allowed_cidrs` -> `127.0.0.1` API requests return HTTP 403 even though LAN access works.
- Relative `data_dir` with a launcher that does not change directory -> data can be created under the caller's working directory.

### 5. Good/Base/Bad Cases

- Good: `scripts/run-local.sh` starts a process whose command line contains `-config /absolute/path/configs/config.local.yaml`, parent PID becomes 1 after detaching, and local/LAN URLs return 200.
- Base: operators can still invoke the binary manually with the same YAML path.
- Bad: extract a password from a Codex rollout at every start.
- Bad: store the real credential in the tracked example file or a shell script.
- Bad: rely on ambient working directory while YAML uses `data_dir: ./data`.

### 6. Tests Required

- Shell: `zsh -n scripts/run-local.sh`.
- Secret boundary: launcher contains no `password`, `ROLLOUT`, or `ROSBOARD_*`; local YAML is ignored and untracked with mode `0600`.
- Authentication: local YAML credential returns HTTP 200 from RouterOS system-resource REST endpoint without logging the credential.
- Delivery: running process command line contains the YAML `-config` argument; `127.0.0.1:8080` and the Mac LAN URL return HTTP 200.
- Runtime: Dashboard `updatedAt` advances across polling intervals.

### 7. Wrong vs Correct

#### Wrong

```zsh
PASSWORD=$(extract_from_chat_history)
ROSBOARD_ROUTEROS_PASSWORD="$PASSWORD" ./rosboard
```

#### Correct

```zsh
cd "$root_dir"
exec "$root_dir/rosboard" -config "$root_dir/configs/config.local.yaml"
```
