<!-- This contract lives beside the generator so future changes preserve executable examples and meaningful diffs. -->
# Executable example generator

This tool refreshes `examples[].code` in extension manifests by running BOE and Envoy against
the declared configuration and commands. The existing extension Makefiles are its public
entrypoints; Composer uses its existing manifest discovery to select child extensions.

```sh
make -C extensions update-examples EXTENSION_PATH=composer
make -C extensions check EXTENSION_PATH=composer
make -C extensions check-examples EXTENSION_PATH=composer
make -C extensions test-examples EXTENSION_PATH=composer
```

The aggregate `check` target depends on the separate `check-examples` target.

## Requirements

- Authors supply `config` and ordered `commands[].argv`; they can omit `code` until generation.
  A present empty configuration object is valid. Missing configuration and `null` are distinct
  from `{}` and are invalid for executable examples. Existing code-only examples remain valid.
- Start the selected extension through the current checkout's local BOE execution path, wait
  for Envoy readiness, and run commands against it. Never substitute expected output for execution.
- Execute BOE with `--local` against the checkout, but display `--extension` with the manifest's
  published extension name. Recorded output verifies the checkout; published releases can differ
  until those changes are released. Require the repository root in displayed commands only when
  they reference staged repository fixtures.
- Optional `preStart` commands prepare inputs before BOE starts. Stage fixtures first, then run
  these commands in order in the same isolated work directory used by later commands. Share command
  execution, timeout, capture, and `expectedExit` handling with `commands`; a pre-start failure
  prevents BOE startup and all manifest writes. Processes must finish before the next step.
- Pin external example inputs to immutable revisions and make download commands fail on HTTP
  errors. Dependency updates require changing the pin, regenerating, and reviewing the transcript;
  unrelated checks must not silently adopt changes from a mutable branch.
- Timeouts and cancellation always fail, regardless of `expectedExit`. Kill the command process
  group on cancellation and bound output-pipe waiting so shell children cannot outlive a timed-out
  command. Cleanup failures must remain errors even when a nonzero exit was expected.
- Render pre-start commands and captured output in Terminal 1 before its BOE invocation. Its
  `${WORK_DIR}` display path is relative to the repository root, matching configuration paths.
  Terminal 2 commands use `.` after their optional fixture-directory `cd`. Generated files must
  remain available to BOE and later commands for the entire example run.
- Capture commands and their stdout/stderr in a reproducible transcript. Authors do not maintain
  a second copy of command text or expected responses.
- An optional `preStart[].comment` or `commands[].comment` is display text emitted as `#` lines
  above that command. It is not executed or placeholder-expanded, and its text is compared literally
  on regeneration.
- Keep simple configuration readable: an empty object or one top-level scalar setting is rendered
  as compact JSON. Multiple settings or any nested object or array use pretty-printed, indented
  JSON in the displayed BOE command.
- Wrap displayed commands at argument boundaries to fit 80 characters, including continuation
  markers and indentation. Use backslash-newline continuations outside quotes; preserve indivisible
  arguments and embedded JSON lines even when they exceed the limit. Never interpret `&&` or `||`
  inside an argument as shell operators or change argument bytes to shorten a line.
- Display local commands using BOE's default setup: listener `http://localhost:10000`, admin
  `http://127.0.0.1:9901`, and the default `httpbin.org` upstream. Expand source placeholders into
  literal default URLs and configuration values. Do not expose allocated port variables,
  generator helper commands, or lifecycle scaffolding in the transcript.
- Show the foreground BOE invocation in one terminal and example commands in a second terminal
  after Envoy is ready. Keep the intentional Envoy version selection visible. Generation uses
  the real default upstream so recorded responses correspond to the displayed commands;
  upstream requests therefore require network access.
- `update-examples` replaces stale generated bodies. `check-examples` runs the same examples,
  reports meaningful differences, and exits unsuccessfully on drift without writing manifests.
- Suppress changes to declared date or duration values only when their format is unchanged.
  Keep the reviewed transcript when those are the only differences.

## Current execution boundaries

One example runs one local extension configuration in one BOE instance, with its default upstream.
`preStart` covers downloads, file creation, and local builds; explicit `sh -c` commands can express
shell variables, pipelines, and heredocs. It does not extend the BOE invocation or supervise services.
Existing examples expose these remaining gaps:

- [DNS lookup](../../../extensions/dns-gateway/manifests/lookup/manifest.yaml) starts resolver and
  lookup together with separate configurations. [Cluster router](../../../extensions/composer/cluster-router/manifest.yaml)
  uses multiple Envoy peers. These need multi-extension configuration and service orchestration.
