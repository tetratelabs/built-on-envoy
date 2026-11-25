# WAF Extension

This extension implements a Web Application Firewall using [OWASP Coraza](https://coraza.io/) and comes with rules from the [OWASP Core Rule Set (CRS)](https://coreruleset.org/) already embedded and ready to use.

## Rule Files

The WAF resolves rule files from five layered filesystems (first match wins):

1. **Embedded rules** (`coraza/directives/`) — tailored configurations shipped with this extension.
2. **CRS setup** (`crs/setup/v<version>/`) — the `@crs-setup.conf` tailored for the selected CRS version.
3. **CRS rules** — upstream OWASP CRS rules and setup file of the selected CRS version.
4. **Coraza recommended configuration** — upstream `@coraza.conf-recommended`.
5. **Local filesystem** — support for user-provided overrides and custom rules.

## CRS versions

The WAF embeds two CRS versions, and a configuration picks one with `crs_version`:

| `crs_version`     | CRS version                |
|-------------------|----------------------------|
| not set, or `lts` | The newest CRS LTS release |
| `latest`          | The newest CRS release     |

Any other value fails to load. Whichever version is selected, its files are included with the same names
(`@owasp_crs/`, `@crs-setup.conf`, `@crs-setup.conf.example` and `@crs-setup.conf.example-nodefaultact`), so the
directives don't change with the version. The [manifest](./manifest.yaml) lists the CRS versions of this release.

## Upgrading CRS

The `crs/v4` and `lts/v4` module versions follow the upstream CRS releases and are tagged as `crs/vX.Y.Z` and
`lts/vX.Y.Z` (see the [coraza-coreruleset tags](https://github.com/corazawaf/coraza-coreruleset/tags)). To upgrade
either of them:

1. Bump the CRS module version in `go.mod`:
```sh
go get github.com/corazawaf/coraza-coreruleset/crs/v4@<new-version>   # or lts/v4@<new-version>
go mod tidy
```

1. Add the tailored setup file of the new version, `crs/setup/v<new-version>/@crs-setup.conf`, and remove the one of the
   former version. Start from the upstream setup example of the new version and carry over the changes the former file
   makes to its own upstream example (diff the two to see them). The setup files are maintained by hand; the `crs`
   package tests only check that each one belongs to its version.

1. Review the ignored test IDs of the version's FTW list, `tests/e2e/testdata/waf/ftw_latest.yaml` or `ftw_lts.yaml`
   (CRS renumbers and adds regression tests), and set its `# CRS version:` line to the new version: `TestFTW` fails
   until it matches.

1. Update the CRS versions in the `manifest.yaml` documentation.

`latest` and `lts` must stay on different CRS versions: each version is registered once, so the `crs` package panics at
start-up if both aliases name the same one.

## Upgrading Coraza

To upgrade to a new Coraza version:

1. Bump the Coraza dependency version in `go.mod`, together with the `coraza/v3` module of Coraza CoreRuleSet, which provides `@coraza.conf-recommended` for the same Coraza version (tagged as `coraza/vX.Y.Z`):
   ```sh
   go get github.com/corazawaf/coraza/v3@<new-version> github.com/corazawaf/coraza-coreruleset/coraza/v3@<new-version>
   go mod tidy
   ```

1. Update the embedded Coraza configuration file (`@coraza.conf`) if there are any relevant changes in the upstream `coraza.conf-recommended`. The embedded config should be updated while preserving any custom configurations specific to this repository.
