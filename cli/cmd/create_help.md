The create command generates a new extension template with the specified name and type.
This is useful for getting started with developing a new extension for Built On Envoy.

By default, it creates a 'composer' type extension, which is an HTTP filter extension.
The generated template includes boilerplate code, a manifest file, and a Makefile
to help you build and install the extension.

## Examples

Create a new composer HTTP filter extension:

    ```shell
    boe create my-extension
    ```

Create an extension in a specific directory:

    ```shell
    boe create my-extension --path ~/src/extensions
    ```

Create an extension with explicit type:

    ```shell
    boe create my-extension --type composer
    ```

## Generated Files

The create command generates the following files in the extension directory:

- **plugin.go**: The main Go source file with HTTP filter implementation boilerplate.
- **manifest.yaml**: The extension manifest defining metadata and configuration.
- **Makefile**: Build targets for compiling and installing the extension.
- **go.mod**: Go module file with required dependencies.

After creation, the command automatically runs `go mod tidy` to fetch dependencies.

## Extension Types

- **composer**: An HTTP filter extension using the Envoy dynamic modules SDK for Go.
  This is currently the only supported type.
