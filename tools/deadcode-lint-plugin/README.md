# FFI-aware dead-code linter

This directory keeps the scanner and its golangci plugin in the existing tools
module so they share the repository's pinned Go toolchain and dependencies.
`scanner/scanner.go` performs actual whole-program analysis using the SSA and
Rapid Type Analysis (RTA) libraries that power `golang.org/x/tools/cmd/deadcode`.
`runtime-plugin/main.go` reports findings through the unmodified golangci-lint
v2.13.2 binary.

The stock deadcode command does not expose a library entrypoint or a flag for
additional roots. Reusing its public analysis libraries lets this scanner supply
real SDK functions as roots without duplicating the SDK's callback dispatch.

## How analysis works

1. Each configured target is loaded separately with `go/packages.Load`, full
   dependency syntax, the selected build environment and tags, and `Tests: false`.
   Reporting patterns include implementation packages that no runtime package
   imports.
2. `ssautil.AllPackages` builds SSA, including dependency bodies and instantiated
   generic functions.
3. Executable `main` functions and their initializers are roots. Explicit
   `entry-packages` supply registration initializers, while `entry-symbols` supply
   package functions exported through mechanisms without source FFI directives,
   such as Go plugin symbol lookup.
4. The scanner discovers every declaration-attached `//export` and
   `//go:wasmexport` function in those entry packages and their transitive imports.
   Their package initializers are also roots. `ffi-packages` adds SDK ABI libraries
   linked by the host even when the extension's registration wrapper does not
   import them. The Envoy ABI currently supplies 31 C exports; no list of callback
   names is embedded in the scanner.
5. RTA follows calls, interface dispatch, and function values from those roots.
   The actual SDK code retains lifecycle callbacks, schedulers, callouts, HTTP
   streams, and watermarks. Reachable generic instantiations retain their source
   declaration.
6. Unreachable declarations are reported only within configured reporting
   packages. Generated files and empty private interface marker methods are
   excluded. Dependencies contribute reachability without receiving diagnostics.

Outbound `//go:wasmimport` declarations are not entrypoints. Unimported reporting
packages are inspected without rooting their initializers or FFI exports:
disconnected code cannot make other disconnected code appear live. Malformed
exports, unresolved explicit roots, and loading/type-checking failures are errors.
Only the analysis executes; extension and SDK functions are never called.

## Runtime golangci plugin

From the repository root:

```sh
make -C extensions/composer format lint
```

Composer's `lint` target builds the runtime plugin, merges the shared root
`.golangci.yml` with `extensions/composer/golangci-plugin.yml`, and runs golangci
once with the generated configuration. The shared configuration supplies the
ordinary linters; the overlay enables `ffideadcode`, Composer discovery, and
production build tags. Both the generated configuration and the plugin live in
this directory's ignored `out/` directory. The overlay uses `gitroot` path mode
to preserve the shared configuration's path rules and resolve the plugin path
from the repository root. Findings fail the normal lint command.

The plugin is a Make file target and is rebuilt only when its production Go
sources, source directories, tools module files, or build recipe change. Repeated
lint calls reuse the compiled `.so`; the dead-code scan still runs each time.

The root configuration remains free of custom plugin settings because the pinned
golangci loads custom plugins even when disabled. Other modules' lint and format
commands keep using that shared configuration without building or scanning this
plugin. Composer no longer needs the old `lint-deadcode` loop or synthetic
`analysis-main` executable.

GitHub Actions' Go extensions job runs the plugin checks and the existing
Composer `make format lint` command. Changes to this tool, the tools module,
linter configuration, and relevant workflows trigger extension checks.

For the plugin's own checks and an isolated smoke test:

```sh
make -C tools/deadcode-lint-plugin format check
make -C tools/deadcode-lint-plugin runtime-plugin-check
```

The second command builds `out/ffideadcode.so` and loads it through `type: goplugin`
to discover and scan the Composer example. Its isolated configuration is
`runtime-plugin/.golangci.yml`; `include: [example]` limits this smoke check and
the example should pass with no findings.
Its diagnostics stay confined to the discovered extensions while ordinary
linters continue handling the rest of the Composer module.

To exclude a deliberately retained function from `ffideadcode` findings in
golangci-lint, place a `//nolint:ffideadcode` comment immediately above its
declaration and explain why the function remains:

```go
//nolint:ffideadcode // Retained as a helper for tests.
func helper() {}
```

This suppresses that function's diagnostic; it does not make its callees reachable.
Tests do not supply entrypoints, so production functions used only by tests are
reported.

The [runtime plugin integration test](runtime-plugin/main_test.go) verifies this exclusion
with cached runs and verifies that removing the comment restores the finding.

## Extension discovery

