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

## How It Works

1. On `POST` requests with a JSON body to a configured endpoint, the filter holds the headers and
   buffers the body. Other requests are not buffered.
2. If the body's `model` is a configured virtual model (and its `match_headers` match), the filter
   decodes the request with Switchyard's translation layer and runs the algorithm.
3. The filter replaces the body's `model` with the selected model and updates `content-length`.
   When the algorithm changes the request beyond the model, the filter sends Switchyard's encoding
   of the request. Otherwise it keeps every other byte of the client's body.
4. It adds the decision headers and dynamic metadata, and lets the request continue.

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
  "decision_headers": true
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

| Type | Behaviour |
|---|---|
| `auto` | Switchyard's recommended preset: a `stage_router` that starts on the efficient tier and escalates on errors and stalls. Needs `capable_target` and `efficient_target`. |
| `stage_router` | Picks a tier per turn by scoring recent tool results. The `classifier` option, which calls a judge model, is not supported. |
| `plan_execute` | Plans on the capable target and hands off to the efficient one after the first edit. The `judge` trigger is not supported. |
| `random` | Weighted split across `targets`. |
| `passthrough` | Always the same `target`. |

Algorithms that call a judge model (`llm_classifier`, `composite`, `vgr`) or produce the answer
themselves (`advisor`) need the gateway to make model calls on their behalf, and are rejected.

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

### Observability

- Request headers, when `decision_headers` is true: `x-switchyard-virtual-model`,
  `x-switchyard-selected-model` and `x-switchyard-reason`. They are also sent to the upstream; set
  `decision_headers` to false to avoid that. These headers are always removed from the client
  request, so the upstream only sees the values set by the filter.
- Dynamic metadata in the `io.builtonenvoy.switchyard` namespace, with the keys `virtual_model`,
  `selected_model` and `reason`. Use them in access logs with `%DYNAMIC_METADATA(io.builtonenvoy.switchyard:selected_model)%`.
- The `switchyard_decisions` counter, with the labels `virtual_model`, `selected_model` and
  `outcome` (`routed`, `default`, `passthrough` or `rejected`).

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
- [`switchyard-policy.yaml`](examples/agent-router/switchyard-policy.yaml): the
  `EnvoyExtensionPolicy` with the filter configuration.
- [`routes.yaml`](examples/agent-router/routes.yaml): the `AIGatewayRoute` with a rule per target
  model, like any other Agent Router setup. A rule for the virtual model is optional: it lists the
  virtual model in `/v1/models`, and serves requests the filter leaves unchanged.
- [`testupstream.yaml`](examples/agent-router/testupstream.yaml): fake model servers for local tests.
- [`kind.sh`](examples/agent-router/kind.sh): creates a kind cluster, installs Envoy Gateway and
  Agent Router, applies the example, and checks the routing.

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

## Compatibility

- Built with the dynamic modules SDK of Envoy 1.39.1. The dynamic modules ABI is unchanged across
  Envoy 1.39 patch releases, including 1.39.6, the default of the Agent Router CLI.
- The Switchyard crates are pinned to a specific commit, as Switchyard has not reached 1.0.
