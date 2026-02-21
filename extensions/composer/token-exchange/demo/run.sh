#!/usr/bin/env bash
# Copyright Built On Envoy
# SPDX-License-Identifier: Apache-2.0
# The full text of the Apache license is available in the LICENSE file at
# the root of the repo.
#
# Obtains a token and tests the token exchange flow.
# Assumes Keycloak (with setup.sh already run) and Envoy are running.
set -uo pipefail

KC=http://localhost:8080
ENVOY=http://localhost:10000

# 1. Get a token as testuser through my-app.
echo "Obtaining access token..."
TOKEN=$(curl -s "$KC/realms/demo/protocol/openid-connect/token" \
  -d grant_type=password -d client_id=my-app \
  -d client_secret=my-app-secret \
  -d username=testuser -d password=password | jq -r .access_token)

if [ "$TOKEN" = "null" ] || [ -z "$TOKEN" ]; then
  echo "ERROR: failed to obtain token."
  exit 1
fi
echo "Token obtained."

# 2. Test the token exchange flow.
jwt_payload() { echo "$1" | cut -d. -f2 | tr '_-' '/+' | awk '{while(length%4)$0=$0"=";print}' | base64 -d; }

echo ""
echo "=== Original token ==="
echo "$TOKEN"
jwt_payload "$TOKEN" | jq '{sub, azp, aud}'

echo ""
echo "=== Exchanged token ==="
EXCHANGED=$(curl -s "$ENVOY/headers" -H "Authorization: Bearer $TOKEN" \
  | jq -r '.headers.Authorization' | sed 's/Bearer //')
echo "$EXCHANGED"
jwt_payload "$EXCHANGED" | jq '{sub, azp, aud}'

echo ""
echo "Token exchange successful."
