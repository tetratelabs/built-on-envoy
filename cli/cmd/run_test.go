// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"testing"

	"github.com/alecthomas/kong"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"

	"github.com/tetratelabs/built-on-envoy/cli/internal/extensions"
	"github.com/tetratelabs/built-on-envoy/cli/internal/oci"
	"github.com/tetratelabs/built-on-envoy/cli/internal/xdg"
	internaltesting "github.com/tetratelabs/built-on-envoy/internal/testing"
)

func TestParseCmdRunHelp(t *testing.T) {
	var cli struct {
		Run Run `cmd:"" help:"Run Envoy with extensions"`
	}

	var buf bytes.Buffer
	parser, err := kong.New(&cli,
		kong.Name("boe"),
		kong.Writers(&buf, &buf),
		kong.Exit(func(int) {}),
		Vars,
	)
	require.NoError(t, err)

	_, _ = parser.Parse([]string{"run", "--help"})

	expected := fmt.Sprintf(`Usage: boe run [flags]

Run Envoy with extensions

%s
Flags:
  -h, --help                       Show context-sensitive help.

      --envoy-version=STRING       Envoy version to use (e.g., 1.31.0, dev,
                                   dev-latest) ($ENVOY_VERSION)
      --envoy-path=STRING          Path to a custom Envoy binary. Skips Envoy
                                   download and version selection ($ENVOY_PATH).
      --log-level="all:error"      Envoy component log level ($ENVOY_LOG_LEVEL).
      --run-id=STRING              Run identifier for this invocation. Overrides
                                   the default timestamp-based ID ($BOE_RUN_ID).
      --listen-port=10000          Port for Envoy listener to accept incoming
                                   traffic.
      --admin-port=9901            Port for Envoy admin interface
                                   ($BOE_ADMIN_PORT).
      --extension=EXTENSION,...    Extensions to enable (in the format: "name"
                                   or "name:version").
      --local=LOCAL                Path to a directory containing a local
                                   Extension to enable.
      --dev                        Whether to allow downloading dev versions of
                                   extensions (with -dev suffix). By default,
                                   only stable versions are allowed.
      --config=CONFIG              Optional JSON config string for extensions.
                                   Applied in order to combined --extension and
                                   --local flags.
      --filter-type=FILTER-TYPE    Set the filter type for an extension. Applied
                                   positionally to the combined --extension
                                   and --local flags. Accepted values: http,
                                   network, listener, udp_listener.
      --native-http-filter-before=NATIVE-HTTP-FILTER-BEFORE
                                   Optional YAML/JSON native HTTP filter list
                                   (or @filepath) per extension position.
                                   Overrides manifest nativeHttpFilters.before.
      --native-http-filter-after=NATIVE-HTTP-FILTER-AFTER
                                   Optional YAML/JSON native HTTP filter list
                                   (or @filepath) per extension position.
                                   Overrides manifest nativeHttpFilters.after.
      --cluster=CLUSTER,...        Optional additional Envoy cluster provided in
                                   the host:tlsPort pattern.
      --cluster-insecure=CLUSTER-INSECURE,...
                                   Optional additional Envoy cluster (with TLS
                                   transport disabled) provided in the host:port
                                   pattern.
      --cluster-json=CLUSTER-JSON
                                   Optional additional Envoy cluster providing
                                   the complete cluster config in JSON format.
      --test-upstream-host=STRING
                                   Hostname for the test upstream
                                   cluster. Mutually exclusive with
                                   --test-upstream-cluster. Defaults to
                                   "httpbin.org".
      --test-upstream-cluster=STRING
                                   Name of an existing configured cluster to
                                   use as the test upstream. The cluster must be
                                   configured via --cluster, --cluster-insecure,
                                   or --cluster-json. Mutually exclusive with
                                   --test-upstream-host.
      --docker                     Run Envoy as a Docker container instead of
                                   using func-e ($BOE_RUN_DOCKER).
      --pull="missing"             Pull policy for the BOE Docker image
                                   (missing, always, never). Only applicable
                                   when running with --docker.
      --docker-image-version=STRING
                                   Override the BOE Docker image tag to use
                                   when running with --docker. By default,
                                   the image version matches the BOE version.
      --registry="%s"
                                   OCI registry URL for the extensions
                                   ($BOE_REGISTRY).
      --insecure                   Allow connecting to an insecure (HTTP)
                                   registry ($BOE_REGISTRY_INSECURE).
      --username=STRING            Username for the OCI registry
                                   ($BOE_REGISTRY_USERNAME).
      --password=STRING            Password for the OCI registry
                                   ($BOE_REGISTRY_PASSWORD).
`, wrapHelp(runHelp), extensions.DefaultOCIRegistry)

	require.Equal(t, expected, buf.String())
}

func TestParseCmdRunDefaults(t *testing.T) {
	t.Setenv("ENVOY_LOG_LEVEL", "all:error")
	t.Setenv("BOE_RUN_DOCKER", "false")
	t.Setenv("BOE_RUN_ID", "")

	var cli struct {
		Run Run `cmd:"" help:"Run Envoy with extensions"`
	}

	parser, err := kong.New(&cli,
		kong.Name("boe"),
		kong.Exit(func(int) {}),
		kong.BindTo(t.Context(), (*context.Context)(nil)),
		kong.Bind(&xdg.Directories{}),
		Vars,
	)
	require.NoError(t, err)

	_, err = parser.Parse([]string{"run"})
	require.NoError(t, err)

	// Verify default values are set
	require.Equal(t, "all:error", cli.Run.LogLevel)
	require.Equal(t, uint32(10000), cli.Run.ListenPort)
	require.Equal(t, uint32(9901), cli.Run.AdminPort)
	require.Empty(t, cli.Run.Envoy.Version)
	require.Empty(t, cli.Run.Envoy.Path)
	require.Empty(t, cli.Run.Extensions)
	require.Equal(t, extensions.DefaultOCIRegistry, cli.Run.OCI.Registry)
	require.False(t, cli.Run.OCI.Insecure)

	require.Empty(t, cli.Run.RunID)
}

func TestParseCmdRunDockerRunID(t *testing.T) {
	tests := []struct {
		name          string
		args          []string
		envRunID      string
		expectedRunID string
	}{
		{
			name:          "empty by default",
			args:          []string{"run", "--docker"},
			expectedRunID: "",
		},
		{
			name:          "explicit run id",
			args:          []string{"run", "--docker", "--run-id=custom-run-id"},
			expectedRunID: "custom-run-id",
		},
		{
			name:          "environment run id",
			args:          []string{"run", "--docker"},
			envRunID:      "env-run-id",
			expectedRunID: "env-run-id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("BOE_RUN_ID", tt.envRunID)

			var cli struct {
				Run Run `cmd:"" help:"Run Envoy with extensions"`
			}

			parser, err := kong.New(&cli,
				kong.Name("boe"),
				kong.Exit(func(int) {}),
				kong.BindTo(t.Context(), (*context.Context)(nil)),
				kong.Bind(&xdg.Directories{}),
				Vars,
			)
			require.NoError(t, err)

			_, err = parser.Parse(tt.args)
			require.NoError(t, err)
			require.Equal(t, tt.expectedRunID, cli.Run.RunID)
		})
	}
}

