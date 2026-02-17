# CLAUDE.md - Built On Envoy

## Project Overview

Built On Envoy is a community-driven marketplace for Envoy Proxy extensions. It provides a CLI tool (`boe`) for discovering, running, and building extensions, plus a catalog of reusable extensions.

**Repository:** `github.com/tetratelabs/built-on-envoy`
**License:** Apache-2.0

## Repository Structure

```
├── cli/                    # CLI tool (Go, module: github.com/tetratelabs/built-on-envoy/cli)
│   ├── cmd/                # CLI commands (list, run, gen-config, create)
│   ├── e2e/                # End-to-end tests
│   ├── internal/
│   │   ├── envoy/          # Envoy runner and config generation
│   │   ├── extensions/     # Extension manifest, downloading, building
│   │   ├── oci/            # OCI registry client
│   │   ├── testing/        # Test helpers
│   │   └── xdg/            # XDG directory handling
│   ├── tools/              # Go tool dependencies (linters, generators)
│   └── Makefile
├── extensions/
│   ├── composer/           # Go dynamic module extension set (type: composer)
│   │   ├── main/           # Composer loader
│   │   ├── goplugin/       # Go plugin loading mechanism
│   │   ├── jwe-decrypt/    # JWE decryption plugin
│   │   ├── waf/            # Web Application Firewall plugin (Coraza)
│   │   ├── example/        # Example plugin template
│   │   └── Makefile
│   ├── ip-restriction/     # Rust dynamic module (type: dynamic_module)
│   └── example-lua/        # Lua HTTP filter examples (type: lua)
├── website/                # Astro static site (marketplace docs)
├── Cargo.toml              # Rust workspace (members: extensions/ip-restriction)
├── .golangci.yml           # Linter config
└── .licenserc.yaml         # License header enforcement
```

## Building

### CLI (Go)

All CLI make targets run from `cli/`:

```bash
cd cli

make build                  # Build binary → out/boe-<os>-<arch>
make build GOOS_LIST="linux darwin" GOARCH_LIST="amd64 arm64"  # Cross-compile

make clean                  # Remove build artifacts
```

Build output binary: `cli/out/boe-$(go env GOOS)-$(go env GOARCH)`

### Composer Extension (Go)

```bash
cd extensions/composer

make build                  # Build shared library → libcomposer.so
make install                # Build and install to ~/.local/share/boe/extensions/
make build_plugins          # Build all sub-plugins
make install_plugins        # Install all sub-plugins
```

### IP Restriction Extension (Rust)

Built via the root Cargo workspace:

```bash
cargo build                 # Debug build
cargo build --release       # Release build
```

### Website

```bash
cd website

npm install
npm run dev                 # Dev server
npm run build               # Production build
npm run preview             # Preview production build
```

### Docker

```bash
cd cli

make docker DOCKER_BUILD_ARGS="--load"                          # Single-platform
make docker ENABLE_MULTI_PLATFORMS=true DOCKER_BUILD_ARGS="--load"  # Multi-platform
make docker TAG=v1.0.0                                          # Custom tag
```

Image: `docker.io/tetratelabs/built-on-envoy-cli:<tag>`

## Testing

### CLI Tests

```bash
cd cli

make test                   # Unit tests (excludes e2e/)
make test-integration       # Unit + integration tests (requires Docker, uses -tags integration)
make test-e2e               # End-to-end tests (30min timeout, builds composer first)
make test-coverage          # Unit + integration tests with coverage

# Run a specific test:
make test GO_TEST_ARGS="-run TestName -v"
```

### Composer Extension Tests

```bash
cd extensions/composer
make test
```

### Rust Extension Tests

```bash
cargo test                  # From repo root
```

## Code Quality

```bash
cd cli

make lint                   # golangci-lint (cli + tools)
make format                 # Format Go code (gci, gofumpt) + fix license headers
make tidy                   # go mod tidy on all modules
make check                  # gen + tidy + format, then verify no uncommitted changes (CI gate)
```

### Go Import Order (gci)

Imports must follow this section order:
1. Standard library
2. Third-party packages
3. `github.com/tetratelabs/built-on-envoy` packages

### Code Generation

```bash
cd cli

make gen                    # All generation (code + docs)
make gen-code               # go generate ./...
make gen-docs               # CLI docs + manifest reference for the website
```

## `boe` CLI Commands

### `boe list`
List available extensions from the built-in catalog.

### `boe run`
Run Envoy with specified extensions.

