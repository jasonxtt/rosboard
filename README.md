# rosboard

Read-only RouterOS monitoring panel focused on system overview, interface status, and unified IPv4/IPv6 terminal monitoring.

## Stack

- Go backend
- SQLite local persistence
- React + TypeScript frontend embedded into the Go binary

## Current scope

- single RouterOS target
- LAN-only deployment
- read-only monitoring
- Linux binary first, Docker later

## Local development

1. Install frontend dependencies:

   ```bash
   cd web
   npm install
   npm run build
   ```

2. Copy the example config and fill in RouterOS credentials:

   ```bash
   cp configs/config.example.yaml config.yaml
   ```

3. Run the backend:

   ```bash
   go run ./cmd/rosboard -config ./config.yaml
   ```

The frontend build writes assets into `internal/ui/dist`, which the Go binary serves directly.

## Environment overrides

You can override the RouterOS connection fields with environment variables:

- `ROSBOARD_ROUTEROS_BASE_URL`
- `ROSBOARD_ROUTEROS_USERNAME`
- `ROSBOARD_ROUTEROS_PASSWORD`

## Local non-interactive start

Create the ignored local configuration once, then start the built binary without environment variables:

```bash
cp configs/config.example.yaml configs/config.local.yaml
chmod 600 configs/config.local.yaml
./scripts/run-local.sh
```

`configs/config.local.yaml` is intentionally ignored because it contains the RouterOS password. Environment variables remain optional overrides and are not used by `scripts/run-local.sh`.

## Build

```bash
cd web
npm install
npm run build
cd ..
go build ./cmd/rosboard
```