func TestParseCmdRunLogLevelEnv(t *testing.T) {
	var cli struct {
		Run Run `cmd:"" help:"Run Envoy with extensions"`
	}

	parser, err := kong.New(&cli,
		kong.Name("boe"),
		kong.Exit(func(int) {}),
		kong.BindTo(t.Context(), (*context.Context)(nil)),
		kong.Bind(&xdg.Directories{}),
		Vars,
	)
	require.NoError(t, err)

	t.Setenv("ENVOY_LOG_LEVEL", "all:debug,upstream:trace")

	_, err = parser.Parse([]string{"run"})
	require.NoError(t, err)

	require.Equal(t, "all:debug,upstream:trace", cli.Run.LogLevel)
}

func TestParseCmdRunCustomValues(t *testing.T) {
	var cli struct {
		Run Run `cmd:"" help:"Run Envoy with extensions"`
	}

	parser, err := kong.New(&cli,
		kong.Name("boe"),
		kong.Exit(func(int) {}),
		kong.BindTo(t.Context(), (*context.Context)(nil)),
		kong.Bind(&xdg.Directories{}),
		Vars,
	)
	require.NoError(t, err)

	t.Setenv("BOE_REGISTRY_INSECURE", "true")
	t.Setenv("BOE_REGISTRY", "localhost:5000")

	_, err = parser.Parse([]string{
		"run",
		"--log-level=all:debug,upstream:trace",
		"--listen-port=8080",
		"--admin-port=9000",
		"--envoy-version=1.31.0",
		"--run-id=custom-run-id",
		"--extension=cors:1.0.0,rate-limiter",
		"--extension=auth-jwt",
	})
	require.NoError(t, err)

	require.Equal(t, "all:debug,upstream:trace", cli.Run.LogLevel)
	require.Equal(t, uint32(8080), cli.Run.ListenPort)
	require.Equal(t, uint32(9000), cli.Run.AdminPort)
	require.Equal(t, "1.31.0", cli.Run.Envoy.Version)
	require.Equal(t, "custom-run-id", cli.Run.RunID)
	require.Equal(t, []string{"cors:1.0.0", "rate-limiter", "auth-jwt"}, cli.Run.Extensions)
	require.Equal(t, "localhost:5000", cli.Run.OCI.Registry)
	require.True(t, cli.Run.OCI.Insecure)
}

func TestRunValidateMutualExclusion(t *testing.T) {
	tests := []struct {
		name        string
		run         Run
		expectedErr string
	}{
		{
			name: "test upstream host and cluster",
			run: Run{
				LogLevel: "all:error",
				Clusters: ClusterFlags{
					TestUpstreamHost:    "example.com",
					TestUpstreamCluster: "example.com:443",
				},
			},
			expectedErr: "--test-upstream-host and --test-upstream-cluster are mutually exclusive",
		},
		{
			name: "envoy path and version",
			run: Run{
				LogLevel: "all:error",
				Envoy: EnvoyFlags{
					Path:    "/usr/local/bin/envoy",
					Version: "1.38.0",
				},
			},
			expectedErr: "--envoy-path and --envoy-version are mutually exclusive",
		},
		{
			name: "docker image version without docker",
			run: Run{
				LogLevel: "all:error",
				Docker: DockerFlags{
					ImageVersion: "custom-version",
				},
			},
			expectedErr: "--docker-image-version can only be used with --docker",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.EqualError(t, tt.run.Validate(), tt.expectedErr)
		})
	}
}

func TestValidateLogLevel(t *testing.T) {
	tests := []struct {
		name        string
		logLevel    string
		expectedErr string
	}{
		{
			name:     "empty log level is valid",
			logLevel: "",
		},
		{
			name:     "single component and level",
			logLevel: "all:error",
		},
		{
			name:     "multiple components",
			logLevel: "all:error,upstream:trace,http:debug",
		},
		{
			name:     "whitespace is trimmed",
			logLevel: " all:error , upstream:trace ",
		},
		{
			name:        "empty entry",
			logLevel:    " all:error,,upstream:trace ",
			expectedErr: `invalid log level format "": expected component:level`,
		},
		{
			name:        "missing colon separator",
			logLevel:    "allerror",
			expectedErr: `invalid log level format "allerror": expected component:level`,
		},
		{
			name:        "empty component",
			logLevel:    ":error",
			expectedErr: `invalid log level format ":error": component cannot be empty`,
		},
		{
			name:        "empty level",
			logLevel:    "all:",
			expectedErr: `invalid log level format "all:": level cannot be empty`,
		},
		{
			name:        "whitespace-only component",
			logLevel:    " :error",
			expectedErr: `invalid log level format " :error": component cannot be empty`,
		},
		{
			name:        "whitespace-only level",
			logLevel:    "all: ",
			expectedErr: `invalid log level format "all: ": level cannot be empty`,
		},
		{
			name:        "missing colon in multi-component string",
			logLevel:    "all:error,badformat",
			expectedErr: `invalid log level format "badformat": expected component:level`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := Run{LogLevel: tt.logLevel}
			err := cmd.Validate()
			if tt.expectedErr == "" {
				require.NoError(t, err)
			} else {
				require.EqualError(t, err, tt.expectedErr)
			}
		})
	}
}

func TestParseLogLevels(t *testing.T) {
	tests := []struct {
		name               string
		input              string
		wantBaseLevel      string
		wantComponentLevel string
	}{
		{
			name:               "empty string defaults to warning",
			input:              "",
			wantBaseLevel:      "error",
			wantComponentLevel: "",
		},
		{
			name:               "all component only",
			input:              "all:debug",
			wantBaseLevel:      "debug",
			wantComponentLevel: "",
		},
		{
			name:               "single component without all",
			input:              "upstream:debug",
			wantBaseLevel:      "error",
			wantComponentLevel: "upstream:debug",
		},
		{
			name:               "multiple components without all",
			input:              "upstream:debug,connection:trace",
			wantBaseLevel:      "error",
			wantComponentLevel: "upstream:debug,connection:trace",
		},
		{
			name:               "all with other components",
			input:              "all:info,upstream:debug,connection:trace",
			wantBaseLevel:      "info",
			wantComponentLevel: "upstream:debug,connection:trace",
		},
		{
			name:               "all at the end",
			input:              "upstream:debug,all:error",
			wantBaseLevel:      "error",
			wantComponentLevel: "upstream:debug",
		},
		{
			name:               "all in the middle",
			input:              "upstream:debug,all:info,connection:trace",
			wantBaseLevel:      "info",
			wantComponentLevel: "upstream:debug,connection:trace",
		},
		{
			name:               "handles whitespace",
			input:              " upstream:debug , all:info , connection:trace ",
			wantBaseLevel:      "info",
			wantComponentLevel: "upstream:debug,connection:trace",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// errors are tested in TestValidateLogLevel
			baseLevel, componentLevels, _ := parseLogLevels(tt.input)
			require.Equal(t, tt.wantBaseLevel, baseLevel)
			require.Equal(t, tt.wantComponentLevel, componentLevels)
		})
	}
}

