#!/usr/bin/env bash
# Copyright Built On Envoy
# SPDX-License-Identifier: Apache-2.0
# The full text of the Apache license is available in the LICENSE file at
# the root of the repo.

# Copyright Built On Envoy
# SPDX-License-Identifier: Apache-2.0
#
# End-to-end setup for Envoy AI Gateway + OpenFGA on a local Kubernetes cluster.
#
# Prerequisites: kind, helm, kubectl, docker
# Kubernetes 1.32+ required (kind v0.27+ defaults to 1.32.2)
#
# Usage:
#   ./setup-ai-gateway.sh              # Create cluster, install everything
#   ./setup-ai-gateway.sh --no-cluster # Skip cluster creation (use existing)
#   ./setup-ai-gateway.sh --force-recreate # Delete existing cluster and recreate (non-interactive)
#
set -euo pipefail

# Check prerequisites
echo "=== Checking prerequisites ==="
missing=()
for tool in kind helm kubectl docker; do
  if ! command -v "$tool" &>/dev/null; then
    missing+=("$tool")
  fi
done
if [[ ${#missing[@]} -gt 0 ]]; then
  echo "ERROR: missing required tools: ${missing[*]}"
  echo "Install them and re-run."
  exit 1
fi
echo "All prerequisites found: kind helm kubectl docker"
echo ""

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
K8S_DIR="$SCRIPT_DIR"
CLUSTER_NAME="${CLUSTER_NAME:-envoy-ai-openfga}"
CREATE_CLUSTER=true
FORCE_RECREATE=false
BASIC_YAML_URL="https://raw.githubusercontent.com/envoyproxy/ai-gateway/main/examples/basic/basic.yaml"

while [[ $# -gt 0 ]]; do
  case $1 in
    --no-cluster)
      CREATE_CLUSTER=false
      shift
      ;;
    --force-recreate)
      FORCE_RECREATE=true
      shift
      ;;
    *)
      echo "Unknown option: $1"
      echo "Usage: $0 [--no-cluster] [--force-recreate]"
      exit 1
      ;;
  esac
done

echo "=== Envoy AI Gateway + OpenFGA Setup ==="
echo "K8s manifests: $K8S_DIR"
echo ""

# Step 1: Create cluster
if [[ "$CREATE_CLUSTER" == true ]]; then
  echo "=== Step 1: Creating kind cluster '$CLUSTER_NAME' ==="
  if kind get clusters 2>/dev/null | grep -q "^${CLUSTER_NAME}$"; then
    if [[ "$FORCE_RECREATE" == true ]]; then
      echo "Cluster '$CLUSTER_NAME' exists. Deleting and recreating (--force-recreate)."
      kind delete cluster --name "$CLUSTER_NAME"
    else
      echo "Cluster '$CLUSTER_NAME' already exists. Use --no-cluster to skip creation or --force-recreate to delete and recreate."
      read -p "Delete and recreate? [y/N] " -n 1 -r
      echo
      if [[ $REPLY =~ ^[Yy]$ ]]; then
        kind delete cluster --name "$CLUSTER_NAME"
      else
        echo "Using existing cluster."
      fi
    fi
  fi
  if ! kind get clusters 2>/dev/null | grep -q "^${CLUSTER_NAME}$"; then
    kind create cluster --name "$CLUSTER_NAME"
  fi
else
  echo "=== Step 1: Skipping cluster creation (--no-cluster) ==="
fi

# Step 2: Install Envoy Gateway with AI Gateway values
# Use v0.0.0-latest: Envoy AI Gateway requires v1.6.x+; v0.0.0-latest (main) adds
# dynamicModule/dynamicModules support needed for the OpenFGA Composer extension.
echo ""
echo "=== Step 2: Installing Envoy Gateway with AI Gateway integration ==="
helm upgrade -i eg oci://docker.io/envoyproxy/gateway-helm \
  --version v0.0.0-latest \
  --namespace envoy-gateway-system \
  --create-namespace \
  -f https://raw.githubusercontent.com/envoyproxy/ai-gateway/main/manifests/envoy-gateway-values.yaml \
  --wait

kubectl wait --timeout=2m -n envoy-gateway-system deployment/envoy-gateway --for=condition=Available

# Step 3: Install Envoy AI Gateway CRDs and controller
echo ""
echo "=== Step 3: Installing Envoy AI Gateway CRDs and controller ==="
helm upgrade -i aieg-crd oci://docker.io/envoyproxy/ai-gateway-crds-helm \
  --version v0.0.0-latest \
  --namespace envoy-ai-gateway-system \
  --create-namespace \
  --wait

helm upgrade -i aieg oci://docker.io/envoyproxy/ai-gateway-helm \
  --version v0.0.0-latest \
  --namespace envoy-ai-gateway-system \
  --create-namespace \
  --wait

kubectl wait --timeout=2m -n envoy-ai-gateway-system deployment/ai-gateway-controller --for=condition=Available

# Step 4: Deploy OpenFGA and run setup job
echo ""
echo "=== Step 4: Deploying OpenFGA and running setup job ==="
# Apply OpenFGA deployment first; wait for it to be ready before the setup job runs
kubectl apply -f "$K8S_DIR/openfga-deployment.yaml"
kubectl rollout status deployment/openfga -n openfga --timeout=120s
# Brief pause so OpenFGA is fully ready to accept connections
sleep 5
kubectl apply -f "$K8S_DIR/setup-job.yaml"
kubectl wait --for=condition=complete job/openfga-setup -n openfga --timeout=120s

# Step 5: Extract store ID
echo ""
echo "=== Step 5: Extracting OpenFGA store ID ==="
SETUP_LOGS=$(kubectl logs -n openfga -l job-name=openfga-setup --tail=50 2>&1 || true)
STORE_ID=$(echo "$SETUP_LOGS" | grep "STORE_ID=" | sed 's/.*STORE_ID=//' | tr -d '\r')
if [[ -z "$STORE_ID" ]]; then
  echo "ERROR: Could not extract STORE_ID from setup job logs"
  kubectl logs -n openfga -l job-name=openfga-setup
  exit 1
fi
echo "Store ID: $STORE_ID"

# Step 6: Deploy Envoy AI Gateway basic example
echo ""
echo "=== Step 6: Deploying Envoy AI Gateway basic example ==="
kubectl apply -f "$BASIC_YAML_URL"

# Wait for Envoy Gateway to create the deployment (can take a few min with extension manager)
echo "Waiting for Envoy Gateway to create deployment..."
GW_DEPLOY=""
for i in $(seq 1 36); do
  GW_DEPLOY=$(kubectl get deployment -n envoy-gateway-system \
    -l "gateway.envoyproxy.io/owning-gateway-name=envoy-ai-gateway-basic" \
    -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)
  if [[ -n "$GW_DEPLOY" ]]; then
    echo "Deployment $GW_DEPLOY found"
    break
  fi
  [[ $i -eq 36 ]] && break
  echo "  ($i/36) not yet — retrying in 5s"
  sleep 5
done

if [[ -z "$GW_DEPLOY" ]]; then
  echo "ERROR: Envoy Gateway did not create deployment within 3m."
  echo "GatewayClass status:" && kubectl get gatewayclass envoy-ai-gateway-basic -o yaml 2>/dev/null | grep -A 15 "status:" || true
  echo "Gateway status:" && kubectl get gateway envoy-ai-gateway-basic -n default -o yaml 2>/dev/null | grep -A 25 "status:" || true
  echo "Envoy Gateway logs:" && kubectl logs -n envoy-gateway-system deployment/envoy-gateway --tail=30 2>/dev/null || true
  exit 1
fi

# Wait for deployment rollout (pod image pull + startup can take 2–3 min)
if ! kubectl rollout status deployment/"$GW_DEPLOY" -n envoy-gateway-system --timeout=5m; then
  echo "ERROR: Envoy deployment $GW_DEPLOY did not become ready after 5m."
  echo ""
  echo "=== Diagnostics ==="
  echo "Deployment:"
  kubectl get deploy "$GW_DEPLOY" -n envoy-gateway-system -o wide 2>/dev/null || true
  echo ""
  echo "Pods:"
  kubectl get pods -n envoy-gateway-system -l gateway.envoyproxy.io/owning-gateway-name=envoy-ai-gateway-basic 2>/dev/null || true
  echo ""
  echo "Pod events (if any):"
  kubectl get events -n envoy-gateway-system --field-selector involvedObject.kind=Pod --sort-by='.lastTimestamp' 2>/dev/null | tail -20 || true
  echo ""
  echo "Envoy Gateway controller logs:"
  kubectl logs -n envoy-gateway-system deployment/envoy-gateway --tail=40 2>/dev/null || true
  echo ""
  echo "AI Gateway controller logs:"
  kubectl logs -n envoy-ai-gateway-system deployment/ai-gateway-controller --tail=30 2>/dev/null || true
  exit 1
fi

# Step 7: Build composer dynamic module, configure EnvoyProxy, and apply EEP
echo ""
echo "=== Step 7: Building composer dynamic module ==="
COMPOSER_DIR="$(cd "$SCRIPT_DIR/../../.." && pwd)"

# Detect the kind node architecture so we build a compatible .so
case "$(uname -m)" in
  arm64|aarch64) PLATFORM="linux/arm64" ;;
  *) PLATFORM="linux/amd64" ;;
