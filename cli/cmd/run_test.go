// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package cmd

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/require"

	"github.com/tetratelabs/built-on-envoy/cli/internal/extensions"
	internaltesting "github.com/tetratelabs/built-on-envoy/cli/internal/testing"
	"github.com/tetratelabs/built-on-envoy/cli/internal/xdg"
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

      --envoy-version=STRING       Envoy version to use (e.g., 1.31.0)
                                   ($ENVOY_VERSION)
      --log-level="all:error"      Envoy component log level.
      --run-id=STRING              Run identifier for this invocation. Defaults
                                   to timestamp-based ID or $BOE_RUN_ID. Use '0'
                                   for Docker/Kubernetes ($BOE_RUN_ID).
      --listen-port=10000          Port for Envoy listener to accept incoming
                                   traffic.
      --admin-port=9901            Port for Envoy admin interface.
      --extension=EXTENSION,...    Extensions to enable (in the format: "name"
                                   or "name:version").
      --local=LOCAL                Path to a directory containing a local
                                   Extension to enable.
      --config=CONFIG              Optional JSON config string for extensions.
                                   Applied in order to combined --extension and
                                   --local flags.
      --registry="ghcr.io/tetratelabs/built-on-envoy"
                                   OCI registry URL for the extensions
                                   ($BOE_REGISTRY).
      --insecure                   Allow connecting to an insecure (HTTP)
                                   registry ($BOE_REGISTRY_INSECURE).
      --username=STRING            Username for the OCI registry
                                   ($BOE_REGISTRY_USERNAME).
      --password=STRING            Password for the OCI registry
                                   ($BOE_REGISTRY_PASSWORD).
