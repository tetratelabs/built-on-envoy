The list command displays all available Envoy extensions.
It provides a quick overview of what extensions you can use when running Envoy or generating configurations,
along with the versions available for each extension.

This command is useful for discovering which extensions are available before using them
with the `run` or `gen-config` commands.

## Examples

List all available extensions:

    ```shell
    boe list
    ```

The output shows a table with the extension name, version, type, and a brief description:

    ```
    NAME          VERSION  TYPE  DESCRIPTION
    example-lua   1.0.0    lua   A simple Lua extension that adds a custom header
    cors          1.0.0    lua   Cross-Origin Resource Sharing (CORS) support
    ```
