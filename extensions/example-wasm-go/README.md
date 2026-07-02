# example-wasm-go

A minimal example Wasm HTTP filter for [Built On Envoy](https://builtonenvoy.io),
written in Go with the
[proxy-wasm Go SDK](https://github.com/proxy-wasm/proxy-wasm-go-sdk).

It adds a configurable `x-wasm-header` header to every HTTP response.

## Run

```shell
boe run --extension example-wasm-go
```

With a custom header value:

```shell
boe run --extension example-wasm-go --config '{"header_value": "my-custom-value"}'
```

## Build

The module is compiled to WebAssembly with the standard Go toolchain:

```shell
env GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o plugin.wasm .
```

`boe run --local .` builds this automatically and caches the result.

## Test

```shell
go test ./...
```

The tests use the proxy-wasm Go SDK's `proxytest` host emulator, so they run
with the regular Go toolchain (no WebAssembly build required).
