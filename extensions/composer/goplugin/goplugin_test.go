// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package goplugin_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/mocks"
	"go.uber.org/mock/gomock"

	"github.com/tetratelabs/built-on-envoy/extensions/composer/goplugin"
)

func Test_Create(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	configHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	configHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	mockFactory := mocks.NewMockHttpFilterConfigFactory(ctrl)

	loadPluginNoError := func(_, _ string) (shared.HttpFilterConfigFactory, error) {
		return mockFactory, nil
	}

	loadPluginWithError := func(_, _ string) (shared.HttpFilterConfigFactory, error) {
		return nil, fmt.Errorf("error")
	}

	t.Run("Invalid plugin config", func(t *testing.T) {
		configFactory := &goplugin.GoPluginLoaderConfigFactory{
			LoadPlugin: loadPluginNoError,
		}

		invalidConfig := []byte(`xxxx`)
		_, err := configFactory.Create(configHandle, invalidConfig)
		if !strings.Contains(err.Error(), "failed to load go plugin config from module config") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("No name or url", func(t *testing.T) {
		configFactory := &goplugin.GoPluginLoaderConfigFactory{
			LoadPlugin: loadPluginNoError,
		}

		noNameOrURLConfig := []byte(`{"name": "", "url": ""}`)
		_, err := configFactory.Create(configHandle, noNameOrURLConfig)
		if !strings.Contains(err.Error(), "plugin name or url is empty") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("Load plugin error", func(t *testing.T) {
		configFactory := &goplugin.GoPluginLoaderConfigFactory{
			LoadPlugin: loadPluginWithError,
		}

		validConfig := []byte(`{"name": "test", "url": "test_url"}`)
		_, err := configFactory.Create(configHandle, validConfig)
		if !strings.Contains(err.Error(), "failed to handle dynamic module plugin") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("Successful case", func(t *testing.T) {
		configFactory := &goplugin.GoPluginLoaderConfigFactory{
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

	loadPluginNoError := func(_, _ string) (shared.HttpFilterConfigFactory, error) {
		return mockFactory, nil
	}

	loadPluginWithError := func(_, _ string) (shared.HttpFilterConfigFactory, error) {
		return nil, fmt.Errorf("error")
	}

	t.Run("Invalid plugin config", func(t *testing.T) {
		configFactory := &goplugin.GoPluginLoaderConfigFactory{
			LoadPlugin: loadPluginNoError,
		}

		invalidConfig := []byte(`xxxx`)
		_, err := configFactory.CreatePerRoute(invalidConfig)
		if !strings.Contains(err.Error(), "failed to load go plugin config from module config") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("No name or url", func(t *testing.T) {
		configFactory := &goplugin.GoPluginLoaderConfigFactory{
			LoadPlugin: loadPluginNoError,
		}

		noNameOrURLConfig := []byte(`{"name": "", "url": ""}`)
		_, err := configFactory.CreatePerRoute(noNameOrURLConfig)
		if !strings.Contains(err.Error(), "plugin name or url is empty") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("Load plugin error", func(t *testing.T) {
		configFactory := &goplugin.GoPluginLoaderConfigFactory{
			LoadPlugin: loadPluginWithError,
		}

		validConfig := []byte(`{"name": "test", "url": "test_url"}`)
		_, err := configFactory.CreatePerRoute(validConfig)
		if !strings.Contains(err.Error(), "failed to handle dynamic module plugin") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("Successful case", func(t *testing.T) {
		configFactory := &goplugin.GoPluginLoaderConfigFactory{
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
