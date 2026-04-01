# WAF Extension

This extension implements a Web Application Firewall using [OWASP Coraza](https://coraza.io/) and comes with rules from the [OWASP Core Rule Set (CRS)](https://coreruleset.org/) already embedded and ready to use.

## Upgrading CRS

The CRS rules are embedded into the binary at build time using [Coraza Corerulset package](https://github.com/corazawaf/coraza-coreruleset). We provide sensible defaults that you can override.

1. Bump the `coraza-corerulese` dependency version in `go.mod`. You can find the latest CRS version on the [coraza-coreruleset releases page](https://github.com/corazawaf/coraza-coreruleset/releases).
   ```sh
   go get github.com/corazawaf/coraza-coreruleset/v4@<new-version>
   go mod tidy
   ```

1. Update `coraza/rules/crs-setup.conf` with the new `crs-setup.conf.example` available in upstream repository at [crs-setup.conf.example](https://github.com/corazawaf/coraza-coreruleset/blob/main/rules/%40crs-setup.conf.example) checking out the new version. Review any changes and merge them into the existing `crs-setup.conf` as needed. Note that some configurations are specific for this repository, and this should not be overwritten.

## Upgrading Coraza

To upgrade to a new Coraza version:

1. Bump the Coraza dependency version in `go.mod` to the new version. You can find the latest Coraza version on the [Coraza releases page](https://github.com/corazawaf/coraza/releases).
   ```sh
   go get github.com/corazawaf/coraza/v3@<new-version>
   go mod tidy
   ```
1. Update `coraza/rules/recommended.conf` with the new `recommended.conf.example` available in upstream repository at [coraza.conf-recommended](https://github.com/corazawaf/coraza/blob/main/coraza.conf-recommended) checking out the new version. Review any changes and merge them into the existing `recommended.conf` as needed. Note that some configurations are specific for this repository, and this should not be overwritten.
