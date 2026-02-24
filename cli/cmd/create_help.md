The create command generates a new extension template with the specified name and type.
This is useful for getting started with developing a new extension for Built On Envoy.

By default, it creates a `go` type extension, which is an HTTP filter extension.
The generated template includes boilerplate code, a manifest file, and build configuration
to help you build and install the extension.

## Examples

Create a new Go HTTP filter extension:

    ```shell
    boe create my-extension
    ```

Create an extension in a specific directory:

    ```shell
    boe create my-extension --path ~/src/extensions
    ```

Create an extension with explicit type:

    ```shell
    boe create my-extension --type go
    ```

Create a Rust dynamic module extension:

    ```shell
    boe create my-extension --type rust
    ```

## Extension Types

    - **go**: An HTTP filter extension using the Envoy dynamic modules SDK for Go.
      Generates Go source files, Makefile, and Dockerfile for building and deploying.
    - **rust**: An HTTP filter extension using the Envoy dynamic modules SDK for Rust.
      Generates Rust source files and Cargo.toml for building a dynamic library.
