# Extension E2E Tests

End-to-end tests that run a real Envoy instance via the `boe` CLI and assert extension behavior over HTTP.

## Writing a test

```go
func TestMyExtension(t *testing.T) {
    ports := internaltesting.FreePorts(t, 2)
    proxyPort, adminPort := ports[0], ports[1]

    internaltesting.RunEnvoy(t, cliBin, proxyPort, adminPort,
        "--log-level", "dynamic_modules:debug",   // turn on debug logs for the extension
        "--local", "../../my-extension",          // path to the extension under test
        "--config", `{"key": "value"}`)           // extension config as JSON

    url := fmt.Sprintf("http://localhost:%d/status/200", proxyPort)
    internaltesting.RequireEventuallyGet(t, url, internaltesting.EqualStatus(200))
}
```

- `FreePorts` allocates two free ports for the proxy listener and Envoy admin API.
- `RunEnvoy` compiles the extension, starts `boe run` with those ports, then waits until Envoy is ready. Cleanup is automatic.
- `RequireEventuallyGet` / `RequireEventuallyPost` retry the request until the condition is met or one minute passes.
- `EqualStatus(code)` is a pre-built condition; pass a custom `func(*http.Response) bool` for anything else.

## Test upstream

The [TestMain](./main_test.go) already starts an [httpbin-go](https://github.com/mccutchen/go-httpbin) server and registers it as the default upstream cluster.
All `RunEnvoy` calls pick it up automatically and proxies all requests to it, to make it very easy to verify the behavior of the extensions.

The httpbin API is available at paths like `/status/200`, `/headers`, `/anything`, etc. through the proxy port.

If you need to use another upstream, you can set the value of the `TestUpstreamCluster` or `TestUpstreamClusterInsecure` in the
[internal testing env file](../../../internal/testing/env.go).

## Running the tests

```sh
make test

# Run a specific test with verbose output
GO_TEST_ARGS="-run TestMyExtension -v" make test
```

## Environment variables

A detailed list of available environment variables can be found in the [internal testing env file](../../../internal/testing/env.go).
