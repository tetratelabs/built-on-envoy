# Go Plugin Loader

This package implements a Dynamic Module for Envoy that can load standalone Go plugins at runtime.

## Overview

The Go Plugin Loader is a built-in Envoy Dynamic Module that enables loading external Go plugins without recompiling the main binary. It uses Go's native [plugin](https://pkg.go.dev/plugin) package to dynamically load shared object files (`.so`) that implement HTTP filters.

## How It Works

1. The loader is registered as a well-known HTTP filter config factory named `goplugin`
2. When Envoy loads the configuration, it parses the `GoPlugin` protobuf message containing:
   - `name`: The plugin name to load
   - `url`: The location of the plugin binary (currently supports `file://` URLs)
   - `config`: The configuration to pass to the loaded plugin
3. Before loading a plugin, the loader validates:
   - The Go version matches between host and plugin
   - The plugin was built with `-buildmode=plugin`
   - All shared dependencies have matching versions and checksums
4. The plugin must export a `WellKnownHttpFilterConfigFactories` function that returns a map of filter factories

## Plugin Requirements

Plugins must:

- Be compiled with the same Go version as the host binary
- Use `-buildmode=plugin` when building
- Have identical versions of all shared dependencies
- Export a `WellKnownHttpFilterConfigFactories` function with signature:
  ```go
  func WellKnownHttpFilterConfigFactories() map[string]shared.HttpFilterConfigFactory
  ```

## Building a Plugin

```bash
go build -buildmode=plugin -o myplugin.so ./myplugin
```

## Configuration

The plugin is configured via the `GoPlugin` protobuf message:

```json
{
  "name": "my-plugin",
  "url": "file:///path/to/myplugin.so",
  "config": {
    // Plugin-specific configuration
  }
}
```

## Limitations

- Only `file://` URLs are currently supported for plugin locations
- Plugins must be compiled for the same OS/architecture as the host
- All shared dependencies must have exact version matches