func TestRunInvalidConfig(t *testing.T) {
	r := &Run{RunID: "///"}
	require.Error(t, r.Run(t.Context(), &xdg.Directories{}, internaltesting.NewTLogger(t)))
}

func TestRunInvalidEnvoyPath(t *testing.T) {
	r := &Run{Envoy: EnvoyFlags{Path: "/nonexistent/path/to/envoy"}}
	err := r.Run(t.Context(), &xdg.Directories{}, internaltesting.NewTLogger(t))
	require.ErrorContains(t, err, "Envoy binary not found")
}

func TestSplitRef(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		wantBundle    string
		wantExtension string
		wantTag       string
	}{
		{
			name:          "simple name without tag",
			input:         "cors",
			wantBundle:    "cors",
			wantExtension: "cors",
			wantTag:       "latest",
		},
		{
			name:          "simple name with tag",
			input:         "cors:1.0.0",
			wantBundle:    "cors",
			wantExtension: "cors",
			wantTag:       "1.0.0",
		},
		{
			name:          "empty string",
			input:         "",
			wantBundle:    "",
			wantExtension: "",
			wantTag:       "latest",
		},
		{
			name:          "bundle prefixed name with tag",
			input:         "composer/example-go:v1.0.0",
			wantBundle:    "composer",
			wantExtension: "example-go",
			wantTag:       "v1.0.0",
		},
		{
			name:          "bundle prefixed name without tag",
			input:         "composer/example-go",
			wantBundle:    "composer",
			wantExtension: "example-go",
			wantTag:       "latest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bundle, extension, tag := splitRef(tt.input)
			require.Equal(t, tt.wantBundle, bundle)
			require.Equal(t, tt.wantExtension, extension)
			require.Equal(t, tt.wantTag, tag)
		})
	}
}

func TestLoadLocalManifests(t *testing.T) {
	logger := internaltesting.NewTLogger(t)
	downloader := &extensions.Downloader{
		Logger: logger,
		Dirs:   &xdg.Directories{DataHome: t.TempDir()},
	}
	t.Run("empty paths", func(t *testing.T) {
		manifests, err := loadLocalManifests(t.Context(), logger, downloader, []string{}, false)
		require.NoError(t, err)
		require.Empty(t, manifests)
	})

	t.Run("multiple valid paths", func(t *testing.T) {
		manifests, err := loadLocalManifests(t.Context(), logger, downloader, []string{"./testdata", "./testdata/push_pull"}, false)
		require.NoError(t, err)
		require.Len(t, manifests, 2)
		require.Equal(t, "test-lua", manifests[0].Name)
		require.Equal(t, "push-pull", manifests[1].Name)
	})

	t.Run("nonexistent path", func(t *testing.T) {
		_, err := loadLocalManifests(t.Context(), logger, downloader, []string{"/nonexistent/path"}, false)
		require.Error(t, err)
		require.ErrorIs(t, err, errFailedToLoadLocalManifest)
	})

	t.Run("invalid path", func(t *testing.T) {
		_, err := loadLocalManifests(t.Context(), logger, downloader, []string{"./"}, false)
		require.Error(t, err)
		require.ErrorIs(t, err, errFailedToLoadLocalManifest)
	})

	t.Run("invalid Go path", func(t *testing.T) {
		// Create a temporary directory and create an template Go plugin with
		// createGoExtension.
		tempDir := t.TempDir()
		err := createGoExtension(logger, downloader.Dirs, tempDir, "test_custom", "0.1.0")
		require.NoError(t, err)

		// Remove go.mod and go.sum to simulate invalid Go extension.
		err = os.Remove(tempDir + "/test_custom/go.mod")
		require.NoError(t, err)
		err = os.Remove(tempDir + "/test_custom/go.sum")
		require.NoError(t, err)

		_, err = loadLocalManifests(t.Context(), logger, downloader, []string{tempDir + "/test_custom"}, true)
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to run 'go mod tidy'")
	})

	t.Run("valid Go path", func(t *testing.T) {
		// Create a temporary directory and create an template Go plugin with
		// createGoExtension.
		tempDir := t.TempDir()
		err := createGoExtension(logger, downloader.Dirs, tempDir, "test_valid", "0.1.0")
		require.NoError(t, err)

		manifests, err := loadLocalManifests(t.Context(), logger, downloader, []string{tempDir + "/test_valid"}, false)
		require.NoError(t, err)
		require.Len(t, manifests, 1)
		require.Equal(t, "test_valid", manifests[0].Name)
	})

	t.Run("Go child resolves parent locally", func(t *testing.T) {
		tmpDir := t.TempDir()
		childDir := filepath.Join(tmpDir, "child")
		require.NoError(t, os.MkdirAll(childDir, 0o750))

		parentYAML := `name: test-parent
version: 9.9.9
composerVersion: 9.9.9
minEnvoyVersion: 1.99.0
type: composer
extensionSet: true
`
		childYAML := `name: test-child
parent: test-parent
categories: [Misc]
author: Test
description: A child extension
longDescription: A child extension
type: go
tags: [test]
license: Apache-2.0
examples: []
`
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "manifest.yaml"), []byte(parentYAML), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(childDir, "manifest.yaml"), []byte(childYAML), 0o600))

		manifests, err := loadLocalManifests(t.Context(), logger, downloader, []string{childDir}, false)
		require.NoError(t, err)
		require.Len(t, manifests, 1)
		require.Equal(t, "test-child", manifests[0].Name)
		require.Equal(t, "9.9.9", manifests[0].Version)
	})

	t.Run("Go child fails when parent not found", func(t *testing.T) {
		tmpDir := t.TempDir()
		childYAML := `name: test-child
parent: composer
categories: [Misc]
author: Test
description: A child extension
longDescription: A child extension
type: go
tags: [test]
license: Apache-2.0
examples: []
`
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "manifest.yaml"), []byte(childYAML), 0o600))

		mock := &mockOCIClient{pullErr: errors.New("registry unavailable")}
		d := newTestDownloader(t, t.TempDir(), mock)
		_, err := loadLocalManifests(t.Context(), logger, d, []string{tmpDir}, false)
		require.ErrorIs(t, err, errFailedToLoadLocalManifest)
		// loadLocalManifests resolves parents locally only (no registry fallback), so a
		// missing local parent fails fast rather than attempting a download.
		require.ErrorContains(t, err, `parent manifest "composer" not found locally`)
	})
}

