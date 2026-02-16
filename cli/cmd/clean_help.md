The clean command removes cached files and directories used by the CLI.
This is useful to free disk space or to reset the CLI state when troubleshooting issues.

By default, no directories are cleaned unless you specify which caches to remove.
Use `--all` to clean everything, or use individual flags to selectively clean specific caches.

## Examples

Clean all cache directories:

    ```shell
    boe clean --all                 # Clean all cache directories
    boe clean --extension-cache     # Clean only the extension cache
    boe clean --state-cache         # Clean the state cache
    ```

## Cache Directories

    - **extension-cache**: Downloaded extensions stored in the data home directory ($BOE_DATA_HOME/extensions or ~/.local/share/boe/extensions).
    - **config-cache**: User-specific configuration files ($BOE_CONFIG_HOME or ~/.config/boe).
    - **data-cache**: User-specific data files ($BOE_DATA_HOME or ~/.local/share/boe).
    - **state-cache**: Persistent state and logs ($BOE_STATE_HOME or ~/.local/state/boe).
    - **runtime-cache**: Ephemeral runtime files ($BOE_RUNTIME_DIR or /tmp/boe-$UID).
