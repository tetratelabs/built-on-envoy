The debug command runs Envoy with local Go or Rust extensions built for debugging, and attaches a debug
server to the Envoy process so you can set breakpoints in the extension code from your IDE.

Extensions run inside the Envoy process (they are loaded as shared libraries), so the debugger has to attach
to Envoy, not to the `boe` CLI. This command builds the local extensions without optimizations and with full
debug information, starts Envoy, and attaches a debug server to it, listening on `127.0.0.1:2345` by default.
Debug builds are stored separately in the cache and are never used by `boe run`.

Only local extensions (`--local`) are supported: Go extensions, composer sub-extensions, or Rust extensions.
Go and Rust extensions can't be debugged in the same session. All other flags behave as in `boe run`.

- **Go extensions** are debugged with Delve. `boe debug` builds its own patched Delve the first time it runs,
  so it does not need to be installed. Use `--rebuild-dlv` to force rebuilding it.
- **Rust extensions** are debugged with the platform's native debug server, which speaks the GDB remote protocol:
  `gdbserver` on Linux (it must be installed), and LLDB's `debugserver` on macOS (from the Xcode Command Line Tools).
  The debug server is only attached to Envoy while a debugger is connected, so Envoy keeps running normally
  between debug sessions and you can reconnect at any time.

<Callout type="note">
Delve can only debug Go code loaded into a non-Go process on Linux. On other platforms, such as macOS,
`boe debug` runs Envoy and Delve in a Linux container for Go extensions, so Docker is required. The local extension
sources are mounted in the container at the same path as in the host, so breakpoints set in the IDE work without
any path mapping. Rust extensions are always debugged natively, and `--docker` is not supported for them.
</Callout>

<Callout type="warning">
On Linux, the debug server attaches to the Envoy process started by `boe`, which is not its parent process. Many
distributions, such as Ubuntu, only allow a process to attach to its own descendants (`kernel.yama.ptrace_scope=1`),
and attaching then fails with "operation not permitted". Allow it by setting `ptrace_scope` to `0` (until the next reboot):

    ```shell
    sudo sysctl -w kernel.yama.ptrace_scope=0
    ```

To make it permanent, add `kernel.yama.ptrace_scope = 0` to a file in `/etc/sysctl.d/`. Alternatively, run `boe debug`
as root, or, for Go extensions, use `boe debug --docker` to run Envoy and Delve in a container.
</Callout>

While the debugger is stopped at a breakpoint all Envoy workers are paused, so in-flight requests may time out.

Breakpoints are resolved using the exact source paths recorded when building the extension, which are the
paths given in `--local`. If those paths go through a symlink (for example, `/tmp` is a symlink to `/private/tmp`
on macOS) and your IDE sets breakpoints using the resolved path, they will not be hit. `boe debug` warns about
this and prints the path mapping to add to the IDE configuration.

## Examples

Debug the `opa` extension from the composer bundle:

    ```shell
    boe debug --local ~/src/built-on-envoy/extensions/composer/opa \
        --config '{"policies":[{"inline":"package envoy.authz\ndefault allow := true"}]}'
    ```

Debug a Rust extension:

    ```shell
    boe debug --local ~/src/built-on-envoy/extensions/ip-restriction \
        --config '{"deny_addresses":["192.168.1.50"]}'
    ```

Debug a Go extension using a custom port for the debug server:

    ```shell
    boe debug --local ./my-go-extension --debug-port 40000
    ```

Once the debug server is listening, attach your IDE to `127.0.0.1:2345`: as a remote Delve session for Go
extensions, or as a GDB remote (`gdb-remote`) session for Rust extensions. From the command line, use
`dlv connect 127.0.0.1:2345` for Go, or `lldb -o 'gdb-remote 127.0.0.1:2345'` for Rust. See the
[Debugging](/docs/debugging-extensions) guide for IDE configurations and caveats.
