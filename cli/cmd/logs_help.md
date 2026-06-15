The logs command prints the Built On Envoy CLI logs to stdout.
By default, it prints all log entries from the current session log file.

Use `--follow` to continuously stream new log entries as they are written,
similar to `tail -f`. Use `--tail` to limit output to the most recent N lines.

## Examples

Print all logs:

    ```shell
    boe logs
    ```

Print the last 50 log lines:

    ```shell
    boe logs --tail 50
    ```

Follow the log output in real time:

    ```shell
    boe logs --follow
    ```

Show the last 20 lines and continue following:

    ```shell
    boe logs --follow --tail 20
    ```
