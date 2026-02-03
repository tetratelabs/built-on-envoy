-- Copyright Built On Envoy
-- SPDX-License-Identifier: Apache-2.0
-- The full text of the Apache license is available in the LICENSE file at
-- the root of the repo.

-- Example Lua extension demonstrating most available Envoy HTTP Lua filter methods
-- Reference: https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_filters/lua_filter

-- envoy_on_request is called when a request is received
-- The request_handle provides access to headers, body, metadata, and various utilities
function envoy_on_request(request_handle)
    -- ============================================================
    -- LOGGING METHODS
    -- Available levels: logTrace, logDebug, logInfo, logWarn, logErr, logCritical
    -- ============================================================
    request_handle:logInfo("Processing incoming request")
    request_handle:logDebug("Request received at: " .. request_handle:timestamp())

    -- ============================================================
    -- HEADER MANIPULATION
    -- ============================================================
    local headers = request_handle:headers()

    -- Get header values
    local method = headers:get(":method")
    local path = headers:get(":path")
    local authority = headers:get(":authority")
    local user_agent = headers:get("user-agent")

    request_handle:logInfo("Request: " .. (method or "nil") .. " " .. (path or "nil"))

    -- Add custom headers
    headers:add("x-lua-processed", "true")
    headers:add("x-request-timestamp", tostring(request_handle:timestamp()))

    -- Replace a header (adds if not present)
    headers:replace("x-custom-header", "custom-value")

    -- Get number of values for a header (useful for multi-value headers)
    local cookie_count = headers:getNumValues("cookie")
    if cookie_count > 0 then
        request_handle:logDebug("Request has " .. cookie_count .. " cookie header(s)")
        -- Get specific cookie by index (0-based)
        local first_cookie = headers:getAtIndex("cookie", 0)
    end

    -- Iterate over all headers
    for key, value in pairs(headers) do
        request_handle:logTrace("Header: " .. key .. " = " .. value)
    end

    -- Remove a header
    headers:remove("x-unwanted-header")

    -- ============================================================
    -- STREAM INFORMATION
    -- ============================================================
    local stream_info = request_handle:streamInfo()

    -- Get protocol version (HTTP/1.0, HTTP/1.1, HTTP/2, HTTP/3)
    local protocol = stream_info:protocol()
    if protocol then
        headers:add("x-protocol", protocol)
    end

    -- Get route name if available
    local route_name = stream_info:routeName()
    if route_name then
        headers:add("x-route-name", route_name)
    end

    -- Get address information
    local downstream_local = stream_info:downstreamLocalAddress()
    local downstream_remote = stream_info:downstreamRemoteAddress()
    local direct_remote = stream_info:downstreamDirectRemoteAddress()

    if downstream_remote then
        headers:add("x-client-ip", downstream_remote)
    end

    -- Get SNI or requested server name
    local server_name = stream_info:requestedServerName()
    if server_name and server_name ~= "" then
        headers:add("x-server-name", server_name)
    end

    -- ============================================================
    -- DYNAMIC METADATA
    -- Can be used to pass data between filters
    -- ============================================================
    local dynamic_metadata = stream_info:dynamicMetadata()

    -- Set metadata that can be read by other filters or in access logs
    dynamic_metadata:set("envoy.filters.http.lua", "request.info", {
        method = method,
        path = path,
        authority = authority,
        timestamp = request_handle:timestamp()
    })

    -- ============================================================
    -- FILTER STATE
    -- Access shared state between filters
    -- ============================================================
    local filter_state = stream_info:filterState()
    -- Filter state values set by other filters can be retrieved
    -- local auth_state = filter_state:get("auth.validated")

    -- ============================================================
    -- SSL/TLS CONNECTION INFO
    -- ============================================================
    local ssl = stream_info:downstreamSslConnection()
    if ssl then
        -- Check if peer certificate was presented
        if ssl:peerCertificatePresented() then
            headers:add("x-client-cert-present", "true")

            -- Get peer certificate details
            local subject = ssl:subjectPeerCertificate()
            if subject then
                headers:add("x-client-cert-subject", subject)
            end

            local serial = ssl:serialNumberPeerCertificate()
            if serial then
                headers:add("x-client-cert-serial", serial)
            end

            -- Get certificate validity
            local valid_from = ssl:validFromPeerCertificate()
            local expiration = ssl:expirationPeerCertificate()

            -- Get SHA256 digest of peer certificate
            local digest = ssl:sha256PeerCertificateDigest()
            if digest then
                headers:add("x-client-cert-digest", digest)
            end

            -- Get DNS SANs
            local dns_sans = ssl:dnsSansPeerCertificate()
            -- Get URI SANs
            local uri_sans = ssl:uriSanPeerCertificate()
        end

        -- Get TLS version and cipher
        local tls_version = ssl:tlsVersion()
        if tls_version then
            headers:add("x-tls-version", tls_version)
        end

        local cipher = ssl:ciphersuiteString()
        if cipher then
            headers:add("x-tls-cipher", cipher)
        end
    end

    -- ============================================================
    -- ROUTE AND VIRTUAL HOST METADATA
    -- ============================================================
    local route = request_handle:route()
    if route then
        local route_metadata = route:metadata()
        -- Access route-specific metadata configured in Envoy
        local custom_config = route_metadata:get("custom_key")
    end

    local virtual_host = request_handle:virtualHost()
    if virtual_host then
        local vh_metadata = virtual_host:metadata()
        -- Access virtual host metadata
    end

    -- ============================================================
    -- FILTER CONTEXT
    -- Access per-route Lua configuration
    -- ============================================================
    local context = request_handle:filterContext()
    if context then
        local enabled = context["enabled"]
        local mode = context["mode"]
    end

    -- ============================================================
    -- CONNECTION INFO
    -- ============================================================
    local connection = request_handle:connection()
    -- Connection object provides ssl() method (deprecated, use streamInfo)

    -- ============================================================
    -- BODY HANDLING (streaming)
    -- Use bodyChunks() for streaming without buffering the entire body
    -- ============================================================
    -- Note: Uncommenting this will process body chunks as they arrive
    -- local total_size = 0
    -- for chunk in request_handle:bodyChunks() do
    --     total_size = total_size + chunk:length()
    --     request_handle:logDebug("Received chunk of size: " .. chunk:length())
    -- end
    -- headers:add("x-request-body-size-streamed", tostring(total_size))

    -- ============================================================
    -- BODY HANDLING (buffered)
    -- Using body() buffers the entire body - use carefully for large payloads
    -- ============================================================
    -- Note: Uncommenting this will buffer the entire request body
    -- local body = request_handle:body()
    -- if body then
    --     local body_size = body:length()
    --     headers:add("x-request-body-size", tostring(body_size))
    --
    --     -- Get body content (first 100 bytes)
    --     if body_size > 0 then
    --         local content = body:getBytes(0, math.min(100, body_size))
    --         request_handle:logDebug("Body preview: " .. content)
    --     end
    --
    --     -- Modify body content
    --     -- body:setBytes("modified content")
    -- end

    -- ============================================================
    -- UTILITY FUNCTIONS
    -- ============================================================

    -- Base64 encoding
    local encoded = request_handle:base64Escape("hello world")
    request_handle:logDebug("Base64 encoded: " .. encoded)

    -- High-resolution timestamps
    local ts_ms = request_handle:timestamp()  -- milliseconds (default)
    local ts_string = request_handle:timestampString()  -- as string

    -- ============================================================
    -- HTTP CALL TO EXTERNAL SERVICE
    -- Make synchronous HTTP calls to upstream clusters
    -- ============================================================
    -- Note: Requires an upstream cluster named "auth_cluster" to be configured
    -- Uncomment to make external HTTP calls:
    --
    -- local auth_headers, auth_body = request_handle:httpCall(
    --     "auth_cluster",
    --     {
    --         [":method"] = "POST",
    --         [":path"] = "/validate",
    --         [":authority"] = "auth-service",
    --         ["content-type"] = "application/json"
    --     },
    --     '{"token": "' .. (headers:get("authorization") or "") .. '"}',
    --     5000  -- timeout in milliseconds
    -- )
    --
    -- if auth_headers and auth_headers[":status"] == "200" then
    --     headers:add("x-auth-validated", "true")
    -- else
    --     -- Return early with 403 Forbidden
    --     request_handle:respond(
    --         {[":status"] = "403", ["content-type"] = "text/plain"},
    --         "Forbidden: Invalid authentication"
    --     )
    --     return
    -- end

    -- ============================================================
    -- DIRECT RESPONSE
    -- Send a response directly without forwarding to upstream
    -- ============================================================
    -- Uncomment to return a direct response for specific paths:
    --
    -- if path == "/health" then
    --     request_handle:respond(
    --         {
    --             [":status"] = "200",
    --             ["content-type"] = "application/json"
    --         },
    --         '{"status": "healthy", "lua_filter": true}'
    --     )
    --     return
    -- end

    -- ============================================================
    -- ROUTE CACHE MANAGEMENT
    -- Clear route cache if headers affecting routing are modified
    -- ============================================================
    -- request_handle:clearRouteCache()

    -- ============================================================
    -- UPSTREAM HOST OVERRIDE
    -- Override the destination host for this request
    -- ============================================================
    -- request_handle:setUpstreamOverrideHost("192.168.1.100:8080", false)

    -- ============================================================
    -- CRYPTOGRAPHIC OPERATIONS
    -- Verify signatures using public keys
    -- ============================================================
    -- local pubkey = request_handle:importPublicKey(key_der_bytes, key_length)
    -- local ok, error = request_handle:verifySignature(
    --     "SHA256",  -- hash function: SHA1, SHA224, SHA256, SHA384, SHA512
    --     pubkey,
    --     signature_bytes,
    --     signature_length,
    --     data_bytes,
    --     data_length
    -- )

    request_handle:logInfo("Request processing complete")
