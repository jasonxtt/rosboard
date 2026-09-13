# Technical design

## Configuration

Add `TrustedProxyCIDRs []string `yaml:"trusted_proxy_cidrs,omitempty"`` to the
process-global `config.Config`. `config.Load` validates every non-empty value
with `net.ParseCIDR`; invalid or blank entries fail startup rather than being
silently ignored. Existing `allowed_cidrs` behavior remains unchanged.

`api.Server` parses the configured trusted ranges at construction time. The
configuration is not included in `settingsResponse`, because it is a
deployment security boundary rather than a panel-managed runtime setting. All
existing config-save paths copy the full `Config`, so the field survives
device/collection saves.

## Effective request origin

Keep the existing pure `sameOriginWrite(request)` helper for direct-request
tests and compatibility, and add the server-aware path used by `ServeHTTP`.
The server-aware check will:

1. Allow GET, HEAD, and OPTIONS as before.
2. Reject `Sec-Fetch-Site: cross-site` writes and missing/malformed Origin.
3. Derive the direct scheme from `request.TLS`.
4. If the immediate peer is in `trusted_proxy_cidrs`, require a single strict
   `X-Forwarded-Proto` token and use a single strict `X-Forwarded-Host` value
   when present. A missing forwarded protocol fails closed because an HTTP
   upstream cannot reveal the public scheme; a missing forwarded host falls
   back to the direct Host so a proxy may preserve the public Host. Malformed
   present values fail closed.
5. Parse the resulting scheme/host/port with the existing exact-origin parser
   and require scheme, normalized port, and host equality with the browser's
   Origin.

Only the immediate `RemoteAddr` is considered for proxy trust. Forwarded
client IPs are not used for the allowlist or login throttling in this slice.

## Session cookie security

Convert cookie helpers to server methods so `Secure` follows the same trusted
effective scheme used by the origin check. This keeps direct HTTPS behavior,
adds `Secure` for HTTPS terminated at a trusted proxy, and leaves direct HTTP
cookies non-Secure for existing HTTP deployments. Cookie path, HttpOnly,
SameSite, expiry, and max-age remain unchanged.

## Compatibility and failure mode

- No configured trusted proxy: current behavior is unchanged.
- Trusted proxy with no `X-Forwarded-Proto`: the request fails closed because
  an HTTP upstream cannot reveal the public scheme; this prompts the operator
  to configure the required forwarding header.
- A trusted proxy that sends `X-Forwarded-Host` but preserves Host is accepted;
  if it rewrites Host, the forwarded host must be present and valid.
- Multiple comma-separated forwarding values are rejected instead of guessing
  which hop is authoritative.

## User operation

The example and deployment docs will show:

```yaml
trusted_proxy_cidrs:
  - "127.0.0.1/32"
```

Lucky terminates HTTPS, proxies to `http://127.0.0.1:8080`, preserves the
external host or sends `X-Forwarded-Host`, and sends
`X-Forwarded-Proto: https`. The operator restarts rosboard after editing the
startup YAML. If `allowed_cidrs` is non-empty, the Lucky source address must
also be included there.
