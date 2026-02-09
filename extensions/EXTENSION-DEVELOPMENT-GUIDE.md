# Extension Development Guide

How to build a new Go Composer extension for Built on Envoy, including a workflow for developing with Claude.

## Table of Contents

- [Prerequisites](#prerequisites)
- [Extension Structure](#extension-structure)
- [Development Lifecycle](#development-lifecycle)
- [Phase 1: Discovery and Design](#phase-1-discovery-and-design)
- [Phase 2: Implementation](#phase-2-implementation)
- [Phase 3: Build Infrastructure](#phase-3-build-infrastructure)
- [Phase 4: Integration Testing](#phase-4-integration-testing)
- [Phase 5: Performance Optimization](#phase-5-performance-optimization)
- [Phase 6: Safety Audit](#phase-6-safety-audit)
- [Phase 7: Unit Tests](#phase-7-unit-tests)
- [Phase 8: Documentation](#phase-8-documentation)
- [Appendix: Claude Prompt Templates](#appendix-claude-prompt-templates)
- [Appendix: Common Issues](#appendix-common-issues)

## Prerequisites

- Go (version matching the Composer module - check `go.mod`)
- The `boe` CLI binary (built from `cli/`)
- Familiarity with the Envoy filter model (request/response headers and body callbacks)

## Extension Structure

Every Go extension must have:

```
extensions/your-extension/your-extension/
├── plugin.go          # Main implementation
├── plugin_test.go     # Unit tests
├── manifest.yaml      # Extension metadata (required)
├── Makefile           # Build targets
├── go.mod / go.sum    # Go module
├── README.md          # Documentation
├── buildandrun.sh     # Build + run script
├── test.sh            # Integration tests
└── rununitperf.sh     # Unit test + benchmark runner
```

### manifest.yaml (required)

```yaml
name: your-extension
version: 0.1.0
categories:
  - Transform        # or: Security, Observability, Traffic, Auth
author: Your Name
description: Short one-line description
longDescription: |
  Detailed multi-line description of what the extension does,
  its modes of operation, and key features.
type: composer
composerVersion: 0.2.1
tags:
  - relevant
  - tags
license: Apache-2.0
examples:
  - title: Basic usage
    description: How to run with default config
    code: boe run --extension your-extension
```

### Makefile (required)

```makefile
PLUGIN_NAME := your-extension
BOE_DATA_HOME ?= $(HOME)/.local/share/boe

.PHONY: build
build:
	go build -buildmode=plugin -o $(PLUGIN_NAME).so .

.PHONY: install
install: build
	@version=$$(grep "version:" manifest.yaml | awk '{print $$2}'); \
	mkdir -p $(BOE_DATA_HOME)/extensions/goplugin/$(PLUGIN_NAME)/$$version; \
	cp $(PLUGIN_NAME).so $(BOE_DATA_HOME)/extensions/goplugin/$(PLUGIN_NAME)/$$version/plugin.so;

.PHONY: clean
clean:
	rm -f $(PLUGIN_NAME).so
```

### plugin.go Skeleton

```go
package main

import (
    "github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
)

// Your filter struct
type myFilter struct {
    shared.EmptyHttpFilter
    handle shared.HttpFilterHandle
    config *myConfig
}

// Request callbacks
func (f *myFilter) OnRequestHeaders(headers shared.HeaderMap, endStream bool) shared.HeadersStatus {
    // Your logic here
    return shared.HeadersStatusContinue
}

func (f *myFilter) OnRequestBody(body shared.BodyBuffer, endStream bool) shared.BodyStatus {
    return shared.BodyStatusContinue
}

// Response callbacks
func (f *myFilter) OnResponseHeaders(headers shared.HeaderMap, endStream bool) shared.HeadersStatus {
    return shared.HeadersStatusContinue
}

func (f *myFilter) OnResponseBody(body shared.BodyBuffer, endStream bool) shared.BodyStatus {
    return shared.BodyStatusContinue
}

// Factory pattern - creates filter instances per-request
type myFilterFactory struct {
    config *myConfig
}

func (fac *myFilterFactory) Create(handle shared.HttpFilterHandle) shared.HttpFilter {
    return &myFilter{handle: handle, config: fac.config}
}

// Config factory - creates filter factory from JSON config
type myConfigFactory struct {
    shared.EmptyHttpFilterConfigFactory
}

func (fac *myConfigFactory) Create(handle shared.HttpFilterConfigHandle, config []byte) (shared.HttpFilterFactory, error) {
    // Parse config, return factory
    return &myFilterFactory{config: parsedConfig}, nil
}

// Entry point - Composer looks up this function
func WellKnownHttpFilterConfigFactories() map[string]shared.HttpFilterConfigFactory {
    return map[string]shared.HttpFilterConfigFactory{
        "your-extension": &myConfigFactory{},
    }
}
```

### Body Buffering Pattern

If your extension needs to read or modify the full request/response body:

```go
func (f *myFilter) OnRequestHeaders(headers shared.HeaderMap, endStream bool) shared.HeadersStatus {
    if needsTransformation(headers) {
        if endStream {
            return shared.HeadersStatusContinue  // no body to buffer
        }
        return shared.HeadersStatusStop  // tell Envoy to buffer
    }
    return shared.HeadersStatusContinue
}

func (f *myFilter) OnRequestBody(body shared.BodyBuffer, endStream bool) shared.BodyStatus {
    if !endStream {
        return shared.BodyStatusStopAndBuffer  // keep buffering
    }
    // endStream == true: full body is available
    rawBody := collectBody(f.handle.BufferedRequestBody(), body)

    // Transform the body...
    newBody := transform(rawBody)

    // Replace
    buffered := f.handle.BufferedRequestBody()
    if buffered != nil {
        buffered.Drain(buffered.GetSize())
    }
    buffered.Append(newBody)
    return shared.BodyStatusContinue
}
```

## Development Lifecycle

The recommended order for building an extension:

```
1. Discovery   -->  Understand existing patterns and SDK
2. Design      -->  Define approach, get alignment
3. Implement   -->  Write the plugin code
4. Build Infra -->  Makefile, buildandrun.sh, manifest
5. Test (E2E)  -->  Integration test script
6. Optimize    -->  Performance review
7. Safety      -->  Memory safety audit
8. Unit Tests  -->  Comprehensive test coverage
9. Document    -->  README, examples
```

Each phase below includes the Claude prompt templates that drive it.

---

## Phase 1: Discovery and Design

**Goal**: Understand the codebase, identify patterns, and design the extension before writing code.

**Why this matters**: Jumping to code without understanding the SDK patterns leads to rewrites. The Envoy filter model has specific callback semantics (headers vs body, buffering, status codes) that must be followed.

### What to do

1. Have Claude read the existing extension examples and SDK
2. Describe your extension's requirements
3. Review the design before proceeding

### Key references to read

- `extensions/example-go/example.go` - Complete filter example
- `extensions/example-go/standalone/plugin.go` - Standalone plugin entry point
- `extensions/internal/goplugin/goplugin.go` - How plugins are loaded
- `extensions/example-go/README.md` - Packaging approaches

### Claude prompt

> Read through the following folders to understand the extension development patterns:
> - `/path/to/built-on-envoy/extensions/example-go/`
> - `/path/to/built-on-envoy/extensions/internal/goplugin/`
> - `/path/to/built-on-envoy/extensions/soap-rest/soap-rest/` (as a reference)
>
> I want to create a new Go extension called `[your-extension-name]` in
> `/path/to/built-on-envoy/extensions/[your-extension-name]/[your-extension-name]/`.
>
> This extension should: [describe what it does in detail]
>
> Analyze and let me know:
> 1. Your proposed approach and architecture
> 2. Key design decisions and trade-offs
> 3. Any questions before you start
>
> **Do not generate any code until I confirm.**

### What to review in Claude's response

- Does it understand the Composer factory pattern?
- Does it correctly use `HeadersStatusStop` / `BodyStatusStopAndBuffer` for body buffering?
- Are the configuration structures sensible?
- Is the approach the simplest that solves the problem?

---

## Phase 2: Implementation

**Goal**: Generate the main plugin code.

### Claude prompt

> I've confirmed the design. Proceed with generating the code.
>
> Create the following files:
> 1. `plugin.go` - Full implementation
> 2. `manifest.yaml` - Extension metadata
>
> Follow the patterns from `example-go` for the factory pattern and entry point.
> Use only Go standard library (no external dependencies beyond the Envoy SDK).

### What to verify

- Build succeeds: `go build -buildmode=plugin -o your-extension.so .`
- Entry point function name is exactly `WellKnownHttpFilterConfigFactories`
- Filter struct embeds `shared.EmptyHttpFilter`
- Config parsing handles empty/missing config gracefully

---

## Phase 3: Build Infrastructure

**Goal**: Create scripts for building, installing, and running the extension.

### Claude prompt

> Create the build and run infrastructure:
>
> 1. A `Makefile` with `build`, `install`, and `clean` targets (follow the pattern from the soap-rest extension)
> 2. A `buildandrun.sh` script that builds, installs, and runs with a test configuration
> 3. Make sure `BOE_BIN` is configurable and auto-detected from the project root

### Verify

```bash
make build          # Should produce .so file
make install        # Should copy to boe data directory
bash buildandrun.sh # Should start boe with the extension
```

---

## Phase 4: Integration Testing

**Goal**: Create end-to-end tests that exercise the running extension.

### Claude prompt

> Create a `test.sh` script with integration tests for the extension.
>
> Include:
> - A connectivity check before running tests
> - Colored output (pass/fail) with test numbering
> - Support for `-v` verbose mode and `-t N` to run specific tests
> - Tests for: happy path, error cases, edge cases, passthrough
>
> The extension is running on `http://localhost:10000` with httpbin.org as upstream.

### Running tests

```bash
# Terminal 1
bash buildandrun.sh

# Terminal 2
bash test.sh        # Run all tests
bash test.sh -v     # Verbose output
bash test.sh -t 3   # Run only test 3
```

---

## Phase 5: Performance Optimization

**Goal**: Review and optimize hot paths for production readiness.

### Claude prompt

> Check the code for any performance optimizations. Make sure the code is optimized for:
>
> 1. Minimal allocations in the request/response path
> 2. Pre-computation at config load time vs per-request
> 3. Buffer pre-sizing where sizes are known
> 4. Avoiding fmt.Sprintf in hot paths
> 5. Singleton patterns for reusable objects
>
> After making changes, verify the build still succeeds and all tests pass.

### Common optimizations

| Pattern | Instead of | Use |
|---------|-----------|-----|
| String building | `fmt.Sprintf` | `bytes.Buffer` with `WriteString`/`WriteByte` |
| Buffer sizing | Default growth | `buf.Grow(estimatedSize)` |
| Numeric formatting | `fmt.Sprintf("%f", v)` | `strconv.FormatFloat(v, 'g', -1, 64)` |
| String search | `strings.Split` then index | `strings.LastIndexByte` |
| Reusable objects | Create per call | Package-level singleton |
| Config processing | Per-request parsing | Pre-compute at config load |

---

## Phase 6: Safety Audit

**Goal**: Find and fix memory safety issues, nil dereferences, and data integrity bugs.

### Claude prompt

> Check the code for any memory corruption or safety issues. Specifically look for:
>
> 1. Nil pointer dereferences (especially on interface types and optional fields)
> 2. Index out of bounds (slice/string access without length checks)
> 3. Unescaped user input in structured output (JSON, XML, HTML)
> 4. Race conditions (shared state across goroutines)
> 5. Resource leaks (unclosed readers, buffers)
> 6. Operator precedence bugs
>
> Fix any issues found and explain what each fix prevents.

### Common issues found in practice

| Issue | Example | Fix |
|-------|---------|-----|
| Nil dereference | `buffer.Append()` when buffer is nil | Guard with nil check + early return |
| JSON injection | Writing user strings directly into JSON | Use `json.Marshal()` for escaping |
| Operator precedence | `len(s) > 0 && s[0] == '@' \|\| s == "#"` | Add parentheses for clarity |
| Panic on empty | `path[idx+1:]` when `idx == len(path)-1` | Bounds check before slice |

---

## Phase 7: Unit Tests

**Goal**: Comprehensive unit test coverage for all pure utility functions.

### Claude prompt

> Add unit tests for the code. Create `plugin_test.go` with:
>
> 1. Tests for all pure utility functions (parsing, building, config helpers)
> 2. Edge cases: empty input, nil values, special characters, deeply nested structures
> 3. Roundtrip tests (input -> transform -> verify -> reverse transform -> compare)
> 4. Benchmark tests for hot-path functions
> 5. Tests that verify the safety fixes (e.g., JSON escaping produces valid JSON)
>
> Note: Functions that depend on the Envoy SDK handle cannot be unit tested without
> mocking. Focus on pure functions that can be tested directly.
>
> Also create a `rununitperf.sh` script that runs unit tests and benchmarks with
> colored output. Support `--unit`, `--bench`, and `--cover` flags.

### Running tests

```bash
go test -v ./...                # Unit tests
go test -bench=. -benchmem ./...  # Benchmarks
bash rununitperf.sh             # All-in-one
bash rununitperf.sh --cover     # With coverage report
```

### What makes good tests

- Test the **contract**, not the implementation
- Include **negative tests** (invalid input, error paths)
- **Roundtrip tests** catch subtle encoding/decoding bugs
- **Benchmarks** establish a baseline for regression detection
- **Edge cases**: empty strings, nil maps, unicode, deeply nested structures

---

## Phase 8: Documentation

**Goal**: Create README and examples for the extension.

### Claude prompt

> Create a README.md for the extension that includes:
>
> 1. Overview and what it does
> 2. Quick start (build, run, example requests)
> 3. Full configuration reference with all fields documented
> 4. Architecture section with flow diagrams (ASCII)
> 5. Testing instructions (unit tests, integration tests, benchmarks)
> 6. Performance data (benchmark results table)
>
> Follow the style of the existing example-go README.

---

## Appendix: Claude Prompt Templates

### The Complete Sequence

Here is the full sequence of prompts used to build the soap-rest extension. Adapt these for your extension:

#### Prompt 1: Discovery (always start here)

```
Read through the following folders:
- /path/to/built-on-envoy/extensions/example-go/
- /path/to/built-on-envoy/extensions/internal/goplugin/
- /path/to/built-on-envoy/cli/

I want to create a new Go extension called [NAME] in
/path/to/built-on-envoy/extensions/[NAME]/[NAME]/.

This extension should [DESCRIBE FUNCTIONALITY].

Analyze and let me know if you have any questions.
Do not generate any code until I confirm. Explain your thinking and approach.
```

#### Prompt 2: Confirm design decisions

Review Claude's analysis. If there are design choices, pick them explicitly:

```
Go with [OPTION]. [Any additional requirements or constraints.]
```

#### Prompt 3: Generate code

```
Proceed with code generation. Create plugin.go and manifest.yaml.
```

#### Prompt 4: Build infrastructure

```
Create a buildandrun.sh and make sure the Makefile has build/install/clean targets.
```

#### Prompt 5: Integration tests

```
Create a test.sh script with integration test cases and validations.
```

#### Prompt 6: Performance optimization

```
Check the code for any optimization. Make sure the code is optimized for performance.
```

#### Prompt 7: Safety audit + unit tests

```
Check for any memory corruption issues that can happen. Also add unit tests for the code.
```

#### Prompt 8: Documentation

```
Create all required README files and a development guide that others can follow.
```

### Tips for Working with Claude

1. **Don't skip the design phase.** Having Claude explain its approach before writing code catches misunderstandings early and avoids rewrites.

2. **Confirm design decisions explicitly.** When Claude presents options, pick one clearly. Ambiguity leads to assumptions.

3. **Build and test after each phase.** Run `make build` after code generation, run `test.sh` after creating tests. Fix issues before moving on.

4. **The optimization and safety phases catch real bugs.** In the soap-rest extension, the safety audit found a nil-pointer dereference and a JSON injection vulnerability. Don't skip these phases.

5. **Ask Claude to read existing code first.** The more context Claude has about the codebase patterns, the more consistent its output will be.

---

## Appendix: Common Issues

### `boe: error: failed to unmarshal config JSON string`

The `boe` CLI uses Kong for flag parsing. If your JSON config contains commas, Kong may split the value. The fix is to add `sep:"none"` to the Configs field in `cli/cmd/run.go` and `cli/cmd/config.go`. This fix has already been applied to the codebase.

### `boe: not found`

The `boe` binary isn't in your PATH. Either:
- Add `cli/out/` to your PATH
- Set `BOE_BIN=/path/to/built-on-envoy/cli/out/boe` before running scripts
- The `buildandrun.sh` script auto-detects from the project root

### `cannot bind: Address already in use`

A previous boe/envoy instance is still running:

```bash
pkill -f envoy
sleep 1
bash buildandrun.sh
```

### Plugin won't load: Go version mismatch

The plugin `.so` must be compiled with the exact same Go version as the Composer module. Check:

```bash
go version                    # Your Go version
grep "^go " go.mod            # Required version
```

### Tests fail with 404

If your integration tests hit httpbin endpoints that don't exist, that's expected behavior. The test validates that the extension processed and forwarded the request correctly. Accept 404 as a valid response when the transformation itself succeeded.

### Unit tests can't import the Envoy SDK

The `shared` package uses CGO bindings that may not compile in standard `go test` mode on all platforms. Focus unit tests on pure utility functions (parsing, building, config) that don't depend on the SDK. The SDK-dependent code (filter callbacks) is tested via integration tests.
