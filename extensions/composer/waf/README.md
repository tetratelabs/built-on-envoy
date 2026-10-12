# WAF Extension

This extension implements a Web Application Firewall using [OWASP Coraza](https://coraza.io/) and comes with rules from the [OWASP Core Rule Set (CRS)](https://coreruleset.org/) already embedded and ready to use.

## Rule Files

The WAF resolves rule files from four layered filesystems (first match wins):

1. **Embedded rules** — tailored configurations shipped with this extension.
2. **Coraza CoreRuleSet `crs/v4` module** — upstream OWASP CRS rules and setup file.
3. **Coraza CoreRuleSet `coraza/v3` module** — upstream Coraza recommended configuration.
4. **Local filesystem** — support for user-provided overrides and custom rules.

## Upgrading CRS

The CRS rules are provided by the `crs/v4` module of [Coraza CoreRuleSet](https://github.com/corazawaf/coraza-coreruleset). To upgrade:

1. Bump the CRS module version in `go.mod`. Its versions follow the upstream CRS releases and are tagged as `crs/vX.Y.Z` (see the [coraza-coreruleset tags](https://github.com/corazawaf/coraza-coreruleset/tags)):
```sh
go get github.com/corazawaf/coraza-coreruleset/crs/v4@<new-version>
go mod tidy
```

1. Update the embedded CRS configuration file (`@crs-setup.conf`) if there are any relevant changes in the upstream `crs-setup.conf.example`. The embedded config should be updated while preserving any custom configurations specific to this repository.

## Upgrading Coraza

To upgrade to a new Coraza version:

1. Bump the Coraza dependency version in `go.mod`, together with the `coraza/v3` module of Coraza CoreRuleSet, which provides `@coraza.conf-recommended` for the same Coraza version (tagged as `coraza/vX.Y.Z`):
   ```sh
   go get github.com/corazawaf/coraza/v3@<new-version> github.com/corazawaf/coraza-coreruleset/coraza/v3@<new-version>
   go mod tidy
   ```

1. Update the embedded Coraza configuration file (`@coraza.conf`) if there are any relevant changes in the upstream `coraza.conf-recommended`. The embedded config should be updated while preserving any custom configurations specific to this repository.