- [Bedrock](../../../extensions/composer/bedrock-guardrails/manifest.yaml),
  [token exchange](../../../extensions/composer/token-exchange/manifest.yaml), and
  [OpenFGA](../../../extensions/composer/openfga/manifest.yaml) pass extra clusters to BOE. Decoder
  examples select an upstream host, and routing examples depend on custom routes. These need a
  way to configure the primary BOE/bootstrap options.
- Cloud/API examples contain credential placeholders in configuration. The current placeholder
  set does not support environment-backed configuration or transcript redaction. Credentials and
  reachable external APIs are also prerequisites for execution.
- [SAML](../../../extensions/composer/saml/manifest.yaml) and token exchange start Docker services.
  Pre-start steps can start containers, but there is no cleanup phase for stopping them after BOE
  or on failure. Do not migrate these examples until teardown can be guaranteed. Background child
  processes do not survive command process-group cleanup.
- SAML examples require browser login and manual inspection, which this command runner cannot
  replay. DNS interception additionally requires privileged host networking setup.
- Commands may declare a bounded HTTP-status retry to make a transient response target observable
  during capture. This retries a command only while it exits as expected and returns a valid HTTP
  response with another status; command failures, timeouts, cancellation, and unparseable output
  fail immediately. Only the matching attempt is captured, and exhaustion fails the example before
  any manifest is written. The displayed comment explains the capture retry, while the copied
  command itself runs once. Retries do not make probabilistic behavior repeatable: the
  [dynamic fault injection](../../../extensions/composer/dynamic-fault-injection/manifest.yaml)
  example captures each randomly sampled status in a separate command. This checks that both cases
  occur within the bound, not their probability. Status changes must never be normalized away.
  Its verbose timing headers use milliseconds with three decimal places. The added-delay header
  appears only when the upstream needs more delay; that presence change remains visible as drift.
- Decoder examples illustrate dynamic metadata in comments. Curl cannot observe those values,
  and current decoder logs do not expose them. A downstream observer filter or configured metadata
  access log is needed to generate that output; successful BOE logs are not captured by this tool.

## Transcript format

Generated `examples[].code` contains separate fenced `sh` command blocks and `text` output blocks.
Command blocks are interleaved with captured output in execution order. The website renders each
block separately and hides generator metadata comments; the comments remain in the manifest so
checks can compare stream and byte-format changes. Legacy code-only examples remain plain shell
snippets. Fences are longer than any backtick sequence in their contents, so command or output text
cannot close its own block.

For HTTP protocol examples, use curl's `--verbose` with `--user-agent boe-example`. The displayed
transcript retains curl's `>` request and `<` response lines. The runner removes only curl's `* `
transport diagnostics and `{ [N bytes data]` / `} [N bytes data]` markers; curl errors and other stderr remain.
Verbose HTTP response-header comparison begins only after a valid initial request line, its request
headers, and the request-header terminator. Request text, malformed or partial requests, status
lines, and response bodies remain literal.

Each captured output fence may be followed by one HTML comment with the stream (`stdout` or
`stderr`) and optional clauses, in this order: `Line endings`, `Bare CR offsets`, `Trailing
whitespace`, and `No trailing newline`. The trailing-whitespace clause contains standard Base64
encoding of a JSON array of zero-based line numbers and their trailing text. Bare carriage return
offsets preserve the distinction between a CR byte and literal `\r` text. These comments are
strictly metadata, outside the output fences; the website parser accepts only this form and never
renders arbitrary code content as HTML. Displayed output uses LF line endings, escapes bare CR
bytes, and omits trailing spaces and tabs; metadata keeps those differences significant.

## Volatile header comparison

Declare header names without a value type:

```yaml
volatileHeaders:
  - x-fault-inserted-latency
  - x-created-at
```

Header names must be valid HTTP tokens. Selection is case-insensitive; repeated declarations
select the same header. Declaration does not remove the header or accept arbitrary changes.

For each selected response-header value, independently try a validated date, then a validated
duration. Dates include supported HTTP date formats and RFC3339. Durations include bare numbers
and supported duration units. A numeric count or ID therefore becomes volatile if its header
is listed; authors must opt in only when changing magnitudes should be ignored.

The HTTP `Date` header is automatic and always uses date recognition, even when explicitly
listed. A malformed numeric `Date` value must not become a duration.

Header recognition starts only when the captured block begins with an HTTP status line or a
complete verbose request followed immediately by a response status line. It
can pass through immediate interim `100`, `102`, or `103` responses, then stops permanently
at the final response's header/body separator. A `101` upgrade is final for this purpose.
Preambles, later final responses, and HTTP-looking text inside a body remain literal; use
separate commands or explicit comparison rules when such output needs scoped normalization.

