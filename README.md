<p align="center"><img width="50%" src="website/public/logo.svg" /></p>

---

[![CLI](https://github.com/tetratelabs/built-on-envoy/actions/workflows/build.yaml/badge.svg)](https://github.com/tetratelabs/built-on-envoy/actions/workflows/build.yaml)
[![Netlify Status](https://api.netlify.com/api/v1/badges/3c996526-432a-4962-ad31-82fbabacbac6/deploy-status)](https://app.netlify.com/projects/builtonenvoy/deploys)
[![codecov](https://codecov.io/gh/tetratelabs/built-on-envoy/graph/badge.svg?token=v4u6VpZSqZ)](https://codecov.io/gh/tetratelabs/built-on-envoy)
[![License](https://img.shields.io/badge/License-Apache%202.0-red)](LICENSE)
[![Slack](https://img.shields.io/badge/Slack-Tetrate%20Community-purple)](https://tetr8.io/tetrate-community)


A community-driven marketplace for Envoy Proxy extensions. Discover, run, and build custom filters with zero friction.

## Project Overview

**Built On Envoy** is designed to make extending Envoy Proxy as simple as possible. It consists of:

1. **Marketplace Repository**: A GitHub repository where each folder contains an extension
2. **CLI Tool (`boe`)**: A command-line tool for discovering, running, and building extensions

## Quick Start

### Install the CLI

```bash
curl -sL https://get.built-on-envoy.io | bash
```

Or build from source:

```bash
git clone https://github.com/tetratelabs/built-on-envoy
cd built-on-envoy/cli
make build
```

### List Available Extensions

```bash
boe list
```

### Run an Extension

```bash
# Run a marketplace extension
boe run --extension rate-limiter

# Run a local extension
boe run --local ./my-extension
```

### Generate Envoy Configuration

```bash
boe gen-config --extension rate-limiter > envoy.yaml
```

## Contributing Extensions

1. Fork this repository
2. Create a new directory under `extensions/` with your extension name
3. Add a `manifest.yaml` file with the required metadata
4. Add your extension code (Lua, etc.)
5. Open a pull request!

See the [Extension Guide](./extensions/) for more details.

## License

Apache 2.0 - See [LICENSE](LICENSE) for details.
