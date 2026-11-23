// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package mcpopenapi

import (
	"context"
	_ "embed"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/mocks"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/tetratelabs/built-on-envoy/extensions/composer/pkg"
)

//go:embed testdata/test_api.yml
var testAPISpec []byte

func TestConfigFactory_Create(t *testing.T) {
	specFile := filepath.Join(t.TempDir(), "api.yml")
	require.NoError(t, os.WriteFile(specFile, testAPISpec, 0o600))

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api.yml" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write(testAPISpec)
	}))
	t.Cleanup(ts.Close)

	inlineSpec, err := json.Marshal(string(testAPISpec))
	require.NoError(t, err)

	tests := []struct {
		name        string
		raw         string
		expectedErr string
	}{
		{"file spec", `{"spec":{"file":"` + specFile + `"}}`, ""},
		{"inline spec", `{"spec":{"inline":` + string(inlineSpec) + `}}`, ""},
		{"url spec", `{"spec":{"url":"` + ts.URL + `/api.yml"}}`, ""},
		{"url spec with timeout", `{"spec":{"url":"` + ts.URL + `/api.yml","timeout":"5s"}}`, ""},
		{"invalid JSON", `not json`, "parsing config: invalid character 'o' in literal null (expecting 'u')"},
		{"empty spec", `{}`, "invalid spec config: one of 'inline', 'file' or 'url' must be set"},
		{"multiple spec sources", `{"spec":{"file":"api.yml","url":"` + ts.URL + `"}}`, "invalid spec config: only one of 'inline', 'file' or 'url' can be set"},
		{"invalid timeout", `{"spec":{"url":"` + ts.URL + `","timeout":"soon"}}`, `invalid spec config: invalid timeout "soon": must be a positive duration`},
		{"negative timeout", `{"spec":{"url":"` + ts.URL + `","timeout":"-1s"}}`, `invalid spec config: invalid timeout "-1s": must be a positive duration`},
		{"missing spec file", `{"spec":{"file":"x.json"}}`, "reading spec x.json: open x.json: no such file or directory"},
		{"spec url not found", `{"spec":{"url":"` + ts.URL + `/x.json"}}`, "reading spec " + ts.URL + "/x.json: status 404"},
		{"invalid inline spec", `{"spec":{"inline":"openapi: 3.0.3"}}`, "loading spec: validating spec: invalid info: must be an object"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "bridge.json")
			raw := withBridgeConfigPath(t, tt.raw, path)
			ff, err := new(configFactory).Create(newConfigHandle(t), raw)
			if tt.expectedErr != "" {
				require.EqualError(t, err, tt.expectedErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, new(filterFactory), ff)
			require.FileExists(t, path)
		})
	}
}

// TestConfigFactory_CreateSpecTimeout checks that a slow spec URL fails after the configured timeout
// instead of blocking the Envoy main thread.
func TestConfigFactory_CreateSpecTimeout(t *testing.T) {
	release := make(chan struct{})
	ts := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() {
		close(release)
		ts.Close()
	})

	start := time.Now()
	raw := withBridgeConfigPath(t, `{"spec":{"url":"`+ts.URL+`","timeout":"200ms"}}`, filepath.Join(t.TempDir(), "bridge.json"))
	_, err := new(configFactory).Create(newConfigHandle(t), raw)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Less(t, time.Since(start), 5*time.Second)
}

func TestSpecSourceTimeout(t *testing.T) {
	require.Equal(t, defaultSpecFetchTimeout, (&specSource{}).timeout())
	s := specSource{URL: "https://example.com/api.yml", Timeout: "2s"}
	require.NoError(t, s.validate())
	require.Equal(t, 2*time.Second, s.timeout())
}

func TestSpecSourceString(t *testing.T) {
	require.Equal(t, "https://example.com/api.yml", specSource{URL: "https://example.com/api.yml"}.String())
	require.Equal(t, "/tmp/api.yml", specSource{DataSource: pkg.DataSource{File: "/tmp/api.yml"}}.String())
	require.Equal(t, "<inline>", specSource{DataSource: pkg.DataSource{Inline: "openapi: 3.0.3"}}.String())
}

// TestConfigFactory_CreateSwagger2FromURL loads a Swagger 2.0 spec from a URL and checks
// that the generated bridge routes include the spec's basePath.
func TestConfigFactory_CreateSwagger2FromURL(t *testing.T) {
	for _, spec := range []string{"testdata/swagger2.json", "testdata/swagger2.yaml"} {
		t.Run(spec, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write(readTestdata(t, spec))
			}))
			t.Cleanup(ts.Close)

			path := filepath.Join(t.TempDir(), "bridge.json")
			raw := withBridgeConfigPath(t, `{"spec":{"url":"`+ts.URL+`/swagger"}}`, path)
			_, err := new(configFactory).Create(newConfigHandle(t), raw)
			require.NoError(t, err)

			data, err := os.ReadFile(filepath.Clean(path))
			require.NoError(t, err)
			var bridge bridgeECDSConfig
			require.NoError(t, json.Unmarshal(data, &bridge))
			tools := bridge.Resources[0].TypedConfig.ToolConfig.Tools
			require.Len(t, tools, 1)
			require.Equal(t, "ping", tools[0].Name)
			require.Equal(t, bridgeHTTPRule{Get: "/v2/ping"}, tools[0].HTTPRule)
		})
	}
}

func TestWellKnownHttpFilterConfigFactories(t *testing.T) {
	factories := WellKnownHttpFilterConfigFactories()
	require.Len(t, factories, 1)
	require.IsType(t, &configFactory{}, factories[filterName])
}

func TestFilterFactory_Create(t *testing.T) {
	h := mocks.NewMockHttpFilterHandle(gomock.NewController(t))
	require.Equal(t, new(filter), new(filterFactory).Create(h))
}

// TestConfigFactory_CreateDefaultPath checks that the bridge config is written to the
// default path in the working directory when no path is configured.
func TestConfigFactory_CreateDefaultPath(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	inlineSpec, err := json.Marshal(string(testAPISpec))
	require.NoError(t, err)

	_, err = new(configFactory).Create(newConfigHandle(t), []byte(`{"spec":{"inline":`+string(inlineSpec)+`}}`))
	require.NoError(t, err)
	require.FileExists(t, filepath.Join(dir, defaultBridgeConfigPath))
}

func TestConfigFactory_CreateWriteError(t *testing.T) {
	inlineSpec, err := json.Marshal(string(testAPISpec))
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "missing", "bridge.json")
	raw := withBridgeConfigPath(t, `{"spec":{"inline":`+string(inlineSpec)+`}}`, path)

	_, err = new(configFactory).Create(newConfigHandle(t), raw)
	require.ErrorContains(t, err, "writing bridge ECDS config: ")
}

func newConfigHandle(t *testing.T) *mocks.MockHttpFilterConfigHandle {
	t.Helper()
	h := mocks.NewMockHttpFilterConfigHandle(gomock.NewController(t))
	h.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	return h
}

// withBridgeConfigPath sets the bridge_config_path in the given JSON config, if it is a valid
// JSON object. Invalid configs are returned unchanged.
func withBridgeConfigPath(t *testing.T, raw, path string) []byte {
	t.Helper()
	var cfg map[string]any
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return []byte(raw)
	}
	cfg["bridge_config_path"] = path
	out, err := json.Marshal(cfg)
	require.NoError(t, err)
	return out
}
