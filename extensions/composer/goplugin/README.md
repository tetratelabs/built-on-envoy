# Go Plugin Loader

This package implements a Dynamic Module for Envoy that can load standalone Go plugins at runtime.

## Overview

The Go Plugin Loader is a built-in Envoy Dynamic Module that enables loading external Go plugins without recompiling the main binary. It uses Go's native [plugin](https://pkg.go.dev/plugin) package to dynamically load shared object files (`.so`) that implement HTTP filters.

Plugins can be loaded from:
- Local filesystem using `file://` URLs
- Remote OCI registries (Docker Hub, GitHub Container Registry, etc.)

## How It Works

1. The loader is registered as a well-known HTTP filter config factory named `goplugin`
2. When Envoy loads the configuration, it parses the `GoPlugin` protobuf message containing:
   - `name`: The plugin name to load
   - `url`: The location of the plugin binary (supports `file://` URLs and OCI image references)
   - `config`: The configuration to pass to the loaded plugin
   - `strict_check`: Whether to perform strict version compatibility checks (default: `true`)
   - `allow_insecure`: Whether to allow insecure registry connections (default: `false`)
3. For remote plugins:
   - Checks local cache first (`~/.cache/built-on-envoy/plugins` on Linux, `~/Library/Caches/built-on-envoy/plugins` on macOS)
   - If not cached, downloads from the OCI registry
   - Extracts the `.so` file from the container image
   - Saves to cache for future use
4. Before loading a plugin, the loader validates:
   - The Go version matches between host and plugin
   - The plugin was built with `-buildmode=plugin`
   - All shared dependencies have matching versions and checksums
5. The plugin must export a `WellKnownHttpFilterConfigFactories` function that returns a map of filter factories

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

### Local File Plugin

```json
{
  "name": "my-plugin",
  "url": "file:///path/to/myplugin.so",
  "config": {
    // Plugin-specific configuration
  }
}
```

### Remote Registry Plugin

```json
{
  "name": "my-plugin",
  "url": "ghcr.io/myorg/myplugin:v1.0.0",
  "config": {
    // Plugin-specific configuration
  },
  "strict_check": true,
  "allow_insecure": false
}
```

### Configuration Fields

- `name` (required): The plugin name to load from the plugin binary
- `url` (required): Plugin location
  - Local: `file:///absolute/path/to/plugin.so`
  - Remote: OCI image reference (e.g., `ghcr.io/owner/plugin:tag`, `docker.io/library/plugin:latest`)
- `config` (optional): Plugin-specific configuration passed to the plugin
- `strict_check` (optional, default: `true`): Perform strict version compatibility checks
- `allow_insecure` (optional, default: `false`): Allow HTTP connections to insecure registries

## Remote Plugin Storage

When packaging plugins as OCI images, the `.so` file should be placed in one of these locations within the container:
- `/plugin.so`
- `/app/plugin.so`
- `/usr/local/bin/plugin.so`

Or any `.so` file in the root directory will be automatically detected.

### Multi-Platform Image Support

The fetcher automatically detects the runtime platform (OS) and architecture from `runtime.GOOS` and `runtime.GOARCH`. When pulling from an OCI registry:

- For **multi-platform images** (image indexes), it selects the manifest matching the current platform/arch
- For **single-platform images**, it downloads the available image
- If no matching platform is found in a multi-platform image, an error is returned

This means you can publish a single multi-platform image and the correct binary will be automatically selected at runtime.

### Example Dockerfile for Plugin Distribution

**Single platform:**
```dockerfile
FROM scratch
COPY myplugin.so /plugin.so
```

**Multi-platform (using Docker Buildx):**
```dockerfile
FROM scratch
COPY myplugin.so /plugin.so
```

Build and push multi-platform:
```bash
# Build for multiple platforms
docker buildx build --platform linux/amd64,linux/arm64 \
  -t ghcr.io/myorg/myplugin:v1.0.0 --push .
```

Or build single platform:
```bash
docker build -t ghcr.io/myorg/myplugin:v1.0.0 .
docker push ghcr.io/myorg/myplugin:v1.0.0
```

## Caching

Downloaded plugins are cached locally to avoid repeated downloads:
- **Linux**: `~/.cache/built-on-envoy/plugins` (or `$XDG_CACHE_HOME/built-on-envoy/plugins`)
- **macOS**: `~/Library/Caches/built-on-envoy/plugins`

Cache keys are generated based on the image reference, platform (OS), and architecture to ensure correct binaries are used.

## Insecure Registries

For development or testing with insecure registries (HTTP instead of HTTPS), set `allow_insecure: true`:

```json
{
  "name": "dev-plugin",
  "url": "localhost:5000/plugin:dev",
  "allow_insecure": true
}
```

⚠️ **Warning**: Only use `allow_insecure` in development environments. Production deployments should always use HTTPS registries.

## Limitations

- Plugins must be compiled for the same OS/architecture as the host
- All shared dependencies must have exact version matches
- Only `.so` files (Linux/Unix shared libraries) are supported
