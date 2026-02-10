# Example Go Plugin

This directory contains an example Go plugin for Envoy HTTP filters using the Composer Dynamic Module system. It demonstrates two packaging approaches: embedding the plugin directly into the Composer binary and compiling it as a standalone Go plugin.

## Directory Structure

```
example/
├── embedded/            # Embedded packaging (compiled into Composer)
│   ├── host.go
│   └── manifest.yaml
│   └── example.go       # Core plugin implementation
│
└── standalone/          # Standalone packaging (loaded at runtime)
    ├── plugin.go
    └── manifest.yaml
    └── example.go       # Core plugin implementation
```

## Plugin Implementation

The core plugin logic is in [example.go](./embedded/example.go). It implements an HTTP filter that demonstrates:

- Reading and modifying request/response headers
- Accessing request attributes and metadata
- Modifying request and response bodies
- Sending local replies
- Working with Envoy metrics (counters, gauges, histograms)

NOTE: To avoid introduce complex dependency, and also make the standalone plugin more like a real-world example, the core plugin implementation is duplicated in both `embedded/` and `standalone/` directories. In a real project, you would typically have a shared package that both packaging approaches import.

## Packaging Approaches

### 1. Standalone Plugin (`standalone/`)

The plugin is compiled as a separate Go plugin binary (`.so` file) and loaded at runtime.

**How it works:**
- [standalone/plugin.go](standalone/plugin.go) exports `WellKnownHttpFilterConfigFactories()` as the entry point
- The Composer's [goplugin loader](../goplugin/goplugin.go) loads the `.so` file at runtime
- Before loading, the loader validates that the plugin was built with the same Go version and matching dependency versions

**Advantages:**
- Plugins can be updated independently of the Composer module
- Supports dynamic plugin discovery and loading

**Disadvantages:**
- **Critical constraint:** The plugin must be compiled with the exact same Go runtime version as the Composer
- All shared dependencies must have matching versions and checksums

**Usage:**
```bash
boe run --extension example-go
```

### 2. Embedded in Composer (`embedded/`)

The plugin is compiled directly into the Composer dynamic module binary.

**How it works:**
- [embedded/host.go](embedded/host.go) imports the plugin package and registers it with the SDK during initialization
- The Composer's [main.go](../main.go) imports the embedded package, including it in the final binary
- The plugin is registered under the name `example` (as defined in the plugin's factory map)

**Advantages:**
- Guaranteed Go runtime compatibility (plugin and host are compiled together)
- No version mismatch issues between dependencies
- Simpler deployment (single binary). The embedded plugin is available without any additional loading steps

**Disadvantages:**
- Requires rebuilding the entire Composer module to update the plugin
- All plugins must be known at compile time

**Usage:**
```bash
boe run --extension example
```

## Go Runtime Compatibility

When loading external plugins, the Go plugin system requires:

1. **Same Go version:** Plugin and host must be compiled with identical Go versions
2. **Same dependency versions:** All shared packages must have matching versions and checksums

The embedded approach eliminates these constraints by compiling everything together, making it the recommended choice when version management is a concern.

## Choosing an Approach

| Consideration | Embedded | Standalone |
|--------------|----------|------------|
| Version compatibility | Guaranteed | Must be managed |
| Update flexibility | Rebuild required | Independent updates |
| Build time | Longer (full rebuild) | Shorter (plugin only) |

For production environments where stability is critical, the **embedded** approach is recommended. The **standalone** approach is useful during development or when plugins need to be distributed and updated independently.
