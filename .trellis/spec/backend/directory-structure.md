# Directory Structure

> How backend code is organized in this project.

## Overview

rosboard uses one Go module with an executable under `cmd/` and application
packages under `internal/`. Keep transport, orchestration, persistence,
RouterOS protocol code, and parsers in their owning packages. Tests live next
to the package they test.

## Directory Layout

```text
cmd/rosboard/       process entrypoint and runtime wiring
internal/api/       HTTP routing, authentication gates, request/response shapes
internal/auth/      administrator and session behavior
internal/config/    YAML configuration and normalization
internal/model/     shared monitoring data models
internal/mosdns/    MosDNS client integration
internal/policy/    source fetching, upload handling, and rule parsing
internal/policyv2/  policy desired state, planning, reconciliation, and jobs
internal/routeros/  RouterOS REST client, typed reads, and allowlisted writes
internal/service/   monitoring and device-level orchestration
internal/store/     SQLite schema, migrations, repositories, and persistence
internal/ui/        embedded frontend asset loading
web/src/             frontend source (built into internal/ui/dist)
```

`cmd/rosboard/main.go` constructs the long-lived services. `internal/api`
receives those dependencies through constructors such as
`NewServerWithManager`; handlers should not create global clients or open
databases themselves.

## Module Organization

- Put HTTP method/path dispatch and projection into `internal/api`. Keep
  domain decisions in `internal/service`, `internal/policy`, or
  `internal/policyv2`.
- Put all SQLite access in `internal/store`. Device-scoped repositories must
  use the store for the selected device and must not fall back to another
  device database.
- Put RouterOS protocol details in `internal/routeros`. Policy code consumes
  the typed reader/mutation interfaces instead of constructing arbitrary REST
  paths.
- Keep source parsing in `internal/policy`; do not make API handlers parse
  YAML or manually entered rules.
- Define small interfaces at the consumer boundary when a package needs a
  fake in tests. `internal/policyv2/manager.go` is the model: it declares
  `PolicyReader`, `PolicyMutation`, and uses a repository interface.
- Add a new package only when it owns a real boundary. Do not create a
  registry, framework, or second implementation for a single helper.

## Naming Conventions

- Package directories are lowercase; filenames use lowercase words joined by
  underscores, for example `source_fetcher.go` and `policy_v2_ip_sources_test.go`.
- Exported Go types and functions use PascalCase; local variables use
  camelCase. Sentinel errors use the `Err...` form.
- Methods accept `context.Context` first when they perform I/O or can block.
- Tests use the production package's `_test.go` files and descriptive
  `Test...` names. Keep test helpers near the tests unless they are shared by
  several packages.

## Examples

- [HTTP dependency wiring](../../../internal/api/server.go):
  `NewServerWithManager` and `ServeHTTP` keep routing in the API package.
- [Device-scoped storage](../../../internal/store/sqlite.go):
  `Store.ForDevice` and `Store.OpenDevice` select an isolated SQLite store.
- [Policy boundaries](../../../internal/policyv2/manager.go): the manager
  consumes reader, mutation, repository, and refresh interfaces.
- [RouterOS writes](../../../internal/routeros/mutation.go): the mutation
  client validates menus, fields, and object IDs before sending a request.

## Common Mistakes

- Adding RouterOS REST calls directly to an API handler bypasses allowlists,
  ownership checks, and test fakes.
- Reading a non-default device through the owner database can mix monitoring
  or policy state between routers.
- Putting source parsing or SQLite SQL in `cmd/rosboard` makes the behavior
  hard to test and breaks the package boundaries above.
