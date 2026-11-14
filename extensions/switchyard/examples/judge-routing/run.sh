#!/usr/bin/env bash
# Copyright Built On Envoy
# SPDX-License-Identifier: Apache-2.0
# The full text of the Apache license is available in the LICENSE file at
# the root of the repo.

# Runs the Switchyard extension with two judges in a local kind cluster: Jev, a decision judge,
# behind the "coding-jev" virtual model, and an LLM classifier behind "coding-classifier". Both
# route to the same two real target models, served by an OpenAI-compatible endpoint through Agent
# Router. Sends every prompt in cases.json to both virtual models and writes out/results.json with
# the model each judge picked and the answer the model gave.
#
# Usage: run.sh [--render-only]
#   --render-only  Only render the manifest templates into out/manifests/ and exit.
#
# Requirements: docker (with buildx), kind, kubectl, helm, curl, jq, and Kubernetes with the
# ImageVolume feature, which is enabled by default since Kubernetes 1.35. python3 builds the HTML
# report when report/build_report.py exists.
#
# Environment:
#   INFERENCE_BASE_URL     OpenAI-compatible base URL serving the target models and the classifier,
#                          for example https://llm.example.com/v1 (required).
#   INFERENCE_API_KEY_FILE File holding the API key of INFERENCE_BASE_URL (required).
#   JEV_API_KEY_FILE       File holding the Jev API key (required).
#   EFFICIENT_MODEL        Efficient target model (default: gpt-6.1-terra).
#   CAPABLE_MODEL          Capable target model (default: claude-opus-5-5).
#   CLASSIFIER_MODEL       Model of the LLM classifier judge (default: claude-haiku-5-5).
#   ANSWER_MAX_TOKENS      max_tokens of each demo request (default: 1200).
#   REASONING_EFFORT       reasoning_effort of each demo request, empty to omit it (default: low).
#                          Reasoning models can spend a whole small budget on hidden reasoning
#                          and return no answer text; a low effort leaves room for the answer.
#   CLUSTER                kind cluster name (default: switchyard-judges). Created if it does not exist.
#   NODE_IMAGE             kind node image (default: kindest/node:v1.37.0).
#   EG_VERSION             Envoy Gateway helm chart version (default: v1.9.2).
#   AR_VERSION             Agent Router helm chart version (default: v0.0.0-latest, built from main).
#   AR_REF                 Agent Router git ref for the Envoy Gateway values file (default: main).
#   KEEP_CLUSTER           Set to "false" to delete the cluster when done (default: true).
#   PORT                   Local port forwarded to the Gateway (default: 18081).
#   SWITCHYARD_VERSION     Tag of the extension image built and mounted in Envoy (default: the
#                          version in the extension's manifest.yaml).
#
# The keys are stored in Secrets and are never printed.

set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SWITCHYARD_VERSION="${SWITCHYARD_VERSION:-$(sed -ne 's/^version: *//p' "${HERE}/../../manifest.yaml")}"
CLUSTER="${CLUSTER:-switchyard-judges}"
NODE_IMAGE="${NODE_IMAGE:-kindest/node:v1.37.0}"
EG_VERSION="${EG_VERSION:-v1.9.2}"
AR_VERSION="${AR_VERSION:-v0.0.0-latest}"
AR_REF="${AR_REF:-main}"
KEEP_CLUSTER="${KEEP_CLUSTER:-true}"
PORT="${PORT:-18081}"
EFFICIENT_MODEL="${EFFICIENT_MODEL:-gpt-6.1-terra}"
CAPABLE_MODEL="${CAPABLE_MODEL:-claude-opus-5-5}"
CLASSIFIER_MODEL="${CLASSIFIER_MODEL:-claude-haiku-5-5}"
ANSWER_MAX_TOKENS="${ANSWER_MAX_TOKENS:-1200}"
REASONING_EFFORT="${REASONING_EFFORT-low}"

OUT="${HERE}/out"
MANIFESTS="${OUT}/manifests"
IMAGE="ghcr.io/tetratelabs/built-on-envoy/extension-switchyard:${SWITCHYARD_VERSION}"
# Judge id -> virtual model. Must match switchyard-policy.yaml.
JUDGES=(jev:coding-jev classifier:coding-classifier)

