// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package mcpopenapi

import (
	_ "embed"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

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
		{"invalid JSON", `not json`, "parsing config: invalid character 'o' in literal null (expecting 'u')"},
		{"empty spec", `{}`, "invalid spec config: one of 'inline', 'file' or 'url' must be set"},
		{"multiple spec sources", `{"spec":{"file":"api.yml","url":"` + ts.URL + `"}}`, "invalid spec config: only one of 'inline', 'file' or 'url' can be set"},
		{"missing spec file", `{"spec":{"file":"x.json"}}`, "reading spec x.json: open x.json: no such file or directory"},
		{"spec url not found", `{"spec":{"url":"` + ts.URL + `/x.json"}}`, "reading spec " + ts.URL + "/x.json: status 404"},
		{"invalid inline spec", `{"spec":{"inline":"openapi: 3.0.3"}}`, "loading spec: validating spec: invalid info: must be an object"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := testRoot(t)
			h := mocks.NewMockHttpFilterConfigHandle(gomock.NewController(t))
			h.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
			ff, err := (&configFactory{root: root}).Create(h, []byte(tt.raw))
			if tt.expectedErr != "" {
				require.EqualError(t, err, tt.expectedErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, new(filterFactory), ff)

			// Both ECDS files are written to the root.
			for _, name := range []string{ecdsFilePath, luaECDSFilePath} {
				_, err := root.Stat(name)
				require.NoError(t, err)
			}
		})
	}
}

func TestSpecSourceString(t *testing.T) {
	require.Equal(t, "https://example.com/api.yml", specSource{URL: "https://example.com/api.yml"}.String())
	require.Equal(t, "/tmp/api.yml", specSource{DataSource: pkg.DataSource{File: "/tmp/api.yml"}}.String())
	require.Equal(t, "<inline>", specSource{DataSource: pkg.DataSource{Inline: "openapi: 3.0.3"}}.String())
}

// TestConfigFactory_CreateSwagger2FromURL loads a Swagger 2.0 spec from a URL and checks
// that the generated bridge routes include the spec's basePath.
func TestConfigFactory_CreateSwagger2FromURL(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(readTestdata(t, "testdata/swagger2.json"))
	}))
	t.Cleanup(ts.Close)

	root := testRoot(t)
	h := mocks.NewMockHttpFilterConfigHandle(gomock.NewController(t))
	h.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	_, err := (&configFactory{root: root}).Create(h, []byte(`{"spec":{"url":"`+ts.URL+`/swagger.json"}}`))
	require.NoError(t, err)

	data, err := root.ReadFile(ecdsFilePath)
	require.NoError(t, err)
	var bridge bridgeECDSConfig
	require.NoError(t, json.Unmarshal(data, &bridge))
	require.Equal(t, []bridgeTool{{Name: "ping", HTTPRule: bridgeHTTPRule{Get: "/v2/ping"}}},
		bridge.Resources[0].TypedConfig.ToolConfig.Tools)
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

// TestConfigFactory_CreateWorkingDirectory checks that the ECDS files are written to the
// working directory when no root is configured.
func TestConfigFactory_CreateWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	inlineSpec, err := json.Marshal(string(testAPISpec))
	require.NoError(t, err)

	h := mocks.NewMockHttpFilterConfigHandle(gomock.NewController(t))
	h.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	_, err = new(configFactory).Create(h, []byte(`{"spec":{"inline":`+string(inlineSpec)+`}}`))
	require.NoError(t, err)

	for _, name := range []string{ecdsFilePath, luaECDSFilePath} {
		require.FileExists(t, filepath.Join(dir, name))
	}
}

func TestConfigFactory_CreateWriteErrors(t *testing.T) {
	inlineSpec, err := json.Marshal(string(testAPISpec))
	require.NoError(t, err)
	raw := []byte(`{"spec":{"inline":` + string(inlineSpec) + `}}`)

	tests := []struct {
		name        string
		dir         string
		expectedErr string
	}{
		{"bridge config", ecdsFilePath, "writing bridge ECDS config: "},
		{"lua config", luaECDSFilePath, "writing Lua ECDS config: "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// A directory with the same name as the ECDS file makes the write fail.
			root := testRoot(t)
			require.NoError(t, root.Mkdir(tt.dir, 0o700))

			h := mocks.NewMockHttpFilterConfigHandle(gomock.NewController(t))
			h.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
			_, err := (&configFactory{root: root}).Create(h, raw)
			require.ErrorContains(t, err, tt.expectedErr)
		})
	}
}
