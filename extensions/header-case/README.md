# Header Case

An HTTP/1 header formatter that controls the casing of header names Envoy writes on the wire.

Envoy lowercases header names. That's valid HTTP, but some legacy clients and servers only accept a
specific casing. This extension uses Envoy's dynamic module header formatter
(`envoy.http.stateful_header_formatters.dynamic_modules`), which first shipped in Envoy 1.40.

## Configuration

| Field | Values | Default | Description |
| ----- | ------ | ------- | ----------- |
| `mode` | `preserve`, `proper_case`, `lowercase` | `preserve` | `preserve` keeps the casing the peer sent. `proper_case` writes `X-Request-Id`. `lowercase` is Envoy's default. |
| `fallback` | `proper_case`, `lowercase` | `proper_case` | Casing for names the peer never sent, such as headers Envoy adds. Only valid with `preserve`. |
| `overrides` | map of name to spelling | none | Exact spellings such as `{"content-type": "Content-type"}`. Overrides win over the mode and may only change casing. |

```bash
boe run --local extensions/header-case --config '
  {
    "mode": "preserve",
    "overrides": {"content-type": "Content-type"}
  }'
```

## Where it applies

The CLI sets the formatter in two places, because Envoy picks the formatter from the side a message
was received on:

- On the HTTP connection manager, it formats request headers sent to the upstream.
- On each HTTP/1 upstream cluster, it formats response headers written back to the client.
  Clusters that already set HTTP protocol options (for example HTTP/2 clusters) are left alone.

Only one header formatter can be enabled per Envoy. It affects HTTP/1 only. Trailers and the
response reason phrase aren't changed.

## Testing

```bash
make -C extensions check EXTENSION_PATH=header-case
make -C extensions/tests/e2e test GO_TEST_ARGS="-run TestHeaderCase -v" ENVOY_VERSION=1.40.0
```