Comparison invariants:

- Both recognized values must have the same inferred kind and format signature. Switching
  between date and duration, or between a recognized and unrecognized value, produces drift.
- Unrecognized or invalid values compare exactly. Inference never makes arbitrary strings
  interchangeable.
- Magnitudes and calendar values can change. Units, spacing, signs, fractional precision,
  zero padding, date layout, and timezone notation remain significant.
- Header spelling, casing, separators, order, presence, and occurrence count remain significant.
- Header rules apply only inside captured HTTP response header sections. They never normalize
  configuration, commands, HTTP status, response body text, or other headers.
- Custom `comparison` rules retain their explicit `type` and exactly one named `value` capture.
  They match captured response text, not generated shell commands or output metadata.
- Generated output retains observed values; inference affects comparison only. Line-ending style,
  bare carriage returns, trailing whitespace, and the presence of a final newline remain significant.

## Execution and update invariants

- Argument boundaries are preserved and commands run without implicit shell evaluation.
  `expectedExit` defaults to zero. A launch failure, signal, timeout, or unexpected exit fails
  generation; an HTTP denial succeeds when the command itself returns its expected exit code.
- An optional `retry` on `commands[]` or `preStart[]` contains `httpStatus` (200–599) and
  `maxAttempts` (1–10000). It repeats the same argv and working directory within the command's
  total timeout. Only a valid HTTP status mismatch is retried; an unexpected exit, timeout,
  cancellation, or response that cannot be parsed fails immediately. Only the selected attempt's
  output is captured. Exhaustion fails the run atomically. Generated comments describe this
  capture-only retry; copied commands do not contain a retry loop.
- Retry commands invoke `curl` directly with `--disable` (or `-q`) first so user curl configuration
  cannot change the capture. Select a declared capture mode: `--verbose` parses
  only its framed stderr exchange, `--include` parses only the initial stdout response, or
  `--write-out` starts with `HTTP/%{http_version} %{http_code}` and a newline while `--output /dev/null`
  diverts the body. Header-style write-out output must terminate with a blank line. Mixed capture
  modes must agree. HTTP-shaped body text cannot select a status. Retries require one HTTP(S) URL;
  redirects, curl-owned retries, multiple transfers, config files, and unsupported curl options fail
  before execution. URL globbing requires `--globoff`; HTTPS include capture requires
  `--suppress-connect-headers` to exclude proxy CONNECT headers.
- Only `${PROXY_URL}`, `${ADMIN_URL}`, `${UPSTREAM_ADDRESS}`, and `${WORK_DIR}` are expanded in
  authored configuration and arguments. Unknown placeholders fail explicitly. Execution uses
  allocated listener/admin addresses privately; display uses the literal defaults. The upstream
  address placeholder is `httpbin.org:443`. Captured output receives the same address mapping.
- Commands run in a temporary working directory populated with the contents of the selected
  extension's `examples/` directory. Fixture symlinks are rejected. BOE configuration, extension
  cache, and runtime state are isolated from the user's installed state; the Envoy binary download
  cache can be reused across invocations.
- For fixtures, terminal two enters the extension's repository-relative `examples/` directory.
  Configuration resolves the working-directory placeholder relative to the repository root;
  command arguments resolve it to `.` in that fixture directory. No temporary directory setup
  or exported working-directory variable is shown. Without fixtures, displayed commands use
  the current directory.
- Startup and commands are bounded by timeouts. Clean up BOE, command process groups, and
  temporary directories on success or failure. Lifecycle management remains internal to the tool.
- Execute and validate every selected example before writing any manifest. A failed example
  must not leave earlier examples partially refreshed.
- Preserve YAML outside generated `code` fields, including comments and unrelated examples.
  Refuse flow-style example entries rather than silently reformatting them.
- End generated code with exactly one newline, matching YAML's literal scalar clipping.
  Serialization must not introduce drift that prevents volatile-value comparison.
- Validate the patched manifest, reject concurrent manifest edits detected before staging, and
  stage all changes before replacing targets. Preserve file permissions. Replacements are
  atomic per file; the batch is not a filesystem transaction.

## Verification

Tests must prove observable comparison boundaries, check-mode non-mutation, value-only
preservation, format-change drift, and failure before writes. Run process and loopback tests
with sufficient permissions and uncached results so skipped tests cannot masquerade as
executed integration checks. Validate the pilot examples against the pinned Envoy version,
then confirm repeated generation leaves the manifests unchanged.
