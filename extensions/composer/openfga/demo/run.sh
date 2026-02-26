#!/usr/bin/env bash
# Copyright Built On Envoy
# SPDX-License-Identifier: Apache-2.0
# The full text of the Apache license is available in the LICENSE file at
# the root of the repo.
#
# Runs the OpenFGA extension with a multi-rule config and sends test curls.
# Assumes OpenFGA (with setup.sh already run) is running. Run from the repo root.
#
# Usage:
#   OPENFGA_STORE_ID=<store_id> ./extensions/composer/openfga/demo/run.sh
#   # or
#   ./extensions/composer/openfga/demo/run.sh <store_id>
set -uo pipefail

STORE_ID="${OPENFGA_STORE_ID:-${1:-}}"
if [ -z "$STORE_ID" ]; then
  echo "Usage: OPENFGA_STORE_ID=<id> $0"
  echo "   or: $0 <store_id>"
  echo ""
  echo "Get the store ID from setup.sh output."
  exit 1
fi

ENVOY=http://localhost:10000
REPO_ROOT="$(cd "$(dirname "$0")/../../../.." && pwd)"
EXTENSION_PATH="$REPO_ROOT/extensions/composer/openfga"

# OpenFGA port (default 8082 when using demo docker-compose with port conflict workaround)
OPENFGA_PORT="${OPENFGA_PORT:-8082}"
OPENFGA_ADDR="127.0.0.1:${OPENFGA_PORT}"

# Multi-rule config: AI model rule, MCP tool rule, catch-all resource rule
CONFIG=$(jq -n --arg store "$STORE_ID" --arg addr "$OPENFGA_ADDR" '{
  cluster: $addr,
  openfga_host: $addr,
  store_id: $store,
  user: {header: "x-user-id", prefix: "user:"},
  rules: [
    {match: {headers: {"x-ai-eg-model": "*"}}, relation: {value: "can_use"}, object: {header: "x-ai-eg-model", prefix: "model:"}},
    {match: {headers: {"x-mcp-tool": "*"}}, relation: {value: "can_invoke"}, object: {header: "x-mcp-tool", prefix: "tool:"}},
    {relation: {value: "can_access"}, object: {header: "x-resource-id", prefix: "resource:"}}
  ]
}')

echo "Starting Envoy with OpenFGA extension (store=$STORE_ID)..."
cd "$REPO_ROOT"
boe run --local "$EXTENSION_PATH" \
  --config "$CONFIG" \
  --cluster-insecure "$OPENFGA_ADDR" &
BOE_PID=$!

echo "Waiting for Envoy..."
until curl -sf -H "x-user-id: alice" -H "x-ai-eg-model: gpt-4" "$ENVOY/get" > /dev/null 2>&1; do
  sleep 1
  if ! kill -0 $BOE_PID 2>/dev/null; then
    echo "ERROR: boe process exited"
    exit 1
  fi
done
echo "Envoy is ready."

cleanup() {
  kill $BOE_PID 2>/dev/null || true
}
trap cleanup EXIT

echo ""
echo "=== AI Model: alice -> gpt-4 (allowed) ==="
curl -s -o /dev/null -w "%{http_code}" -H "x-user-id: alice" -H "x-ai-eg-model: gpt-4" "$ENVOY/get"
echo " (expected 200)"

echo ""
echo "=== AI Model: bob -> gpt-4 (denied) ==="
curl -s -o /dev/null -w "%{http_code}" -H "x-user-id: bob" -H "x-ai-eg-model: gpt-4" "$ENVOY/get"
echo " (expected 403)"

echo ""
echo "=== MCP Tool: alice -> github__issue_read (allowed) ==="
curl -s -o /dev/null -w "%{http_code}" -H "x-user-id: alice" -H "x-mcp-tool: github__issue_read" "$ENVOY/get"
echo " (expected 200)"

echo ""
echo "=== MCP Tool: bob -> github__issue_read (denied) ==="
curl -s -o /dev/null -w "%{http_code}" -H "x-user-id: bob" -H "x-mcp-tool: github__issue_read" "$ENVOY/get"
echo " (expected 403)"

echo ""
echo "=== Catch-all resource: alice -> planning (allowed) ==="
curl -s -o /dev/null -w "%{http_code}" -H "x-user-id: alice" -H "x-resource-id: planning" "$ENVOY/get"
echo " (expected 200)"

echo ""
echo "=== Catch-all resource: bob -> planning (denied) ==="
curl -s -o /dev/null -w "%{http_code}" -H "x-user-id: bob" -H "x-resource-id: planning" "$ENVOY/get"
echo " (expected 403)"

echo ""
echo "Demo complete."
