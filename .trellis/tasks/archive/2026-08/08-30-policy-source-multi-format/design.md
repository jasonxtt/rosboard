# Policy source multi-format parsing design

## 1. Scope and invariant

The existing source kind is the only selector. The selected kind controls what
the parser emits:

```text
selected domain kind ──> domain rules only ──> existing domain source path
selected ip kind     ──> IP/CIDR rules only ──> existing IP source path
```

The same URL may be used in two separate source records when both its domain
and IP rules are wanted, but one source record never changes kind or emits both
rule families. No new database table, repository, reconciler, RouterOS menu,
or association model is needed.

## 2. Parser selection

Add one shared source-content preparation path that tries the existing safe
Clash YAML parser first and falls back to a line parser when the body is not a
Clash `payload` document. The fallback is content-based rather than based on
the filename or URL extension.

The fallback uses the existing kind-specific parsers:

- domain: bare domain / `domain:` / `full:` plus supported Clash domain rules;
- IP: bare address/CIDR plus `IP-CIDR` / `IP-CIDR6` rules.

The line parsers skip blank lines and full-line `#` comments. Inline comments
are not generalized; a `#` inside a value remains subject to the existing
normalizer. For a comma rule, only the rule type and first value field are
used, so Clash policy names and options do not affect routing semantics.

The fallback must still fail when it produces no valid rule. A structurally
valid Clash YAML with no applicable rules must retain the existing meaningful
error rather than becoming an empty prepared version.

## 3. Stored content and lifecycle

`PreparedSourceContent` continues to retain the original bytes and SHA-256 and
stores compressed source bytes in the existing `CompressedYAML` field. The
field name is historical; no schema or lifecycle change is warranted. Rules
remain in the existing source-version rule table and continue through pending
promotion and RouterOS reconcile exactly as before.

## 4. Upload boundary

Expand the existing filename allowlist to `.yaml`, `.yml`, `.txt`, and `.list`.
The upload service continues to:

- sanitize the display filename;
- copy through a bounded reader to a 0600 temporary file;
- validate content through `PrepareSourceContent`;
- remove the temporary file on every exit path.

The API remains multipart and the source kind remains the query parameter.

## 5. Frontend

Keep the existing parameterized `PolicySourcesPage` and source editor. Change
only the format-neutral labels, URL placeholder/error copy, upload hint, and
file `accept` list. Set the shared new-source draft and the visible default
refresh option to 7 days. Domain and IP user-visible semantics remain
separated by the selected page/kind; no new association state is introduced.

## 6. Error and compatibility behavior

- Existing domain YAML input follows the same parser and produces the same
  rules, type, and normalized values.
- Unsupported entries remain bounded ignored summaries.
- A line-list comment/header is ignored instead of shown as an invalid domain
  or IP entry.
- URL content-type and SSRF controls are unchanged; `text/plain` already
  covers `.txt`/`.list` responses.
- An omitted URL refresh interval defaults to 7 days at the API and scheduler
  boundaries; explicit intervals, including 24 hours, remain supported.
- Unknown/omitted source kind continues to normalize to `domain` at the API
  boundary.

## 7. Tests

Add table-driven parser cases for Clash YAML, Clash list, plain lists,
comments, mixed kinds, duplicates, malformed values, and empty applicable
content. Extend upload tests for `.txt`/`.list` acceptance and other-extension
rejection. Add API coverage proving URL/upload previews return only the
selected kind. Run existing domain regression tests unchanged.