Use one Composer profile instead of maintaining a target for each extension:

```yaml
settings:
  composer:
    directory: ../../extensions/composer
    build-tags:
      - coraza.rule.case_sensitive_args_keys
      - coraza.rule.no_regex_multiline
      - coraza.rule.mandatory_rule_id_check
```

Discovery selects immediate child directories with a `manifest.yaml` declaring
`type: go`. For each extension it derives the reporting subtree, registration
package, standalone factory symbol when present, and shared Envoy ABI package.
It creates a separate analysis target for each directory, including extensions
not registered in Composer's aggregate `plugins.go`.

Registration packages normally live in `embedded/`; the built-in Go plugin loader
registers in its own package instead. Discovery checks selected Go files under
the configured build tags and environment rather than looking for a particular
filename. Existing registration files are named `embedded/host.go`, not `main.go`.
The registration initializer must directly call the SDK's factory registration
function, and the extension must declare `WellKnownHttpFilterConfigFactories`.
Malformed manifests, missing entrypoints, and an empty selection are errors.
Directories without a manifest and non-Go extensions are skipped.

The profile accepts `directory`, `build-tags`, `env`, and an optional `include`
list of directory names. Omitting `include` discovers all Go extensions; an
explicitly empty list selects none and is an error. To scan all filters, omit
that setting and invoke golangci with `./...` from the Composer
module. Its build tags and environment must match the discovery profile.

Explicit targets remain available for layouts that do not follow these
conventions. For example, an individual filter has this configuration:

```yaml
settings:
  targets:
    - directory: ../../extensions/composer
      packages: [./saml/...]
      entry-packages: [./saml/embedded]
      ffi-packages: [github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/abi]
      entry-symbols:
        - package: ./saml/standalone
          name: WellKnownHttpFilterConfigFactories
```

Configure either discovery or explicit targets. Each `targets` item accepts:

| Setting | Purpose |
| --- | --- |
| `directory` | Module directory, relative to the invoking working directory or absolute. |
| `packages` | Reporting patterns, defaulting to `./...`. |
| `entry-packages` | Packages whose initialization registers the extension. |
| `ffi-packages` | Additional host-linked ABI packages and their imported dependencies. |
| `entry-symbols` | Entries containing `package` and `name` for exported package functions. |
| `build-tags` | Tags used to load and analyze the target. |
| `env` | Go environment overrides, such as `GOOS` and `GOARCH`. |
| `disable-ffi` | Executable-only roots, for comparison with stock root selection. |

Use separate targets for separate extension programs. The golangci invocation
must include every reporting package and use compatible build tags and platform
settings. The example configuration carries Composer's production build tags in
both scanner and golangci settings. The plugin path follows golangci's
`run.relative-path-mode`, which defaults to the configuration directory; target
directories are relative to the invoking working directory.

Golangci runs analyzers and caches issues per package, but reachability also
depends on callers in other packages. The plugin scans in its `New` constructor
before golangci consults the issue cache, then uses immutable findings in its
per-package reporter. A second, empty analyzer has a name containing a digest of
the complete findings, settings, and scanner version. The pinned golangci's cache
includes analyzer names, so changed reachability invalidates cached diagnostics
even when the affected declaration's package did not change. This deliberately
depends on v2.13.2's cache behavior and can invalidate other combined analyzer
results. The whole-program scan runs on every invocation.

No custom golangci executable is required. Go runtime plugins require CGO and a
supported platform, such as Linux or macOS. Build the plugin and golangci tool
with matching Go versions, shared dependencies, and build settings. These commands
use the pinned tools module; an arbitrary downloaded golangci binary may be
incompatible. The module-plugin mechanism would require a custom executable.

## Verification and limits

The real Envoy SDK fixture checks all supported callback groups and reports
exactly four disconnected declarations: an exported test-only function, an
unused helper, an unimported package function, and an unregistered filter method.
Additional scanner tests cover imported and explicitly configured ABI libraries,
Wasm exports with different host symbol names, outbound imports, initializers,
generic declarations, marker methods, and explicit root failures.

The golangci integration test builds and loads the actual `.so`, discovers a real
SDK fixture, repeats the scan with the same cache, verifies adding and removing
a preceding `nolint` comment, and discovers a second extension without changing
configuration. Calls from that extension cannot hide functions dead in the first.
Removing its manifest removes its findings. The test also changes only a caller
package to make a callee live, restores the caller to make it dead again, and
checks that broken source fails explicitly.

Discovery currently combines each extension's embedded and standalone entrypoints;
this checks whether a function is live in either supported packaging mode rather
than requiring it to be live in both. The normal lint invocation includes the
complete Composer reporting scope. RTA is conservative,
especially around reflection, and cannot prove every retained function has a
production caller.
