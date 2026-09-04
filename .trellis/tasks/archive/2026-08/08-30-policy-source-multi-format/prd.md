# Policy source multi-format parsing

## Goal

Extend the existing policy-routing source content input so that remote URLs
and local uploads can use Clash YAML, Clash rule-list text, or plain lists,
while preserving the source kind contract:

- a `domain` source extracts only domain rules;
- an `ip` source extracts only IPv4/IPv6 address and CIDR rules.

All accepted content continues through the existing preview, pending/active
version, source refresh, and RouterOS reconciliation pipeline.

## Requirements

1. Keep the existing `domain` / `ip` source kind model. Do not add a source
   type that produces both kinds or create a second source/routing subsystem.
2. For URL and upload content, support:
   - the existing Clash YAML `payload` format;
   - Clash line rules such as `DOMAIN`, `DOMAIN-SUFFIX`, `IP-CIDR`, and
     `IP-CIDR6`, with trailing policy/options ignored;
   - plain one-value-per-line domain, IP, or CIDR lists according to the
     selected source kind.
3. Ignore blank lines and `#` comments. Keep unsupported rule types in the
   existing ignored counts/error samples; never reinterpret them as a domain
   or IP.
4. Preserve existing normalization and validation:
   - domain normalization and exact/suffix semantics remain unchanged;
   - IP family checks, masked CIDR canonicalization, zone rejection, and
     type/value deduplication remain unchanged;
   - size, UTF-8, NUL, YAML safety, and valid-rule limits remain enforced.
5. Accept local upload filenames ending in `.yaml`, `.yml`, `.txt`, or
   `.list`; filename extension selects no parser and remains display metadata
   only.
6. Keep GitHub `blob` URL normalization, HTTPS/SSRF protections, content-type
   checks, preview/save contracts, and source lifecycle behavior unchanged.
7. Update frontend copy and file input filtering so users can select the new
   text/list formats without changing existing domain behavior.
8. New URL-based domain and IP lists default to a 7-day refresh interval when
   no interval is supplied; explicitly selected existing intervals remain
   unchanged.

## Acceptance Criteria

- [x] A domain source preview/save from Clash YAML, `.list`, `.txt`, or an
      equivalent local upload stores only valid domain rules.
- [x] An IP source preview/save from Clash YAML, `.list`, `.txt`, or an
      equivalent local upload stores only valid IPv4/IPv6 address/CIDR rules.
- [x] A mixed Clash list reports the other kind and unsupported types as
      ignored rather than importing them into the selected source.
- [x] Comments and blank lines do not inflate ignored/error counts.
- [x] Existing domain YAML tests and behavior remain unchanged.
- [x] Upload path accepts `.yaml`, `.yml`, `.txt`, and `.list`, rejects other
      extensions, and still isolates temporary files safely.
- [x] URL and upload API tests cover format fallback, kind filtering,
      normalization, duplicates, invalid entries, and backward compatibility.
- [x] Frontend build/lint and targeted Go tests pass; no unrelated dirty files
      are changed.
- [x] Deployment acceptance gate is completed before committing program
      changes.
- [x] New URL-based domain and IP lists show and persist a 7-day default
      refresh interval.
