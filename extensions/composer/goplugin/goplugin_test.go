// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package goplugin

import (
	"fmt"
	"strings"
	"testing"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/mocks"
	"go.uber.org/mock/gomock"
)

func Test_Create(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	configHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	configHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	mockFactory := mocks.NewMockHttpFilterConfigFactory(ctrl)

	loadPluginNoError := func(_, _ string, _ bool) (shared.HttpFilterConfigFactory, error) {
		return mockFactory, nil
	}

	loadPluginWithError := func(_, _ string, _ bool) (shared.HttpFilterConfigFactory, error) {
		return nil, fmt.Errorf("error")
	}

	t.Run("Invalid plugin config", func(t *testing.T) {
		configFactory := &GoPluginLoaderConfigFactory{
			LoadPlugin: loadPluginNoError,
		}

		invalidConfig := []byte(`xxxx`)
		_, err := configFactory.Create(configHandle, invalidConfig)
		if !strings.Contains(err.Error(), "failed to load go plugin config from module config") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("No name or url", func(t *testing.T) {
		configFactory := &GoPluginLoaderConfigFactory{
			LoadPlugin: loadPluginNoError,
		}

		noNameOrURLConfig := []byte(`{"name": "", "url": ""}`)
		_, err := configFactory.Create(configHandle, noNameOrURLConfig)
		if !strings.Contains(err.Error(), "plugin name or url is empty") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("Load plugin error", func(t *testing.T) {
		configFactory := &GoPluginLoaderConfigFactory{
			LoadPlugin: loadPluginWithError,
		}

		validConfig := []byte(`{"name": "test", "url": "test_url"}`)
		_, err := configFactory.Create(configHandle, validConfig)
		if !strings.Contains(err.Error(), "failed to handle dynamic module plugin") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("Successful case", func(t *testing.T) {
		configFactory := &GoPluginLoaderConfigFactory{
			LoadPlugin: loadPluginNoError,
		}

		mockFactory.EXPECT().Create(configHandle, gomock.Any()).Return(nil, nil)

		validConfig := []byte(`{"name": "test", "url": "test_url"}`)
		_, err := configFactory.Create(configHandle, validConfig)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

func Test_CreatePerRoute(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	configHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	configHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	mockFactory := mocks.NewMockHttpFilterConfigFactory(ctrl)

	loadPluginNoError := func(_, _ string, _ bool) (shared.HttpFilterConfigFactory, error) {
		return mockFactory, nil
	}

	loadPluginWithError := func(_, _ string, _ bool) (shared.HttpFilterConfigFactory, error) {
		return nil, fmt.Errorf("error")
	}

	t.Run("Invalid plugin config", func(t *testing.T) {
		configFactory := &GoPluginLoaderConfigFactory{
			LoadPlugin: loadPluginNoError,
		}

		invalidConfig := []byte(`xxxx`)
		_, err := configFactory.CreatePerRoute(invalidConfig)
		if !strings.Contains(err.Error(), "failed to load go plugin config from module config") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("No name or url", func(t *testing.T) {
		configFactory := &GoPluginLoaderConfigFactory{
			LoadPlugin: loadPluginNoError,
		}

		noNameOrURLConfig := []byte(`{"name": "", "url": ""}`)
		_, err := configFactory.CreatePerRoute(noNameOrURLConfig)
		if !strings.Contains(err.Error(), "plugin name or url is empty") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("Load plugin error", func(t *testing.T) {
		configFactory := &GoPluginLoaderConfigFactory{
			LoadPlugin: loadPluginWithError,
		}

		validConfig := []byte(`{"name": "test", "url": "test_url"}`)
		_, err := configFactory.CreatePerRoute(validConfig)
		if !strings.Contains(err.Error(), "failed to handle dynamic module plugin") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("Successful case", func(t *testing.T) {
		configFactory := &GoPluginLoaderConfigFactory{
			LoadPlugin: loadPluginNoError,
		}

		mockFactory.EXPECT().CreatePerRoute(gomock.Any()).Return(nil, nil)

		validConfig := []byte(`{"name": "test", "url": "test_url"}`)
		_, err := configFactory.CreatePerRoute(validConfig)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

func TestLoadGoPlugin_DefaultValues(t *testing.T) {
	tests := []struct {
		name         string
		config       string
		expectStrict bool
	}{
		{
			name:         "no defaults specified",
			config:       `{"name": "test", "url": "file:///tmp/test.so"}`,
			expectStrict: true,
		},
		{
			name:         "strict_check false",
			config:       `{"name": "test", "url": "file:///tmp/test.so", "strict_check": false}`,
			expectStrict: false,
		},
		{
			name:         "strict_check true",
			config:       `{"name": "test", "url": "file:///tmp/test.so", "strict_check": true}`,
			expectStrict: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			goPlugin, _, err := loadGoPlugin([]byte(tt.config))
			if err != nil {
				t.Fatalf("loadGoPlugin failed: %v", err)
			}

			if goPlugin.StrictCheck == nil {
				t.Error("StrictCheck should be set")
			} else if *goPlugin.StrictCheck != tt.expectStrict {
				t.Errorf("StrictCheck: got %v, want %v", *goPlugin.StrictCheck, tt.expectStrict)
			}
		})
	}
}

func TestLoadGoPlugin_WithConfig(t *testing.T) {
	configJSON := `{
		"name": "test-plugin",
		"url": "file:///tmp/test.so",
		"config": {
			"key1": "value1",
			"key2": 42,
			"key3": true
		}
	}`

	goPlugin, innerConfig, err := loadGoPlugin([]byte(configJSON))
	if err != nil {
		t.Fatalf("loadGoPlugin failed: %v", err)
	}

	if goPlugin.Name != "test-plugin" {
		t.Errorf("Name: got %s, want test-plugin", goPlugin.Name)
	}

	if goPlugin.URL != "file:///tmp/test.so" {
		t.Errorf("URL: got %s, want file:///tmp/test.so", goPlugin.URL)
	}

	if len(goPlugin.Config) != 3 {
		t.Errorf("Config length: got %d, want 3", len(goPlugin.Config))
	}

	if len(innerConfig) == 0 {
		t.Error("innerConfig should not be empty")
	}

	// Verify innerConfig is valid JSON
	if !strings.Contains(string(innerConfig), "key1") {
		t.Error("innerConfig should contain key1")
	}
}