`, internaltesting.WrapHelp(runHelp))

	require.Equal(t, expected, buf.String())
}

func TestParseCmdRunDefaults(t *testing.T) {
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
	require.Empty(t, cli.Run.EnvoyVersion)
	require.Empty(t, cli.Run.Extensions)
	require.Equal(t, extensions.DefaultOCIRegistry, cli.Run.OCI.Registry)
	require.False(t, cli.Run.OCI.Insecure)

	// Verify RunID is generated with expected format: YYYYMMDD_HHMMSS_UUU
	require.NotEmpty(t, cli.Run.RunID)
	require.Regexp(t, `^\d{8}_\d{6}_\d{3}$`, cli.Run.RunID)
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
	require.Equal(t, "1.31.0", cli.Run.EnvoyVersion)
	require.Equal(t, "custom-run-id", cli.Run.RunID)
	require.Equal(t, []string{"cors:1.0.0", "rate-limiter", "auth-jwt"}, cli.Run.Extensions)
	require.Equal(t, "localhost:5000", cli.Run.OCI.Registry)
	require.True(t, cli.Run.OCI.Insecure)
}

func TestValidateLogLevel(t *testing.T) {
	tests := []struct {
		name      string
		logLevel  string
		wantError string
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
			name:      "empty entry",
			logLevel:  " all:error,,upstream:trace ",
			wantError: `invalid log level format "": expected component:level`,
		},
		{
			name:      "missing colon separator",
			logLevel:  "allerror",
			wantError: `invalid log level format "allerror": expected component:level`,
		},
		{
			name:      "empty component",
			logLevel:  ":error",
			wantError: `invalid log level format ":error": component cannot be empty`,
		},
		{
			name:      "empty level",
			logLevel:  "all:",
			wantError: `invalid log level format "all:": level cannot be empty`,
		},
		{
			name:      "whitespace-only component",
			logLevel:  " :error",
			wantError: `invalid log level format " :error": component cannot be empty`,
		},
		{
			name:      "whitespace-only level",
			logLevel:  "all: ",
			wantError: `invalid log level format "all: ": level cannot be empty`,
		},
		{
			name:      "missing colon in multi-component string",
			logLevel:  "all:error,badformat",
			wantError: `invalid log level format "badformat": expected component:level`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := Run{LogLevel: tt.logLevel}
			err := cmd.Validate()
			if tt.wantError == "" {
				require.NoError(t, err)
			} else {
				require.EqualError(t, err, tt.wantError)
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
	require.Error(t, r.Run(t.Context(), &xdg.Directories{}))
}

func TestSplitRef(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantRepo string
		wantTag  string
	}{
		{
			name:     "simple name without tag",
			input:    "cors",
			wantRepo: "cors",
			wantTag:  "latest",
		},
		{
			name:     "simple name with tag",
			input:    "cors:1.0.0",
			wantRepo: "cors",
			wantTag:  "1.0.0",
		},
		{
			name:     "empty string",
			input:    "",
			wantRepo: "",
			wantTag:  "latest",
		},
		{
			name:     "name with multiple colons takes last",
			input:    "foo:bar:baz",
			wantRepo: "foo:bar",
			wantTag:  "baz",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, tag := splitRef(tt.input)
			require.Equal(t, tt.wantRepo, repo)
			require.Equal(t, tt.wantTag, tag)
		})
	}
}

func TestLoadLocalManifests(t *testing.T) {
	dirs := &xdg.Directories{DataHome: t.TempDir()}
	t.Run("empty paths", func(t *testing.T) {
		manifests, err := loadLocalManifests(dirs, []string{}, false)
		require.NoError(t, err)
		require.Empty(t, manifests)
	})

	t.Run("multiple valid paths", func(t *testing.T) {
		manifests, err := loadLocalManifests(dirs, []string{"./testdata", "./testdata/push_pull"}, false)
		require.NoError(t, err)
		require.Len(t, manifests, 2)
		require.Equal(t, "test-lua", manifests[0].Name)
		require.Equal(t, "push-pull", manifests[1].Name)
	})

	t.Run("nonexistent path", func(t *testing.T) {
		_, err := loadLocalManifests(dirs, []string{"/nonexistent/path"}, false)
		require.Error(t, err)
		require.ErrorIs(t, err, errFailedToLoadLocalManifest)
	})

	t.Run("invalid path", func(t *testing.T) {
		_, err := loadLocalManifests(dirs, []string{"./"}, false)
		require.Error(t, err)
		require.ErrorIs(t, err, errFailedToLoadLocalManifest)
	})

	t.Run("invalid composer path", func(t *testing.T) {
		// Create a temporary directory and create an template composer plugin with
		// createComposerHTTPFilter.
		tempDir := t.TempDir()
		err := createComposerHTTPFilter(dirs, tempDir, "test_custom")
		require.NoError(t, err)

		// Remove go.mod and go.sum to simulate invalid composer extension.
		err = os.Remove(tempDir + "/test_custom/go.mod")
		require.NoError(t, err)
		err = os.Remove(tempDir + "/test_custom/go.sum")
		require.NoError(t, err)

		_, err = loadLocalManifests(dirs, []string{tempDir + "/test_custom"}, true)
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to run 'go mod tidy'")
	})

	t.Run("valid composer path", func(t *testing.T) {
		// Create a temporary directory and create an template composer plugin with
		// createComposerHTTPFilter.
		tempDir := t.TempDir()
		err := createComposerHTTPFilter(dirs, tempDir, "test_valid")
		require.NoError(t, err)

		manifests, err := loadLocalManifests(dirs, []string{tempDir + "/test_valid"}, false)
		require.NoError(t, err)
		require.Len(t, manifests, 1)
		require.Equal(t, "test_valid", manifests[0].Name)
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

func TestRunIncomaptibleEnvoyVersion(t *testing.T) {
	r := &Run{
		EnvoyVersion: "1.37.0",
		Local:        []string{"./testdata/input_lua_inline"},
	}

	var err error
	r.extensionPositions, err = saveExtensionPositions([]string{"--local", "./testdata/input_lua_inline"})
	require.NoError(t, err)

	err = r.Run(t.Context(), nil)
	require.ErrorIs(t, err, errIncompatibleEnvoyVersion)
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

func TestValidateComposerCompat(t *testing.T) {
	tests := []struct {
		name       string
		extensions []*extensions.Manifest
		wantErr    string
	}{
		{
			name: "no composer extensions",
			extensions: []*extensions.Manifest{
				{Name: "ext-1", Version: "1.0.0", Type: "http-filter"},
				{Name: "ext-2", Version: "2.0.0", Type: "network-filter"},
			},
		},
		{
			name: "single composer extension",
			extensions: []*extensions.Manifest{
				{Name: "ext-1", Version: "1.0.0", Type: "composer", ComposerVersion: "1.2.3"},
				{Name: "ext-2", Version: "2.0.0", Type: "http-filter"},
			},
		},
		{
			name: "multiple composer extensions with same version",
			extensions: []*extensions.Manifest{
				{Name: "ext-1", Version: "1.0.0", Type: "composer", ComposerVersion: "1.2.3"},
				{Name: "ext-2", Version: "2.0.0", Type: "composer", ComposerVersion: "1.2.3"},
			},
		},
		{
			name: "multiple composer extensions with different versions",
			extensions: []*extensions.Manifest{
				{Name: "ext-1", Version: "1.0.0", Type: "composer", ComposerVersion: "1.2.3"},
				{Name: "ext-2", Version: "2.0.0", Type: "composer", ComposerVersion: "2.0.0"},
				{Name: "ext-3", Version: "2.0.0", Type: "composer", ComposerVersion: "2.0.0"},
			},
			wantErr: `incompatible composer versions found:
  - version 1.2.3 used by extensions: ext-1
  - version 2.0.0 used by extensions: ext-2, ext-3
all composer extensions must use the same composer version`,
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
