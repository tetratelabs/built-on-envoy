// This go.mod is used to build the Composer dynamic module (libcomposer.so).
// All embedded Go plugins must use this same go.mod to ensure Go runtime and
// dependency version compatibility. Do not create separate go.mod files for
// embedded plugins; they should be part of this module.

module github.com/tetratelabs/built-on-envoy/extensions

go 1.25.6

require (
	github.com/envoyproxy/envoy/source/extensions/dynamic_modules v0.0.0-20260129014508-e8c1dc7dcbcd
	github.com/stretchr/testify v1.11.1
	go.uber.org/mock v0.6.0
	google.golang.org/protobuf v1.36.11
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/kr/pretty v0.3.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/rogpeppe/go-internal v1.14.1 // indirect
	gopkg.in/check.v1 v1.0.0-20201130134442-10cb98267c6c // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
