// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package cmd

import (
	"fmt"
	"path/filepath"
	"slices"

	"github.com/tetratelabs/built-on-envoy/cli/internal/extensions"
)

// EnvoyFlags holds flags for Envoy configuration.
type EnvoyFlags struct {
	Version     string `name:"envoy-version" help:"Envoy version to use (e.g., 1.31.0, dev, dev-latest)" env:"ENVOY_VERSION"`
	VersionsURL string `name:"envoy-versions-url" help:"URL of the Envoy versions JSON. Override to use debug builds (see archive-envoy)." env:"ENVOY_VERSIONS_URL" hidden:""`
	Path        string `name:"envoy-path" help:"Path to a custom Envoy binary. Skips Envoy download and version selection." env:"ENVOY_PATH"`
}

// OCIFlags holds flags for OCI registry authentication and configuration.
type OCIFlags struct {
	Registry string `name:"registry" env:"BOE_REGISTRY" help:"OCI registry URL for the extensions." default:"${default_registry}"`
	Insecure bool   `name:"insecure" env:"BOE_REGISTRY_INSECURE" help:"Allow connecting to an insecure (HTTP) registry." default:"false"`
	Username string `name:"username" env:"BOE_REGISTRY_USERNAME" help:"Username for the OCI registry."`
	Password string `name:"password" env:"BOE_REGISTRY_PASSWORD" help:"Password for the OCI registry." type:"password" sensitive:"true"`
}

// ClusterFlags holds flags for additional Envoy clusters.
type ClusterFlags struct {
	Secure              []string `name:"cluster" help:"Optional additional Envoy cluster provided in the host:tlsPort pattern." `
	Insecure            []string `name:"cluster-insecure" help:"Optional additional Envoy cluster (with TLS transport disabled) provided in the host:port pattern." `
	JSONSpec            []string `name:"cluster-json" sep:"none" help:"Optional additional Envoy cluster providing the complete cluster config in JSON format." `
	TestUpstreamHost    string   `name:"test-upstream-host" help:"Hostname for the test upstream cluster. Mutually exclusive with --test-upstream-cluster. Defaults to \"httpbin.org\"."`
	TestUpstreamCluster string   `name:"test-upstream-cluster" help:"Name of an existing configured cluster to use as the test upstream. The cluster must be configured via --cluster, --cluster-insecure, or --cluster-json. Mutually exclusive with --test-upstream-host."`
}

// DockerFlags holds flags for Docker configuration.
type DockerFlags struct {
	Enabled      bool   `name:"docker" help:"Run Envoy as a Docker container instead of using func-e." default:"false" env:"BOE_RUN_DOCKER"`
	Pull         string `name:"pull" help:"Pull policy for the BOE Docker image (missing, always, never). Only applicable when running with --docker." enum:"missing,always,never" default:"missing"`
	ImageVersion string `name:"docker-image-version" help:"Override the BOE Docker image tag to use when running with --docker. By default, the image version matches the BOE version."`
}

// extensionPositions keeps track of the original position of extensions specified via --extension and --local flags.
type extensionPositions struct {
	local  map[string][]int // maps local extension flag values to their positions
	remote map[string][]int // maps remote extension flag values to their positions
}

// sort takes a list of extension manifests and returns a new list sorted according to the original positions of the extension
// specified via --extension and --local flags.
func (e extensionPositions) sort(manifests []*extensions.Manifest) ([]*extensions.Manifest, error) {
	sorted := make([]*extensions.Manifest, len(manifests))

	for l, positions := range e.local {
		pos := slices.IndexFunc(manifests, func(m *extensions.Manifest) bool {
			flagValue, err := filepath.Abs(filepath.Dir(m.Path))
			if err != nil {
				return false
			}
			return flagValue == l
		})
		if pos == -1 {
			return nil, fmt.Errorf("failed to find manifest for local extension with path %s", l)
		}
		for _, p := range positions {
			sorted[p] = manifests[pos]
		}
	}

	for r, positions := range e.remote {
		pos := slices.IndexFunc(manifests, func(m *extensions.Manifest) bool {
			return m.Remote && (m.RemoteRef == r)
		})
		if pos == -1 {
			return nil, fmt.Errorf("failed to find manifest for remote extension with reference %s", r)
		}
		for _, p := range positions {
			sorted[p] = manifests[pos]
		}
	}

	return sorted, nil
}

// saveExtensionPositions iterates through os.Args to find the positions of --extension and --local flags and
// saves them in GenConfig.extensionPositions.
func saveExtensionPositions(args []string) (extensionPositions, error) {
	var (
		extensionIndex int
		positions      = extensionPositions{
			local:  make(map[string][]int),
			remote: make(map[string][]int),
		}
	)

	for i, arg := range args {
		switch arg {
		case "--local":
			key, err := filepath.Abs(args[i+1])
			if err != nil {
				return positions, fmt.Errorf("failed to get absolute path for local extension flag value %s: %w", args[i+1], err)
			}
			positions.local[key] = append(positions.local[key], extensionIndex)
			extensionIndex++
		case "--extension":
			key := args[i+1]
			positions.remote[key] = append(positions.remote[key], extensionIndex)
			extensionIndex++
		}
	}

	return positions, nil
}
