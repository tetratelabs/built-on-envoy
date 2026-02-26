# OpenFGA Extension Demo

This demo shows the OpenFGA authorization filter with **multi-rule configuration**:
AI model access control, MCP tool access control, and a catch-all resource rule.

## Authorization Model

The demo uses an OpenFGA store with the following types and relations:

| Type     | Relation   | Description                          |
|----------|------------|--------------------------------------|
| `user`   | —          | User identity                        |
| `model`  | `can_use`  | User can use an AI model             |
| `tool`   | `can_invoke` | User can invoke an MCP tool        |
| `resource` | `can_access` | User can access a generic resource |

## Demo Scenario

```
                    ┌─────────────────────────────────────────────────────────┐
                    │                     OpenFGA Store                         │
                    │                                                          │
  user:alice ───────►│  can_use   model:gpt-4          ✓                       │
                    │  can_invoke tool:github__issue_read  ✓                   │
                    │  can_access resource:planning       ✓                   │
                    │                                                          │
  user:bob ─────────►│  (no relations)                    ✗ all checks fail    │
                    └─────────────────────────────────────────────────────────┘
```

**Tuples:**
- `user:alice` → `can_use` → `model:gpt-4`
- `user:alice` → `can_invoke` → `tool:github__issue_read`
- `user:alice` → `can_access` → `resource:planning`
- `user:bob` has no relations

## Prerequisites

- [Docker](https://docs.docker.com/get-docker/)
- [Built On Envoy CLI](https://github.com/tetratelabs/built-on-envoy) (`boe`)
- [jq](https://jqlang.github.io/jq/)

## 1. Start OpenFGA

```bash
cd extensions/composer/openfga/demo
docker compose up
```

Leave this running. OpenFGA runs on port 8082 (HTTP API) and 8083 (gRPC) by default to avoid conflicts with other services.

## 2. Configure the Store

In another terminal, run the setup script to create the store, authorization model, and tuples:

```bash
cd extensions/composer/openfga/demo
./setup.sh
```

The script prints the store ID. Export it for the next step:

```bash
export OPENFGA_STORE_ID=<store_id_from_output>
```

## 3. Run the Extension

From the **repo root**:

```bash
OPENFGA_STORE_ID=<your_store_id> ./extensions/composer/openfga/demo/run.sh
```

`run.sh` starts Envoy with the OpenFGA extension (multi-rule config) and sends test curls.

### Manual Run

To run the extension manually and test with your own curls:

```bash
boe run --local extensions/composer/openfga \
  --config '{
    "cluster": "127.0.0.1:8082",
    "openfga_host": "127.0.0.1:8082",
    "store_id": "YOUR_STORE_ID",
    "user": {"header": "x-user-id", "prefix": "user:"},
    "rules": [
      {"match": {"headers": {"x-ai-eg-model": "*"}}, "relation": {"value": "can_use"}, "object": {"header": "x-ai-eg-model", "prefix": "model:"}},
      {"match": {"headers": {"x-mcp-tool": "*"}}, "relation": {"value": "can_invoke"}, "object": {"header": "x-mcp-tool", "prefix": "tool:"}},
      {"relation": {"value": "can_access"}, "object": {"header": "x-resource-id", "prefix": "resource:"}}
    ]
  }' \
  --cluster-insecure 127.0.0.1:8082
```

Then in another terminal:

```bash
# Allowed (alice has can_use on gpt-4)
curl -H "x-user-id: alice" -H "x-ai-eg-model: gpt-4" http://localhost:10000/get

# Denied (bob has no relations)
curl -H "x-user-id: bob" -H "x-ai-eg-model: gpt-4" http://localhost:10000/get

# Allowed (alice has can_invoke on github__issue_read)
curl -H "x-user-id: alice" -H "x-mcp-tool: github__issue_read" http://localhost:10000/get

# Denied (bob has no relations)
curl -H "x-user-id: bob" -H "x-mcp-tool: github__issue_read" http://localhost:10000/get

# Catch-all: alice allowed on resource:planning
curl -H "x-user-id: alice" -H "x-resource-id: planning" http://localhost:10000/get

# Catch-all: bob denied
curl -H "x-user-id: bob" -H "x-resource-id: planning" http://localhost:10000/get
```

## 4. Expected Results

| Request                    | User  | Headers                          | Expected |
|---------------------------|-------|----------------------------------|----------|
| AI model gpt-4            | alice | x-ai-eg-model: gpt-4             | 200      |
| AI model gpt-4            | bob   | x-ai-eg-model: gpt-4             | 403      |
| MCP tool github__issue_read | alice | x-mcp-tool: github__issue_read | 200      |
| MCP tool github__issue_read | bob   | x-mcp-tool: github__issue_read | 403      |
| Resource planning         | alice | x-resource-id: planning          | 200      |
| Resource planning         | bob   | x-resource-id: planning          | 403      |

## 5. Cleanup

```bash
docker compose down -v
```

## Troubleshooting

- **Port conflicts:** The demo uses ports 8082 and 8083 by default. If these are in use, edit `docker-compose.yaml` and set `OPENFGA_PORT` / `OPENFGA_URL` when running `setup.sh` and `run.sh`.
- **boe fails to download libcomposer (401):** Build from source: `cd extensions/composer && make install BOE_DATA_HOME=~/.local/share/boe COMPOSER_LITE=true`
- **boe not found:** Build with `make build` from the `cli/` directory, then add `cli/out` to your `PATH`.
