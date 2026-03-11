<p align="center"><img width="50%" src="website/public/logo.svg" /></p>

---

[![CLI](https://github.com/tetratelabs/built-on-envoy/actions/workflows/cli.yaml/badge.svg)](https://github.com/tetratelabs/built-on-envoy/actions/workflows/cli.yaml)
[![CLI Coverage](https://img.shields.io/codecov/c/github/tetratelabs/built-on-envoy?token=v4u6VpZSqZ&logo=codecov&logoColor=lightgray&flag=cli&label=CLI)](https://codecov.io/gh/tetratelabs/built-on-envoy)
[![Extensions](https://github.com/tetratelabs/built-on-envoy/actions/workflows/extensions.yaml/badge.svg)](https://github.com/tetratelabs/built-on-envoy/actions/workflows/extensions.yaml)
[![Extensions Coverage](https://img.shields.io/codecov/c/github/tetratelabs/built-on-envoy?token=v4u6VpZSqZ&logo=codecov&logoColor=lightgray&flag=extensions&label=Extensions)](https://codecov.io/gh/tetratelabs/built-on-envoy)
[![License](https://img.shields.io/badge/License-Apache%202.0-red)](LICENSE)
[![Slack](https://img.shields.io/badge/Slack-Tetrate%20Community-purple)](https://join.slack.com/t/tetrate-community/shared_invite/zt-3rvq88b6s-siA~G2zGSF~sVoxwnl_krw)


A community-driven marketplace for Envoy Proxy extensions. Discover, run, and build custom filters with zero friction.

## Project Overview

**Built On Envoy** is designed to make extending [Envoy Proxy](https://www.envoyproxy.io/) as simple as possible. It consists of:

1. **Marketplace Repository**: A central place where the community can find and contribute extensions.
2. **CLI Tool (`boe`)**: A command-line tool for discovering, running, and building extensions.

## Documentation

* [Installation and Quick Start](https://builtonenvoy.io/docs/getting-started/)
* [Extension Catalog](https://builtonenvoy.io/extensions/)
* [CLI Reference](https://builtonenvoy.io/docs/cli/run)
* [Security Considerations](https://builtonenvoy.io/docs/security-considerations)

## Get In Touch

* Share your feedback and ideas in [issues](https://github.com/tetratelabs/built-on-envoy/issues) or [discussions](https://github.com/tetratelabs/built-on-envoy/discussions).
* Join the [Tetrate Community Slack](https://join.slack.com/t/tetrate-community/shared_invite/zt-3rvq88b6s-siA~G2zGSF~sVoxwnl_krw) if you're not already a member.
Otherwise, use the [#built-on-envoy](https://tetrate-community.slack.com/archives/C0AG8GLT41E) channel to start collaborating with the community.

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
