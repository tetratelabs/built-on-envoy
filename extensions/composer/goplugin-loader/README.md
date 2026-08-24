# Go Plugin Loader

This package implements a Dynamic Module for Envoy that can load standalone Go plugins at runtime.

## Overview

The Go Plugin Loader is a built-in Envoy Dynamic Module that enables loading external Go plugins without recompiling the main binary. It uses Go's native [plugin](https://pkg.go.dev/plugin) package to dynamically load shared object files (`.so`) that implement HTTP filters.

## How It Works

1. The loader is registered as a well-known HTTP filter config factory named `goplugin-loader`
2. When Envoy loads the configuration, it parses the `GoPlugin` protobuf message containing:
   - `name`: The plugin name to load
   - `url`: The location of the plugin binary (currently supports `file://` URLs)
   - `config`: The configuration to pass to the loaded plugin
   - `versioned_url_suffix`: Whether to append the composer version to the tag of `url` before fetching the plugin
3. Before loading a plugin, the loader validates:
   - The Go version matches between host and plugin
   - The plugin was built with `-buildmode=plugin`
   - All shared dependencies have matching versions and checksums
4. The plugin must export a `WellKnownHttpFilterConfigFactories` function that returns a map of filter factories

## Plugin Requirements

Plugins must:

- Be compiled with the same Go version as the host binary
- Use `-buildmode=plugin` when building
- Use the same build flags as the host binary, including `-trimpath` when the host uses it
- Have identical versions of all shared dependencies
- Export a `WellKnownHttpFilterConfigFactories` function with signature:
  ```go
  func WellKnownHttpFilterConfigFactories() map[string]shared.HttpFilterConfigFactory
  ```

## Building a Plugin

```bash
go build -trimpath -buildmode=plugin -o myplugin.so ./myplugin
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

## URL Schemes

The loader supports the following URL schemes for plugin locations:

- `file://` — Load from a local file path (e.g. `file:///path/to/plugin.so`)
- `oci://` — Fetch from an OCI registry at runtime (e.g. `oci://ghcr.io/tetratelabs/built-on-envoy/extension-my-plugin:v1.0.0`)

`boe gen-config` generates `oci://` URLs for remote composer (Go plugin) extensions, allowing
Envoy to fetch the plugin binary directly from the OCI registry at runtime. `boe run` uses
`file://` URLs pointing to locally cached binaries.

The following environment variables configure OCI plugin fetching at runtime:

- `GOPLUGIN_CACHE_DIR` — Directory to cache downloaded plugin binaries
- `GOPLUGIN_PULL_SECRET` — Credentials for authenticating to the OCI registry
- `GOPLUGIN_INSECURE` — Set to `true` to allow insecure (HTTP) registry connections

## Versioned URL Suffix

A Go plugin can only be loaded by the composer version it was built against, so upgrading the
proxy (and the `libcomposer.so` inside it) requires a plugin binary rebuilt for the new composer
version. Setting `versioned_url_suffix` to `true` makes the loader append the composer version to
the tag of the configured `url` before fetching the image:

```json
{
  "name": "my-plugin",
  "url": "oci://ghcr.io/tetratelabs/built-on-envoy/extension-my-plugin:1.0.0",
  "versioned_url_suffix": true
}
```

With composer `0.12.0` this fetches
`oci://ghcr.io/tetratelabs/built-on-envoy/extension-my-plugin:1.0.0-0.12.0`, and the very same
configuration fetches the `1.0.0-0.13.0` image once the proxy ships composer `0.13.0`. The
registry is therefore expected to hold one plugin image tag per composer version.

The suffix only applies to `oci://` URLs; URLs of other schemes, and `oci://` references pinned
by digest, are fetched as configured. The flag defaults to `false`.

## Limitations

- Plugins must be compiled for the same OS/architecture as the host
- All shared dependencies must have exact version matches