func TestValidateEnvoyCompat(t *testing.T) {
	tests := []struct {
		name         string
		envoyVersion string
		extensions   []*extensions.Manifest
		errContains  []string
	}{
		{
			name:         "empty extensions list",
			envoyVersion: "1.31.0",
			extensions:   []*extensions.Manifest{},
		},
		{
			name:         "nil extensions list",
			envoyVersion: "1.31.0",
			extensions:   nil,
		},
		{
			name:         "compatible",
			envoyVersion: "1.31.0",
			extensions: []*extensions.Manifest{
				{Name: "ext-1", Version: "1.0.0", MinEnvoyVersion: "1.30.0"},
				{Name: "ext-2", Version: "2.0.0", MaxEnvoyVersion: "1.32.0"},
				{Name: "ext-3", Version: "3.0.0", MinEnvoyVersion: "1.29.0", MaxEnvoyVersion: "1.33.0"},
				{Name: "ext-4", Version: "4.0.0"},
			},
		},
		{
			name:         "incompatible",
			envoyVersion: "1.31.0",
			extensions: []*extensions.Manifest{
				{Name: "ext-1", Version: "1.0.0", MinEnvoyVersion: "1.32.0"},
				{Name: "ext-2", Version: "2.0.0", MaxEnvoyVersion: "1.30.0"},
				{Name: "ext-3", Version: "2.0.0", MinEnvoyVersion: "1.31.1", MaxEnvoyVersion: "1.32.0"},
				{Name: "ext-4", Version: "2.0.0", MinEnvoyVersion: "1.31.0", MaxEnvoyVersion: "1.32.0"},
			},
			errContains: []string{
				`incompatible Envoy version 1.31.0: extension ext-1 (1.0.0) requires Envoy ">= 1.32.0"`,
				`incompatible Envoy version 1.31.0: extension ext-2 (2.0.0) requires Envoy "<= 1.30.0"`,
				`incompatible Envoy version 1.31.0: extension ext-3 (2.0.0) requires Envoy ">= 1.31.1 && <= 1.32.0"`,
			},
		},
		{
			name:         "dev aliases are compatible with constrained extensions",
			envoyVersion: "dev",
			extensions: []*extensions.Manifest{
				{Name: "ext-1", Version: "1.0.0", MinEnvoyVersion: "1.38.0", MaxEnvoyVersion: "1.39.0"},
			},
		},
		{
			name:         "dev-latest alias is compatible with constrained extensions",
			envoyVersion: "dev-latest",
			extensions: []*extensions.Manifest{
				{Name: "ext-1", Version: "1.0.0", MinEnvoyVersion: "1.38.0", MaxEnvoyVersion: "1.39.0"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateEnvoyCompat(tt.envoyVersion, tt.extensions)
			if len(tt.errContains) == 0 {
				require.NoError(t, err)
			} else {
				errs := err.(interface{ Unwrap() []error }).Unwrap()
				require.Len(t, errs, len(tt.errContains))
				for i := range errs {
					require.EqualError(t, errs[i], tt.errContains[i])
				}
			}
		})
	}
}

func TestRunWarnsOnIncompatibleEnvoyVersion(t *testing.T) {
	// An Envoy version incompatible with an extension's constraints no longer aborts the run;
	// it only logs a warning and proceeds. (testdata/input_lua_inline declares maxEnvoyVersion
	// 1.35.0, so 1.38.0 is incompatible.) Verify the warning is emitted and the incompatibility
	// is not surfaced as a fatal error. RunID "///" forces a clean failure in the runner so the
	// test stops before actually launching Envoy.
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	r := &Run{
		Envoy: EnvoyFlags{Version: "1.38.0"},
		Local: []string{"./testdata/input_lua_inline"},
		RunID: "///",
	}

	var err error
	r.extensionPositions, err = saveExtensionPositions([]string{"--local", "./testdata/input_lua_inline"})
	require.NoError(t, err)

	err = r.Run(t.Context(), &xdg.Directories{}, logger)
	require.Error(t, err)
	require.NotErrorIs(t, err, errIncompatibleEnvoyVersion)
	require.Contains(t, buf.String(), "some extensions may not be compatible with the specified Envoy version")
}

func TestRunMultipleConfigArgsWithCommas(t *testing.T) {
	config1 := `{"header":"value1","header2":"value2"}`
	config2 := `{"another_config":"value3","yet_another_config":"value4"}`

	var cli struct {
		Run Run `cmd:"" help:"Run Envoy with specified extensions"`
	}

	var buf bytes.Buffer
	parser, err := kong.New(&cli,
		kong.Name("boe"),
		kong.Writers(&buf, &buf),
		kong.Exit(func(int) {}),
		Vars,
	)
	require.NoError(t, err)

	_, err = parser.Parse([]string{"run", "--config", config1, "--config", config2})
	require.NoError(t, err)
	require.Equal(t, []string{config1, config2}, cli.Run.Configs)
}

// mockOCIClient is a configurable mock for oci.RepositoryClient used in downloadExtensions tests.
type mockOCIClient struct {
	annotations map[string]string // Annotations returned by FetchManifest
	tags        []string          // Tags returned by Tags()
	pullErr     error             // Error returned by Pull
}

func (m *mockOCIClient) Push(context.Context, string, string, map[string]string) (string, error) {
	return "", nil
}

func (m *mockOCIClient) Pull(context.Context, string, string, *ocispec.Platform) (*ocispec.Manifest, string, error) {
	if m.pullErr != nil {
		return nil, "", m.pullErr
	}
	return nil, "sha256:abc", nil
}

func (m *mockOCIClient) Tags(context.Context) ([]string, error) {
	return m.tags, nil
}

func (m *mockOCIClient) FetchManifest(_ context.Context, tag string, _ *ocispec.Platform) (*ocispec.Manifest, error) {
	ann := make(map[string]string)
	maps.Copy(ann, m.annotations)
	// Set version from tag if not explicitly set
	if _, ok := ann[ocispec.AnnotationVersion]; !ok {
		ann[ocispec.AnnotationVersion] = tag
	}
	return &ocispec.Manifest{Annotations: ann}, nil
}

// newTestDownloader creates a Downloader with the given mock client and data directory.
func newTestDownloader(t *testing.T, dataHome string, mock *mockOCIClient) *extensions.Downloader {
	logger := internaltesting.NewTLogger(t)
	d := &extensions.Downloader{Logger: logger, Dirs: &xdg.Directories{DataHome: dataHome}}
	d.SetClientFactory(func(_ *slog.Logger, _, _, _ string, _ bool) (oci.RepositoryClient, error) {
		return mock, nil
	})
	return d
}

func TestDownloadExtensions(t *testing.T) {
	t.Run("empty refs returns empty list", func(t *testing.T) {
		d := &extensions.Downloader{
			Logger: internaltesting.NewTLogger(t),
			Dirs:   &xdg.Directories{DataHome: t.TempDir()},
		}
		manifests, err := downloadExtensions(t.Context(), d, nil, false)
		require.NoError(t, err)
		require.Empty(t, manifests)

		manifests, err = downloadExtensions(t.Context(), d, []string{}, false)
		require.NoError(t, err)
		require.Empty(t, manifests)
	})

	t.Run("binary lua extension", func(t *testing.T) {
		mock := &mockOCIClient{
			annotations: map[string]string{
				ocispec.AnnotationTitle:               "my-lua-ext",
				extensions.OCIAnnotationExtensionType: string(extensions.TypeLua),
				extensions.OCIAnnotationArtifact:      extensions.ArtifactBinary,
			},
		}
		d := newTestDownloader(t, t.TempDir(), mock)

		manifests, err := downloadExtensions(t.Context(), d, []string{"my-lua-ext:1.0.0"}, false)
		require.NoError(t, err)
		require.Len(t, manifests, 1)
		require.Equal(t, "my-lua-ext", manifests[0].Name)
		require.Equal(t, "1.0.0", manifests[0].Version)
		require.True(t, manifests[0].Remote)
	})

	t.Run("binary Rust extension", func(t *testing.T) {
		mock := &mockOCIClient{
			annotations: map[string]string{
				ocispec.AnnotationTitle:               "my-dym",
				extensions.OCIAnnotationExtensionType: string(extensions.TypeRust),
				extensions.OCIAnnotationArtifact:      extensions.ArtifactBinary,
			},
		}
		d := newTestDownloader(t, t.TempDir(), mock)

		manifests, err := downloadExtensions(t.Context(), d, []string{"my-dym:2.0.0"}, false)
		require.NoError(t, err)
		require.Len(t, manifests, 1)
		require.Equal(t, "my-dym", manifests[0].Name)
		require.Equal(t, extensions.TypeRust, manifests[0].Type)
	})

	t.Run("binary ExtProc extension", func(t *testing.T) {
		mock := &mockOCIClient{
			annotations: map[string]string{
				ocispec.AnnotationTitle:               "my-ext-proc",
				extensions.OCIAnnotationExtensionType: string(extensions.TypeExtProc),
				extensions.OCIAnnotationArtifact:      extensions.ArtifactBinary,
			},
		}
		d := newTestDownloader(t, t.TempDir(), mock)

		manifests, err := downloadExtensions(t.Context(), d, []string{"my-ext-proc:2.0.0"}, false)
		require.NoError(t, err)
		require.Len(t, manifests, 1)
		require.Equal(t, "my-ext-proc", manifests[0].Name)
		require.Equal(t, extensions.TypeExtProc, manifests[0].Type)
	})

	t.Run("binary Go extension", func(t *testing.T) {
		dataHome := t.TempDir()
		composerVersion := "0.1.0"

		// Pre-create libcomposer-lite.so so CheckOrDownloadLibComposerLite succeeds without network.
		composerLiteLib := extensions.LocalCacheComposerLiteLib(&xdg.Directories{DataHome: dataHome}, composerVersion)
		require.NoError(t, os.MkdirAll(filepath.Dir(composerLiteLib), 0o750))
		require.NoError(t, os.WriteFile(composerLiteLib, []byte("fake"), 0o600))

		mock := &mockOCIClient{
			annotations: map[string]string{
				ocispec.AnnotationTitle:                 "my-go-ext",
				extensions.OCIAnnotationExtensionType:   string(extensions.TypeGo),
				extensions.OCIAnnotationArtifact:        extensions.ArtifactBinary,
				extensions.OCIAnnotationComposerVersion: composerVersion,
			},
		}
		d := newTestDownloader(t, dataHome, mock)

		manifests, err := downloadExtensions(t.Context(), d, []string{"my-go-ext:1.0.0"}, false)
		require.NoError(t, err)
		require.Len(t, manifests, 1)
		require.Equal(t, "my-go-ext", manifests[0].Name)
		require.Equal(t, extensions.TypeGo, manifests[0].Type)
	})

	t.Run("multiple extensions", func(t *testing.T) {
		// A single shared mock returns the same artifact for every ref, and
		// ResolveExtensionManifest now keys off the ref name matching the artifact title,
		// so both refs use the titled name (only the count is asserted here).
		mock := &mockOCIClient{
			annotations: map[string]string{
				ocispec.AnnotationTitle:               "ext-a",
				extensions.OCIAnnotationExtensionType: string(extensions.TypeLua),
				extensions.OCIAnnotationArtifact:      extensions.ArtifactBinary,
			},
		}
		d := newTestDownloader(t, t.TempDir(), mock)

		manifests, err := downloadExtensions(t.Context(), d, []string{"ext-a:1.0.0", "ext-a:2.0.0"}, false)
		require.NoError(t, err)
		require.Len(t, manifests, 2)
	})

	t.Run("ref without version defaults to latest", func(t *testing.T) {
		mock := &mockOCIClient{
			annotations: map[string]string{
				ocispec.AnnotationTitle:               "my-ext",
				extensions.OCIAnnotationExtensionType: string(extensions.TypeLua),
				extensions.OCIAnnotationArtifact:      extensions.ArtifactBinary,
			},
			tags: []string{"3.0.0", "2.0.0", "1.0.0"},
		}
		d := newTestDownloader(t, t.TempDir(), mock)

		manifests, err := downloadExtensions(t.Context(), d, []string{"my-ext"}, false)
		require.NoError(t, err)
		require.Len(t, manifests, 1)
		require.Equal(t, "3.0.0", manifests[0].Version)
	})

	t.Run("download error", func(t *testing.T) {
		errDownload := errors.New("download failed")
		mock := &mockOCIClient{
			annotations: map[string]string{
				extensions.OCIAnnotationExtensionType: string(extensions.TypeLua),
				extensions.OCIAnnotationArtifact:      extensions.ArtifactBinary,
			},
			pullErr: errDownload,
		}
		d := newTestDownloader(t, t.TempDir(), mock)

		_, err := downloadExtensions(t.Context(), d, []string{"bad-ext:1.0.0"}, false)
		require.ErrorIs(t, err, errDownload)
	})

	t.Run("unknown artifact type", func(t *testing.T) {
		mock := &mockOCIClient{
			annotations: map[string]string{
				ocispec.AnnotationTitle:               "my-ext",
				extensions.OCIAnnotationExtensionType: string(extensions.TypeLua),
				extensions.OCIAnnotationArtifact:      "unknown-type",
			},
		}
		d := newTestDownloader(t, t.TempDir(), mock)

		_, err := downloadExtensions(t.Context(), d, []string{"my-ext:1.0.0"}, false)
		require.Error(t, err)
		require.Contains(t, err.Error(), "unknown artifact type")
	})

	t.Run("source Go extension", func(t *testing.T) {
		composerVersion := "1.0.0"
		composer := &extensions.Manifest{Name: "composer", Version: composerVersion, Type: extensions.TypeComposer}
		dirs := &xdg.Directories{DataHome: t.TempDir()}

		// Precreate the manifests to simulate a successful download
		composerDir := extensions.LocalCacheExtensionSourceArtifactDir(dirs, composer)
		childDir := filepath.Join(composerDir, "composer-child")
		require.NoError(t, os.MkdirAll(childDir, 0o750))

		composerManifest, err := os.ReadFile("testdata/composer_test.yaml")
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(composerDir, "manifest.yaml"), composerManifest, 0o600))

		childManifest, err := os.ReadFile("testdata/composer_child.yaml")
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(childDir, "manifest.yaml"), childManifest, 0o600))

		mock := &mockOCIClient{
			annotations: map[string]string{
				ocispec.AnnotationTitle:                 "composer",
				extensions.OCIAnnotationExtensionType:   string(extensions.TypeComposer),
				extensions.OCIAnnotationArtifact:        extensions.ArtifactSource,
				extensions.OCIAnnotationComposerVersion: composerVersion,
			},
		}
		d := newTestDownloader(t, dirs.DataHome, mock)

		exts, err := downloadExtensions(t.Context(), d, []string{"composer-child:" + composerVersion}, false)
		require.NoError(t, err)
		require.Len(t, exts, 1)
		require.Equal(t, "Test Author", exts[0].Author)
		require.Equal(t, "1.38.0", exts[0].MinEnvoyVersion)
		require.Equal(t, "1.39.0", exts[0].MaxEnvoyVersion) // This is computed when loading based on the min version
	})

	t.Run("source Go extension with missing source dir", func(t *testing.T) {
		mock := &mockOCIClient{
			annotations: map[string]string{
				ocispec.AnnotationTitle:                 "my-go-src",
				extensions.OCIAnnotationExtensionType:   string(extensions.TypeComposer),
				extensions.OCIAnnotationArtifact:        extensions.ArtifactSource,
				extensions.OCIAnnotationComposerVersion: "0.1.0",
			},
		}
		d := newTestDownloader(t, t.TempDir(), mock)

		_, err := downloadExtensions(t.Context(), d, []string{"my-go-src:1.0.0"}, true)
		require.Error(t, err)
		require.Contains(t, err.Error(), "source directory for extension my-go-src does not exist")
	})

	t.Run("source Rust extension but missing source dir", func(t *testing.T) {
		mock := &mockOCIClient{
			annotations: map[string]string{
				ocispec.AnnotationTitle:               "my-rust-src",
				extensions.OCIAnnotationExtensionType: string(extensions.TypeRust),
				extensions.OCIAnnotationArtifact:      extensions.ArtifactSource,
			},
		}
		d := newTestDownloader(t, t.TempDir(), mock)

		_, err := downloadExtensions(t.Context(), d, []string{"my-rust-src:1.0.0"}, true)
		require.Error(t, err)
		require.Contains(t, err.Error(), "source directory for extension my-rust-src does not exist")
	})

	t.Run("source Rust extension with no Cargo.toml", func(t *testing.T) {
		dataHome := t.TempDir()
		mock := &mockOCIClient{
			annotations: map[string]string{
				ocispec.AnnotationTitle:               "my-rust-src",
				extensions.OCIAnnotationExtensionType: string(extensions.TypeRust),
				extensions.OCIAnnotationArtifact:      extensions.ArtifactSource,
			},
		}
		d := newTestDownloader(t, dataHome, mock)

		// Pre-create the source artifact directory so os.Stat passes and the build tool is invoked.
		srcDir := extensions.LocalCacheExtensionSourceArtifactDir(d.Dirs, &extensions.Manifest{Name: "my-rust-src", Version: "1.0.0"})
		require.NoError(t, os.MkdirAll(srcDir, 0o750))

		_, err := downloadExtensions(t.Context(), d, []string{"my-rust-src:1.0.0"}, true)
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to build Rust dynamic module")
	})

	t.Run("source ExtProc extension with no go.mod", func(t *testing.T) {
		dataHome := t.TempDir()
		mock := &mockOCIClient{
			annotations: map[string]string{
				ocispec.AnnotationTitle:               "my-extproc-src",
				extensions.OCIAnnotationExtensionType: string(extensions.TypeExtProc),
				extensions.OCIAnnotationArtifact:      extensions.ArtifactSource,
			},
		}
		d := newTestDownloader(t, dataHome, mock)

		// Pre-create the source artifact directory so os.Stat passes and the build tool is invoked.
		srcDir := extensions.LocalCacheExtensionSourceArtifactDir(d.Dirs, &extensions.Manifest{Name: "my-extproc-src", Version: "1.0.0"})
		require.NoError(t, os.MkdirAll(srcDir, 0o750))

		_, err := downloadExtensions(t.Context(), d, []string{"my-extproc-src:1.0.0"}, true)
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to build ext_proc server")
	})

	t.Run("source non-composer non-dynamic-module extension", func(t *testing.T) {
		mock := &mockOCIClient{
			annotations: map[string]string{
				ocispec.AnnotationTitle:               "my-lua-src",
				extensions.OCIAnnotationExtensionType: string(extensions.TypeLua),
				extensions.OCIAnnotationArtifact:      extensions.ArtifactSource,
			},
		}
		d := newTestDownloader(t, t.TempDir(), mock)

		manifests, err := downloadExtensions(t.Context(), d, []string{"my-lua-src:1.0.0"}, false)
		require.NoError(t, err)
		require.Len(t, manifests, 1)
		require.Equal(t, "my-lua-src", manifests[0].Name)
	})

	t.Run("error stops processing remaining extensions", func(t *testing.T) {
		callCount := 0
		errFail := errors.New("fail on second")
		d := &extensions.Downloader{
			Logger: internaltesting.NewTLogger(t),
			Dirs:   &xdg.Directories{DataHome: t.TempDir()},
		}
		d.SetClientFactory(func(_ *slog.Logger, _, _, _ string, _ bool) (oci.RepositoryClient, error) {
			callCount++
			if callCount > 1 {
				return &mockOCIClient{pullErr: errFail}, nil
			}
			return &mockOCIClient{
				annotations: map[string]string{
					// Title must match the ref name so the first extension resolves cleanly
					// and processing reaches (and fails on) the second extension.
					ocispec.AnnotationTitle:               "ext-ok",
					extensions.OCIAnnotationExtensionType: string(extensions.TypeLua),
					extensions.OCIAnnotationArtifact:      extensions.ArtifactBinary,
				},
			}, nil
		})

		_, err := downloadExtensions(t.Context(), d, []string{"ext-ok:1.0.0", "ext-fail:1.0.0"}, false)
		require.ErrorIs(t, err, errFail)
	})

	t.Run("goplugin-loader resolves from the composer bundle", func(t *testing.T) {
		dataHome := t.TempDir()
		dirs := &xdg.Directories{DataHome: dataHome}

		// The bare "goplugin-loader" reference is remapped to the composer bundle, which is
		// downloaded and then walked for the embedded goplugin-loader child manifest. Pre-stage the
		// composer bundle cache (root manifest + embedded child) so the mock Pull (a no-op) leaves
		// the resolver real files to read.
		composerCacheDir := extensions.LocalCacheComposerDir(dirs, "1.0.0")
		require.NoError(t, os.MkdirAll(filepath.Join(composerCacheDir, "metadatas", "goplugin-loader"), 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(composerCacheDir, "manifest.yaml"), []byte(`name: composer
version: 1.0.0
composerVersion: 1.0.0
categories:
  - Misc
author: Test
description: test composer
longDescription: |
  test composer
type: composer
tags:
  - go
license: Apache-2.0
examples: []
extensionSet: true
`), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(composerCacheDir, "metadatas", "goplugin-loader", "manifest.yaml"), []byte(`name: goplugin-loader
parent: composer
categories:
  - Misc
author: Test
description: built-in go plugin loader
longDescription: |
  built-in go plugin loader
type: go
tags:
  - go
license: Apache-2.0
examples: []
`), 0o600))

		mock := &mockOCIClient{annotations: map[string]string{
			ocispec.AnnotationTitle:                 extensions.ComposerBundle,
			ocispec.AnnotationVersion:               "1.0.0",
			extensions.OCIAnnotationExtensionType:   string(extensions.TypeComposer),
			extensions.OCIAnnotationComposerVersion: "1.0.0",
			extensions.OCIAnnotationCShared:         "true",
			extensions.OCIAnnotationArtifact:        extensions.ArtifactBinary,
		}}
		d := newTestDownloader(t, dataHome, mock)

		manifests, err := downloadExtensions(t.Context(), d, []string{extensions.GoPluginLoaderName + ":1.0.0"}, false)
		require.NoError(t, err)
		require.Len(t, manifests, 1)

		m := manifests[0]
		require.Equal(t, extensions.GoPluginLoaderName, m.Name)
		require.Equal(t, extensions.TypeGo, m.Type)
		require.True(t, m.CShared, "goplugin-loader must be treated as a c-shared dynamic module")
		// Hosted by the full composer bundle, not composer-lite.
		require.Equal(t, extensions.ComposerBundle, m.Parent)
		require.Equal(t, "1.0.0", m.Version)
		require.Equal(t, "1.0.0", m.ComposerVersion)
		require.True(t, m.Remote)
		// ApplyDefaults should have set the default HTTP filter type.
		require.Equal(t, []extensions.FilterType{extensions.FilterTypeHTTP}, m.FilterTypes)
	})

	t.Run("goplugin-loader fails when the composer bundle cannot be pulled", func(t *testing.T) {
		// A concrete tag with no cached bundle: the composer bundle download must hit the (mock)
		// client, and a pull error surfaces as a wrapped error.
		mock := &mockOCIClient{pullErr: errors.New("no network")}
		d := newTestDownloader(t, t.TempDir(), mock)

		_, err := downloadExtensions(t.Context(), d, []string{extensions.GoPluginLoaderName + ":9.9.9"}, false)
		require.Error(t, err)
		require.Contains(t, err.Error(), extensions.ComposerBundle)
	})
}

