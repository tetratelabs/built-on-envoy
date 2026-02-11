# Embedded Go Plugin

This directory contains the embedded version of the example Go plugin, compiled directly into the Composer dynamic module binary.

See the [example-go README](../README.md) for details on the plugin implementation and packaging approaches.

## How it works

The `host.go` file imports the plugin package and registers it with the Envoy SDK during initialization. The Composer's `main.go` imports this package to include the plugin in the final binary.

> [!Warning]
> Note that there is no `go.mod` for embedded plugins.
> To ensure runtime version compatibility, all plugins must inherit the `go.mod` in the `extensions/` root directory,
> which is used to build `libcomposer.so` as well.

## Usage

To use the plugin with `boe`, run it as follows:

```shell
boe run --extension example-go-embedded
```

Or for local testing:

```shell
boe run --local extensions/example-go/embedded
```
