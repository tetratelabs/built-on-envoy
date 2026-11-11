#!/usr/bin/env bash
# Copyright Built On Envoy
# SPDX-License-Identifier: Apache-2.0
# The full text of the Apache license is available in the LICENSE file at
# the root of the repo.

# Runs the Switchyard extension with Agent Router in a local kind cluster, and checks that requests
# for the "coding-auto" virtual model reach the model Switchyard selects.
#
# Requirements: docker (with buildx), kind, kubectl, helm, curl, and Kubernetes with the ImageVolume
# feature, which is enabled by default since Kubernetes 1.35.
#
# Environment:
#   CLUSTER          kind cluster name (default: switchyard). Created if it does not exist.
#   NODE_IMAGE       kind node image (default: kindest/node:v1.37.0).
#   EG_VERSION       Envoy Gateway helm chart version (default: v1.9.2).
#   AR_VERSION       Agent Router helm chart version (default: v0.0.0-latest, built from main).
#   AR_REF           Agent Router git ref for the Envoy Gateway values file (default: main).
#   KEEP_CLUSTER     Set to "false" to delete the cluster when done (default: true).

set -euo pipefail

CLUSTER="${CLUSTER:-switchyard}"
NODE_IMAGE="${NODE_IMAGE:-kindest/node:v1.37.0}"
EG_VERSION="${EG_VERSION:-v1.9.2}"
AR_VERSION="${AR_VERSION:-v0.0.0-latest}"
AR_REF="${AR_REF:-main}"
KEEP_CLUSTER="${KEEP_CLUSTER:-true}"

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(git -C "${HERE}" rev-parse --show-toplevel)"
# Must match the image volume reference in gateway.yaml.
IMAGE="ghcr.io/tetratelabs/built-on-envoy/extension-switchyard:0.1.0"
RUST_VERSION="$(sed -ne 's/^channel = "\(.*\)"/\1/p' "${ROOT}/rust-toolchain.toml")"
ARCH="$(docker version --format '{{.Server.Arch}}')"

log() { echo "==> $*"; }

if [[ "${KEEP_CLUSTER}" == "false" ]]; then
  trap 'kind delete cluster --name "${CLUSTER}"' EXIT
fi

if ! kind get clusters | grep -qx "${CLUSTER}"; then
  log "Creating kind cluster ${CLUSTER}"
  kind create cluster --name "${CLUSTER}" --image "${NODE_IMAGE}" --wait 2m
fi
kubectl config use-context "kind-${CLUSTER}" >/dev/null

log "Building ${IMAGE} for linux/${ARCH}"
docker buildx build --load --provenance=false \
  --platform "linux/${ARCH}" \
  --tag "${IMAGE}" \
  --build-arg RUST_VERSION="${RUST_VERSION}" \
  --build-arg EXTENSION_PATH=switchyard \
  --file "${ROOT}/extensions/Dockerfile.rust" "${ROOT}"
kind load docker-image --name "${CLUSTER}" "${IMAGE}"

log "Installing Envoy Gateway ${EG_VERSION}"
helm upgrade -i eg oci://docker.io/envoyproxy/gateway-helm \
  --version "${EG_VERSION}" \
  --namespace envoy-gateway-system --create-namespace \
  -f "https://raw.githubusercontent.com/theagentrouter/agent-router/${AR_REF}/manifests/envoy-gateway-values.yaml"
kubectl wait --timeout=3m -n envoy-gateway-system deployment/envoy-gateway --for=condition=Available

log "Installing Agent Router ${AR_VERSION}"
helm upgrade -i aieg-crd oci://docker.io/envoyproxy/ai-gateway-crds-helm \
  --version "${AR_VERSION}" \
  --namespace envoy-ai-gateway-system --create-namespace
# Maps the Switchyard decision headers to Agent Router's metric and span attributes.
helm upgrade -i aieg oci://docker.io/envoyproxy/ai-gateway-helm \
  --version "${AR_VERSION}" \
  --namespace envoy-ai-gateway-system --create-namespace \
  --set controller.requestHeaderAttributes='x-switchyard-virtual-model:switchyard.virtual_model\,x-switchyard-reason:switchyard.reason'
kubectl wait --timeout=3m -n envoy-ai-gateway-system deployment/ai-gateway-controller --for=condition=Available

log "Applying the example"
kubectl apply -f "${HERE}/testupstream.yaml"
kubectl apply -f "${HERE}/gateway.yaml"
kubectl apply -f "${HERE}/routes.yaml"
kubectl apply -f "${HERE}/switchyard-policy.yaml"
kubectl wait --timeout=3m deployment/efficient deployment/capable --for=condition=Available
# Not "Programmed": kind has no load balancer to assign the Gateway an address.
kubectl wait --timeout=3m gateway/switchyard --for=condition=Accepted
kubectl wait --timeout=3m envoyextensionpolicy/switchyard \
  --for=jsonpath='{.status.ancestors[0].conditions[?(@.type=="Accepted")].status}'=True
SELECTOR="gateway.envoyproxy.io/owning-gateway-name=switchyard,gateway.envoyproxy.io/owning-gateway-namespace=default"
# The Envoy deployment is created asynchronously.
for _ in $(seq 60); do
  kubectl get deployment -n envoy-gateway-system -l "${SELECTOR}" -o name | grep -q . && break
  sleep 2
