The push command publishes a local extension to an OCI-compliant container registry.
This allows you to share extensions with others or deploy them across different environments.

The extension directory must contain a valid `manifest.yaml` file that defines the extension
metadata and configuration. The extension version from the manifest is used as the image tag.
You can specify registry credentials via flags or environment variables for authenticated registries.

## Source and Binary Packages

By default, `push` creates a source package at `extension-src-<name>:<version>` containing
the extension source code as a tar.gz OCI artifact.

For composer-type extensions (Go plugins), you can optionally build and push a Docker image
with pre-compiled `plugin.so` binaries using the `--build` flag. This creates an additional
package at `extension-<name>:<version>`.

## Examples

Push source code of a local extension to the default registry:

    ```shell
    boe push ~/src/my-extension
    ```

Push source code to a custom OCI registry:

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

Build and push Docker image with pre-compiled plugin (composer extensions only and the source code will not be pushed if --build is set):

    ```shell
    boe push ~/src/my-plugin --build
    ```

Build for multiple platforms:

    ```shell
    boe push ~/src/my-plugin --build --platforms linux/amd64,linux/arm64
    ```

## Prerequisites for --build

- Docker installed and running
- Docker Buildx plugin installed (included by default in Docker Desktop and modern Docker Engine)

**Supported Platforms**: Only `linux/amd64` and `linux/arm64` are supported.

**OCI Annotations**: Built images include standard OCI annotations with extension metadata:
- Standard annotations: name, version, description, author, license, creation timestamp
- Git information: source repository URL and commit SHA (automatically detected if in a git repository)
- Built-on-Envoy annotations: extension type, composer version and so on

Images are created with OCI media types for maximum compatibility.
