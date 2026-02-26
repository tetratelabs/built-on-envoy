#!/usr/bin/env bash
# Copyright Built On Envoy
# SPDX-License-Identifier: Apache-2.0
# The full text of the Apache license is available in the LICENSE file at
# the root of the repo.
#
# Configures OpenFGA for the demo: creates a store, authorization model, and
# relationship tuples. Run once after "docker compose up" and OpenFGA is healthy.
set -uo pipefail

OPENFGA="${OPENFGA_URL:-http://localhost:8082}"

echo "Waiting for OpenFGA..."
until curl -sf "$OPENFGA/stores" > /dev/null 2>&1; do sleep 2; done
echo "OpenFGA is ready."

# 1. Create a store
echo ""
echo "Creating store..."
STORE_RESP=$(curl -s -X POST "$OPENFGA/stores" \
  -H "Content-Type: application/json" \
  -d '{"name": "OpenFGA Demo Store"}')
STORE_ID=$(echo "$STORE_RESP" | jq -r '.id')
if [ -z "$STORE_ID" ] || [ "$STORE_ID" = "null" ]; then
  echo "ERROR: failed to create store. Response: $STORE_RESP"
  exit 1
fi
echo "Store created: $STORE_ID"

# 2. Write authorization model (user, model, tool with can_use and can_invoke)
echo ""
echo "Writing authorization model..."
MODEL_RESP=$(curl -s -X POST "$OPENFGA/stores/$STORE_ID/authorization-models" \
  -H "Content-Type: application/json" \
  -d '{
    "schema_version": "1.1",
    "type_definitions": [
      {"type": "user"},
      {
        "type": "model",
        "relations": {"can_use": {"this": {}}},
        "metadata": {"relations": {"can_use": {"directly_related_user_types": [{"type": "user"}]}}}
      },
      {
        "type": "tool",
        "relations": {"can_invoke": {"this": {}}},
        "metadata": {"relations": {"can_invoke": {"directly_related_user_types": [{"type": "user"}]}}}
      },
      {
        "type": "resource",
        "relations": {"can_access": {"this": {}}},
        "metadata": {"relations": {"can_access": {"directly_related_user_types": [{"type": "user"}]}}}
      }
    ]
  }')
MODEL_ID=$(echo "$MODEL_RESP" | jq -r '.authorization_model_id')
if [ -z "$MODEL_ID" ] || [ "$MODEL_ID" = "null" ]; then
  echo "ERROR: failed to create authorization model. Response: $MODEL_RESP"
  exit 1
fi
echo "Authorization model created: $MODEL_ID"

# 3. Write relationship tuples
# user:alice can_use model:gpt-4
# user:alice can_invoke tool:github__issue_read
# user:bob has no relations
echo ""
echo "Writing relationship tuples..."
WRITE_RESP=$(curl -s -X POST "$OPENFGA/stores/$STORE_ID/write" \
  -H "Content-Type: application/json" \
  -d "{
    \"authorization_model_id\": \"$MODEL_ID\",
    \"writes\": {
      \"tuple_keys\": [
        {\"user\": \"user:alice\", \"relation\": \"can_use\", \"object\": \"model:gpt-4\"},
        {\"user\": \"user:alice\", \"relation\": \"can_invoke\", \"object\": \"tool:github__issue_read\"},
        {\"user\": \"user:alice\", \"relation\": \"can_access\", \"object\": \"resource:planning\"}
      ]
    }
  }")
if echo "$WRITE_RESP" | jq -e '.error' > /dev/null 2>&1; then
  echo "ERROR: failed to write tuples. Response: $WRITE_RESP"
  exit 1
fi
echo "Tuples written successfully."

echo ""
echo "=============================================="
echo "Setup complete. Use these values for the demo:"
echo "  STORE_ID=$STORE_ID"
echo "  MODEL_ID=$MODEL_ID"
echo ""
echo "Export for run.sh:"
echo "  export OPENFGA_STORE_ID=$STORE_ID"
echo "=============================================="
