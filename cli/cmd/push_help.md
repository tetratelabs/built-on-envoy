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

Build and push Docker image with pre-compiled plugin (composer extensions only):

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
- `Dockerfile.plugin` in the extension directory

**Supported Platforms**: Only `linux/amd64` and `linux/arm64` are supported.

The `Dockerfile.plugin` should build the Go plugin. See `extensions/internal/libcomposer/Dockerfile.plugin`
for a reference implementation.

**OCI Annotations**: Built images include standard OCI annotations with extension metadata:
- Standard annotations: name, version, description, author, license, creation timestamp
- Git information: source repository URL and commit SHA (automatically detected if in a git repository)
- Built-on-Envoy annotations: extension type, composer version

Images are created with OCI media types for maximum compatibility.

**Note**: A temporary buildx builder instance is created for each build and automatically cleaned up
afterward, ensuring no leftover build infrastructure.

## Authentication

Registry credentials can be provided via flags or environment variables:

    ```shell
    # Using flags (credentials passed securely via stdin)
    boe push ~/src/my-plugin --build \
      --username "$REGISTRY_USER" \
      --password "$REGISTRY_TOKEN"
    
    # Using environment variables
    export BOE_REGISTRY_USERNAME=myuser
    export BOE_REGISTRY_PASSWORD=mytoken
    boe push ~/src/my-plugin --build
    ```

For enhanced security, use Docker credential helpers:

    ```shell
    # One-time setup (stores credentials in system keychain)
    docker login ghcr.io
    
    # Subsequent pushes use stored credentials automatically
    boe push ~/src/my-plugin --build
    ```

**Security Note**: Passwords are passed via stdin using `--password-stdin`, which prevents them from
appearing in process listings or command history.
