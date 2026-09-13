# Implementation plan

1. Extend `internal/config` with the YAML field, strict CIDR validation, and
   load/save tests.
2. Extend `internal/api.Server` construction with parsed trusted proxy ranges.
3. Add strict forwarded-origin resolution, wire it into the API admission
   check, and convert session-cookie helpers to use the effective external
   scheme.
4. Add API regression tests for trusted/untrusted peers, HTTP/HTTPS schemes,
   Host forwarding, malformed headers, missing headers, and Secure cookies.
5. Update `configs/config.example.yaml`, `docs/configuration.md`, and
   `docs/deployment.md` with the startup-only configuration and Lucky steps.
6. Run focused tests, `gofmt`, `go test ./...`, `go build ./...`, `go vet
   ./...`, and `git diff --check`.
7. Inspect only the intended staged paths before the checkpoint commit; do not
   stage existing unrelated untracked files.
