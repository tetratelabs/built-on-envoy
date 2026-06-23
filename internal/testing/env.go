// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package internaltesting

import (
	"os"
	"strconv"
	"testing"
	"time"
)

var (
	RunEnvoyTimeout = NewEnvVar("TEST_BOE_RUN_ENVOY_TIMEOUT",
		"Timeout duration for waiting for Envoy to start",
		90*time.Second)

	TestRequestTimeout = NewEnvVar("TEST_BOE_REQUEST_TIMEOUT",
		"Timeout duration for waiting for a request to complete in tests",
		5*time.Second)

	TestBoeRegistry = NewEnvVar("TEST_BOE_REGISTRY",
		"OCI Registry to use for testing. When set, it will override the BOE_REGISTRY environment variable for the current test.",
		"")

	TestBoeRegistryInsecure = NewEnvVar("TEST_BOE_REGISTRY_INSECURE",
		"Whether to use insecure OCI registry for testing. When set, it will override the BOE_REGISTRY_INSECURE environment variable for the current test.",
		false)

	TestBoeRegistryUsername = NewEnvVar("TEST_BOE_REGISTRY_USERNAME",
		"Username for registry authentication. When set, it will override the BOE_REGISTRY_USERNAME environment variable for the current test.",
		"")

	TestBoeRegistryPassword = NewEnvVar("TEST_BOE_REGISTRY_PASSWORD",
		"Password for registry authentication. When set, it will override the BOE_REGISTRY_PASSWORD environment variable for the current test.",
		"")

	TestUpstreamCluster = NewEnvVar("TEST_BOE_UPSTREAM_CLUSTER",
		"Upstream cluster to use for testing. When set, the `--cluster` and `--test-upstream-cluster` flags will be added when running Envoy.",
		"")

	TestUpstreamClusterInsecure = NewEnvVar("TEST_BOE_UPSTREAM_CLUSTER_INSECURE",
		"Whether to use insecure upstream cluster for testing. When set, the `--cluster-insecure` and `--test-upstream-cluster` flags will be added when running Envoy.",
		"")

	TestCLIOutputFile = NewEnvVar("TEST_BOE_CLI_OUTPUT_FILE",
		"File path to write the output of the BOE CLI command. When set, the output will be written to this file in addition to the memory buffers.",
		"")
)

// EnvVar is a generic interface for environment variables.
// It provides a method to get the value of the environment variable, with a default value if the variable is not set.
type EnvVar[T any] interface {
	// Name returns the name of the environment variable.
	Name() string
	// Configured returns true if the environment variable is set, false otherwise.
	Configured() bool
	// Get returns the value of the environment variable, or the default value if the variable is not set or cannot be parsed.
	Get() T
	// Set sets the value of the environment variable for the current test.
	Set(t *testing.T, value T)
}

// envVar is a generic struct that implements the EnvVar interface.
type envVar[T any] struct {
	name         string
	description  string
	defaultValue T
}

// NewEnvVar creates a new EnvVar with the given name, description, and default value.
func NewEnvVar[T any](name string, description string, defaultValue T) EnvVar[T] {
	return &envVar[T]{
		name:         name,
		description:  description,
		defaultValue: defaultValue,
	}
}

// Name returns the name of the environment variable.
func (v *envVar[T]) Name() string {
	return v.name
}

// Configured returns true if the environment variable is set, false otherwise.
func (v *envVar[T]) Configured() bool {
	_, ok := os.LookupEnv(v.name)
	return ok
}

// Get returns the value of the environment variable, or the default value if the variable is not set or cannot be parsed.
func (v *envVar[T]) Get() T {
	raw, ok := os.LookupEnv(v.name)
	if !ok {
		return v.defaultValue
	}

	switch any(v.defaultValue).(type) {
	case string:
		return any(raw).(T)
	case bool:
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			return v.defaultValue
		}
		return any(parsed).(T)
	case int:
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			return v.defaultValue
		}
		return any(parsed).(T)
	case float64:
		parsed, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return v.defaultValue
		}
		return any(parsed).(T)
	case time.Duration:
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			return v.defaultValue
		}
		return any(parsed).(T)
	default:
		return v.defaultValue
	}
}

// Set sets the value of the environment variable for the current test.
func (v *envVar[T]) Set(t *testing.T, value T) {
	t.Helper()

	var strValue string
	switch any(value).(type) {
	case string:
		strValue = any(value).(string)
	case bool:
		strValue = strconv.FormatBool(any(value).(bool))
	case int:
		strValue = strconv.Itoa(any(value).(int))
	case float64:
		strValue = strconv.FormatFloat(any(value).(float64), 'f', -1, 64)
	case time.Duration:
		strValue = any(value).(time.Duration).String()
	default:
		t.Fatalf("unsupported type for env var: %T", value)
	}

	t.Setenv(v.name, strValue)
}
