// This go.mod is used to build the Composer dynamic module (libcomposer.so).
// All embedded Go plugins must use this same go.mod to ensure Go runtime and
// dependency version compatibility. Do not create separate go.mod files for
// embedded plugins; they should be part of this module.

module github.com/tetratelabs/built-on-envoy/extensions

go 1.25.6

require (
	github.com/envoyproxy/envoy/source/extensions/dynamic_modules v0.0.0-20260129014508-e8c1dc7dcbcd
	github.com/lestrrat-go/jwx/v3 v3.0.13
	go.uber.org/mock v0.6.0
	google.golang.org/protobuf v1.36.11
)

require (
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.4.0 // indirect
	github.com/goccy/go-json v0.10.3 // indirect
	github.com/lestrrat-go/blackmagic v1.0.4 // indirect
	github.com/lestrrat-go/httpcc v1.0.1 // indirect
	github.com/lestrrat-go/httprc/v3 v3.0.2 // indirect
	github.com/lestrrat-go/option/v2 v2.0.0 // indirect
	github.com/segmentio/asm v1.2.1 // indirect
	golang.org/x/crypto v0.46.0 // indirect
	golang.org/x/sys v0.39.0 // indirect
)
