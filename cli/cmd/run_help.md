The run command starts an Envoy proxy with the specified extensions enabled.
It automatically downloads the required Envoy binary if not already present, generates the necessary
configuration, and launches Envoy with the extensions configured in the HTTP filter chain.

You can enable multiple extensions using the `--extension` flag, and also load extensions from
local directories using `--local` for development and testing purposes. The command manages
all runtime files, logs, and the Envoy admin interface automatically.

> ⚠️ `boe run` will try to download Envoy for your operating system and platform, but Envoy does
> not provide packages for all platforms. If there are no Envoy binaries for your platform, `boe`
> can transparently run Envoy in Docker. Just use `boe run --docker` or set the `BOE_RUN_DOCKER=true`
> environment variable.

## Examples

Run the `example-lua` extension from the default registry with `debug` logs enabled for Lua:

    ```shell
    boe run --extension example-lua --log-level lua:debug
    ```

Run a specific version of the `example-lua` extension from a custom OCI registry. This is useful if you are hosting the
extensions in a corporate OCI registry.

    ```shell
    export BOE_REGISTRY=acme.org/extensions
    export BOE_REGISTRY_USERNAME=username
    export BOE_REGISTRY_PASSWORD=password

    boe run --extension example-lua:1.0.0 --log-level lua:debug
    ```

Run a local extension from a local directory. The directory must contain the extension `manifest.yaml`
and all the files needed to execute the extension locally.

    ```shell
    boe run --local ~/src/built-on-envoy/extensions/example-lua
    ```

Run extensions with custom JSON configuration strings. Configs are applied in order to the
combined list of `--extension` and `--local` flags. Use an empty string `''` to skip an extension:

    ```shell
    boe run --extension ext1 --extension ext2 --config '{"key":"value"}' --config ''
    ```

If the extension needs to reach external services, you can provide additional Envoy clusters using the `--cluster`, `--cluster-insecure`, and `--cluster-json` flags. For example:

    ```shell
    # Configure cluster for a given URL
    boe run --extension ext1 --cluster example.com:443
    # Configure cluster for a given URL that is not TLS
    boe run --extension ext1 --cluster-insecure example.com:80
    # Configure a full cluster
    boe run --extension ext1 --cluster-json '{"name":"my-cluster","type":"STRICT_DNS","load_assignment":{"cluster_name":"my-cluster","endpoints":[{"lb_endpoints":[{"endpoint":{"address":{"socket_address":"address":"example.com","port_value":8081}}}}]}]}}'
    ```