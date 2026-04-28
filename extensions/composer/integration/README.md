# WAF Integration tests

Run the integration tests with:

```shell
make test
```

You can take a look at the [Makefile](./Makefile) for a detailed list of environment variables.

## Running on Mac OSX

Integration tests run against an Envoy container by default, and expect the `libcomposer.so` dynamic module
to be compiled for the Linux operating system.

On OSX, it is easier to run Envoy locally outside Docker. You can install it with Homebrew, then run the
tests as follows:

```shell
make test ENVOY_IMAGE= ENVOY_VERSION=1.38.0
```

* Setting the `ENVOY_IMAGE` to an empty string will fallback to running Envoy as a local process.
* Setting the `ENVOY_VERSION` variable will check that the Envoy version is the expected one. Set it to an
  empty string to disable this validation.