log() { echo "==> $*"; }
die() { echo "$*" >&2; exit 1; }

# Checks the environment and splits INFERENCE_BASE_URL into INFERENCE_SCHEME, INFERENCE_HOST,
# INFERENCE_PORT, INFERENCE_PREFIX (the base path, "/" when empty) and INFERENCE_CHAT_PATH.
parse_env() {
  [[ -n "${INFERENCE_BASE_URL:-}" ]] \
    || die "Set INFERENCE_BASE_URL to an OpenAI-compatible base URL, for example https://llm.example.com/v1."
  local name
  for name in INFERENCE_API_KEY_FILE JEV_API_KEY_FILE; do
    [[ -n "${!name:-}" && -s "${!name}" ]] || die "Set ${name} to a non-empty file holding the API key."
  done

  local url="${INFERENCE_BASE_URL%/}" rest hostport base_path=""
  INFERENCE_SCHEME="${url%%://*}"
  rest="${url#*://}"
  [[ "${INFERENCE_SCHEME}" == "http" || "${INFERENCE_SCHEME}" == "https" ]] && [[ "${rest}" != "${url}" ]] \
    || die "INFERENCE_BASE_URL must start with http:// or https://."
  hostport="${rest%%/*}"
  [[ "${rest}" == */* ]] && base_path="/${rest#*/}"
  INFERENCE_HOST="${hostport%%:*}"
  if [[ "${hostport}" == *:* ]]; then
    INFERENCE_PORT="${hostport##*:}"
  elif [[ "${INFERENCE_SCHEME}" == "https" ]]; then
    INFERENCE_PORT=443
  else
    INFERENCE_PORT=80
  fi
  [[ "${INFERENCE_HOST}" =~ ^[A-Za-z0-9.-]+$ ]] || die "INFERENCE_BASE_URL has no valid host."
  [[ "${INFERENCE_PORT}" =~ ^[0-9]+$ ]] || die "INFERENCE_BASE_URL has no valid port."
  # Agent Router uses its own default, /v1, for an empty prefix.
  INFERENCE_PREFIX="${base_path:-/}"
  INFERENCE_CHAT_PATH="${base_path}/chat/completions"

  # The values are substituted into YAML with sed, so they are limited to safe characters.
  for name in INFERENCE_PREFIX EFFICIENT_MODEL CAPABLE_MODEL CLASSIFIER_MODEL; do
    [[ "${!name}" =~ ^[A-Za-z0-9._/:@+-]+$ ]] || die "${name} has unsupported characters: ${!name}"
  done
}

# Renders the manifest templates into MANIFESTS. Lines between "# https-only-begin" and
# "# https-only-end" are dropped for a plain http endpoint.
render_manifests() {
  local tls_filter=(-e '/# https-only-begin/d' -e '/# https-only-end/d') file
  [[ "${INFERENCE_SCHEME}" == "http" ]] && tls_filter=(-e '/# https-only-begin/,/# https-only-end/d')
  mkdir -p "${MANIFESTS}"
  for file in gateway.yaml routes.yaml switchyard-policy.yaml; do
    sed "${tls_filter[@]}" \
      -e "s|\${INFERENCE_HOST}|${INFERENCE_HOST}|g" \
      -e "s|\${INFERENCE_PORT}|${INFERENCE_PORT}|g" \
      -e "s|\${INFERENCE_PREFIX}|${INFERENCE_PREFIX}|g" \
      -e "s|\${INFERENCE_CHAT_PATH}|${INFERENCE_CHAT_PATH}|g" \
      -e "s|\${EFFICIENT_MODEL}|${EFFICIENT_MODEL}|g" \
      -e "s|\${CAPABLE_MODEL}|${CAPABLE_MODEL}|g" \
      -e "s|\${CLASSIFIER_MODEL}|${CLASSIFIER_MODEL}|g" \
      -e "s|\${SWITCHYARD_VERSION}|${SWITCHYARD_VERSION}|g" \
      "${HERE}/${file}" > "${MANIFESTS}/${file}"
    if grep -nE '\$\{[A-Z_]+\}' "${MANIFESTS}/${file}" >&2; then
      die "Unrendered placeholders in ${MANIFESTS}/${file}"
    fi
  done
}

# run_record JUDGE CASE_ID HTTP_STATUS SECONDS RESPONSE_FILE
#
# Prints one JSON line for a request: the status, the end-to-end latency, and the answer text,
# truncated to 1200 characters, or null when the response has none.
run_record() {
  jq -Rsc --arg judge "$1" --arg id "$2" --arg status "$3" --arg seconds "$4" '
    (fromjson? // {}) as $r
    | {judge: $judge, id: $id,
       status: ($status | tonumber? // 0),
       latency_ms: (($seconds | tonumber? // 0) * 1000 | round),
       answer: ($r | try .choices[0].message.content catch null
                   | if type == "string" and . != "" then .[0:1200] else null end),
       finish_reason: ($r | try .choices[0].finish_reason catch null)}' "$5"
}

# assemble_results CASES_JSON RUNS_JSONL ACCESS_JSONL RUN_ID
#
# Prints the results JSON (v2) from the cases, the run_record lines, and this run's access log
# lines, whose "case" field is "RUN_ID:JUDGE:CASE_ID".
assemble_results() {
  local judges
  judges="$(jq -n --arg classifier_model "${CLASSIFIER_MODEL}" '[
    {id: "jev", virtual_model: "coding-jev", kind: "decision", format: "system_one",
     model: "jev-latest", algorithm: {type: "llm_classifier", mode: "capability", cutoff: 0.15}},
    {id: "classifier", virtual_model: "coding-classifier", kind: "llm", format: "openai_chat",
     model: $classifier_model,
     algorithm: {type: "llm_classifier", mode: "capability", base_threshold: 0.6}}]')"
  jq -n --slurpfile cases "$1" --slurpfile runs "$2" --slurpfile access "$3" \
    --arg run "$4:" --arg now "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    --arg efficient "${EFFICIENT_MODEL}" --arg capable "${CAPABLE_MODEL}" --argjson judges "${judges}" '
    def present: if . == null or . == "" or . == "-" then null else . end;
    def number: present | if type == "string" then tonumber? else . end;
    ($access | map(select((.case // "") | startswith($run)) | {key: (.case | ltrimstr($run)), value: .})
     | from_entries) as $log
    | ($runs | map({key: "\(.judge):\(.id)", value: .}) | from_entries) as $res
    | {
      generated_at: $now,
      environment: "kind + Envoy Gateway + Agent Router",
      targets: {efficient: $efficient, capable: $capable},
      judges: $judges,
      cases: ($cases[0] | map(. as $c | {id, title, prompt, expected_tier,
        runs: ($judges | map(.id as $j | ($log["\($j):\($c.id)"] // {}) as $l
          | ($res["\($j):\($c.id)"] // {}) as $r
          | {key: $j, value: {
              selected_model: ($l.selected_model | present),
              served_model: ($l.served_model | present),
              reason: ($l.reason | present),
              evidence: (($l.evidence | present) as $e
                         | if $e == null then null else ($e | if type == "string" then (fromjson? // .) else . end) end),
              status: ($r.status // ($l.status | number)),
              latency_ms: $r.latency_ms,
              upstream_latency_ms: ($l.upstream_latency_ms | number),
              answer: $r.answer, finish_reason: $r.finish_reason}}) | from_entries)}))
    }'
}

main() {
  parse_env
  render_manifests
  if [[ "${1:-}" == "--render-only" ]]; then
    log "Rendered the manifests into ${MANIFESTS}"
    return 0
  fi
  [[ -z "${1:-}" ]] || die "Usage: run.sh [--render-only]"

  local tool
  for tool in docker kind kubectl helm curl jq; do
    command -v "${tool}" >/dev/null || die "${tool} is required"
  done

  local ROOT RUST_VERSION ARCH
  ROOT="$(git -C "${HERE}" rev-parse --show-toplevel)"
  RUST_VERSION="$(sed -ne 's/^channel = "\(.*\)"/\1/p' "${ROOT}/rust-toolchain.toml")"
  ARCH="$(docker version --format '{{.Server.Arch}}')"

  trap cleanup EXIT

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
  helm upgrade -i aieg oci://docker.io/envoyproxy/ai-gateway-helm \
    --version "${AR_VERSION}" \
    --namespace envoy-ai-gateway-system --create-namespace \
    --set controller.requestHeaderAttributes='x-switchyard-virtual-model:switchyard.virtual_model\,x-switchyard-reason:switchyard.reason'
  kubectl wait --timeout=3m -n envoy-ai-gateway-system deployment/ai-gateway-controller --for=condition=Available

  log "Creating the API key Secrets"
  # The Envoy pod mounts judge-keys, so it must exist, in the namespace of the Envoy pods, before
  # the Gateway is applied. Agent Router reads inference-api-key next to its route.
  kubectl create secret generic judge-keys -n envoy-gateway-system \
    --from-file=jev-api-key="${JEV_API_KEY_FILE}" \
    --from-file=inference-api-key="${INFERENCE_API_KEY_FILE}" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  kubectl create secret generic inference-api-key -n default \
    --from-file=apiKey="${INFERENCE_API_KEY_FILE}" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null

  log "Applying the example"
  kubectl apply -f "${MANIFESTS}/gateway.yaml"
  kubectl apply -f "${MANIFESTS}/routes.yaml"
  kubectl apply -f "${MANIFESTS}/switchyard-policy.yaml"
  # Not "Programmed": kind has no load balancer to assign the Gateway an address.
  kubectl wait --timeout=3m gateway/switchyard --for=condition=Accepted
  kubectl wait --timeout=3m envoyextensionpolicy/switchyard \
    --for=jsonpath='{.status.ancestors[0].conditions[?(@.type=="Accepted")].status}'=True
  local SELECTOR="gateway.envoyproxy.io/owning-gateway-name=switchyard,gateway.envoyproxy.io/owning-gateway-namespace=default"
  # The Envoy deployment is created asynchronously.
  for _ in $(seq 60); do
    kubectl get deployment -n envoy-gateway-system -l "${SELECTOR}" -o name | grep -q . && break
    sleep 2
  done
  kubectl wait --timeout=3m -n envoy-gateway-system deployment -l "${SELECTOR}" --for=condition=Available
  local DEPLOYMENT
  DEPLOYMENT="$(kubectl get deployment -n envoy-gateway-system -l "${SELECTOR}" -o name)"
  # The module image volume and the key Secret are only read when a pod starts. Restart Envoy so
  # that it loads the image built above and the current keys, also when the cluster is reused.
  kubectl rollout restart -n envoy-gateway-system "${DEPLOYMENT}"
  kubectl rollout status --timeout=3m -n envoy-gateway-system "${DEPLOYMENT}"

  local SERVICE
  SERVICE="$(kubectl get service -n envoy-gateway-system -l "${SELECTOR}" -o name)"
  kubectl port-forward -n envoy-gateway-system "${SERVICE}" "${PORT}:80" >/dev/null &
  PF_PID=$!
  for _ in $(seq 30); do
    curl -s -o /dev/null "http://localhost:${PORT}" && break
    sleep 1
  done

  # The configuration may take a few seconds to reach Envoy. A target model does not call a judge.
  local status
  for _ in $(seq 30); do
    status="$(curl -s -o /dev/null -w '%{http_code}' --max-time 90 "http://localhost:${PORT}/v1/chat/completions" \
      -H 'content-type: application/json' \
      -d "$(jq -cn --arg m "${EFFICIENT_MODEL}" '{model: $m, max_tokens: 16, messages: [{role: "user", content: "hi"}]}')")" || true
    [[ "${status}" == "200" ]] && break
    sleep 2
  done
  [[ "${status}" == "200" ]] || die "${EFFICIENT_MODEL} is not served through the Gateway (status ${status})."

  mkdir -p "${OUT}"
  local WORK RUN_ID START_TIME
  WORK="$(mktemp -d)"
  # Tags this run's access log lines, so lines from earlier runs on a reused cluster are ignored.
  RUN_ID="$(date +%s)"
  START_TIME="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

  jq -c '.[]' "${HERE}/cases.json" > "${WORK}/cases.jsonl"
  local CASE_COUNT
  CASE_COUNT="$(wc -l < "${WORK}/cases.jsonl" | tr -d ' ')"
  log "Sending ${CASE_COUNT} cases to each of ${#JUDGES[@]} virtual models"
  : > "${WORK}/runs.jsonl"
  local entry judge vm case_json id timing seconds
  for entry in "${JUDGES[@]}"; do
    judge="${entry%%:*}"
    vm="${entry#*:}"
    while IFS= read -r case_json; do
      id="$(jq -r .id <<<"${case_json}")"
      jq -c --arg vm "${vm}" --argjson mt "${ANSWER_MAX_TOKENS}" --arg effort "${REASONING_EFFORT}" \
        '{model: $vm, max_tokens: $mt, messages: [{role: "user", content: .prompt}]}
         + (if $effort == "" then {} else {reasoning_effort: $effort} end)' \
        <<<"${case_json}" > "${WORK}/${judge}-${id}.body"
      timing="$(curl -s -o "${WORK}/${judge}-${id}.out" -w '%{http_code} %{time_total}' --max-time 90 \
        "http://localhost:${PORT}/v1/chat/completions" \
        -H 'content-type: application/json' \
        -H "x-demo-case: ${RUN_ID}:${judge}:${id}" \
        --data-binary "@${WORK}/${judge}-${id}.body")" || timing="000 0"
      touch "${WORK}/${judge}-${id}.out"
      read -r status seconds <<<"${timing}"
      run_record "${judge}" "${id}" "${status}" "${seconds}" "${WORK}/${judge}-${id}.out" >> "${WORK}/runs.jsonl"
      echo "${vm} ${id}: status ${status}, ${seconds}s"
    done < "${WORK}/cases.jsonl"
  done

  log "Reading decisions from the Envoy access log"
  local expected=$((CASE_COUNT * ${#JUDGES[@]}))
  # Envoy flushes the access log about once a second. Read every Envoy pod: right after a rollout
  # an old pod may still be terminating, and kubectl logs on the deployment picks just one.
  for _ in $(seq 30); do
    kubectl logs -n envoy-gateway-system -l "${SELECTOR}" -c envoy --since-time="${START_TIME}" \
      --prefix=false --max-log-requests=10 --tail=-1 \
      | jq -Rc --arg run "${RUN_ID}:" 'fromjson? | select((.case // "") | startswith($run))' \
      > "${WORK}/access.jsonl" || true
    [[ "$(wc -l < "${WORK}/access.jsonl" | tr -d ' ')" -ge "${expected}" ]] && break
    sleep 1
  done

  jq -s . "${WORK}/cases.jsonl" > "${WORK}/cases.json"
  assemble_results "${WORK}/cases.json" "${WORK}/runs.jsonl" "${WORK}/access.jsonl" "${RUN_ID}" \
    > "${OUT}/results.json"
  rm -rf "${WORK}"

  print_summary "${OUT}/results.json"
  log "Wrote ${OUT}/results.json"

  if [[ -f "${HERE}/report/build_report.py" ]]; then
    (cd "${HERE}" && python3 report/build_report.py out/results.json out/report.html)
    log "Wrote ${OUT}/report.html"
  fi

  log "Done. The cluster ${CLUSTER} is kept unless KEEP_CLUSTER=false."
}

# Prints, per case, the tier each judge picked, and each judge's agreement with the human label.
print_summary() {
  jq -r '.targets as $t | ($t | to_entries | map({key: .value, value: .key}) | from_entries) as $tier
    | (["case", "expected"] + (.judges | map(.id))),
      (.cases[] | [.id, .expected_tier] + ([.runs[] | ($tier[.selected_model // ""] // "-")]))
    | @tsv' "$1" | column -t -s $'\t'
  jq -r '.targets as $t | ($t | to_entries | map({key: .value, value: .key}) | from_entries) as $tier
    | .cases as $cases | .judges[].id as $j
    | "\($j) agreement: \($cases | map(select($tier[.runs[$j].selected_model // ""] == .expected_tier)) | length)/\($cases | length)"' "$1"
}

cleanup() {
  [[ -n "${PF_PID:-}" ]] && kill "${PF_PID}" 2>/dev/null
  [[ "${KEEP_CLUSTER}" == "false" ]] && kind delete cluster --name "${CLUSTER}"
  return 0
}

# Sourcing the script defines the functions only, for offline checks.
if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
