#!/usr/bin/env bash
#
# Build, install, and run the soap-rest extension with test configuration.
#
# Usage:
#   bash buildandrun.sh           # Build + install + run
#   bash buildandrun.sh --no-config  # Run without config (defaults only)
#

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BOE_BIN="${BOE_BIN:-$(cd "$SCRIPT_DIR"/../../.. && pwd)/cli/out/boe}"

if [[ ! -x "$BOE_BIN" ]]; then
  echo "Error: boe binary not found at $BOE_BIN"
  echo "Set BOE_BIN to the path of your boe binary, or ensure it is built."
  exit 1
fi

cd "$SCRIPT_DIR"

echo "==> Building soap-rest plugin..."
make build

echo "==> Installing soap-rest plugin..."
make install

echo "==> Starting boe with soap-rest extension..."
echo ""

if [[ "${1:-}" == "--no-config" ]]; then
  exec "$BOE_BIN" run --local . --log-level all:info
else
  exec "$BOE_BIN" run --local . --log-level all:info --config '{"operations":{"GetUser":{"restMethod":"GET","restPath":"/get"},"CreateUser":{"restMethod":"POST","restPath":"/post"},"CreateOrder":{"restMethod":"POST","restPath":"/post"},"SearchProducts":{"restMethod":"POST","restPath":"/post"},"Ping":{"restMethod":"POST","restPath":"/post"}},"defaults":{"restMethod":"POST","restPathPrefix":"/api","soapEndpoint":"/post","soapNamespace":"http://example.com/services"}}'
fi
