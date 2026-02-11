// This go.mod is used to build the Composer dynamic module (libcomposer.so).
// All embedded Go plugins must use this same go.mod to ensure Go runtime and
// dependency version compatibility. Do not create separate go.mod files for
// embedded plugins; they should be part of this module.

module github.com/tetratelabs/built-on-envoy/extensions/composer

go 1.25.6

require (
	github.com/envoyproxy/envoy/source/extensions/dynamic_modules v0.0.0-20260129014508-e8c1dc7dcbcd
	go.uber.org/mock v0.6.0
	google.golang.org/protobuf v1.36.11
)
