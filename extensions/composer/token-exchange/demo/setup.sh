#!/usr/bin/env bash
# Copyright Built On Envoy
# SPDX-License-Identifier: Apache-2.0
# The full text of the Apache license is available in the LICENSE file at
# the root of the repo.
#
# Configures Keycloak for the OAuth2 token exchange demo.
# Run once after "docker compose up" and Keycloak is healthy.
#
# Keycloak requires explicit authorization before a client can perform token
# exchanges. This is Keycloak-specific plumbing, not part of the RFC 8693
# standard itself.
set -uo pipefail

KC=http://localhost:8080

echo "Waiting for Keycloak..."
until curl -sf "$KC/realms/demo" > /dev/null 2>&1; do sleep 2; done
echo "Keycloak is ready."

# Get admin access token
ADMIN_KC_TOKEN=""
retries=0
max_retries=3
while [ -z "$ADMIN_KC_TOKEN" ] || [ "$ADMIN_KC_TOKEN" = "null" ]; do
  retries=$((retries + 1))
  if [ "$retries" -gt $max_retries ]; then
    echo "ERROR: failed to obtain admin token after $max_retries attempts. Is Keycloak reachable?"
    exit 1
  fi
  [ "$retries" -gt 1 ] && echo "  retrying in 3s..." && sleep 3
  ADMIN_KC_TOKEN=$(curl -s "$KC/realms/master/protocol/openid-connect/token" \
    -d grant_type=password -d client_id=admin-cli \
    -d username=admin -d password=admin | jq -r .access_token)
done

auth() { echo "Authorization: Bearer $ADMIN_KC_TOKEN"; }

get_client_id() {
  curl -s "$KC/admin/realms/demo/clients?clientId=$1" -H "$(auth)" | jq -r '.[0].id'
}

MY_APP_ID=$(get_client_id my-app)
GATEWAY_ID=$(get_client_id gateway)
HTTPBIN_ID=$(get_client_id httpbin)
REALM_MGMT_ID=$(get_client_id realm-management)

echo "my-app           id: $MY_APP_ID"
echo "gateway          id: $GATEWAY_ID"
echo "httpbin          id: $HTTPBIN_ID"
echo "realm-management id: $REALM_MGMT_ID"

# 1. Add audience mapper so tokens from my-app include "gateway" in "aud".
echo ""
echo "Adding audience mapper to my-app..."
RESP=$(curl -s -w "\n%{http_code}" \
  "$KC/admin/realms/demo/clients/$MY_APP_ID/protocol-mappers/models" \
  -H "$(auth)" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "gateway-audience",
    "protocol": "openid-connect",
    "protocolMapper": "oidc-audience-mapper",
    "consentRequired": false,
    "config": {
      "included.client.audience": "gateway",
      "id.token.claim": "false",
      "access.token.claim": "true"
    }
  }')
HTTP_CODE=$(echo "$RESP" | tail -1)
if [ "$HTTP_CODE" = "201" ]; then echo "created."
elif [ "$HTTP_CODE" = "409" ]; then echo "already exists (OK)."
else echo "unexpected response ($HTTP_CODE): $(echo "$RESP" | head -1)"; fi

# 2. Enable fine-grained permissions on my-app and httpbin.
#    This must happen before creating policies, because it initializes the
#    authorization resource server on realm-management.
for client_name in my-app httpbin; do
  if [ "$client_name" = "my-app" ]; then cid=$MY_APP_ID; else cid=$HTTPBIN_ID; fi
  echo ""
  echo "Enabling permissions on $client_name..."
  curl -s -o /dev/null \
    -X PUT "$KC/admin/realms/demo/clients/$cid/management/permissions" \
    -H "$(auth)" \
    -H "Content-Type: application/json" \
    -d '{"enabled": true}'
  echo "done."
done

# 3. Create a client policy that matches gateway.
echo ""
echo "Creating client policy for gateway..."
POLICY_PAYLOAD=$(jq -n --arg cid "$GATEWAY_ID" '{
  "name": "gateway-exchange-policy",
  "type": "client",
  "logic": "POSITIVE",
  "decisionStrategy": "UNANIMOUS",
  "clients": [$cid]
}')
RESP=$(curl -s -w "\n%{http_code}" \
  "$KC/admin/realms/demo/clients/$REALM_MGMT_ID/authz/resource-server/policy/client" \
  -H "$(auth)" \
  -H "Content-Type: application/json" \
  -d "$POLICY_PAYLOAD")
HTTP_CODE=$(echo "$RESP" | tail -1)
if [ "$HTTP_CODE" = "201" ]; then
  POLICY_ID=$(echo "$RESP" | head -1 | jq -r '.id')
  echo "created (id: $POLICY_ID)."
elif [ "$HTTP_CODE" = "409" ]; then
  POLICY_ID=$(curl -s \
    "$KC/admin/realms/demo/clients/$REALM_MGMT_ID/authz/resource-server/policy?name=gateway-exchange-policy" \
    -H "$(auth)" | jq -r '.[0].id')
  echo "already exists (id: $POLICY_ID)."
else
  echo "unexpected response ($HTTP_CODE): $(echo "$RESP" | head -1)"
  exit 1
fi

# 4. Assign the gateway policy to token-exchange permissions on both clients.
for client_name in my-app httpbin; do
  if [ "$client_name" = "my-app" ]; then cid=$MY_APP_ID; else cid=$HTTPBIN_ID; fi
  echo ""
  echo "Assigning policy to $client_name token-exchange permission..."

  perm_id=$(curl -s \
    "$KC/admin/realms/demo/clients/$cid/management/permissions" \
    -H "$(auth)" | jq -r '.scopePermissions."token-exchange"')

  if [ "$perm_id" = "null" ] || [ -z "$perm_id" ]; then
    echo "  ERROR: no token-exchange permission found for $client_name."
    continue
  fi
  echo "  permission id: $perm_id"

  perm_json=$(curl -s \
    "$KC/admin/realms/demo/clients/$REALM_MGMT_ID/authz/resource-server/permission/scope/$perm_id" \
    -H "$(auth)")
  updated=$(echo "$perm_json" | jq --arg pid "$POLICY_ID" '.policies = [$pid] | .decisionStrategy = "AFFIRMATIVE"')

  RESP=$(curl -s -w "\n%{http_code}" \
    -X PUT "$KC/admin/realms/demo/clients/$REALM_MGMT_ID/authz/resource-server/permission/scope/$perm_id" \
    -H "$(auth)" \
    -H "Content-Type: application/json" \
    -d "$updated")
  HTTP_CODE=$(echo "$RESP" | tail -1)
  if [ "$HTTP_CODE" = "201" ] || [ "$HTTP_CODE" = "200" ]; then
    echo "  done."
  else
    echo "  unexpected response ($HTTP_CODE): $(echo "$RESP" | head -1)"
  fi
done

echo ""
echo "Setup complete. Get a token with:"
echo ""
echo "  TOKEN=\$(curl -s $KC/realms/demo/protocol/openid-connect/token \\"
echo "    -d grant_type=password -d client_id=my-app \\"
echo "    -d client_secret=my-app-secret \\"
echo "    -d username=testuser -d password=password | jq -r .access_token)"
