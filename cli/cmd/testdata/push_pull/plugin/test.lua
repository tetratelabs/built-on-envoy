-- Copyright Built On Envoy
-- SPDX-License-Identifier: Apache-2.0
-- The full text of the Apache license is available in the LICENSE file at
-- the root of the repo.

function envoy_on_request(request_handle)
    request_handle:logInfo("lua-e2e-test: received request")
end

function envoy_on_response(response_handle)
    response_handle:headers():add("x-e2e-lua", "lua-e2e-test")
end
