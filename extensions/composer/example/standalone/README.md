# Standalone Go Plugin

This directory contains the standalone version of the example Go plugin, compiled as a separate `.so` file that can be loaded by the Composer at runtime.

See the [example-go README](../README.md) for details on the plugin implementation and packaging approaches.

## Writing

Standalone Go plugins must have a `main` package with the following method:

```go
func WellKnownHttpFilterConfigFactories() map[string]shared.HttpFilterConfigFactory
```

The method will be used as an entrypoint for the Go plugin.

## Building

```bash
make
make install
```

This produces `example-go.so` which can be loaded by the Composer's goplugin loader.

> [!Caution]
> The plugin must be compiled with the exact same Go version and dependency versions as the Composer dynamic module.

## Usage

You can use the plugin as follows:

```shell
boe run --extension example-go
```

Or for local testing:

```shell
boe run --local extensions/example-go/standalone
```