done
kubectl wait --timeout=3m -n envoy-gateway-system deployment -l "${SELECTOR}" --for=condition=Available

SERVICE="$(kubectl get service -n envoy-gateway-system -l "${SELECTOR}" -o name)"
PORT=18080
kubectl port-forward -n envoy-gateway-system "${SERVICE}" "${PORT}:80" >/dev/null &
PF_PID=$!
trap 'kill ${PF_PID} 2>/dev/null || true' EXIT
if [[ "${KEEP_CLUSTER}" == "false" ]]; then
  trap 'kill ${PF_PID} 2>/dev/null || true; kind delete cluster --name "${CLUSTER}"' EXIT
fi
for _ in $(seq 30); do
  curl -s -o /dev/null "http://localhost:${PORT}" && break
  sleep 1
done

b64() { printf '%s' "$1" | base64 | tr -d '\n'; }

# check DESCRIPTION EXPECTED_UPSTREAM EXPECTED_HEADERS BODY [CURL_ARGS...]
#
# Sends BODY to the chat completions endpoint and checks the backend that served it, and the
# request headers it received. Agent Router's test upstream rejects the request otherwise.
check() {
  local description="$1" upstream="$2" headers="$3" body="$4"
  shift 4
  local args=(-s -o /dev/stderr -w '%{http_code}' "http://localhost:${PORT}/v1/chat/completions"
    -H 'content-type: application/json'
    -H "x-expected-testupstream-id: ${upstream}"
    -d "${body}")
  [[ -n "${headers}" ]] && args+=(-H "x-expected-headers: $(b64 "${headers}")")
  local status
  # The configuration may take a few seconds to reach Envoy.
  for _ in $(seq 30); do
    status="$(curl "${args[@]}" "$@" 2>/dev/null)" || true
    [[ "${status}" == "200" ]] && break
    sleep 2
  done
  if [[ "${status}" != "200" ]]; then
    echo "FAIL: ${description} (status ${status})"
    curl "${args[@]}" "$@" || true
    echo
    exit 1
  fi
  echo "PASS: ${description}"
}

FIRST_TURN='{"model":"coding-auto","messages":[{"role":"user","content":"write a hello world in Go"}]}'
TOOL_ERROR='{"model":"coding-auto","messages":[{"role":"user","content":"run the tests"},{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"bash","arguments":"{\"command\":\"make test\"}"}}]},{"role":"tool","tool_call_id":"call_1","content":"{\"output\":\"out of memory\",\"exit_code\":1}"}]}'

log "Checking routing"
# The first turn of a task goes to the efficient tier. The upstream also checks that the body it
# received has the selected model.
REWRITTEN="${FIRST_TURN/coding-auto/gpt-4.1-mini}"
check "first turn is routed to the efficient model" efficient \
  "x-switchyard-virtual-model:coding-auto,x-switchyard-selected-model:gpt-4.1-mini" \
  "${FIRST_TURN}" -H "x-expected-request-body: $(b64 "${REWRITTEN}")"
check "a critical tool error escalates to the capable model" capable \
  "x-switchyard-virtual-model:coding-auto,x-switchyard-selected-model:gpt-5" \
  "${TOOL_ERROR}" -H 'x-switchyard-session-id: s1' -H 'x-tenant-id: acme'
check "free tier always gets the efficient model" efficient \
  "x-switchyard-selected-model:gpt-4.1-mini" \
  "${TOOL_ERROR}" -H 'x-tier: free'
check "other models are routed as usual" capable "" \
  '{"model":"gpt-5","messages":[{"role":"user","content":"hi"}]}'

log "Models listed by the gateway"
curl -s "http://localhost:${PORT}/v1/models"
echo

log "Filter chain order (the Switchyard module must come before Agent Router's ext_proc)"
POD="$(kubectl get pod -n envoy-gateway-system -l "${SELECTOR}" -o jsonpath='{.items[0].metadata.name}')"
ADMIN_PORT=19100
kubectl port-forward -n envoy-gateway-system "pod/${POD}" "${ADMIN_PORT}:19000" >/dev/null &
ADMIN_PF_PID=$!
for _ in $(seq 10); do
  curl -s -o /dev/null "http://localhost:${ADMIN_PORT}/ready" && break
  sleep 1
done
FILTERS="$(curl -s "http://localhost:${ADMIN_PORT}/config_dump?mask=dynamic_listeners" \
  | grep -oE '"envoy\.filters\.http\.[^"]+"' | tr -d '"' | grep -E 'dynamic_modules|ext_proc/aigateway|router$' | uniq)"
kill "${ADMIN_PF_PID}" 2>/dev/null || true
echo "${FILTERS}"
if [[ "$(echo "${FILTERS}" | grep -nE 'dynamic_modules|ext_proc/aigateway' | head -1)" != *dynamic_modules* ]]; then
  echo "FAIL: the Switchyard module does not run before Agent Router's ext_proc"
  exit 1
fi
echo "PASS: the Switchyard module runs before Agent Router's ext_proc"

log "Done. The cluster ${CLUSTER} is kept unless KEEP_CLUSTER=false."
