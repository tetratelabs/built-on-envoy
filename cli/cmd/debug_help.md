The debug command runs Envoy with local Go extensions built for debugging, and attaches a headless
Delve server to the Envoy process so you can set breakpoints in the extension code from your IDE.

Go extensions run inside the Envoy process (they are loaded as a shared library), so the debugger has
to attach to Envoy, not to the `boe` CLI. This command builds the local Go extensions (or their composer
bundle) without optimizations and with full debug information, starts Envoy, and attaches Delve to it,
listening on `127.0.0.1:2345` by default. Debug builds are stored separately in the cache and are never
used by `boe run`.

Only local extensions (`--local`) are supported, and they must be Go extensions or composer sub-extensions.
All other flags behave as in `boe run`.

<Callout type="warning">
Delve can only debug Go code loaded into a non-Go process on Linux. On other platforms, such as macOS,
`boe debug` runs Envoy and Delve in a Linux container, so Docker is required. The local extension sources
are mounted in the container at the same path as in the host, so breakpoints set in the IDE work without
any path mapping. `boe debug` builds its own Delve the first time it runs, so it does not need to be installed.
</Callout>

<Callout type="warning">
On Linux, Delve attaches to the Envoy process started by `boe`, which is not its parent process. Many distributions,
such as Ubuntu, only allow a process to attach to its own descendants (`kernel.yama.ptrace_scope=1`), and attaching
then fails with "operation not permitted". Allow it by setting `ptrace_scope` to `0` (until the next reboot):

    ```shell
    sudo sysctl -w kernel.yama.ptrace_scope=0
    ```

To make it permanent, add `kernel.yama.ptrace_scope = 0` to a file in `/etc/sysctl.d/`. Alternatively, run `boe debug`
as root, or use `boe debug --docker` to run Envoy and Delve in a container.
</Callout>

While the debugger is stopped at a breakpoint all Envoy workers are paused, so in-flight requests may time out.

Breakpoints are resolved using the exact source paths recorded when building the extension, which are the
paths given in `--local`. If those paths go through a symlink (for example, `/tmp` is a symlink to `/private/tmp`
on macOS) and your IDE sets breakpoints using the resolved path, they will not be hit. `boe debug` warns about
this and prints the `substitutePath` setting to add to the VS Code launch configuration.

## Examples

Debug the `opa` extension from the composer bundle:

    ```shell
    boe debug --local ~/src/built-on-envoy/extensions/composer/opa \
        --config '{"policies":[{"inline":"package envoy.authz\ndefault allow := true"}]}'
    ```

Debug a Go extension using a custom port for Delve:

    ```shell
    boe debug --local ./my-go-extension --dlv-port 40000
    ```

Once Delve is listening, attach your IDE to it. For VS Code, use a launch configuration like the following,
then set breakpoints in the extension code and send requests to Envoy:

    ```json
    {
      "name": "Attach to Envoy (boe debug)",
      "type": "go",
      "request": "attach",
      "mode": "remote",
      "host": "127.0.0.1",
      "port": 2345
    }
    ```

You can also use the Delve CLI directly:

    ```shell
    dlv connect 127.0.0.1:2345
    ```
