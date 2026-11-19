<!-- This contract keeps execution and comparison guarantees beside the generator. -->
# Executable example generator contract

For manifest authoring, see
[Executable Extension Examples](../../../website/src/pages/docs/writing-extension-examples.mdx).
The extension Makefiles select manifests; Composer reuses its child-manifest discovery.

```sh
make -C extensions update-examples EXTENSION_PATH=composer
make -C extensions check-examples EXTENSION_PATH=composer
make -C extensions test-examples EXTENSION_PATH=composer
```

The aggregate `check` target includes `check-examples`.

## Manifest inputs

- Executable examples require a `config` object and ordered `commands[].argv`. `{}` is valid;
  missing or `null` configuration is not. `code` may be absent before generation. Existing
  code-only examples remain valid.
- `argv` preserves argument boundaries and runs without implicit shell evaluation. Explicit
  `sh -c` commands can use variables, pipelines, and heredocs.
- `preStart` accepts the same command fields and prepares files needed by BOE. Commands in either
  phase must finish; background children cannot provide persistent companion services.
- Pin external inputs to immutable revisions and fail downloads on HTTP errors. Updating a pin
  requires regenerating and reviewing its transcript.

## Execution and update invariants

- Compile comparison policies during input validation, before starting BOE, and reuse them during
  reconciliation. Invalid rules must fail even if the existing transcript would compare equal.
- Copy the extension's `examples/` contents into an isolated temporary working directory. Reject
  symlinks and non-regular fixture files. Isolate BOE configuration, extension cache, and runtime
  state from the user's installation; the Envoy binary cache may be reused.
- Run `preStart` sequentially after fixture staging. Start BOE with `--local` against the checkout,
  wait for Envoy readiness, then run `commands` sequentially in the same working directory.
  A pre-start failure prevents BOE startup.
- Expand only `${PROXY_URL}`, `${ADMIN_URL}`, `${UPSTREAM_ADDRESS}`, and `${WORK_DIR}` in
  configuration strings and arguments; reject unknown names. Use allocated proxy/admin addresses
  for execution and map them to display addresses in commands and captured output. The upstream
  address is `httpbin.org:443`; forwarded requests require network access.
- Require the command's exit code to equal `expectedExit`, defaulting to zero. Launch failures,
  signals, timeouts, cancellation, and cleanup failures always fail, regardless of the expected exit. An HTTP denial succeeds
  if the command returns its expected exit code.
- Bound startup and command duration. Clean up BOE, command process groups, and temporary
  directories on success or failure. Bound output-pipe waiting so shell children cannot keep a
  canceled command alive.
- Execute all selected examples and validate patched manifests before writing any manifest.
  `check-examples` reports drift without writing. Equivalent runs preserve the reviewed transcript.
- Preserve YAML outside generated `code`, including comments and unrelated examples. Reject
  flow-style example entries. End generated code with one newline to match YAML scalar clipping.
- Reject concurrent manifest edits before staging. Stage all changes before replacing targets
  and preserve file permissions. Replacement is atomic per file; a replacement failure may leave
  earlier files updated.

## HTTP-status retries

`commands[]` and `preStart[]` may declare `retry` with `httpStatus` (200–599) and `maxAttempts`
(1–10000). Repeat the same arguments and working directory within one total command timeout.
Retry only a valid HTTP status mismatch; an unexpected exit, timeout, cancellation, or unparseable
response fails immediately. Record only the selected attempt. Exhaustion fails before writes.

Require direct curl invocation, `--disable` or `-q` first, one HTTP(S) URL, and a declared capture
mode:

- `--verbose`: parse the framed stderr exchange.
- `--include`: parse the initial stdout response. HTTPS requires `--suppress-connect-headers`
  to exclude proxy CONNECT headers.
- `--write-out`: require a template starting with `HTTP/%{http_version} %{http_code}` and a newline,
  followed by any header lines and a blank line. Require `--output /dev/null` to divert the body.

Mixed modes must agree. HTTP-shaped body text cannot select a status. Reject redirects,
curl-owned retries, multiple transfers, config files, and unsupported options before execution.
URL globbing requires `--globoff`.

The generated comment states the retry target and bound; the displayed command runs once.
The [fault-injection example](../../../extensions/composer/dynamic-fault-injection/manifest.yaml)
captures each sampled status in a separate command. This verifies occurrence within the bound,
not the configured probability.

## Transcript format

- Display `boe run --extension` with the manifest's published name. The displayed command may differ
  from a released extension's behavior until the checkout's changes are released.
- Use default display addresses: proxy `http://localhost:10000`, admin `http://127.0.0.1:9901`,
  and upstream `httpbin.org:443`. Keep the selected Envoy version visible; omit allocated port
  variables, generator helpers, and lifecycle scaffolding.
- Show pre-start commands and output before the foreground BOE invocation in Terminal 1.
  Require the repository root only when referencing staged fixtures. Configuration and pre-start
  `${WORK_DIR}` paths are repository-relative; Terminal 2 enters `examples/` when present and uses
  `.` for command `${WORK_DIR}` paths. Generated files remain available throughout execution.
- Render `comment` as literal `#` lines above its command, without execution or placeholder
  expansion. Empty configuration or one scalar setting uses compact JSON; multiple settings,
  nested objects, and arrays use indented JSON.
- Wrap shell commands at argument boundaries within 80 columns, including indentation and
  continuation markers. Preserve indivisible arguments and JSON lines that exceed the limit.
  Do not interpret `&&` or `||` inside arguments or alter argument bytes to shorten lines.
- Use fenced `sh` command blocks and separate `text` stdout/stderr captures. Fences exceed every
  backtick sequence in their contents. Legacy code-only examples render as one shell block.
