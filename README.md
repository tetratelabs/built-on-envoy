<p align="center"><img width="50%" src="website/public/logo.svg" /></p>

---

[![CLI](https://github.com/tetratelabs/built-on-envoy/actions/workflows/cli.yaml/badge.svg)](https://github.com/tetratelabs/built-on-envoy/actions/workflows/cli.yaml)
[![CLI Coverage](https://img.shields.io/codecov/c/github/tetratelabs/built-on-envoy?token=v4u6VpZSqZ&logo=codecov&logoColor=lightgray&flag=cli&label=CLI)](https://codecov.io/gh/tetratelabs/built-on-envoy)
[![Extensions](https://github.com/tetratelabs/built-on-envoy/actions/workflows/extensions.yaml/badge.svg)](https://github.com/tetratelabs/built-on-envoy/actions/workflows/extensions.yaml)
[![Extensions Coverage](https://img.shields.io/codecov/c/github/tetratelabs/built-on-envoy?token=v4u6VpZSqZ&logo=codecov&logoColor=lightgray&flag=extensions&label=Extensions)](https://codecov.io/gh/tetratelabs/built-on-envoy)
[![License](https://img.shields.io/badge/License-Apache%202.0-red)](LICENSE)


A community-driven marketplace for Envoy Proxy extensions. Discover, run, and build custom filters with zero friction.

## Project Overview

**Built On Envoy** is designed to make extending [Envoy Proxy](https://www.envoyproxy.io/) as simple as possible. It consists of:

1. **Marketplace Repository**: A central place where the community can find and contribute extensions.
2. **CLI Tool (`boe`)**: A command-line tool for discovering, running, and building extensions.

## Quick Start

### Install the CLI

```shell
curl -sL https://builtonenvoy.io/install.sh | sh
```

Or build from source:

```shell
git clone https://github.com/tetratelabs/built-on-envoy
cd built-on-envoy/cli
make
```

### Run an Extension

```bash
# Run a marketplace extension
boe run --extension example-go

# Run a local extension
boe run --local ./my-extension
```

For more examples, take a look at the [extension catalog](https://builtonenvoy.io/extensions/) and the
[run command](https://builtonenvoy.io/docs/cli/run/).


### Generate Envoy Configuration

```bash
boe gen-config --extension example-go > envoy.yaml
```

Read more about [exporting configuration and extensions](https://builtonenvoy.io/docs/custom-envoy-config/) and the
[gen-config command](https://builtonenvoy.io/docs/cli/gen-config/).

## Contributing Extensions

1. Fork this repository.
2. Create a new directory under `extensions/` with your extension name.
3. Add a `manifest.yaml` file with the required metadata.
4. Add your extension code.
5. Add tests.
6. Open a pull request!

See the [Contributing Guidelines](./CONTRIBUTING.md) and the [Project Documentation](https://builtonenvoy.io/docs) for more details.

## License

Apache 2.0 - See [LICENSE](LICENSE) for details.
