-- Copyright Built On Envoy
-- SPDX-License-Identifier: Apache-2.0
-- The full text of the Apache license is available in the LICENSE file at
-- the root of the repo.

-- Answers tools/list locally with the generated tool definitions. This is needed
-- because:
--   * In Envoy 1.38 the bridge can only forward tools/list upstream:
--     https://github.com/envoyproxy/envoy/blob/v1.38.7/source/extensions/filters/http/mcp_json_rest_bridge/mcp_json_rest_bridge_filter.cc#L300
--   * Envoy 1.39+ adds a local response (tool_list_local), but it does not support
--     tool annotations (readOnlyHint, destructiveHint, idempotentHint):
--     https://github.com/envoyproxy/envoy/blob/v1.40.0/api/envoy/extensions/filters/http/mcp_json_rest_bridge/v3/mcp_json_rest_bridge.proto#L253
-- This filter can be replaced by tool_list_local once 1.38 support is dropped,
-- at the cost of losing the annotations, or when the bridge supports them.

-- Replaced at config time with the actual tools list JSON.
local TOOLS_JSON = [=[__TOOLS_LIST_JSON__]=]

function envoy_on_request(request_handle)
  local meta = request_handle:streamInfo():dynamicMetadata():get("envoy.filters.http.mcp")
  if not meta or meta["method"] ~= "tools/list" then
    return
  end

  -- JSON-RPC 2.0 id can be string or number. The MCP filter preserves the
  -- original type in dynamic metadata (string_value vs number_value).
  local id = meta["id"]
  local id_str
  if type(id) == "number" then
    if id == math.floor(id) then
      id_str = string.format("%d", id)
    else
      id_str = tostring(id)
    end
  elseif type(id) == "string" then
    id_str = string.format("%q", id)
  else
    id_str = "null"
  end

  request_handle:respond(
    {[":status"] = "200", ["content-type"] = "application/json"},
    '{"jsonrpc":"2.0","id":' .. id_str .. ',"result":' .. TOOLS_JSON .. '}'
  )
end