- Retain verbose curl's `>` request and `<` response lines. Remove only `* ` transport diagnostics
  and `{ [N bytes data]` / `} [N bytes data]` markers; preserve errors and other stderr.

Output metadata is one HTML comment after its fence, containing `stdout` or `stderr` and optional
clauses in this order: `Line endings`, `Bare CR offsets`, `Trailing whitespace`, `No trailing
newline`. Trailing whitespace is Base64-encoded JSON with zero-based line numbers and text.
Bare CR offsets distinguish carriage-return bytes from literal `\r` text. Display uses LF, escapes
bare CR, and strips trailing spaces and tabs; metadata keeps these byte distinctions significant.
The website hides only recognized metadata and never renders arbitrary code as HTML.

One codec owns encoding, restoration, and round-trip validation. Header reordering and elision
consume its structured body and metadata; Markdown rendering is a separate operation.

## Volatile header comparison

`volatileHeaders` contains case-insensitive HTTP field names; duplicate declarations select the
same field. For each selected value, try a validated date, then a duration. Dates include HTTP
date formats and RFC3339; durations include bare numbers and supported units. Numeric IDs and
counts therefore become volatile if their headers are listed. HTTP `Date` is selected
automatically and uses date recognition only, including when listed explicitly.

HTTP analysis begins at an initial status line or a complete verbose request immediately followed
by a response status. It follows interim `100`, `102`, and `103` sections and stops at the final
response. A `101` upgrade is final. Preambles, malformed or partial requests, and HTTP-looking
body text do not establish header context. Header normalization requires complete sections.

- Compare recognized values by kind and format. Magnitudes and calendar values may change;
  invalid or unrecognized values compare exactly. Switching kind or recognition state is drift.
- Ignore decimal precision and integer-versus-decimal notation for `ms` components, including
  compound durations. Preserve units, spacing, signs, leading decimal points, zero padding,
  date layout, timezone notation, and fractional precision in dates, other units, and bare numbers.
- Compare each complete header section independently. Ignore ordering between different names;
  group names case-insensitively while preserving repeated-field order. Header spelling, casing,
  separators, presence, and occurrence count remain significant.
- Move byte metadata with reordered headers; malformed metadata prevents order normalization.
  Apply header value rules only within recognized response sections.
- Custom `comparison` rules require `type: date` or `duration` and exactly one named `value`
  capture. Match original captured output before reordering, never commands or metadata. Both
  compared values must satisfy the rule in full context; fresh capture spans alone are insufficient.
  Coalesce compatible captures of the same span and leave conflicting overlaps literal.
- Status changes must never be normalized away. Reject custom captures intersecting recognized
  HTTP status lines, including interim responses and statuses with incomplete headers. This
  restriction does not apply to HTTP-shaped body text.

## Output elision

With `elideOutput: true`, standalone `...` lines in fenced `text` output match one or more complete
lines, including blank lines. The whole-line rule follows Istio's
[`_verify_elided`](https://github.com/istio/istio.io/blob/master/tests/README.md#verify-functions).
Matching is anchored to each output block, independently per stream. Leading, trailing, and
consecutive markers are allowed; each consumes at least one line. Inline or indented dots, and
all dots without the opt-in, remain literal.

Apply elision after successful execution and retry selection. Analyze the complete fresh capture
before selecting retained lines so omitted framing cannot promote body text into HTTP headers.
Record recognized status positions before validating their headers: malformed or truncated
headers must not allow a status to be hidden. Retained values use the same comparison rules.

Keep header terminators to compare retained headers independently of order. Repeated retained
fields preserve their relative order; omitted headers may change, appear, or disappear if each
marker still consumes a line. The fault-injection example uses this to omit its conditional
added-delay header while retaining other timings and statuses.

Omitted content and formatting are unchecked. Metadata describes only retained lines and
synthetic LF-terminated markers; refresh it after authors edit an excerpt. A retained content
mismatch expands that output block to the full capture for review, leaving matching excerpts
compact. Changes only to retained byte metadata refresh the excerpt without expansion.

## Current execution boundaries

Each example runs one extension configuration in one BOE instance with the default upstream.
The runner has no additional bootstrap options, service supervision, teardown phase, credential
expansion/redaction, browser interaction, or privileged host-network setup. These gaps prevent
migration of examples requiring:

- Multiple configurations or peers:
  [DNS lookup](../../../extensions/dns-gateway/manifests/lookup/manifest.yaml) and
  [cluster router](../../../extensions/composer/cluster-router/manifest.yaml).
- Extra clusters, upstream selection, or routes:
  [Bedrock](../../../extensions/composer/bedrock-guardrails/manifest.yaml),
  [token exchange](../../../extensions/composer/token-exchange/manifest.yaml),
  [OpenFGA](../../../extensions/composer/openfga/manifest.yaml), and decoder/routing examples.
- Credential substitution for external cloud/API calls.
- Docker services or login flows: [SAML](../../../extensions/composer/saml/manifest.yaml) and token
  exchange. Pre-start commands could launch containers but cannot guarantee their cleanup.
- Privileged networking for DNS interception.
- Dynamic-metadata inspection in decoder examples. Curl cannot observe it, decoder logs do not
  expose it, and BOE's successful-run logs are not captured. An observer filter or metadata access
  log is needed.

## Verification

Tests must prove comparison boundaries, check-mode non-mutation, equivalent-value preservation,
format-change drift, and failure before writes. Run process and loopback tests uncached with the
required permissions. Validate pilot examples against the pinned Envoy version, then confirm
repeated generation leaves manifests unchanged.
