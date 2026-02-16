The create command generates a new extension template with the specified name and type.
This is useful for getting started with developing a new extension for Built On Envoy.

By default, it creates a `composer` type extension, which is an HTTP filter extension.
The generated template includes boilerplate code, a manifest file, and build configuration
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

Create a Rust dynamic module extension:

    ```shell
    boe create my-extension --type dynamic_module_rust
    ```

## Extension Types

    - **composer**: An HTTP filter extension using the Envoy dynamic modules SDK for Go.
      Generates Go source files, Makefile, and Dockerfile for building and deploying.
    
    - **dynamic_module_rust**: An HTTP filter extension using the Envoy dynamic modules SDK for Rust.
      Generates Rust source files and Cargo.toml for building a dynamic library.
