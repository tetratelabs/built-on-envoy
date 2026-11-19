# WAF Extension

This extension implements a Web Application Firewall using [OWASP Coraza](https://coraza.io/) and comes with rules from the [OWASP Core Rule Set (CRS)](https://coreruleset.org/) already embedded and ready to use.

## Rule Files

The WAF resolves rule files from five layered filesystems (first match wins):

1. **Embedded rules** (`coraza/directives/`) — tailored configurations shipped with this extension.
2. **CRS setup** (`crs/setup/v<version>/`) — the `@crs-setup.conf` tailored for the selected CRS version.
3. **CRS rules** — upstream OWASP CRS rules and setup file of the selected CRS version.
4. **Coraza CoreRuleSet `coraza/v3` module** — upstream Coraza recommended configuration.
5. **Local filesystem** — support for user-provided overrides and custom rules.

## CRS versions

The [`crs`](./crs) package holds the CRS versions available to `crs_version`, and the default one. It registers the
newest CRS release, from the `crs/v4` module of [Coraza CoreRuleSet](https://github.com/corazawaf/coraza-coreruleset),
selected by `latest`, and the CRS LTS release, from its `lts/v4` module, selected by `lts` and the default. The
`lts/v4` module serves its files as `@owasp_crs_lts/` and `@crs-setup-lts.conf.example`; the WAF serves them as
`@owasp_crs/` and `@crs-setup.conf.example`, so that every CRS version is included with the same names.

The versions are not selectable by their exact version: each CRS bump replaces them, so such a configuration would fail
to load after an upgrade.

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
   former version. Start from the upstream `crs-setup.conf.example` (`crs-setup-lts.conf.example` for `lts/v4`) of the
   new version, and apply the customizations of the former file: so far, rule 900120 (`tx.early_blocking=1`) is
   uncommented, and for `lts/v4` also rule 900510 (`tx.crs_xml_attr_inspect=1`, GHSA-6jp8-c2w2-x7wr). The setup files are maintained by hand and can differ per version; the `crs` package tests only check
   that each one belongs to its version.

1. Review the ignored test IDs of the version's FTW list, `tests/e2e/testdata/waf/ftw_latest.yaml` or `ftw_lts.yaml`
   (CRS renumbers and adds regression tests), and set its `# CRS version:` line to the new version: `TestFTW` fails
   until it matches.

1. Update the CRS versions in the `manifest.yaml` documentation.

`latest` and `lts` must stay on different CRS versions: each version is registered once, so the `crs` package panics at
start-up if both aliases name the same one.

Moving the default or removing a CRS version changes the rules of configurations that rely on it: announce it in
advance, so that users can test the new version. A bump moves the `latest` or `lts` alias: list the CRS versions it
moves from and to in the release notes.

## Upgrading Coraza

To upgrade to a new Coraza version:

1. Bump the Coraza dependency version in `go.mod`, together with the `coraza/v3` module of Coraza CoreRuleSet, which provides `@coraza.conf-recommended` for the same Coraza version (tagged as `coraza/vX.Y.Z`):
   ```sh
   go get github.com/corazawaf/coraza/v3@<new-version> github.com/corazawaf/coraza-coreruleset/coraza/v3@<new-version>
   go mod tidy
   ```

1. Update the embedded Coraza configuration file (`@coraza.conf`) if there are any relevant changes in the upstream `coraza.conf-recommended`. The embedded config should be updated while preserving any custom configurations specific to this repository.