func TestResolveParent(t *testing.T) {
	parentYAML := `name: test-parent
version: 9.9.9
composerVersion: 9.9.9
minEnvoyVersion: 1.99.0
type: composer
extensionSet: true
`
	childYAML := `name: test-child
parent: test-parent
categories: [Misc]
author: Test
description: A child extension
longDescription: A child extension
type: go
tags: [test]
license: Apache-2.0
examples: []
`
	childComposerYAML := `name: test-child
parent: composer
categories: [Misc]
author: Test
description: A child extension
longDescription: A child extension
type: go
tags: [test]
license: Apache-2.0
examples: []
`

	t.Run("found locally", func(t *testing.T) {
		tmpDir := t.TempDir()
		childDir := filepath.Join(tmpDir, "child")
		require.NoError(t, os.MkdirAll(childDir, 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "manifest.yaml"), []byte(parentYAML), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(childDir, "manifest.yaml"), []byte(childYAML), 0o600))

		m, err := extensions.LoadLocalManifest(filepath.Join(childDir, "manifest.yaml"))
		require.NoError(t, err)

		d := &extensions.Downloader{Logger: internaltesting.NewTLogger(t)}
		parent, err := resolveParent(t.Context(), d, m)
		require.NoError(t, err)
		require.Equal(t, "test-parent", parent.Name)
		require.Equal(t, "9.9.9", parent.Version)
	})

	t.Run("fallback to registry", func(t *testing.T) {
		tmpDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "manifest.yaml"), []byte(childComposerYAML), 0o600))

		m, err := extensions.LoadLocalManifest(filepath.Join(tmpDir, "manifest.yaml"))
		require.NoError(t, err)

		mock := &mockOCIClient{
			annotations: map[string]string{
				ocispec.AnnotationTitle:                 extensions.ComposerArtifactLite,
				extensions.OCIAnnotationExtensionType:   string(extensions.TypeComposer),
				extensions.OCIAnnotationComposerVersion: "0.5.0",
			},
			tags: []string{"0.5.0", "0.4.0"},
		}
		d := newTestDownloader(t, t.TempDir(), mock)

		parent, err := resolveParent(t.Context(), d, m)
		require.NoError(t, err)
		require.Equal(t, "0.5.0", parent.Version)
	})

	t.Run("fallback uses composerVersion from manifest", func(t *testing.T) {
		m := &extensions.Manifest{
			Name:            "test-child",
			Parent:          "composer",
			ComposerVersion: "0.4.0",
		}

		mock := &mockOCIClient{
			annotations: map[string]string{
				ocispec.AnnotationTitle:                 extensions.ComposerArtifactLite,
				extensions.OCIAnnotationExtensionType:   string(extensions.TypeComposer),
				extensions.OCIAnnotationComposerVersion: "0.4.0",
			},
			tags: []string{"0.5.0", "0.4.0"},
		}
		d := newTestDownloader(t, t.TempDir(), mock)

		parent, err := resolveParent(t.Context(), d, m)
		require.NoError(t, err)
		require.Equal(t, "0.4.0", parent.Version)
	})

	t.Run("fallback for non-composer parent", func(t *testing.T) {
		m := &extensions.Manifest{
			Name:    "test-child",
			Parent:  "custom-parent",
			Version: "1.2.3",
		}

		mock := &mockOCIClient{
			annotations: map[string]string{
				// Title must match the parent name so ResolveExtensionManifest treats the
				// downloaded artifact as the parent itself rather than searching it for a
				// differently-named child extension.
				ocispec.AnnotationTitle:               "custom-parent",
				extensions.OCIAnnotationExtensionType: string(extensions.TypeComposer),
			},
			tags: []string{"1.2.3"},
		}
		d := newTestDownloader(t, t.TempDir(), mock)

		parent, err := resolveParent(t.Context(), d, m)
		require.NoError(t, err)
		require.NotNil(t, parent)
	})

	t.Run("registry error", func(t *testing.T) {
		tmpDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "manifest.yaml"), []byte(childComposerYAML), 0o600))

		m, err := extensions.LoadLocalManifest(filepath.Join(tmpDir, "manifest.yaml"))
		require.NoError(t, err)

		mock := &mockOCIClient{pullErr: errors.New("registry unavailable")}
		d := newTestDownloader(t, t.TempDir(), mock)

		_, err = resolveParent(t.Context(), d, m)
		require.Error(t, err)
		require.Contains(t, err.Error(), "downloading parent composer")
	})
}

