# Switchyard Model Routing

A Rust dynamic module that resolves **virtual models** with [NVIDIA NeMo Switchyard](https://github.com/NVIDIA-NeMo/Switchyard)
routing algorithms.

Clients send a virtual model name, such as `coding-auto`. For every request, the filter runs a
Switchyard algorithm in process and rewrites the `model` field of the request body to the model
that should serve it. For example, the `auto` algorithm sends agent steps that only read tool output
to an efficient model, and escalates to a capable model when tool calls fail.

The filter only changes the request. Routing to the selected model is left to the filters and
routes that follow it:

- With [Agent Router](https://github.com/theagentrouter/agent-router), nothing else is needed:
  Agent Router reads the model from the body after this filter has rewritten it. See
  [Using with Agent Router](#using-with-agent-router).
- On plain Envoy, set `model_header` and `clear_route_cache`, and match routes on that header.
- To let a later filter make the final choice, set `rewrite_body` to false. The body keeps the
  virtual model, and the decision is only carried in headers, dynamic metadata and filter state. See
  [Decision-only mode](#decision-only-mode).

## How It Works

1. On `POST` requests with a JSON body to a configured endpoint, the filter holds the headers and
   buffers the body. Other requests are not buffered.
2. If the body's `model` is a configured virtual model (and its `match_headers` match), the filter
   decodes the request with Switchyard's translation layer and runs the algorithm.
3. Unless `rewrite_body` is false, the filter replaces the body's `model` with the selected model
   and updates `content-length`. When the algorithm changes the request beyond the model, the
   filter sends Switchyard's encoding of the request. Otherwise it keeps every other byte of the
   client's body.
4. It records the decision in dynamic metadata, filter state and, when enabled, the decision
   headers, and lets the request continue.

Requests for any other model pass through unchanged.

Some algorithms call models before they decide: a judge that classifies the task, a verifier, or
an advisor that reviews an answer. Some even write the answer themselves. For those, the filter
holds the request and makes the calls as HTTP callouts to the cluster set in `model_calls`, usually
the gateway itself. When the algorithm is done, the filter either rewrites the model and resumes
the request, or sends the algorithm's answer to the client. See
[Algorithms that call models](#algorithms-that-call-models).

The algorithms run on a small Tokio runtime owned by the module. Envoy worker threads only wait
for the algorithm's first step, which never waits on I/O. Model calls are made asynchronously and
never block a worker thread.

## Configuration

```json
{
  "virtual_models": [
    {
      "name": "coding-auto",
      "match_headers": [{"name": "x-tier", "value": "free"}],
      "targets": {"efficient": "gpt-4.1-mini"},
      "algorithm": {"type": "passthrough", "target": "efficient"}
    },
    {
      "name": "coding-auto",
      "targets": {"efficient": "gpt-4.1-mini", "capable": "gpt-5"},
      "algorithm": {"type": "auto", "capable_target": "capable", "efficient_target": "efficient"},
      "default_model": "gpt-4.1-mini",
      "failure_mode": "use_default"
    }
  ],
  "session": {
    "id_header": "x-switchyard-session-id",
    "fallback_id_headers": ["agent-session-id"],
    "scope_headers": ["x-tenant-id"],
    "agent_id_header": "x-switchyard-agent-id",
    "subagent_header": "x-switchyard-is-subagent"
  },
  "endpoints": {
    "/v1/chat/completions": "openai_chat",
    "/v1/responses": "openai_responses",
    "/v1/messages": "anthropic_messages"
  },
  "model_header": null,
  "clear_route_cache": false,
  "decision_headers": true,
  "rewrite_body": true,
  "model_calls": null,
  "decision_calls": null,
  "eligible_models_header": null,
  "routing_headers": false
}
```

Only `virtual_models` is required. The other values above are the defaults.

### Virtual models

| Field | Description |
|---|---|
| `name` | The model name clients send. Several entries can share a name; the first whose `match_headers` all match wins. |
| `targets` | Switchyard target names mapped to the model names they route to. The algorithm refers to the target names. |
| `algorithm` | The Switchyard algorithm, in [Switchyard's route format](https://github.com/NVIDIA-NeMo/Switchyard). |
| `default_model` | Model used when routing fails. Must be one of the target models. |
| `failure_mode` | `use_default` (route to `default_model`), `passthrough` (leave the request unchanged), or `reject` (reply with a 503). Defaults to `use_default` when `default_model` is set, and to `passthrough` otherwise. |
| `match_headers` | Exact request header matches, for example to give a tenant or a tier its own policy. |

### Supported algorithms

| Type | Behaviour | Calls models |
|---|---|---|
| `auto` | Switchyard's recommended preset: a `stage_router` that starts on the efficient tier and escalates on errors and stalls. Needs `capable_target` and `efficient_target`. | No |
| `stage_router` | Picks a tier per turn by scoring recent tool results. | With a `classifier` |
| `plan_execute` | Plans on the capable target and hands off to the efficient one after the first edit, or when a judge approves. | With a `judge` |
| `random` | Weighted split across `targets`. | No |
| `passthrough` | Always the same `target`. | No |
| `llm_classifier` | A judge model, or a decision judge, picks the target. | Yes |
| `composite` | A judge picks the tier at each user turn, and a stage router runs the turns within it. | Yes |
| `vgr` | Tries the efficient model, verifies its answer, and escalates to the capable model when the verification fails. Answers with the efficient model's answer otherwise. | Yes, may answer |
| `advisor` | An executor model answers, and an advisor model reviews some answers before the client sees them. | Yes, answers |
| `noop` | Answers `OK` without calling a model, for smoke tests. | No, answers |

`prefill_router`, which runs a PyTorch model in process, is not supported.

### Algorithms that call models

Judges, verifiers and advisors are called through `model_calls`. Decision judges, configured with a
`decision` block, are called through `decision_calls`. The filter rejects a configuration whose
algorithms need calls that are not configured.

```json
{
  "virtual_models": [{
    "name": "coding-auto",
    "targets": {"efficient": "gpt-4.1-mini", "capable": "gpt-5", "judge": "gpt-4.1-nano"},
    "algorithm": {"type": "llm_classifier", "classifier_target": "judge",
                  "strong_target": "capable", "weak_target": "efficient", "base_threshold": 0.5},
    "default_model": "gpt-5"
  }],
  "model_calls": {
    "cluster": "gateway_loopback",
    "timeout_ms": 10000,
    "forward_headers": ["authorization", "x-tenant-id", "agent-session-id"],
    "trace_context": "w3c"
  },
  "routing_headers": true
}
```

`model_calls`:

| Field | Description |
|---|---|
| `cluster` | The Envoy cluster the calls are sent to. Point it at the gateway itself, so that the gateway applies credentials, provider translation, rate limits, costs and telemetry to the calls like to any other request. The judge is then a regular model of the gateway. |
| `authority` | The host of the calls. Defaults to the client request's. |
| `path` | The path of the calls. Defaults to the client request's path, or to the standard path of `format` when it differs from the client's. |
| `format` | The API format of the calls: `openai_chat`, `openai_responses` or `anthropic_messages`. Defaults to the client request's. |
| `timeout_ms` | Timeout of each call. Defaults to 10000. |
| `forward_headers` | Client request headers copied to the calls, such as credentials, tenant and session headers, so that the gateway authenticates and charges each call like the client request. |
| `headers` | Headers added to the calls. |
| `trace_context` | `w3c` (`traceparent`), `b3` or `none`. Makes the calls join the client request's trace. Defaults to `w3c`. |

`decision_calls` has the same fields, except that `path` (such as `/v1/systemone` or
`/v1/decisions`) and `format` (`system_one` or `openai_decisions`) are required.

Every call also carries:

- `x-switchyard-call`: `judge` for a model that only decides, `model` for a model that may answer
  the client, such as the efficient attempt of `vgr` or the executor of `advisor`, and `decision`
  for decision judges.
- `x-switchyard-parent-id`: the `x-request-id` of the client request, to join a call to the
  request it was made for.

Calls are always buffered, even when the client asked for a stream. When the algorithm writes the
answer, the filter sends it to the client as a local response in the client's API format, as
server-sent events when the client asked for a stream. The decision is recorded with the outcome
`answered`.

When a call fails, the algorithm applies its own fallback policy, such as `fail_open`. When the
algorithm fails, the virtual model's `failure_mode` applies.

Algorithms that call models only run while the request is held, so their judges add latency to
every routed request. Set the judge's own deadline, such as `judge_deadline_ms` or `vgr`'s
`deadline_seconds`, below `timeout_ms`.

#### Calling the gateway itself

With Envoy Gateway, an `EnvoyPatchPolicy` adds a cluster that points at the Gateway's own listener:

```yaml
apiVersion: gateway.envoyproxy.io/v1alpha1
kind: EnvoyPatchPolicy
metadata:
  name: switchyard-loopback
spec:
  targetRef: {group: gateway.networking.k8s.io, kind: Gateway, name: switchyard}
  type: JSONPatch
  jsonPatches:
    - type: type.googleapis.com/envoy.config.cluster.v3.Cluster
      name: gateway_loopback
      operation:
        op: add
        path: ""
        value:
          name: gateway_loopback
          type: STATIC
          connect_timeout: 1s
          load_assignment:
            cluster_name: gateway_loopback
            endpoints:
              - lb_endpoints:
                  - endpoint:
                      address:
                        socket_address: {address: 127.0.0.1, port_value: 10080}
```

`10080` is the container port of a Gateway listener on port 80. Agent Router routes the calls by
their model, so the judge must be a model of an `AIGatewayRoute`, not a virtual model.

### Eligible models

Set `eligible_models_header` to limit, per request, the models a request may be routed to, for
example to the models of a tenant's plan. The header holds comma-separated model names. The
algorithm only sees the eligible targets, and routing fails when none is eligible. Judges are not
limited, as they never answer. The header must be set by a trusted filter, such as an ext_authz
service, and never by the client. For a few static sets, `match_headers` gives each set its own
virtual model instead.

### Sessions

Stateful algorithms, such as `auto` and `stage_router`, keep per-session state, for example to hold
the capable tier for a few turns after an escalation. The session ID is the first non-empty header
among `id_header` and `fallback_id_headers`. `agent-session-id` is the header Agent Router already
records as the session of a request. The values of `scope_headers` prefix the session ID, so that
sessions of different tenants never share state.

Session state lives in the memory of each Envoy process. Send the requests of a session to the same
Envoy instance, for example with session affinity on the session header. Without a session ID,
requests are routed from their own content only.

Requests whose `subagent_header` is `true` or `1` are sub-agent requests. When the algorithm has a
`subagents` block, they are routed by it instead of by the parent algorithm. A sub-agent request
must also carry the agent ID in `agent_id_header` to keep its own session state; without it, the
request is routed statelessly.

### Decision-only mode

By default, the filter rewrites the body, so the filters after it only see the selected model. Set
`rewrite_body` to false to keep the virtual model in the body and only record the decision. A later
filter then sees both the virtual model and the decision, and can apply its own policy, such as
quotas, budgets or fallbacks, before it picks the backend:

```json
{
  "virtual_models": [{
    "name": "coding-auto",
    "targets": {"efficient": "gpt-4.1-mini", "capable": "gpt-5"},
    "algorithm": {"type": "auto", "capable_target": "capable", "efficient_target": "efficient"}
  }],
  "rewrite_body": false,
  "decision_headers": false
}
```

The decision is always available to later filters as:

- Dynamic metadata in the `io.builtonenvoy.switchyard` namespace.
- Filter state, as plain strings under the `io.builtonenvoy.switchyard.virtual_model`,
  `io.builtonenvoy.switchyard.selected_model` and `io.builtonenvoy.switchyard.reason` keys. Filter
  state is never sent upstream, and is available to other filters, to access logs with
  `%FILTER_STATE(io.builtonenvoy.switchyard.selected_model:PLAIN)%`, to header formatters, and to
  CEL expressions as `filter_state['io.builtonenvoy.switchyard.selected_model']`.

The decision headers and `model_header` carry it too, when enabled. Headers are easy to consume
from any filter, including ext_proc and ext_authz services, but they are also sent to the upstream
unless a later filter removes them.

Clients can never set the decision: the filter removes client-sent decision headers, and dynamic
metadata and filter state are only set by filters. Envoy does not, however, protect filter state or
dynamic metadata from other filters: since Envoy removed read-only filter state, a filter can
overwrite any key. Treat the decision as trusted only if the filters after this one are.

When the algorithm changes the request beyond the model, those changes are dropped in this mode,
as only the decision is passed on.

With Agent Router, the body's model is what Agent Router routes on, so in this mode Agent Router
routes the virtual model unless a filter between the two acts on the decision.

### Observability

- Request headers, when `decision_headers` is true: `x-switchyard-virtual-model`,
  `x-switchyard-selected-model` and `x-switchyard-reason`. They are also sent to the upstream; set
  `decision_headers` to false to avoid that. These headers are always removed from the client
  request, so the upstream only sees the values set by the filter.
- Dynamic metadata in the `io.builtonenvoy.switchyard` namespace, with the keys `virtual_model`,
  `selected_model` and `reason`. Use them in access logs with
  `%DYNAMIC_METADATA(io.builtonenvoy.switchyard:selected_model)%`.
- Filter state under the `io.builtonenvoy.switchyard.virtual_model`,
  `io.builtonenvoy.switchyard.selected_model` and `io.builtonenvoy.switchyard.reason` keys. Use
  them in access logs with `%FILTER_STATE(io.builtonenvoy.switchyard.selected_model:PLAIN)%`.
- The `switchyard_decisions` counter, with the labels `virtual_model`, `selected_model` and
  `outcome` (`routed`, `answered`, `default`, `passthrough` or `rejected`).
- For algorithms that call models, the cost of the routing calls in the `io.builtonenvoy.switchyard`
  dynamic metadata: `routing_calls`, `routing_input_tokens`, `routing_output_tokens`,
  `routing_total_tokens`, `routing_latency_ms` and `routing_models`. With `routing_headers`, the
  response also carries `x-switchyard-routing-calls`, `x-switchyard-routing-input-tokens`,
  `x-switchyard-routing-output-tokens` and `x-switchyard-routing-total-tokens`.

## Running Locally

```shell
boe run --local extensions/switchyard --config '{
  "virtual_models": [{
    "name": "chat-split",
    "targets": {"a": "model-a", "b": "model-b"},
    "algorithm": {"type": "random", "targets": ["a", "b"], "weights": [0.5, 0.5]}
  }],
  "model_header": "x-model",
  "clear_route_cache": true
}'

curl localhost:10000/anything/v1/chat/completions -H 'content-type: application/json' \
  -d '{"model": "chat-split", "messages": [{"role": "user", "content": "hi"}]}'
```

The default upstream echoes the request, so the response shows the rewritten `model` and the
decision headers.

## Using with Agent Router

[Agent Router](https://github.com/theagentrouter/agent-router) (formerly Envoy AI Gateway) runs on
Envoy Gateway. The extension works with an unmodified Agent Router: it is added to the Gateway with
Envoy Gateway's dynamic module support, and Agent Router routes the rewritten request.

### Where the filter runs

```
client ─▶ … ─▶ switchyard ─▶ Agent Router ext_proc ─▶ … ─▶ router ─▶ selected model's backend
               (this module)  (envoy.filters.http.ext_proc/aigateway)
```

Agent Router's ext_proc filter reads the model from the request body, sets the `x-ai-eg-model`
header that Agent Router routes match, and makes Envoy pick the route again. Because this filter runs
first and rewrites the body, Agent Router sees the selected model, as if the client had asked for it.
Its credentials, request translation, rate limits, costs and metrics then apply to the selected
model's backend.

Two settings make this work:

1. **The filter must come before Agent Router's filter.** Agent Router inserts its ext_proc filter
   before the first `lua`, `ext_proc` or `wasm` filter of the listener. Envoy Gateway places dynamic
   modules after those, so the `EnvoyProxy` sets `filterOrder` to move the module before them.
   Without any such filter on the listener, the module is already in the right place.
2. **The policy must target the whole Gateway.** Envoy picks the filters for a request from the
   first route it matches, before the model is known. For Agent Router routes, that is usually the
   catch-all `route-not-found` rule, which a policy targeting specific routes or rules would miss.

### Resources

The [`examples/agent-router`](examples/agent-router) directory has a complete setup:

- [`gateway.yaml`](examples/agent-router/gateway.yaml): the `Gateway`, and an `EnvoyProxy` that
  registers the module, sets the filter order, and mounts the extension image as a volume.
- [`policies/`](examples/agent-router/policies): an `EnvoyExtensionPolicy` per feature. They all
  share the name `switchyard`, so applying one replaces the previous one.
- [`routes.yaml`](examples/agent-router/routes.yaml): the `AIGatewayRoute` with a rule per target
  model and for the judge, like any other Agent Router setup. A rule for the virtual model is
  optional: it lists the virtual model in `/v1/models`, and serves requests the filter leaves
  unchanged.
- [`loopback.yaml`](examples/agent-router/loopback.yaml): an `EnvoyPatchPolicy` with the clusters
  the algorithms call: the Gateway's own listener, for judge, verifier and advisor calls, and the
  decision API.
- [`testupstream.yaml`](examples/agent-router/testupstream.yaml) and
  [`judge.yaml`](examples/agent-router/judge.yaml): fake model servers and a fake judge for local
  tests. The fake judge rates tasks that contain `[hard]` as beyond the efficient model.
- [`kind.sh`](examples/agent-router/kind.sh): creates a kind cluster, installs Envoy Gateway and
  Agent Router, applies the example, and checks the routing of the given policies.

| Policy | Shows |
|---|---|
| [`auto`](examples/agent-router/policies/auto.yaml) | The recommended preset, a free tier with its own policy, and tenant-scoped sessions. |
| [`passthrough`](examples/agent-router/policies/passthrough.yaml) | Every request to one model. |
| [`random`](examples/agent-router/policies/random.yaml) | A weighted split. |
| [`stage-router`](examples/agent-router/policies/stage-router.yaml) | A tier per agent step, from the tool results. |
| [`stage-router-judge`](examples/agent-router/policies/stage-router-judge.yaml) | A judge decides the steps the tool results cannot. |
| [`plan-execute`](examples/agent-router/policies/plan-execute.yaml) | Planning on the capable model, and a hand-off on the first edit. |
| [`plan-execute-judge`](examples/agent-router/policies/plan-execute-judge.yaml) | A judge decides the hand-off. |
| [`llm-classifier`](examples/agent-router/policies/llm-classifier.yaml) | A judge model picks the model. |
| [`decision-judge`](examples/agent-router/policies/decision-judge.yaml) | A System One decision API picks the model. |
| [`composite`](examples/agent-router/policies/composite.yaml) | A judge per user turn, and a stage router within it. |
| [`vgr`](examples/agent-router/policies/vgr.yaml) | Verified answers of the efficient model, served by the filter, and escalation otherwise. |
| [`advisor`](examples/agent-router/policies/advisor.yaml) | An executor's answers, reviewed by an advisor. |
| [`noop`](examples/agent-router/policies/noop.yaml) | A fixed answer, without a model. |
| [`subagents`](examples/agent-router/policies/subagents.yaml) | A different policy for sub-agents. |
| [`eligible-models`](examples/agent-router/policies/eligible-models.yaml) | Eligible models limited per request. |
| [`decision-only`](examples/agent-router/policies/decision-only.yaml) | The decision passed on, without rewriting the body. |

```shell
# Set up the cluster and check a policy.
./examples/agent-router/kind.sh vgr
# Check other policies on the same cluster, or all of them.
./examples/agent-router/kind.sh llm-classifier advisor
./examples/agent-router/kind.sh all
# Or apply a policy, and send your own requests.
kubectl apply -f examples/agent-router/policies/composite.yaml
```

The key parts of the `EnvoyProxy`:

```yaml
spec:
  dynamicModules:
    - name: switchyard
      source:
        type: Local
        local:
          path: /var/boe/extensions/switchyard/libswitchyard.so
      doNotClose: true
  filterOrder:
    - name: envoy.filters.http.dynamic_modules
      before: envoy.filters.http.lua
    - name: envoy.filters.http.dynamic_modules
      before: envoy.filters.http.ext_proc
    - name: envoy.filters.http.dynamic_modules
      before: envoy.filters.http.wasm
  provider:
    type: Kubernetes
    kubernetes:
      envoyDeployment:
        pod:
          volumes:
            - name: switchyard
              image:
                reference: ghcr.io/tetratelabs/built-on-envoy/extension-switchyard:0.1.0
        container:
          volumeMounts:
            - name: switchyard
              mountPath: /var/boe/extensions/switchyard
              readOnly: true
```

### Getting the module into the Envoy pods

The example mounts the extension's OCI image as a Kubernetes
[image volume](https://kubernetes.io/docs/concepts/storage/volumes/#image), which is enabled by
default since Kubernetes 1.35. The image also contains `libgcc_s.so.1`, which the module needs and
which distroless Envoy images, such as Envoy Gateway's default, do not have.

Without image volumes, build an Envoy image that contains the module and set it as the
`envoyDeployment.container.image` of the `EnvoyProxy`:

```dockerfile
FROM ghcr.io/tetratelabs/built-on-envoy/extension-switchyard:0.1.0 AS switchyard
FROM docker.io/envoyproxy/envoy:distroless-v1.39.1
COPY --from=switchyard /libswitchyard.so /libgcc_s.so.1 /var/boe/extensions/switchyard/
```

The module can also be served over HTTP and referenced with a `Remote` source and its SHA-256, but
then `libgcc_s.so.1` must already be in the Envoy image.

### Telemetry

Agent Router can add the decision headers to its metrics, spans and access logs. For example, with
the Agent Router helm chart:

```shell
--set controller.requestHeaderAttributes='x-switchyard-virtual-model:switchyard.virtual_model\,x-switchyard-reason:switchyard.reason'
```

Agent Router records the selected model as both the original and the request model, because it never
sees the virtual model. The `switchyard.virtual_model` attribute keeps it.

When the algorithm calls models through the Gateway, Agent Router handles each call as its own
request: it applies the judge's credentials, records its token usage and cost, and counts it against
the rate limits of the headers in `forward_headers`. To tell routing calls apart from answers, and
join each call to the request it was made for, add the call headers to Agent Router's telemetry:

```shell
--set controller.requestHeaderAttributes='x-switchyard-call:switchyard.call' \
--set controller.spanRequestHeaderAttributes='x-switchyard-parent-id:switchyard.parent_id' \
--set controller.logRequestHeaderAttributes='x-switchyard-parent-id:switchyard.parent_id'
```

With `trace_context`, the calls are part of the client request's trace. The client request's
access log line can show the cost of the answer next to the cost of routing it:

```yaml
answer_total_tokens: "%DYNAMIC_METADATA(io.envoy.ai_gateway:llm_total_token)%"
routing_total_tokens: "%DYNAMIC_METADATA(io.builtonenvoy.switchyard:routing_total_tokens)%"
routing_models: "%DYNAMIC_METADATA(io.builtonenvoy.switchyard:routing_models)%"
```

When the algorithm answers, the answer comes from a call that Agent Router already processed and
accounted for, and the client's request is answered by the filter, before it reaches Agent Router.

## Compatibility

- Built with the dynamic modules SDK of Envoy 1.39.1. The dynamic modules ABI is unchanged across
  Envoy 1.39 patch releases, including 1.39.6, the default of the Agent Router CLI.
- The Switchyard crates are pinned to a specific commit, as Switchyard has not reached 1.0.
