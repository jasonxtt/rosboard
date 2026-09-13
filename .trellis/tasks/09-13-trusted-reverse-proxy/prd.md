# Support trusted HTTPS reverse proxies

## Goal

Allow an operator to terminate HTTPS at a trusted reverse proxy such as Lucky
while keeping rosboard's internal listener on HTTP, without weakening the
same-origin/CSRF boundary for direct or untrusted requests.

## Requirements

- Add a startup-only YAML field named `trusted_proxy_cidrs`.
- Treat an empty list as the default disabled state; do not expose this setting
  as a panel-managed UI field or API write operation.
- Trust `X-Forwarded-Proto` and `X-Forwarded-Host` only when the immediate
  peer address belongs to a configured trusted proxy CIDR.
- Validate forwarded values strictly enough to prevent ambiguous proxy chains,
  invalid schemes, ports, paths, userinfo, or host injection from being used
  for same-origin decisions.
- Preserve the existing direct-connection behavior for peers outside the
  trusted proxy list and for deployments that do not configure the field.
- Use the effective external HTTPS scheme when setting session cookies so
  reverse-proxied sessions receive `Secure` cookies.
- Keep `allowed_cidrs` as a separate network access control; document that a
  proxy's source address must be allowed there when that list is non-empty.
- Document Lucky-style HTTPS termination and HTTP upstream operation in the
  configuration/deployment documentation and example YAML.

## Acceptance Criteria

- [ ] A valid config with `trusted_proxy_cidrs` loads and round-trips through
  YAML without appearing in the settings UI/API projection.
- [ ] HTTPS browser requests through a configured trusted proxy pass the
  login same-origin check when the proxy supplies matching forwarded scheme
  and host, and the issued cookie is `Secure`.
- [ ] The same request is rejected when the peer is not trusted, the forwarded
  scheme/host is missing or malformed, or the forwarded origin does not match.
- [ ] Direct HTTP/HTTPS same-origin requests continue to behave as before.
- [ ] Config validation rejects malformed or empty trusted proxy CIDR values.
- [ ] Focused Go tests, full Go validation, and `git diff --check` pass.

## Constraints

- Do not add a CORS allowlist or disable the existing same-origin write check.
- Do not trust forwarded headers merely because they are present; the trust
  decision must be based on the immediate TCP peer address.
- Do not return or log credentials or full configuration contents.
