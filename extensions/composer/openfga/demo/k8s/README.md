# OpenFGA Extension — Kubernetes / Envoy Gateway Deployment

This guide deploys the openfga Composer extension on Kubernetes with Envoy Gateway,
using JWT claim injection for user identity so no custom identity headers are required.

## Architecture

```
Client → Envoy Gateway → [JWT authn + claimsToHeaders] → [openfga filter] → Upstream
                                                                  ↕
                                                          OpenFGA (ClusterIP)
```

1. Envoy Gateway's `SecurityPolicy` validates the JWT and injects the `sub` claim as
   `x-user-id` before the filter chain runs.
2. The openfga Composer extension reads `x-user-id`, builds the OpenFGA Check tuple,
   and allows or denies the request inline — no sidecar needed.

## Prerequisites

- Kubernetes cluster with [Envoy Gateway](https://gateway.envoyproxy.io/docs/install/install-helm/) installed
- `kubectl` configured for the cluster
- An OIDC provider (e.g. Auth0, Keycloak, Google) — update `security-policy.yaml` with your issuer URL and JWKS URI

## Step 1: Deploy OpenFGA

```bash
kubectl apply -f openfga-deployment.yaml
kubectl rollout status deployment/openfga -n openfga
```

This creates the `openfga` namespace, a single-replica OpenFGA deployment (in-memory
store for demo; see below for production notes), and a `ClusterIP` service on port 8080.

> **Production note:** The in-memory store loses data on restart. For production, configure
> OpenFGA with a PostgreSQL or MySQL backend by adding the appropriate environment variables
> and a database `Secret`. See the
> [OpenFGA documentation](https://openfga.dev/docs/getting-started/setup-openfga/docker)
> for details.

## Step 2: Set Up the OpenFGA Store

Run the setup `Job` to create the store, authorization model, and initial tuples:

```bash
kubectl apply -f setup-job.yaml
kubectl wait --for=condition=complete job/openfga-setup -n openfga --timeout=60s
kubectl logs -n openfga -l job-name=openfga-setup
```

The Job output includes the store ID. Export it for the next step:

```bash
export STORE_ID=<store_id_from_logs>
```

Update `envoy-extension-policy.yaml` with the store ID (replace `YOUR_STORE_ID`).

## Step 3: Apply the Envoy Extension Policy

The `EnvoyExtensionPolicy` attaches the openfga filter to your `HTTPRoute`.
Update `envoy-extension-policy.yaml`:
- Set `store_id` to your OpenFGA store ID
- Adjust the `targetRef` to match your `HTTPRoute` name and namespace
- Adjust the `rules` to match your authorization model

```bash
kubectl apply -f envoy-extension-policy.yaml
```

## Step 4: Apply the Security Policy

The `SecurityPolicy` configures JWT validation and maps the `sub` claim to the
`x-user-id` header that the openfga filter reads.

Update `security-policy.yaml`:
- Set `issuer` to your OIDC provider's issuer URL
- Set the `uri` under `remoteJWKS` to your provider's JWKS endpoint

```bash
kubectl apply -f security-policy.yaml
```

## Step 5: Verify

Send a request with a valid JWT to your route. The `x-user-id` header is injected
automatically from the `sub` claim — no manual header needed:

```bash
TOKEN=$(curl -s -X POST https://your-idp/oauth/token \
  -d "grant_type=client_credentials&client_id=...&client_secret=..." | jq -r .access_token)

# Allowed (if the JWT sub has the required relation in OpenFGA)
curl -H "Authorization: Bearer $TOKEN" https://your-gateway/api/documents/planning

# Denied (if no relation exists)
curl -H "Authorization: Bearer $TOKEN" https://your-gateway/api/documents/restricted
```

Check metrics from the Envoy admin endpoint:

```bash
kubectl port-forward -n envoy-gateway-system svc/envoy-<gateway-name> 9901:9901
curl -s http://localhost:9901/stats | grep openfga
# openfga_requests_total{decision="allowed"} ...
# openfga_check_duration_ms ...
```

## Envoy AI Gateway (Local Cluster)

For a local Kubernetes cluster with [Envoy AI Gateway](https://aigateway.envoyproxy.io/) and the OpenFGA plugin, use the automated setup script:

### Prerequisites

- **Kubernetes 1.32+** (kind v0.27+ defaults to 1.32.2)
- **kind**, **helm**, **kubectl**, **curl**, **jq**

### One-Command Setup

```bash
./setup-ai-gateway.sh
```

This creates a kind cluster, installs Envoy Gateway with AI Gateway integration, deploys OpenFGA, runs the setup job, deploys the basic AI Gateway example, and attaches the openfga filter via `EnvoyExtensionPolicy`.

**Options:**
- `--no-cluster` — Use existing cluster instead of creating one
- `--force-recreate` — Delete existing cluster and recreate (non-interactive; useful for CI/agents)

**Automated tests:**
```bash
./run-openfga-tests.sh
```
Runs alice (200) and bob (403) authorization tests. Exit 0 = pass.

### Manual Steps (Alternative)

1. Create cluster: `kind create cluster --name envoy-ai-openfga`
2. Install Envoy Gateway with AI Gateway values (see [Envoy AI Gateway prerequisites](https://aigateway.envoyproxy.io/docs/getting-started/prerequisites))
3. Install Envoy AI Gateway CRDs and controller
4. Apply `ai-gateway-openfga.yaml`
5. Wait for setup job, extract store ID from logs
6. Apply basic example: `kubectl apply -f https://raw.githubusercontent.com/envoyproxy/ai-gateway/v0.4.0/examples/basic/basic.yaml`
7. Apply `envoy-extension-policy-ai-gateway.yaml` with store ID substituted

### Test

```bash
kubectl port-forward -n envoy-gateway-system svc/eg-envoy-ai-gateway-basic 8080:80
export GATEWAY_URL=http://localhost:8080

# Allowed (alice has can_use on some-cool-self-hosted-model)
curl -H "x-user-id: alice" -H "x-ai-eg-model: some-cool-self-hosted-model" \
  -H "Content-Type: application/json" \
  -d '{"model":"some-cool-self-hosted-model","messages":[{"role":"user","content":"Hi"}]}' \
  $GATEWAY_URL/v1/chat/completions
# Expected: 200

# Denied (bob has no relations)
curl -H "x-user-id: bob" -H "x-ai-eg-model: some-cool-self-hosted-model" \
  -H "Content-Type: application/json" \
  -d '{"model":"some-cool-self-hosted-model","messages":[{"role":"user","content":"Hi"}]}' \
  $GATEWAY_URL/v1/chat/completions
# Expected: 403
```

## Files

| File | Purpose |
|------|---------|
| `openfga-deployment.yaml` | OpenFGA `Namespace`, `Deployment`, and `Service` |
| `setup-job.yaml` | One-shot `Job` to create the store, model, and tuples |
| `envoy-extension-policy.yaml` | `EnvoyExtensionPolicy` attaching the openfga filter (generic HTTPRoute) |
| `envoy-extension-policy-ai-gateway.yaml` | `EnvoyExtensionPolicy` for Envoy AI Gateway (targets AIGatewayRoute-generated HTTPRoute) |
| `security-policy.yaml` | `SecurityPolicy` for JWT validation and claim injection |
| `ai-gateway-openfga.yaml` | Combined OpenFGA deployment + setup job for AI Gateway demo |
| `setup-ai-gateway.sh` | End-to-end setup script for local Envoy AI Gateway + OpenFGA |
| `run-openfga-tests.sh` | Runs alice/bob authorization tests (exit 0 = pass) |

## Troubleshooting

- **Filter not applied:** Check that `targetRef` in `EnvoyExtensionPolicy` matches the
  exact name and namespace of your `HTTPRoute`. For Envoy AI Gateway, the HTTPRoute
  name matches the AIGatewayRoute name (e.g. `envoy-ai-gateway-basic`).
- **JWT rejected:** Verify `issuer` and `remoteJWKS.uri` match your provider's discovery
  document (`/.well-known/openid-configuration`).
- **All requests denied:** Check that OpenFGA tuples exist for your test user. Review
  Envoy logs: `kubectl logs -n envoy-gateway-system deploy/envoy-<gateway-name>`.
- **OpenFGA unreachable:** Verify the `openfga` Service is reachable from the Envoy
  pod: `kubectl exec -n envoy-gateway-system deploy/envoy-<name> -- curl http://openfga.openfga.svc.cluster.local:8080/healthz`.
- **Envoy AI Gateway / Kubernetes 1.32:** Envoy AI Gateway requires Kubernetes 1.32+.
  Use `kind create cluster` with kind v0.27+ (defaults to 1.32.2) or ensure your cluster
  meets the version requirement.
- **Envoy pods not ready:** The setup uses Envoy Gateway v0.0.0-latest (main) for
  dynamicModule/dynamicModules support. If pods fail to appear, check the diagnostics
  printed on failure (GatewayClass/Gateway status, Envoy Gateway controller logs).
- **EnvoyExtensionPolicy "unknown field dynamicModule":** Envoy Gateway v1.6.x does not
  support dynamicModule. Use v0.0.0-latest (main) which includes this feature.
