# Production reachability analysis

This package represents the callbacks Envoy invokes through the Go SDK. It uses
embedded registration, creates global and per-route configuration, creates the
stream filter, invokes every `shared.HttpFilter` lifecycle method, and models
scheduler dispatch. Only SDK entrypoints are called explicitly; implementation
helpers must be reachable through those entrypoints.

The handle models stream scheduling. Filters that use HTTP callouts, HTTP streams,
watermark callbacks, or configuration scheduling need additional modeled handle
methods that dispatch those callbacks before adopting this entrypoint.

The `analysis` build tag keeps this dummy executable out of ordinary builds and
tests. Compile or analyze it without executing it: placeholder arguments do not
provide an Envoy runtime.

From the repository root:

```sh
make -C extensions/composer lint-deadcode ANALYSIS_MAINS=example/analysis-main
```

Composer's normal `make lint` discovers each filter's `analysis-main/main.go` and
runs the pinned `deadcode` analyzer separately for each entrypoint. Reports are
restricted to the corresponding filter and its implementation subpackages,
including packages not imported by the entrypoint. The `main` and `standalone`
host adapter packages remain covered by golangci-lint; `analysis-main` supplies
the modeled host calls for reachability. Tests are excluded, and any reported
dead function fails lint. Shared golangci-lint settings remain in the root
`.golangci.yml`, and Composer enables the `analysis` tag when running them.

When updating the SDK or adding host callbacks, update the modeled lifecycle and
handle dispatch. Registration alone stores factories without making their
callbacks reachable to the analyzer.
