The pull command downloads an extension from an OCI-compliant container registry.
You can specify either a simple extension name (which uses the default registry) or a full
OCI reference including registry, repository, and tag.

The extension is extracted to a local directory and can then be used with the `run` or
`gen-config` commands via the `--local` flag. If no destination path is specified,
the extension is saved to the default data directory.

## Examples

Pull an extension by name from the default registry:

    ```shell
    boe pull example-lua
    ```

Pull a specific version of an extension:

    ```shell
    boe pull example-lua:1.0.0
    ```

Pull an extension and extract it to a specific directory:

    ```shell
    boe pull example-lua --path ~/extensions/example-lua
    ```

Pull from a custom OCI registry:

    ```shell
    export BOE_REGISTRY=acme.org/extensions
    export BOE_REGISTRY_USERNAME=username
    export BOE_REGISTRY_PASSWORD=password

    boe pull example-lua:1.0.0
    ```

Pull using a full OCI reference:

    ```shell
    boe pull ghcr.io/myorg/my-extension:1.0.0
    ```

Pull from an insecure (HTTP) registry for local development:

    ```shell
    boe pull localhost:5000/my-extension --insecure
    ```

## Using Pulled Extensions

Once an extension is pulled, you can use it with the `run` or `gen-config` commands:

    ```shell
    # Use with run
    boe run --local ~/extensions/example-lua

    # Use with gen-config
    boe gen-config --local ~/extensions/example-lua
    ```
