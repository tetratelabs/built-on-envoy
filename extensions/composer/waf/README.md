# WAF Extension

This extension implements a Web Application Firewall using [OWASP Coraza](https://coraza.io/) and comes with rules from the [OWASP Core Rule Set (CRS)](https://coreruleset.org/) already embedded and ready to use.

## Upgrading CRS

The CRS rules are located under [coraza/rules/](coraza/rules/) and embedded into the binary at build time in [coraza/rule_fs.go](coraza/rule_fs.go) via Go's `//go:embed` directive.

To upgrade to a new CRS version:

1. Download the new CRS release minimal archive from the [coreruleset releases page](https://github.com/coreruleset/coreruleset/releases).
1. Replace the contents of `coraza/rules/owasp_crs/` with the `.conf` and `.data` files from the new release `rules/` directory.
1. Update `coraza/rules/crs-setup.conf` with the new `crs-setup.conf.example` available in the new release root folder, reviewing any changes and merging them into the existing `crs-setup.conf` as needed. Note that some configurations are specific for this repository, and this should not be overwritten.

Or as a one-liner:

```bash
CRS_VERSION="4.25.0"; TMP_DIR="/tmp/coreruleset-v$CRS_VERSION"; \
mkdir -p "$TMP_DIR" && \
curl -fsSL "https://github.com/coreruleset/coreruleset/releases/download/v$CRS_VERSION/coreruleset-$CRS_VERSION-minimal.tar.gz" -o "$TMP_DIR/crs.tar.gz" && \
tar -xzf "$TMP_DIR/crs.tar.gz" -C "$TMP_DIR" && \
find coraza/rules/owasp_crs -type f \( -name '*.conf' -o -name '*.data' \) -delete && \
mv "$TMP_DIR/coreruleset-$CRS_VERSION/rules/"*.conf "$TMP_DIR/coreruleset-$CRS_VERSION/rules/"*.data coraza/rules/owasp_crs/ && \
mv "$TMP_DIR/coreruleset-$CRS_VERSION/crs-setup.conf.example" coraza/rules/crs-setup.conf.example.upstream && \
rm -rf "$TMP_DIR"
```

> [!NOTE]
> Manual review is needed afterward. Some configurations in this code are specific to the tetratelabs/built-on-envoy repository and should not be overwritten when making updates or migrations. Please ensure that repository-specific settings are preserved during any refactoring or dependency updates.

## Upgrading Coraza

To upgrade to a new Coraza version:

1. Bump the Coraza dependency version in `go.mod` to the new version. You can find the latest Coraza version on the [Coraza releases page](https://github.com/corazawaf/coraza/releases).
   ```sh
   go get github.com/corazawaf/coraza/v3@<new-version>
   go mod tidy
   ```
1. Update `coraza/rules/recommended.conf` with the new `recommended.conf.example` available in upstream repository at [coraza.conf-recommended](https://github.com/corazawaf/coraza/blob/main/coraza.conf-recommended) checking out the new version. Review any changes and merge them into the existing `recommended.conf` as needed. Note that some configurations are specific for this repository, and this should not be overwritten.
