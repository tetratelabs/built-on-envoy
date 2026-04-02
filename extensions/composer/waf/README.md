# WAF Extension

This extension implements a Web Application Firewall using [OWASP Coraza](https://coraza.io/) and comes with rules from the [OWASP Core Rule Set (CRS)](https://coreruleset.org/) already embedded and ready to use.

## Rule Files

The WAF resolves rule files from three layered filesystems (first match wins):

1. **Embedded rules** — tailored configurations shipped with this extension.
2. **Coraza CoreRuleSet package** — upstream OWASP CRS rules.
3. **Local filesystem** — support for user-provided overrides and custom rules.

## Upgrading CRS

The CRS rules are provided by the [Coraza CoreRuleSet package](https://github.com/corazawaf/coraza-coreruleset). To upgrade:

1. Bump the CRS dependency version in `go.mod`. You can find the latest CRS version on the [coraza-coreruleset releases page](https://github.com/corazawaf/coraza-coreruleset/releases):
```sh
go get github.com/corazawaf/coraza-coreruleset/v4@<new-version>
go mod tidy
```

1. Update the embedded CRS configuration file (`@crs-setup.conf`) if there are any relevant changes in the upstream `crs-setup.conf.example`. The embedded config should be updated while preserving any custom configurations specific to this repository.

## Upgrading Coraza

To upgrade to a new Coraza version:

1. Bump the Coraza dependency version in `go.mod`:
   ```sh
   go get github.com/corazawaf/coraza/v3@<new-version>
   go mod tidy
   ```

1. Update the embedded Coraza configuration file (`@coraza.conf`) if there are any relevant changes in the upstream `coraza.conf-recommended`. The embedded config should be updated while preserving any custom configurations specific to this repository.
