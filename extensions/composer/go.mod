// This go.mod is used to build the Composer dynamic module (libcomposer.so).
// All embedded Go plugins must use this same go.mod to ensure Go runtime and
// dependency version compatibility. Do not create separate go.mod files for
// embedded plugins; they should be part of this module.

module github.com/tetratelabs/built-on-envoy/extensions/composer

go 1.25.7

require (
	github.com/corazawaf/coraza/v3 v3.3.3
	github.com/envoyproxy/envoy/source/extensions/dynamic_modules v0.0.0-20260129014508-e8c1dc7dcbcd
	github.com/google/go-containerregistry v0.20.7
	github.com/stretchr/testify v1.10.0
	go.uber.org/mock v0.6.0
	go.uber.org/zap v1.27.1
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/containerd/stargz-snapshotter/estargz v0.18.1 // indirect
	github.com/corazawaf/libinjection-go v0.2.2 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/docker/cli v29.0.3+incompatible // indirect
	github.com/docker/distribution v2.8.3+incompatible // indirect
	github.com/docker/docker-credential-helpers v0.9.3 // indirect
	github.com/klauspost/compress v1.18.1 // indirect
	github.com/magefile/mage v1.15.1-0.20241126214340-bdc92f694516 // indirect
	github.com/mitchellh/go-homedir v1.1.0 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/opencontainers/image-spec v1.1.1 // indirect
	github.com/petar-dambovaliev/aho-corasick v0.0.0-20240411101913-e07a1f0e8eb4 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/sirupsen/logrus v1.9.3 // indirect
	github.com/tidwall/gjson v1.18.0 // indirect
	github.com/tidwall/match v1.1.1 // indirect
	github.com/tidwall/pretty v1.2.1 // indirect
	github.com/valllabh/ocsf-schema-golang v1.0.3 // indirect
	github.com/vbatts/tar-split v0.12.2 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/net v0.43.0 // indirect
	golang.org/x/sync v0.18.0 // indirect
	golang.org/x/sys v0.38.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
	rsc.io/binaryregexp v0.2.0 // indirect
)
