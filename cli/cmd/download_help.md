The download command is used to download the contents of published extension
packages for a specific platform to a given directory. By default it downloads
the packages for the current platform.

This command is mostly useful to download a compiled version of an extension to
use it in production or other environments. Unlike the `gen-config` command, this
one can be used to download compiled extensions for a concrete target paltform.

Download extensions for a given platform:

    ```shell
    boe download ip-restriction --platform linux/amd64
    boe download example-go --platform linux/amd64 --path /tmp/extensions
    ```

Download the composer dynamic module that bundles all Go extensions:

    ```shell
    boe download composer --platform linux/amd64
    ```