esac
echo "Building for platform: $PLATFORM (this may take a few minutes)"

docker buildx build \
  --platform "$PLATFORM" \
  --load \
  --provenance=false \
  -t composer-init:local \
  -f "$K8S_DIR/Dockerfile.composer-init" \
  "$COMPOSER_DIR"

kind load docker-image composer-init:local --name "$CLUSTER_NAME"

echo ""
echo "=== Step 7b: Patching EnvoyProxy to register composer dynamic module and add OpenFGA upstream cluster ==="
# The bootstrap merge adds openfga as a static cluster so the dynamic module's
# HttpCallout can reach it.  Envoy Gateway does not auto-create upstream clusters
# for dynamic module configs (unlike spec.wasm), so we must add it manually.
# GODEBUG=cgocheck=0 works around Go CGO "unpinned Go pointer" panic in the
# composer dynamic module (Go 1.21+ stricter CGO rules).
kubectl patch envoyproxy envoy-ai-gateway-basic -n default --type=merge -p '{
  "spec": {
    "bootstrap": {
      "type": "Merge",
      "value": "static_resources:\n  clusters:\n  - name: \"openfga|openfga.openfga.svc.cluster.local|8080\"\n    type: STRICT_DNS\n    connect_timeout: 10s\n    load_assignment:\n      cluster_name: \"openfga|openfga.openfga.svc.cluster.local|8080\"\n      endpoints:\n      - lb_endpoints:\n          - endpoint:\n              address:\n                socket_address:\n                  address: openfga.openfga.svc.cluster.local\n                  port_value: 8080\n"
    },
    "dynamicModules": [
      {
        "name": "composer",
        "libraryName": "composer",
        "loadGlobally": true
      }
    ],
    "provider": {
      "kubernetes": {
        "envoyDeployment": {
          "initContainers": [
            {
              "name": "install-composer",
              "image": "composer-init:local",
              "imagePullPolicy": "Never",
              "command": ["sh", "-c", "cp /opt/libcomposer.so /mnt/composer/ && cp /opt/openfga.so /mnt/composer/"],
              "volumeMounts": [
                {
                  "name": "composer-lib",
                  "mountPath": "/mnt/composer"
                }
              ]
            }
          ],
          "container": {
            "env": [
              {
                "name": "ENVOY_DYNAMIC_MODULES_SEARCH_PATH",
                "value": "/mnt/composer"
              },
              {
                "name": "GODEBUG",
                "value": "cgocheck=0"
              }
            ],
            "volumeMounts": [
              {
                "name": "composer-lib",
                "mountPath": "/mnt/composer"
              }
            ]
          },
          "pod": {
            "volumes": [
              {
                "name": "composer-lib",
                "emptyDir": {}
              }
            ]
          }
        }
      }
    }
  }
}'

