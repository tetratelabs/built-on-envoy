#!/usr/bin/env bash
# Copyright Built On Envoy
# SPDX-License-Identifier: Apache-2.0
#
# Runs OpenFGA authorization tests (alice allowed, bob denied) against the
# Envoy AI Gateway. Call after setup-ai-gateway.sh completes.
#
# Usage:
#   ./run-openfga-tests.sh              # Discovers GW_SVC via kubectl
#   GW_SVC=my-svc ./run-openfga-tests.sh  # Use specific service name
#
# Exit: 0 if both tests pass, 1 otherwise
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CLUSTER_NAME="${CLUSTER_NAME:-envoy-ai-openfga}"
GATEWAY_URL="${GATEWAY_URL:-http://localhost:8080}"
MODEL="some-cool-self-hosted-model"

# Discover gateway service if not set
if [[ -z "${GW_SVC:-}" ]]; then
  GW_SVC=$(kubectl get svc -n envoy-gateway-system \
    -l "gateway.envoyproxy.io/owning-gateway-name=envoy-ai-gateway-basic,gateway.envoyproxy.io/owning-gateway-namespace=default" \
    -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)
  if [[ -z "$GW_SVC" ]]; then
    echo "ERROR: Could not find gateway service. Run setup-ai-gateway.sh first."
    exit 1
  fi
fi

# Start port-forward in background
echo "Starting port-forward for $GW_SVC..."
kubectl port-forward -n envoy-gateway-system "svc/$GW_SVC" 8080:80 &
PF_PID=$!
trap "kill $PF_PID 2>/dev/null || true" EXIT

# Brief wait for port-forward to establish
sleep 3

# Test alice (expected 200)
ALICE_CODE=$(curl -s -o /dev/null -w '%{http_code}' \
  -H "x-user-id: alice" \
  -H "x-ai-eg-model: $MODEL" \
  -H "Content-Type: application/json" \
  -d "{\"model\":\"$MODEL\",\"messages\":[{\"role\":\"user\",\"content\":\"Hi\"}]}" \
  "$GATEWAY_URL/v1/chat/completions")

# Test bob (expected 403)
BOB_CODE=$(curl -s -o /dev/null -w '%{http_code}' \
  -H "x-user-id: bob" \
  -H "x-ai-eg-model: $MODEL" \
  -H "Content-Type: application/json" \
  -d "{\"model\":\"$MODEL\",\"messages\":[{\"role\":\"user\",\"content\":\"Hi\"}]}" \
  "$GATEWAY_URL/v1/chat/completions")

echo "alice: $ALICE_CODE (expected 200)"
echo "bob:   $BOB_CODE (expected 403)"

if [[ "$ALICE_CODE" == "200" && "$BOB_CODE" == "403" ]]; then
  echo "PASS: OpenFGA authorization tests succeeded"
  exit 0
else
  echo "FAIL: OpenFGA authorization tests failed"
  exit 1
fi