```bash
boe run --extension example-go          # Run a marketplace extension
boe run --extension waf:0.2.1           # Pin to specific version
boe run --local ./my-extension          # Run a local extension
boe run --extension waf --config '{"rules":["..."]}'  # With JSON config
```

Key flags:
- `--extension <name[:version]>` - Extensions to enable (repeatable)
- `--local <path>` - Local extension directory (repeatable)
- `--config <json>` - JSON config for extensions (applied in order)
- `--envoy-version <version>` - Envoy version (e.g. 1.31.0)
- `--log-level <component:level,...>` - Log levels (default: `all:error`)
- `--listen-port <port>` - Listener port (default: 10000)
- `--admin-port <port>` - Admin port (default: 9901)
- `--run-id <id>` - Run identifier (default: timestamp-based)
- `--registry <url>` - OCI registry (default: `ghcr.io/tetratelabs/built-on-envoy`)
- `--insecure` - Allow HTTP registry
- `--username` / `--password` - Registry auth

### `boe gen-config`
Generate Envoy configuration YAML without running Envoy.

```bash
boe gen-config --extension example-go > envoy.yaml
boe gen-config --extension waf --minimal    # Only extension resources
```

Same extension/config/OCI flags as `run`, plus:
- `--minimal` - Only extension-generated resources (HTTP filters and clusters)

### `boe create`
Scaffold a new extension from a template.

```bash
boe create my-extension                     # Creates my-extension/ with composer template
boe create --type composer my-extension     # Explicit type (composer is default/only option)
boe create --path /some/dir my-extension    # Custom output directory
```

### Global Flags

| Flag | Env Variable | Default | Description |
|------|-------------|---------|-------------|
| `--config-home` | `BOE_CONFIG_HOME` | `~/.config/boe` | Configuration directory |
| `--data-home` | `BOE_DATA_HOME` | `~/.local/share/boe` | Data directory (extensions, binaries) |
| `--state-home` | `BOE_STATE_HOME` | `~/.local/state/boe` | State and logs directory |
| `--runtime-dir` | `BOE_RUNTIME_DIR` | `/tmp/boe-$UID` | Ephemeral runtime directory |

## Architecture

### Extension Types

| Type | Description | Language |
|------|-------------|----------|
| `lua` | Envoy built-in Lua HTTP filter | Lua |
| `dynamic_module` | Native shared library loaded by Envoy | Rust, C++ |
| `composer` | Go plugin bundle loaded via a dynamic module | Go |

### Extension Lifecycle

1. Extensions are defined by a `manifest.yaml` in their directory
2. Published as OCI artifacts to `ghcr.io/tetratelabs/built-on-envoy`
3. `boe run` downloads extensions from the registry (or builds local ones), generates Envoy config, and starts Envoy via the func-e library
4. Composer extensions require the `libcomposer` dynamic module loader, which is auto-downloaded/built

### Key Dependencies

- **Kong** (`github.com/alecthomas/kong`) - CLI framework
- **func-e** (`github.com/tetratelabs/func-e`) - Envoy binary management and execution
- **oras-go** (`oras.land/oras-go/v2`) - OCI artifact distribution
- **go-control-plane** - Envoy protobuf definitions for config generation
- **Envoy Rust SDK** - For Rust dynamic module extensions

### Go Version

Go 1.25.7 (specified in `cli/go.mod`).

## CI/CD

GitHub Actions workflows in `.github/workflows/`:

- `cli.yaml` - CLI checks, build, test, coverage, e2e, lint
- `extensions.yaml` / `extensions-go.yaml` / `extensions-rust.yaml` - Extension tests
- `release.yaml` / `release-composer.yaml` / `release-extensions.yaml` / `release-lua.yaml` / `release-rust.yaml` - Release pipelines
- `website.yaml` - Website build and deploy

### E2E Test Environment Variables

- `TEST_BOE_REGISTRY` - Registry URL
- `TEST_BOE_REGISTRY_USERNAME` - Registry username
- `TEST_BOE_REGISTRY_PASSWORD` - Registry password

## Quick Reference

| Task | Command |
|------|---------|
| Build CLI | `cd cli && make build` |
| Run CLI tests | `cd cli && make test` |
| Run all checks (pre-commit) | `cd cli && make check` |
| Lint | `cd cli && make lint` |
| Format code | `cd cli && make format` |
| Build composer | `cd extensions/composer && make build` |
| Build Rust extension | `cargo build` (from repo root) |
| Run website dev server | `cd website && npm run dev` |
