module github.com/tetratelabs/built-on-envoy/extensions

go 1.25.6

require (
	github.com/envoyproxy/envoy/source/extensions/dynamic_modules v0.0.0-00010101000000-000000000000
	go.uber.org/mock v0.6.0
	google.golang.org/genproto/googleapis/api v0.0.0-20260126211449-d11affda4bed
	google.golang.org/protobuf v1.36.11
)

replace github.com/envoyproxy/envoy/source/extensions/dynamic_modules => github.com/wbpcode/envoy/source/extensions/dynamic_modules v0.0.0-20260128123219-3a5bf3e00204