# Wait for the new Envoy pod (with init container) to be ready
GW_DEPLOY=$(kubectl get deployment -n envoy-gateway-system \
  -l "gateway.envoyproxy.io/owning-gateway-name=envoy-ai-gateway-basic" \
  -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)
if [[ -n "$GW_DEPLOY" ]]; then
  echo "Waiting for Envoy pod to roll out with composer module..."
  if ! kubectl rollout status deployment/"$GW_DEPLOY" -n envoy-gateway-system --timeout=3m; then
    echo "ERROR: Envoy rollout did not complete — the composer module may not have loaded."
    echo "Recent Envoy pod logs:"
    kubectl logs -n envoy-gateway-system -l "gateway.envoyproxy.io/owning-gateway-name=envoy-ai-gateway-basic" --tail=30 || true
    exit 1
  fi

  # Verify the dynamic module loaded. Envoy logs "openfga: loaded config" on success.
  # A missing log line means the EEP hasn't been applied yet (expected here — applied below)
  # or the module itself failed to initialize.
  GW_POD=$(kubectl get pod -n envoy-gateway-system \
    -l "gateway.envoyproxy.io/owning-gateway-name=envoy-ai-gateway-basic" \
    -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)
  if [[ -n "$GW_POD" ]]; then
    echo "Checking Envoy pod logs for dynamic module errors..."
    if kubectl logs -n envoy-gateway-system "$GW_POD" 2>/dev/null | grep -qi "failed to load\|dlopen\|error.*composer\|error.*dynamic_module"; then
      echo "ERROR: Dynamic module failed to load. This is usually an ABI mismatch between"
      echo "       libcomposer.so (built with the SDK in go.mod) and the Envoy binary"
      echo "       (from the Envoy Gateway helm chart). Check the logs:"
      kubectl logs -n envoy-gateway-system "$GW_POD" | grep -i "composer\|dynamic_module\|error" | tail -20 || true
      exit 1
    fi
  fi
fi

