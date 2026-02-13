# OAuth2 Token Exchange Demo

This demo uses **impersonation** semantics (RFC 8693, Section 1.1): the gateway
authenticates with its own credentials and exchanges the user's token, but the
resulting token only identifies the user — httpbin has no indication that a
gateway was involved. This is in contrast to **delegation**, where the token
would carry both identities via the `act` claim (Section 4.1).

1. A user authenticates through `my-app` and gets a general-purpose token.
2. The request passes through Envoy that needs to call an endpoint (`httpbin`) on behalf of the user.
3. Envoy uses its own credentials to authenticate to the STS (Security Token Service, Keycloak in this demo) and performs a token exchange,
   asking the STS to exchange this user's token for one targeting the `httpbin` audience.
4. The STS issues a new token that still identifies the user (`sub: testuser`) but is now scoped for `httpbin`. The gateway is
   impersonating the user, and it is transparent to the backend. No `actor_token` is sent, so the STS treats this as impersonation rather than delegation.
5. The request is sent to httpbin which prints the headers, including the `Authorization` header with the exchanged token.

```
User ──Bearer token──▶ Envoy (gateway) ──exchanged token──▶ httpbin
      (sub: testuser        │              (sub: testuser
       aud: account)        │               aud: httpbin)
                            ▼
                      STS (Keycloak)
                      gateway authenticates with its
                      own credentials, exchanges the
                      user's token for audience=httpbin
```

## Keycloak clients

| Client | RFC Role | Description |
|---|---|---|
| `my-app` | Token issuer | The app the user authenticates with. Produces the original token. |
| `gateway` | Intermediary (client) | The Envoy gateway's identity. Authenticates to the STS to perform the exchange. |
| `httpbin` | Target audience | Represents the backend service. The exchanged token targets this audience. |

## 1. Start Keycloak

```bash
cd extensions/oauth2/demo
docker compose up
```

## 2. Configure Keycloak for token exchange

In another terminal, configure Keycloak. It requires explicit authorization before a client can perform token exchanges.
The setup script configures it via the Keycloak APIs:

```bash
cd extensions/oauth2/demo
./setup.sh
```
## 3. Run Envoy with the oauth2 extension

The extension is configured with `gateway` credentials (the intermediary) and `audience=httpbin` (the target service). For every request, it tells the STS that the the gateway has to exchange this user's token for one targeting httpbin:

```bash
boe run --local extensions/oauth2 --config '{"cluster":"sts_server","token_exchange_endpoint":"/realms/demo/protocol/openid-connect/token","token_exchange_host":"localhost:8080","client_id":"gateway","client_secret":"gateway-secret","audience":"httpbin"}' \
  --cluster '{"name":"sts_server","type":"STRICT_DNS","dns_lookup_family":"V4_ONLY","load_assignment":{"cluster_name":"sts_server","endpoints":[{"lb_endpoints":[{"endpoint":{"address":{"socket_address":{"address":"127.0.0.1","port_value":8080}}}}]}]}}'
```

> **Note:** The `--cluster` uses a full JSON definition to use plain HTTP connecting to dev Keycloak

## 4. Test the token exchange

In another terminal, obtain a token and send it through Envoy.

You can run `./run.sh` to do this automatically, or follow the manual steps below.

### Obtain an initial access token

Get a token as `testuser` through `my-app`:

```bash
TOKEN=$(curl -s http://localhost:8080/realms/demo/protocol/openid-connect/token \
  -d grant_type=password -d client_id=my-app -d client_secret=my-app-secret \
  -d username=testuser -d password=password | jq -r .access_token) && echo "$TOKEN"
```

### Send a request through Envoy

```bash
curl -s http://localhost:10000/headers -H "Authorization: Bearer $TOKEN" | jq .
```

The `Authorization` header seen by httpbin should contain the **exchanged**
token. Decode both to verify the exchange:

```bash
# Original token — issued to my-app, general-purpose
echo "$TOKEN" | cut -d. -f2 | tr '_-' '/+' | awk '{while(length%4)$0=$0"=";print}' | base64 -d | jq '{sub, azp, aud}'

# Exchanged token — issued by gateway, targeting httpbin
EXCHANGED=$(curl -s http://localhost:10000/headers -H "Authorization: Bearer $TOKEN" \
  | jq -r '.headers.Authorization' | sed 's/Bearer //')
echo "$EXCHANGED" | cut -d. -f2 | tr '_-' '/+' | awk '{while(length%4)$0=$0"=";print}' | base64 -d | jq '{sub, azp, aud}'
```

- **`sub`** is the same — the gateway is impersonating the same user
- **`azp`** changed from `my-app` to `gateway` — the new token was issued to the gateway, the intermediary that performed the exchange
- **`aud`** changed to `httpbin` — the new token targets the backend service, which is what we asked for with `audience=httpbin` in the extension config

## Cleanup

```bash
docker compose down -v
```
