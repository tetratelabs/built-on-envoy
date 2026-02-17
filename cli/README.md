# Built On Envoy CLI (`boe`)

Command-line tool for working with Built On Envoy extensions.

## Quick Start

```bash
# Build
make build

# Run
./out/boe-$(go env GOOS)-$(go env GOARCH) --help
```

## Development

### Build

```bash
make build
```

The binary will be output to `out/boe-<os>-<arch>`.

### Test

```bash
# Unit tests
make test

# End-to-end tests (in e2e/)
make test-e2e

# Unit tests with coverage
make test-coverage
```

### Lint & Format

```bash
# Run linters
make lint

# Format code and fix license headers
make format

# Run all checks (format + verify no uncommitted changes)
make check
```

### Docker

```bash
# Build Docker image
make build_image

# Multi-platform build
make push_image
```

## Project Structure

```
cli/
├── cmd/              # CLI commands (list, run, gen-config)
├── e2e/              # End-to-end tests
├── internal/         # Internal packages
│   ├── envoy/        # Envoy runner and config generation
│   └── extensions/   # Extension manifest model and schama validation
└── main.go           # Entry point
```

## Useful Make Variables

| Variable | Description | Example |
|----------|-------------|---------|
| `GO_TEST_ARGS` | Extra args for `go test` | `make test GO_TEST_ARGS="-run TestRun -v"` |
| `TAG` | Docker image tag | `make build_image TAG=v1.0.0` |
