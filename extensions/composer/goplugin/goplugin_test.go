// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package goplugin_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/mocks"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/tetratelabs/built-on-envoy/extensions/composer/goplugin"
)

func TestCreateStreamPluginConfigFactory(t *testing.T) {
	t.Run("Binary not found", func(t *testing.T) {
		_, err := goplugin.CreateStreamPluginConfigFactory("plugin", "/nonexistent/path.so", true)
		require.ErrorContains(t, err, "failed to find a plugin implementation")
	})

	t.Run("Not a Go binary", func(t *testing.T) {
		tmpFile := filepath.Join(t.TempDir(), "notgo.so")
		err := os.WriteFile(tmpFile, []byte("not a go binary"), 0o600)
		require.NoError(t, err)

		_, err = goplugin.CreateStreamPluginConfigFactory("plugin", tmpFile, true)
		require.ErrorContains(t, err, "failed to read go plugin build info")
	})
}

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
		configFactory := &goplugin.GoPluginLoaderConfigFactory{
			LoadPlugin: loadPluginNoError,
		}

		invalidConfig := []byte(`xxxx`)
		_, err := configFactory.Create(configHandle, invalidConfig)
		require.ErrorContains(t, err, "failed to load go plugin config from module config")
	})

	t.Run("No name or url", func(t *testing.T) {
		configFactory := &goplugin.GoPluginLoaderConfigFactory{
			LoadPlugin: loadPluginNoError,
		}

		noNameOrURLConfig := []byte(`{"name": "", "url": ""}`)
		_, err := configFactory.Create(configHandle, noNameOrURLConfig)
		require.ErrorContains(t, err, "plugin name or url is empty")
	})

	t.Run("Load plugin error", func(t *testing.T) {
		configFactory := &goplugin.GoPluginLoaderConfigFactory{
			LoadPlugin: loadPluginWithError,
		}

		validConfig := []byte(`{"name": "test", "url": "test_url"}`)
		_, err := configFactory.Create(configHandle, validConfig)
		require.ErrorContains(t, err, "failed to handle dynamic module plugin")
	})

	t.Run("Successful case", func(t *testing.T) {
		configFactory := &goplugin.GoPluginLoaderConfigFactory{
			LoadPlugin: loadPluginNoError,
		}

		mockFactory.EXPECT().Create(configHandle, gomock.Any()).Return(nil, nil)

		validConfig := []byte(`{"name": "test", "url": "test_url"}`)
		_, err := configFactory.Create(configHandle, validConfig)
		require.NoError(t, err)
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
		configFactory := &goplugin.GoPluginLoaderConfigFactory{
			LoadPlugin: loadPluginNoError,
		}

		invalidConfig := []byte(`xxxx`)
		_, err := configFactory.CreatePerRoute(invalidConfig)
		require.ErrorContains(t, err, "failed to load go plugin config from module config")
	})

	t.Run("No name or url", func(t *testing.T) {
		configFactory := &goplugin.GoPluginLoaderConfigFactory{
			LoadPlugin: loadPluginNoError,
		}

		noNameOrURLConfig := []byte(`{"name": "", "url": ""}`)
		_, err := configFactory.CreatePerRoute(noNameOrURLConfig)
		require.ErrorContains(t, err, "plugin name or url is empty")
	})

	t.Run("Load plugin error", func(t *testing.T) {
		configFactory := &goplugin.GoPluginLoaderConfigFactory{
			LoadPlugin: loadPluginWithError,
		}

		validConfig := []byte(`{"name": "test", "url": "test_url"}`)
		_, err := configFactory.CreatePerRoute(validConfig)
		require.ErrorContains(t, err, "failed to handle dynamic module plugin")
	})

	t.Run("Successful case", func(t *testing.T) {
		configFactory := &goplugin.GoPluginLoaderConfigFactory{
			LoadPlugin: loadPluginNoError,
		}

		mockFactory.EXPECT().CreatePerRoute(gomock.Any()).Return(nil, nil)

		validConfig := []byte(`{"name": "test", "url": "test_url"}`)
		_, err := configFactory.CreatePerRoute(validConfig)
		require.NoError(t, err)
	})
}
