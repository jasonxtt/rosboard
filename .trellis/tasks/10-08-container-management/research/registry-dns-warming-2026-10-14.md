# Container registry DNS warming checkpoint — 2026-10-14

## Authorized boundary

The user approved registry-aware external A/AAAA queries from rosboard to the
selected RouterOS before confirmed image creation, TTL refresh during download,
archive bypass, and reuse on image updates/recovery. Routing policy code and
configuration must remain unchanged; no additional connectivity-validation gate.
Test-machine checks must pass before the authorized production replacement.

## Implementation and automated checks

The container service now prepares Docker Hub registry/authentication/layer
hosts, GHCR registry/layer hosts, or the explicit custom registry host. Bare
references follow private RouterOS registry/assumed-registry configuration. A
separate closed internal config read exposes only these URL properties; snapshot
reads still exclude them and all registry credentials. DNS queries target only
the selected router's port 53, never the default host resolver for image names.

The bounded wire client validates replies and uses TCP after UDP truncation.
Refresh uses the minimum A/AAAA/CNAME TTL. Query original known names rather than
arbitrary CNAME targets, which could select a different DNS policy. Preparation
is best effort; safe optional job warnings do not abort the existing import.
Workers are deduplicated per device/job, stop on terminal import/job state, and
restore for journaled pending registry adds without repeating container commands.
Unknown imports retain refresh while import continues. No DNS answers/secrets
are persisted and no policy, DNS or firewall mutations are added to the program.

Go full tests/build/vet and containers/routeros/api race passed. Frontend 91
tests, lint, production build and dual-UI emitted asset checks passed; four
existing fasttrack JSX-key lint warnings remain. Tests cover registry selection,
UDP/TCP/reply validation, TTL, safe warnings, ordering before add, replay, archive
bypass, worker deduplication, restart restoration and terminal stop.

## Actual independent lab checks

A new isolated supervised service on the disposable test machine uses its own
configuration, administrator and fresh SQLite/journals, and the independent lab
RouterOS only. Existing test/production service configurations are preserved.

GHCR Alpine import completed successfully. A GHCR update was interrupted by
service restart during import: the journal restored unknown state, retained a
single replacement, and explicit recovery completed without duplicate add.
The original runtime was retained as designed. Temporary exact GHCR DNS fixtures
were used to supply the lab's pre-existing output route, then removed.

Docker Hub warming produced dynamic addresses and repeated queries during the
job, but actual downloads failed. Packet capture showed the first registry TLS
connection traversing the intended proxy gateway and exchanging application
data. A later internal lookup returned additional registry IPs; its immediate
TLS connection used the lab's ordinary gateway before external warming picked
up the new address. This demonstrates a DNS/address-list race in the approved
pure-query design. A temporary FQDN address-list-parent proof on the lab did not
pass either and was removed; it was not added to program code or production.

The existing lab Nginx remained running and returned HTTP 200. All disposable
containers/VETHs and test DNS fixtures were removed. After confirming no live
container references, the fixture-only scratch namespace was deleted. Raw captures, real
credentials, private config and session cookies are excluded from Git.

## Blocking acceptance finding

Pure pre-query/TTL warming cannot yet be considered a reliable Docker download
fix for rotating DNS answers. No new production deployment is authorized by
these failed checks. The previous production build remains in service. Await
user direction on expanding only the container download address-synchronization
scope; policy functionality/settings remain outside the current change. Keep
the same Draft PR active, with no APPROVED, merge, release or task completion.
