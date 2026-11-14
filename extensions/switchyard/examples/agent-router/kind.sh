#!/usr/bin/env bash
# Copyright Built On Envoy
# SPDX-License-Identifier: Apache-2.0
# The full text of the Apache license is available in the LICENSE file at
# the root of the repo.

# Runs the Switchyard extension with Agent Router in a local kind cluster, and checks that requests
# for the "coding-auto" virtual model reach the model Switchyard selects.
#
# Usage: kind.sh [POLICY...]
#
# POLICY is the name of a file in the policies directory, without the extension, such as "vgr".
# Each policy is applied in turn, replacing the previous one, and its routing is checked. Defaults
# to "auto". Use "all" to check every policy. Once the cluster is set up, other policies can also be
# applied with kubectl: kubectl apply -f policies/vgr.yaml
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
#   SWITCHYARD_VERSION  Tag of the extension image built and mounted in Envoy (default: the version
#                    in the extension's manifest.yaml).

set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SWITCHYARD_VERSION="${SWITCHYARD_VERSION:-$(sed -ne 's/^version: *//p' "${HERE}/../../manifest.yaml")}"
CLUSTER="${CLUSTER:-switchyard}"
NODE_IMAGE="${NODE_IMAGE:-kindest/node:v1.37.0}"
EG_VERSION="${EG_VERSION:-v1.9.2}"
AR_VERSION="${AR_VERSION:-v0.0.0-latest}"
AR_REF="${AR_REF:-main}"
KEEP_CLUSTER="${KEEP_CLUSTER:-true}"
POLICIES=("$@")
[[ ${#POLICIES[@]} -eq 0 ]] && POLICIES=(auto)

if [[ "${POLICIES[*]}" == "all" ]]; then
  POLICIES=()
  for f in "${HERE}"/policies/*.yaml; do POLICIES+=("$(basename "${f}" .yaml)"); done
fi
for policy in "${POLICIES[@]}"; do
  [[ -f "${HERE}/policies/${policy}.yaml" ]] || { echo "unknown policy ${policy}"; exit 1; }
done
ROOT="$(git -C "${HERE}" rev-parse --show-toplevel)"
# The image volume reference in gateway.yaml is rewritten to this tag when it is applied.
IMAGE="ghcr.io/tetratelabs/built-on-envoy/extension-switchyard:${SWITCHYARD_VERSION}"
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
# Maps the Switchyard decision headers to Agent Router's metric and span attributes. The call
# headers tell the judge, verifier and advisor calls of the algorithms apart from client requests,
# and join each call to the client request it was made for.
helm upgrade -i aieg oci://docker.io/envoyproxy/ai-gateway-helm \
  --version "${AR_VERSION}" \
  --namespace envoy-ai-gateway-system --create-namespace \
  --set controller.requestHeaderAttributes='x-switchyard-virtual-model:switchyard.virtual_model\,x-switchyard-reason:switchyard.reason\,x-switchyard-call:switchyard.call' \
  --set controller.spanRequestHeaderAttributes='x-switchyard-parent-id:switchyard.parent_id' \
  --set controller.logRequestHeaderAttributes='x-switchyard-parent-id:switchyard.parent_id'
kubectl wait --timeout=3m -n envoy-ai-gateway-system deployment/ai-gateway-controller --for=condition=Available

log "Applying the example"
kubectl apply -f "${HERE}/testupstream.yaml"
# Also creates the judge-keys Secret, which the Envoy pods mount, so it comes before the Gateway.
kubectl apply -f "${HERE}/judge.yaml"
sed -e "s|extension-switchyard:[^[:space:]]*|extension-switchyard:${SWITCHYARD_VERSION}|" \
  "${HERE}/gateway.yaml" | kubectl apply -f -
kubectl apply -f "${HERE}/routes.yaml"
kubectl apply -f "${HERE}/loopback.yaml"
kubectl apply -f "${HERE}/policies/${POLICIES[0]}.yaml"
kubectl wait --timeout=3m deployment/efficient deployment/capable deployment/judge --for=condition=Available
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
DEPLOYMENT="$(kubectl get deployment -n envoy-gateway-system -l "${SELECTOR}" -o name)"
# The module image volume and the judge-keys Secret are only read when a pod starts. Restart Envoy so
# that it loads the image built above and the current key, also when the cluster is reused.
kubectl rollout restart -n envoy-gateway-system "${DEPLOYMENT}"
kubectl rollout status --timeout=3m -n envoy-gateway-system "${DEPLOYMENT}"

SERVICE="$(kubectl get service -n envoy-gateway-system -l "${SELECTOR}" -o name)"
PORT=18080
# kubectl port-forward exits when a connection fails, for example while Envoy is still starting,
# so it is restarted until the script ends.
(while true; do
  kubectl port-forward -n envoy-gateway-system "${SERVICE}" "${PORT}:80" >/dev/null 2>&1
  sleep 1
done) &
PF_PID=$!
stop_port_forward() { pkill -P "${PF_PID}" 2>/dev/null || true; kill "${PF_PID}" 2>/dev/null || true; }
trap stop_port_forward EXIT
if [[ "${KEEP_CLUSTER}" == "false" ]]; then
  trap 'stop_port_forward; kind delete cluster --name "${CLUSTER}"' EXIT
fi
for _ in $(seq 30); do
  curl -s -o /dev/null "http://localhost:${PORT}" && break
  sleep 1
done

b64() { printf '%s' "$1" | base64 | tr -d '\n'; }

# request [CURL_ARGS...]
#
# Sends a request to the chat completions endpoint, retrying until it succeeds, as the
# configuration may take a few seconds to reach Envoy. Prints the response headers and body.
request() {
  local out
  for _ in $(seq 30); do
    out="$(curl -s -i "http://localhost:${PORT}/v1/chat/completions" -H 'content-type: application/json' "$@")" || true
    [[ "${out}" == "HTTP/1.1 200"* ]] && break
    sleep 2
  done
  echo "${out}"
}

# check DESCRIPTION EXPECTED_UPSTREAM EXPECTED_HEADERS BODY [CURL_ARGS...]
#
# Sends BODY to the chat completions endpoint and checks the backend that served it, and the
# request headers it received. Agent Router's test upstream rejects the request otherwise.
check() {
  local description="$1" upstream="$2" headers="$3" body="$4"
  shift 4
  local args=(-H "x-expected-testupstream-id: ${upstream}" -d "${body}")
  [[ -n "${headers}" ]] && args+=(-H "x-expected-headers: $(b64 "${headers}")")
  local out
  out="$(request "${args[@]}" "$@")"
  if [[ "${out}" != "HTTP/1.1 200"* ]]; then
    echo "FAIL: ${description}"
    echo "${out}"
    exit 1
  fi
  echo "PASS: ${description}"
}

# check_answer DESCRIPTION EXPECTED_TEXT BODY [CURL_ARGS...]
#
# Sends BODY and checks that the response, written by the algorithm, contains EXPECTED_TEXT.
check_answer() {
  local description="$1" text="$2" body="$3"
  shift 3
  local out
  out="$(request -d "${body}" "$@")"
  if [[ "${out}" != "HTTP/1.1 200"* || "${out}" != *"${text}"* ]]; then
    echo "FAIL: ${description}"
    echo "${out}"
    exit 1
  fi
  echo "PASS: ${description}"
  echo "${out}" | grep -i '^x-switchyard-routing' || true
}

# Requests for the coding-auto virtual model. Tasks that contain "[hard]" are rated as hard by the
# fake judge (see judge.yaml).
body() { printf '{"model":"coding-auto","messages":[{"role":"user","content":"%s"}]}' "$1"; }
EASY="$(body 'rename the variable x to count')"
HARD="$(body '[hard] design a lock-free queue')"
FIRST_TURN="$(body 'write a hello world in Go')"
TOOL_ERROR='{"model":"coding-auto","messages":[{"role":"user","content":"run the tests"},{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"bash","arguments":"{\"command\":\"make test\"}"}}]},{"role":"tool","tool_call_id":"call_1","content":"{\"output\":\"out of memory\",\"exit_code\":1}"}]}'
EDIT_STEP='{"model":"coding-auto","messages":[{"role":"user","content":"TASK"},{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"apply_patch","arguments":"{\"patch\":\"*** Begin Patch\"}"}}]},{"role":"tool","tool_call_id":"call_1","content":"ok"}]}'
READ_STEP='{"model":"coding-auto","messages":[{"role":"user","content":"TASK"},{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"main.go\"}"}}]},{"role":"tool","tool_call_id":"call_1","content":"package main"}]}'
EFFICIENT="x-switchyard-selected-model:gpt-4.1-mini"
CAPABLE="x-switchyard-selected-model:gpt-5"
# A new session per run, as stateful algorithms remember the tier of a session.
RUN="$(date +%s)"
session() { echo "x-switchyard-session-id: ${RUN}-$1"; }

check_policy() {
  case "$1" in
    auto)
      # The first turn of a task goes to the efficient tier. The upstream also checks that the
      # body it received has the selected model.
      check "first turn is routed to the efficient model" efficient \
        "x-switchyard-virtual-model:coding-auto,${EFFICIENT}" \
        "${FIRST_TURN}" -H "x-expected-request-body: $(b64 "${FIRST_TURN/coding-auto/gpt-4.1-mini}")"
      check "a critical tool error escalates to the capable model" capable "${CAPABLE}" \
        "${TOOL_ERROR}" -H "$(session auto)" -H 'x-tenant-id: acme'
      check "free tier always gets the efficient model" efficient "${EFFICIENT}" \
        "${TOOL_ERROR}" -H 'x-tier: free'
      ;;
    passthrough)
      check "every request goes to the capable model" capable "${CAPABLE}" "${EASY}"
      ;;
    random)
      check "requests are split across the targets" "" "x-switchyard-reason:random" "${EASY}"
      ;;
    stage-router)
      check "a read-only step goes to the efficient model" efficient "${EFFICIENT}" \
        "${READ_STEP/TASK/fix the parser}" -H "$(session stage-read)"
      check "a critical tool error escalates to the capable model" capable "${CAPABLE}" \
        "${TOOL_ERROR}" -H "$(session stage-error)"
      ;;
    stage-router-judge)
      check "the judge sends an easy task to the efficient model" efficient "${EFFICIENT}" "${EASY}"
      check "the judge sends a hard task to the capable model" capable "${CAPABLE}" "${HARD}"
      ;;
    plan-execute)
      check "the first turn is planned on the capable model" capable "${CAPABLE}" "${EASY}" \
        -H "$(session plan-first)"
      check "the first edit hands off to the efficient model" efficient "${EFFICIENT}" \
        "${EDIT_STEP/TASK/fix the parser}" -H "$(session plan-edit)"
      ;;
    plan-execute-judge)
      check "the first turn is planned on the capable model" capable "${CAPABLE}" "${EASY}" \
        -H "$(session plan-judge-first)"
      check "the judge hands off an easy task" efficient "${EFFICIENT}" \
        "${READ_STEP/TASK/rename the variable x}" -H "$(session plan-judge-easy)"
      check "the judge keeps planning a hard task" capable "${CAPABLE}" \
        "${READ_STEP/TASK/[hard] design a lock-free queue}" -H "$(session plan-judge-hard)"
      ;;
    llm-classifier)
      check "the judge sends an easy task to the efficient model" efficient "${EFFICIENT}" "${EASY}"
      check "the judge sends a hard task to the capable model" capable "${CAPABLE}" "${HARD}"
      ;;
    decision-judge)
      check "the decision judge sends an easy task to the efficient model" efficient "${EFFICIENT}" "${EASY}"
      check "the decision judge sends a hard task to the capable model" capable "${CAPABLE}" "${HARD}"
      ;;
    composite)
      check "the judge sends an easy task to the efficient model" efficient "${EFFICIENT}" "${EASY}" \
        -H "$(session composite-easy)"
      check "the judge sends a hard task to the capable model" capable "${CAPABLE}" "${HARD}" \
        -H "$(session composite-hard)"
      ;;
    vgr)
      # The answer comes from the efficient model's backend, through a call the filter makes.
      check_answer "a verified answer of the efficient model is served by the filter" \
        '"model":"gpt-4.1-mini"' "${EASY}"
      check "an answer the verifiers reject escalates to the capable model" capable "${CAPABLE}" "${HARD}"
      ;;
    advisor)
      check_answer "the executor answers, after the advisor approves" \
        '"model":"gpt-4.1-mini"' "${EASY}" -H "$(session advisor)"
      ;;
    noop)
      check_answer "the filter answers OK" '"content":"OK"' "${EASY}"
      ;;
    subagents)
      check "the main agent gets the capable model" capable "${CAPABLE}" "${EASY}"
      check "sub-agents get the efficient model" efficient "${EFFICIENT}" "${EASY}" \
        -H "$(session subagents)" -H 'x-switchyard-agent-id: explorer' -H 'x-switchyard-is-subagent: true'
      ;;
    eligible-models)
      check "only the eligible model is selected" efficient "${EFFICIENT}" "${EASY}" \
        -H 'x-switchyard-eligible-models: gpt-4.1-mini'
      check "only the eligible model is selected" capable "${CAPABLE}" "${EASY}" \
        -H 'x-switchyard-eligible-models: gpt-5'
      ;;
    decision-only)
      # The body keeps the virtual model, which Agent Router routes to the efficient backend.
      check "the decision is passed on, and the body keeps the virtual model" efficient \
        "x-switchyard-virtual-model:coding-auto,${CAPABLE}" \
        "${EASY}" -H "x-expected-request-body: $(b64 "${EASY}")"
      ;;
  esac
  check "other models are routed as usual" capable "" \
    '{"model":"gpt-5","messages":[{"role":"user","content":"hi"}]}'
}

for policy in "${POLICIES[@]}"; do
  log "Checking the ${policy} policy"
  kubectl apply -f "${HERE}/policies/${policy}.yaml" >/dev/null
  kubectl wait --timeout=2m envoyextensionpolicy/switchyard \
    --for=jsonpath='{.status.ancestors[0].conditions[?(@.type=="Accepted")].status}'=True >/dev/null
  # Envoy applies the new filter config asynchronously. Wait until the listener reports this
  # policy's generation before checking it.
  GENERATION="$(kubectl get envoyextensionpolicy/switchyard -o jsonpath='{.metadata.generation}')"
  for _ in $(seq 30); do
    OBSERVED="$(kubectl get envoyextensionpolicy/switchyard \
      -o jsonpath='{.status.ancestors[0].conditions[?(@.type=="Accepted")].observedGeneration}')"
    [[ "${OBSERVED}" == "${GENERATION}" ]] && break
    sleep 1
  done
  # The xDS push still trails the status; a short settle avoids checking the previous config.
  sleep 3
  check_policy "${policy}"
done

log "Models listed by the gateway"
curl -s "http://localhost:${PORT}/v1/models"
echo

log "Filter chain order (the Switchyard module must come before Agent Router's ext_proc)"
POD="$(kubectl get pod -n envoy-gateway-system -l "${SELECTOR}" --field-selector=status.phase=Running \
  -o jsonpath='{.items[0].metadata.name}')"
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
