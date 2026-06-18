This command provides a web console to browse, configure, and run
Built On Envoy extensions. It opens automatically in your default browser.

## Features

* Browse the full extension catalog with search and filters
* Configure extensions using auto-generated forms based on JSON Schema
* Reorder extensions in the HTTP filter chain
* Run Envoy with selected extensions using `boe run`

## Examples

Start the UI on the default port (18000):

    ```shell
    boe ui
    ```

Start the UI on a custom port customizing the Envoy log levels:

    ```shell
    boe ui --port 9090 --log-level dynamic_modules:debug
    ```

Start the UI enabling also some local extensions:

    ```shell
    boe ui --local /path/to/my/extension1 --local /path/to/my/extension2 
    ```

Start the UI to run the extensions against a custom upstream host instead of the default `httpbin.org`:

    ```shell
    boe ui --test-upstream-host api.openai.com
    ```
