The push command publishes a local extension to an OCI-compliant container registry.
This allows you to share extensions with others or deploy them across different environments.

The extension directory must contain a valid `manifest.yaml` file that defines the extension
metadata and configuration. The extension version from the manifest is used as the image tag.
You can specify registry credentials via flags or environment variables for authenticated registries.

## Examples

Push a local extension to the default registry:

    ```shell
    boe push ~/src/my-extension
    ```

Push to a custom OCI registry:

    ```shell
    export BOE_REGISTRY=acme.org/extensions
    export BOE_REGISTRY_USERNAME=username
    export BOE_REGISTRY_PASSWORD=password

    boe push ~/src/my-extension
    ```

Push to a local insecure (HTTP) registry for development:

    ```shell
    boe push ~/src/my-extension --registry localhost:5000 --insecure
    ```
