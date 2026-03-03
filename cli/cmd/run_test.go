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
      --cluster=CLUSTER,...        Optional additional Envoy cluster provided in
                                   the host:tlsPort pattern.
      --cluster-insecure=CLUSTER-INSECURE,...
                                   Optional additional Envoy cluster (with TLS
                                   transport disabled) provided in the host:port
                                   pattern.
      --cluster-json=CLUSTER-JSON
                                   Optional additional Envoy cluster providing
                                   the complete cluster config in JSON format.
      --docker                     Run Envoy as a Docker container instead of
                                   using func-e ($BOE_RUN_DOCKER).
      --pull="missing"             Pull policy for the BOE Docker image
                                   (missing, always, never). Only applicable
                                   when running with --docker.
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
	require.Error(t, r.Run(t.Context(), &xdg.Directories{}, internaltesting.NewTLogger(t)))
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
		err := createGoExtension(logger, downloader.Dirs, tempDir, "test_custom")
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
		err := createGoExtension(logger, downloader.Dirs, tempDir, "test_valid")
		require.NoError(t, err)

		manifests, err := loadLocalManifests(t.Context(), logger, downloader, []string{tempDir + "/test_valid"}, false)
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

	err = r.Run(t.Context(), nil, internaltesting.NewTLogger(t))
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

	t.Run("binary Go extension", func(t *testing.T) {
		dataHome := t.TempDir()
		composerVersion := "0.1.0"

		// Pre-create the libcomposer.so so CheckOrDownloadLibComposer succeeds without network.
		composerDir := extensions.LocalCacheComposerDir(&xdg.Directories{DataHome: dataHome}, composerVersion)
		require.NoError(t, os.MkdirAll(composerDir, 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(composerDir, "libcomposer.so"), []byte("fake"), 0o600))

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
		mock := &mockOCIClient{
			annotations: map[string]string{
				extensions.OCIAnnotationExtensionType: string(extensions.TypeLua),
				extensions.OCIAnnotationArtifact:      extensions.ArtifactBinary,
			},
		}
		d := newTestDownloader(t, t.TempDir(), mock)

		manifests, err := downloadExtensions(t.Context(), d, []string{"ext-a:1.0.0", "ext-b:2.0.0"}, false)
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

		_, err := downloadExtensions(t.Context(), d, []string{"my-go-src:1.0.0"}, false)
		require.Error(t, err)
		require.Contains(t, err.Error(), "missing expected source directory")
	})

	t.Run("source Rust extension with no Cargo.toml", func(t *testing.T) {
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
		require.Contains(t, err.Error(), "no Cargo.toml found")
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
					extensions.OCIAnnotationExtensionType: string(extensions.TypeLua),
					extensions.OCIAnnotationArtifact:      extensions.ArtifactBinary,
				},
			}, nil
		})

		_, err := downloadExtensions(t.Context(), d, []string{"ext-ok:1.0.0", "ext-fail:1.0.0"}, false)
		require.ErrorIs(t, err, errFail)
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
