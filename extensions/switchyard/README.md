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

The algorithms run on a small Tokio runtime owned by the module. Envoy worker threads block on it
for the duration of a decision, which never waits on I/O: only algorithms that decide from the
request alone are supported.

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
  "rewrite_body": true
}
```

Only `virtual_models` is required. The other values above are the defaults. Judges, model calls
and eligible models use the optional fields described in [Judges and model calls](#judges-and-model-calls)
and [Eligible models](#eligible-models).

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

| Type | Behaviour |
|---|---|
| `auto` | Switchyard's recommended preset: a `stage_router` that starts on the efficient tier and escalates on errors and stalls. Needs `capable_target` and `efficient_target`. |
| `stage_router` | Picks a tier per turn by scoring recent tool results, optionally with a `classifier` judge. |
| `plan_execute` | Plans on the capable target and hands off to the efficient one after the first edit, or when a `judge` approves. |
| `random` | Weighted split across `targets`. |
| `passthrough` | Always the same `target`. |
| `llm_classifier` | A judge picks the target. |
| `composite` | A judge picks the tier at each user turn, and a stage router runs the turns within it. |
| `vgr` | Answers with the efficient model, unless a verifier escalates the request to the capable model. |
| `advisor` | An executor model answers, and an advisor model reviews some answers first. |
| `noop` | Answers `OK` without calling a model, for smoke tests. |

`prefill_router`, which runs a PyTorch model in process, is not supported.

### Judges and model calls

Algorithms that call a model reach it in one of two ways:

- `decision_targets` and `judge_targets` hold judges with their own endpoint and API key, such as
  Jev or a provider's chat API. The filter never sends them client headers.
- `model_calls` sends calls to the gateway itself, so that its credentials, rate limits, costs and
  telemetry apply. Judges named by a key of `targets`, and the models `vgr` and `advisor` call,
  go this way.

An algorithm names a judge as in Switchyard, for example with `classifier_target`. The filter looks
the name up in `decision_targets`, then `judge_targets`, then the virtual model's `targets`. A
decision judge, set with an algorithm `decision` block, must be in `decision_targets`. Config load
fails when a name is in both judge maps or in none, or when an algorithm needs `model_calls` and it
isn't set.

`decision_targets` and `judge_targets` map a judge name to:

| Field | Description |
|---|---|
| `format` | `system_one` or `openai_decisions` for decision targets. `openai_chat`, `openai_responses` or `anthropic_messages` for judge targets. |
| `model` | The provider model sent in the request body, for example `jev-latest`. |
| `cluster` | The Envoy cluster the calls go to. |
| `authority` | The `Host` header of the calls. It must match the host name the judge's TLS certificate expects. |
| `path` | Defaults to `/v1/systemone`, `/v1/decisions`, `/v1/chat/completions`, `/v1/responses` or `/v1/messages`, by format. |
| `api_key` | Optional. `{"inline": "..."}` or `{"file": "/path"}`, read once at config load. Sent as `authorization: Bearer <key>`, or as `x-api-key` for `anthropic_messages`. |
| `headers` | Extra request headers. |
| `timeout_ms` | Defaults to 5000 for decision targets and 30000 for judge targets. |
| `trace_context` | `w3c`, `b3` or `none`. Defaults to `w3c`. |

`model_calls`:

| Field | Description |
|---|---|
| `cluster` | Required. The Envoy cluster the calls go to, usually one that loops back to the gateway. |
| `authority` | Defaults to the client request's. |
| `path` | Defaults to the client request's path, or to the standard path of `format` when it differs from the client's. |
| `format` | `openai_chat`, `openai_responses` or `anthropic_messages`. Defaults to the client request's. |
| `timeout_ms` | Defaults to 10000. |
| `forward_headers` | Client request headers copied to the calls, such as credentials and tenant headers, so that the gateway authenticates and charges each call like the client request. |
| `headers` | Extra request headers. |
| `trace_context` | `w3c`, `b3` or `none`. Defaults to `w3c`. |

This example routes `coding-auto` with Jev. The `typesafe` cluster must exist in Envoy and connect
to `api.typesafe.ai:443` with TLS:

```json
{
  "decision_targets": {
    "jev": {
      "format": "system_one",
      "model": "jev-latest",
      "cluster": "typesafe",
      "authority": "api.typesafe.ai",
      "api_key": {"file": "/etc/switchyard/jev-api-key"}
    }
  },
  "virtual_models": [{
    "name": "coding-auto",
    "targets": {"efficient": "gpt-4.1-mini", "capable": "gpt-5"},
    "algorithm": {
      "type": "llm_classifier", "mode": "capability", "classifier_target": "jev",
      "strong_target": "capable", "weak_target": "efficient", "classify_trigger": "new_session",
      "decision": {"cutoff": 0.05, "candidates": {"candidate_a": "capable", "candidate_b": "efficient"}, "evidence": {}}
    },
    "default_model": "gpt-4.1-mini"
  }]
}
```

For `model_calls`, [`loopback.yaml`](examples/agent-router/loopback.yaml) adds a cluster that loops
back to the Gateway listener. Agent Router routes those calls by their model, so a gateway judge
must be a model of an `AIGatewayRoute`, not a virtual model.

Calls are non-blocking Envoy HTTP callouts, but the request waits for the algorithm, so each call
adds latency. Bound it with the algorithm's own deadline, such as `judge_deadline_ms`, and with a
`classify_trigger` such as `new_session` instead of `every_request`. A failed model call moves to
the next candidate model on a transport error, a 5xx, 403, 408 or 429 status, or an exceeded context
window. Otherwise the algorithm applies its own fallback, such as `fail_open`, and when the
algorithm fails, the virtual model's `failure_mode` applies. When the client goes away, the filter
stops the run.

Every call carries `x-switchyard-call` (`judge`, `model` or `decision`), `x-switchyard-parent-id`
(the client request's `x-request-id`) and, when Envoy traces the request, trace context in the
format of its `trace_context`. Calls are sampled unless the client's trace context says it is not.

The algorithms of a virtual model that calls models through `model_calls` cannot route to another
virtual model: the call would loop back through the filter and start another routing run.

When `vgr`, `advisor` or `noop` writes the answer, the filter sends it to the client as a 200
response in the client's API format, as server-sent events when the client asked for a stream.

### Eligible models

Set `eligible_models_header` to limit the models of each request, for example to the models of a
tenant's plan. The header holds comma-separated model names, and the algorithm only sees the
eligible targets. Judges are never limited. Routing fails, and `failure_mode` applies, when no
target is eligible. A trusted filter, such as an ext_authz service, must set the header. Never let
the client set it.

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
  `outcome` (`routed`, `answered`, `default`, `passthrough` or `rejected`). Answers sent by the
  filter have the response details `switchyard_answered`.
- For judge algorithms, the judge's evidence as compact JSON, in the `evidence` dynamic metadata
  key and the `io.builtonenvoy.switchyard.evidence` filter state key.
- The `switchyard_judge_calls` counter, with the labels `judge` and `outcome` (`ok`, `error` or
  `timeout`), one per call.
- For algorithms that call models, the dynamic metadata keys `routing_calls`,
  `routing_input_tokens`, `routing_output_tokens`, `routing_total_tokens`, `routing_models`, and
  `routing_latency_ms` (from the start of routing to its outcome). When `routing_headers` is true,
  the calls and tokens also go in the `x-switchyard-routing-calls`, `-input-tokens`,
  `-output-tokens` and `-total-tokens` response headers.

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
- [`policies/`](examples/agent-router/policies): one `EnvoyExtensionPolicy` per algorithm and
  feature, with the filter configuration. [`policies/auto.yaml`](examples/agent-router/policies/auto.yaml)
  is the default.
- [`judge.yaml`](examples/agent-router/judge.yaml) and
  [`loopback.yaml`](examples/agent-router/loopback.yaml): a fake judge and decision API, and the
  clusters that reach them and the gateway itself.
- [`routes.yaml`](examples/agent-router/routes.yaml): the `AIGatewayRoute` with a rule per target
  model, like any other Agent Router setup. A rule for the virtual model is optional: it lists the
  virtual model in `/v1/models`, and serves requests the filter leaves unchanged.
- [`testupstream.yaml`](examples/agent-router/testupstream.yaml): fake model servers for local tests.
- [`kind.sh`](examples/agent-router/kind.sh): creates a kind cluster, installs Envoy Gateway and
  Agent Router, applies a policy, and checks the routing. `./kind.sh all` checks every policy.

[`examples/judge-routing`](examples/judge-routing) runs Jev and an LLM classifier side by side
against real models, and writes an HTML report.

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
                reference: ghcr.io/tetratelabs/built-on-envoy/extension-switchyard:0.1.1
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
FROM ghcr.io/tetratelabs/built-on-envoy/extension-switchyard:0.1.1 AS switchyard
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

## Compatibility

- Built with the dynamic modules SDK of Envoy 1.39.1. The dynamic modules ABI is unchanged across
  Envoy 1.39 patch releases, including 1.39.6, the default of the Agent Router CLI.
- The Switchyard crates are pinned to a specific commit, as Switchyard has not reached 1.0.
