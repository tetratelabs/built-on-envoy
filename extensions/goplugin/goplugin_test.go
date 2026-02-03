package goplugin_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/mocks"
	"github.com/tetratelabs/built-on-envoy/extensions/goplugin"
	"go.uber.org/mock/gomock"
)

func Test_Create(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	configHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	configHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	mockFactory := mocks.NewMockHttpFilterConfigFactory(ctrl)

	loadPluginNoError := func(name, url string) (shared.HttpFilterConfigFactory, error) {
		return mockFactory, nil
	}

	loadPluginWithError := func(name, url string) (shared.HttpFilterConfigFactory, error) {
		return nil, fmt.Errorf("error")
	}

	// Invalid plugin config.
	{
		configFactory := &goplugin.GoPluginLoaderConfigFactory{
			LoadPlugin: loadPluginNoError,
		}

		invalidConfig := []byte(`xxxx`)
		_, err := configFactory.Create(configHandle, invalidConfig)
		if !strings.Contains(err.Error(), "failed to load go plugin config from module config") {
			t.Errorf("unexpected error: %v", err)
		}
	}

	// No name or url.
	{
		configFactory := &goplugin.GoPluginLoaderConfigFactory{
			LoadPlugin: loadPluginNoError,
		}

		noNameOrUrlConfig := []byte(`{"name": "", "url": ""}`)
		_, err := configFactory.Create(configHandle, noNameOrUrlConfig)
		if !strings.Contains(err.Error(), "plugin name or url is empty") {
			t.Errorf("unexpected error: %v", err)
		}
	}

	// Load plugin error.
	{
		configFactory := &goplugin.GoPluginLoaderConfigFactory{
			LoadPlugin: loadPluginWithError,
		}

		validConfig := []byte(`{"name": "test", "url": "test_url"}`)
		_, err := configFactory.Create(configHandle, validConfig)
		if !strings.Contains(err.Error(), "failed to handle dynamic module plugin") {
			t.Errorf("unexpected error: %v", err)
		}
	}

	// Successful case.
	{
		configFactory := &goplugin.GoPluginLoaderConfigFactory{
			LoadPlugin: loadPluginNoError,
		}

		mockFactory.EXPECT().Create(configHandle, gomock.Any()).Return(nil, nil)

		validConfig := []byte(`{"name": "test", "url": "test_url"}`)
		_, err := configFactory.Create(configHandle, validConfig)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	}
}

func Test_CreatePerRoute(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	configHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	configHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	mockFactory := mocks.NewMockHttpFilterConfigFactory(ctrl)

	loadPluginNoError := func(name, url string) (shared.HttpFilterConfigFactory, error) {
		return mockFactory, nil
	}

	loadPluginWithError := func(name, url string) (shared.HttpFilterConfigFactory, error) {
		return nil, fmt.Errorf("error")
	}

	// Invalid plugin config.
	{
		configFactory := &goplugin.GoPluginLoaderConfigFactory{
			LoadPlugin: loadPluginNoError,
		}

		invalidConfig := []byte(`xxxx`)
		_, err := configFactory.CreatePerRoute(invalidConfig)
		if !strings.Contains(err.Error(), "failed to load go plugin config from module config") {
			t.Errorf("unexpected error: %v", err)
		}
	}

	// No name or url.
	{
		configFactory := &goplugin.GoPluginLoaderConfigFactory{
			LoadPlugin: loadPluginNoError,
		}

		noNameOrUrlConfig := []byte(`{"name": "", "url": ""}`)
		_, err := configFactory.CreatePerRoute(noNameOrUrlConfig)
		if !strings.Contains(err.Error(), "plugin name or url is empty") {
			t.Errorf("unexpected error: %v", err)
		}
	}

	// Load plugin error.
	{
		configFactory := &goplugin.GoPluginLoaderConfigFactory{
			LoadPlugin: loadPluginWithError,
		}

		validConfig := []byte(`{"name": "test", "url": "test_url"}`)
		_, err := configFactory.CreatePerRoute(validConfig)
		if !strings.Contains(err.Error(), "failed to handle dynamic module plugin") {
			t.Errorf("unexpected error: %v", err)
		}
	}

	// Successful case.
	{
		configFactory := &goplugin.GoPluginLoaderConfigFactory{
			LoadPlugin: loadPluginNoError,
		}

		mockFactory.EXPECT().CreatePerRoute(gomock.Any()).Return(nil, nil)

		validConfig := []byte(`{"name": "test", "url": "test_url"}`)
		_, err := configFactory.CreatePerRoute(validConfig)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	}
}