func TestValidateComposerCompat(t *testing.T) {
	tests := []struct {
		name       string
		extensions []*extensions.Manifest
		wantErr    string
	}{
		{
			name: "no Go extensions",
			extensions: []*extensions.Manifest{
				{Name: "ext-1", Version: "1.0.0", Type: extensions.TypeRust},
				{Name: "ext-2", Version: "2.0.0", Type: extensions.TypeLua},
			},
		},
		{
			name: "single Go extension",
			extensions: []*extensions.Manifest{
				{Name: "ext-1", Version: "1.0.0", Type: extensions.TypeGo, ComposerVersion: "1.2.3"},
				{Name: "ext-2", Version: "2.0.0", Type: extensions.TypeRust},
			},
		},
		{
			name: "multiple Go extensions with same version",
			extensions: []*extensions.Manifest{
				{Name: "ext-1", Version: "1.0.0", Type: extensions.TypeGo, ComposerVersion: "1.2.3"},
				{Name: "ext-2", Version: "2.0.0", Type: extensions.TypeGo, ComposerVersion: "1.2.3"},
			},
		},
		{
			name: "multiple Go extensions with different versions",
			extensions: []*extensions.Manifest{
				{Name: "ext-1", Version: "1.0.0", Type: extensions.TypeGo, ComposerVersion: "1.2.3"},
				{Name: "ext-2", Version: "2.0.0", Type: extensions.TypeGo, ComposerVersion: "2.0.0"},
				{Name: "ext-3", Version: "2.0.0", Type: extensions.TypeGo, ComposerVersion: "2.0.0"},
			},
			wantErr: `incompatible Go versions found:
  - version 1.2.3 used by extensions: ext-1
  - version 2.0.0 used by extensions: ext-2, ext-3
all Go extensions must use the same composer version`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateComposerCompat(tt.extensions)
			if tt.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.EqualError(t, err, tt.wantErr)
			}
		})
	}
}

