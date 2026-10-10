# FFI-aware dead-code linter

`ffideadcode` checks each Composer Go extension for functions unreachable from
its runtime entrypoints, including the imported SDK's FFI exports. It uses the
SSA and Rapid Type Analysis libraries behind `golang.org/x/tools/cmd/deadcode`
and runs through the repository's pinned golangci-lint binary without rebuilding
that executable.

## Run

From the repository root:

```sh
make -C extensions/composer format lint
```

The [root configuration](../../.golangci.yml) leaves `ffideadcode` disabled.
Composer's `lint` target checks the plugin and enables it alongside ordinary
linters. Findings fail lint. Go lint targets ensure `out/ffideadcode.so` exists.
The plugin is rebuilt only when its sources, tools module dependencies, or build recipe change;
analysis runs on every Composer lint invocation. The plugin is ignored in `out/`.

The pinned golangci version loads configured plugins even when disabled. A
`module` setting limits scanning to the matching Go module and skips discovery
elsewhere, including CLI and UI checks. GitHub Actions uses Composer's normal
lint target; plugin, dependency, and configuration changes trigger extension checks.

For plugin development:

```sh
make -C tools/deadcode-lint-plugin format check
make -C tools/deadcode-lint-plugin runtime-plugin-check
```

The second command loads the real plugin and scans the Composer example.
`GO_TEST_ARGS` forwards optional arguments to the plugin's test target.

## Analysis and configuration

Discovery selects immediate child directories whose `manifest.yaml` declares
`type: go`. Each extension is analyzed independently, including implementation
packages that are not imported. A caller in another extension cannot hide dead
code in the extension being checked. Other Composer code receives ordinary lint
checks.

Roots include executable entrypoints, registration initializers, standalone
factory exports, and SDK functions marked `//export` or `//go:wasmexport`.
The scanner follows the SDK's actual callback dispatch rather than maintaining
a callback list. Dependencies contribute reachability but receive no findings.
Tests are excluded; functions used only by tests are reported. Generated code
and empty private interface marker methods are skipped.

The Composer profile accepts `directory`, `build-tags`, `env`, and optional
`include` extension names. Omit `include` to scan all Go extensions; an empty
selection is an error. Registration normally lives in `embedded/` and must call
`sdk.RegisterHttpFilterConfigFactories` from `init`. The extension must declare
`WellKnownHttpFilterConfigFactories`; standalone adapters are included when present.

For other layouts, use `settings.targets` with the fields defined by
[`scanner.Target`](scanner/scanner.go). Configure either Composer discovery or
explicit targets. With `settings.module`, target directories are relative to that
module's root; otherwise they are relative to the invoking working directory.
The nearest `go.mod` determines the active module, including from subdirectories;
nested modules have their own scope. The plugin path follows golangci's
`run.relative-path-mode`.
Run golangci over all reporting packages with the same build tags and environment
as the scanner. Composer lint reads both sets of tags from the root configuration,
independently of full/lite packaging flags. Invoke it from the Composer module,
as the Make target does; the repository root is a different Go module.

## Exclude a finding

Place a comment immediately above a deliberately retained function:

```go
//nolint:ffideadcode // Retained as a helper for tests.
func helper() {}
```

This suppresses the function's diagnostic without making its callees reachable.

## Requirements and limits

- Go runtime plugins require CGO and a supported platform, such as Linux or macOS.
  Build with the pinned tools module so the plugin and golangci share Go versions,
  dependencies, and build settings.
- Embedded and standalone roots are combined: a function is live if either
  packaging mode reaches it. RTA is conservative, especially around reflection,
  so some unused functions may remain unreported.
- Invalid configuration, missing entrypoints, and package loading errors fail
  explicitly. Analysis does not execute extension code.
- Whole-program findings participate in golangci's cache key, so changes in a
  caller invalidate affected diagnostics. This depends on the pinned golangci
  version's cache behavior.

Tests cover SDK callbacks, disconnected packages, test-only functions,
suppression comments, independent extension scans, and cache invalidation through
the [real golangci plugin](runtime-plugin/main_test.go).