end

-- envoy_on_response is called when preparing to send the response
-- The response_handle provides similar methods to request_handle
function envoy_on_response(response_handle)
    response_handle:logInfo("Processing outgoing response")

    -- ============================================================
    -- RESPONSE HEADER MANIPULATION
    -- ============================================================
    local headers = response_handle:headers()

    -- Get response status
    local status = headers:get(":status")
    response_handle:logInfo("Response status: " .. (status or "nil"))

    -- Add custom response headers
    headers:add("x-lua-response-processed", "true")
    headers:add("x-response-timestamp", tostring(response_handle:timestamp()))

    -- Add server timing header
    headers:add("server-timing", "lua;desc=\"Lua filter processing\"")

    -- Set custom HTTP/1.1 reason phrase (only affects HTTP/1.x responses)
    -- headers:setHttp1ReasonPhrase("Custom Status Message")

    -- ============================================================
    -- ACCESS REQUEST METADATA IN RESPONSE
    -- Retrieve metadata set during request processing
    -- ============================================================
    local stream_info = response_handle:streamInfo()
    local dynamic_metadata = stream_info:dynamicMetadata()

    -- Get metadata set during request phase
    local request_info = dynamic_metadata:get("envoy.filters.http.lua")
    if request_info and request_info["request.info"] then
        local info = request_info["request.info"]
        response_handle:logDebug("Original request was: " ..
            (info.method or "?") .. " " .. (info.path or "?"))
    end

    -- ============================================================
    -- RESPONSE BODY HANDLING
    -- ============================================================
    -- Buffer and inspect/modify response body
    -- Note: This buffers the entire response body
    --
    -- local body = response_handle:body()
    -- if body then
    --     local body_size = body:length()
    --     headers:add("x-response-body-size", tostring(body_size))
    --
    --     -- Modify response body
    --     -- body:setBytes("modified response content")
    -- end

    -- ============================================================
    -- RESPONSE TRAILERS
    -- Access trailers after body is consumed
    -- ============================================================
    -- local trailers = response_handle:trailers()
    -- if trailers then
    --     trailers:add("x-trailer-header", "trailer-value")
    -- end

    -- ============================================================
    -- CONNECTION DRAINING
    -- Signal that the connection should be closed after this response
    -- ============================================================
    -- Useful for graceful shutdown or after sending certain responses
    -- stream_info:drainConnectionUponCompletion()

    response_handle:logInfo("Response processing complete")
end