func TestWarnMultipleGoExtensions(t *testing.T) {
	tests := []struct {
		name         string
		extensions   []*extensions.Manifest
		wantWarning  bool
		wantContains string // names expected in the warning; defaults to "ext-1, ext-2"
	}{
		{
			name: "single c-shared Go extension - no warning",
			extensions: []*extensions.Manifest{
				{Name: "ext-1", Type: extensions.TypeGo, CShared: true},
			},
		},
		{
			name: "multiple plugin Go extensions - no warning",
			extensions: []*extensions.Manifest{
				{Name: "ext-1", Type: extensions.TypeGo},
				{Name: "ext-2", Type: extensions.TypeGo},
			},
		},
		{
			name: "multiple c-shared Go extensions - warning",
			extensions: []*extensions.Manifest{
				{Name: "ext-1", Type: extensions.TypeGo, CShared: true, Version: "1.0.0"},
				{Name: "ext-2", Type: extensions.TypeGo, CShared: true, Version: "2.0.0"},
			},
			wantWarning:  true,
			wantContains: "ext-1:1.0.0, ext-2:2.0.0",
		},
		{
			name: "one c-shared one plugin Go extension - no warning",
			extensions: []*extensions.Manifest{
				{Name: "ext-1", Type: extensions.TypeGo, CShared: true},
				{Name: "ext-2", Type: extensions.TypeGo},
			},
		},
		{
			name: "mixed types with one c-shared Go - no warning",
			extensions: []*extensions.Manifest{
				{Name: "ext-1", Type: extensions.TypeGo, CShared: true},
				{Name: "ext-2", Type: extensions.TypeRust},
			},
		},
		{
			name: "multiple bundle members share one runtime - no warning",
			// Distinct bundle members (different names) that share the composer runtime via
			// Parent must collapse to a single library and not warn.
			extensions: []*extensions.Manifest{
				{Name: "ext-1", Type: extensions.TypeGo, CShared: true, Parent: extensions.ComposerBundle, Version: "1.0.0"},
				{Name: "ext-2", Type: extensions.TypeGo, CShared: true, Parent: extensions.ComposerBundle, Version: "1.0.0"},
			},
		},
		{
			name: "goplugin-loader plus a distinct c-shared Go - warning",
			extensions: []*extensions.Manifest{
				{Name: extensions.GoPluginLoaderName, Type: extensions.TypeGo, CShared: true, Parent: extensions.ComposerBundle, Version: "1.0.0"},
				{Name: "ext-2", Type: extensions.TypeGo, CShared: true, Version: "2.0.0"},
			},
			wantWarning:  true,
			wantContains: "composer:1.0.0, ext-2:2.0.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Capture stderr output
			oldStderr := os.Stderr
			r, w, _ := os.Pipe()
			os.Stderr = w

			warnMultipleGoExtensions(tt.extensions)

			_ = w.Close()
			os.Stderr = oldStderr

			var buf bytes.Buffer
			_, _ = buf.ReadFrom(r)
			output := buf.String()

			if tt.wantWarning {
				require.Contains(t, output, "Multiple Go extensions detected")
				require.Contains(t, output, tt.wantContains)
				require.Contains(t, output, "goplugin loader")
			} else {
				require.Empty(t, output)
			}
		})
	}
}