echo ""
echo "=== Step 7c: Applying EnvoyExtensionPolicy with OpenFGA filter ==="
# The AI Gateway controller creates an HTTPRoute from the AIGatewayRoute; wait for it
# before applying the EEP so the policy is programmed immediately on attach.
echo "Waiting for HTTPRoute envoy-ai-gateway-basic to be created by the AI Gateway controller..."
for i in $(seq 1 24); do
  if kubectl get httproute envoy-ai-gateway-basic -n default &>/dev/null; then
    echo "HTTPRoute ready"
    break
  fi
  echo "  ($i/24) not yet — retrying in 5s"
  sleep 5
done
if ! kubectl get httproute envoy-ai-gateway-basic -n default &>/dev/null; then
  echo "ERROR: HTTPRoute envoy-ai-gateway-basic not found after 2m — is the AI Gateway controller running?"
  exit 1
fi
sed "s/YOUR_STORE_ID/$STORE_ID/g" "$K8S_DIR/envoy-extension-policy-ai-gateway.yaml" | kubectl apply -f -

# Wait for Envoy Gateway to accept the EEP, then allow time for xDS to propagate to Envoy.
# Without this wait the test commands below will race against xDS reconciliation and get 404.
# Note: EnvoyExtensionPolicy stores conditions under status.ancestors[].conditions, not
# status.conditions, so kubectl wait --for=condition=Accepted does not work.
echo "Waiting for EnvoyExtensionPolicy to be accepted..."
for i in $(seq 1 24); do
  ACCEPTED=$(kubectl get envoyextensionpolicy openfga-ai-gateway -n default \
    -o jsonpath='{.status.ancestors[0].conditions[?(@.type=="Accepted")].status}' 2>/dev/null || true)
  if [[ "$ACCEPTED" == "True" ]]; then
    echo "EnvoyExtensionPolicy accepted"
    break
  fi
  if [[ $i -eq 24 ]]; then
    echo "WARNING: EnvoyExtensionPolicy not accepted after 2m — check for configuration errors:"
    kubectl describe envoyextensionpolicy openfga-ai-gateway -n default || true
  else
    echo "  ($i/24) not yet — retrying in 5s"
    sleep 5
  fi
done
echo "Waiting 15s for xDS to propagate to Envoy..."
sleep 15

# Confirm the filter loaded: "openfga: loaded config" appears in Envoy logs on success.
if [[ -n "${GW_POD:-}" ]]; then
  if kubectl logs -n envoy-gateway-system "$GW_POD" 2>/dev/null | grep -q "openfga: loaded config"; then
    echo "OpenFGA filter loaded successfully in Envoy"
  else
    echo "WARNING: Could not confirm openfga filter load from Envoy logs."
    echo "         The filter may still be loading, or may have failed. Check:"
    echo "         kubectl logs -n envoy-gateway-system $GW_POD | grep -i openfga"
  fi
fi

GW_SVC=$(kubectl get svc -n envoy-gateway-system \
  -l "gateway.envoyproxy.io/owning-gateway-name=envoy-ai-gateway-basic,gateway.envoyproxy.io/owning-gateway-namespace=default" \
  -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)
if [[ -z "$GW_SVC" ]]; then
  GW_SVC="<run: kubectl get svc -n envoy-gateway-system -l gateway.envoyproxy.io/owning-gateway-name=envoy-ai-gateway-basic>"
fi

echo ""
echo "=== Setup complete ==="
echo ""
echo "To access the gateway, run in another terminal:"
echo "  kubectl port-forward -n envoy-gateway-system svc/$GW_SVC 8080:80"
echo ""
echo "Then set:"
echo "  export GATEWAY_URL=http://localhost:8080"
echo ""
echo "Test OpenFGA authorization (alice allowed, bob denied):"
echo ""
echo "  # Allowed - alice has can_use on some-cool-self-hosted-model"
echo "  curl -s -o /dev/null -w '%{http_code}' -H 'x-user-id: alice' -H 'x-ai-eg-model: some-cool-self-hosted-model' \\"
echo "    -H 'Content-Type: application/json' \\"
echo "    -d '{\"model\":\"some-cool-self-hosted-model\",\"messages\":[{\"role\":\"user\",\"content\":\"Hi\"}]}' \\"
echo "    \$GATEWAY_URL/v1/chat/completions"
echo "  # Expected: 200"
echo ""
echo "  # Denied - bob has no relations"
echo "  curl -s -o /dev/null -w '%{http_code}' -H 'x-user-id: bob' -H 'x-ai-eg-model: some-cool-self-hosted-model' \\"
echo "    -H 'Content-Type: application/json' \\"
echo "    -d '{\"model\":\"some-cool-self-hosted-model\",\"messages\":[{\"role\":\"user\",\"content\":\"Hi\"}]}' \\"
echo "    \$GATEWAY_URL/v1/chat/completions"
echo "  # Expected: 403"
echo ""
echo "Or run the automated test script:"
echo "  ./run-openfga-tests.sh"
echo ""
