// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package goplugin provides functionality to load and manage Go plugins.
package goplugin

import (
	"debug/buildinfo"
	"fmt"
	"maps"
	"os"
	"plugin"
	"runtime/debug"
	"slices"
	"strings"

	sdk "github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
	"google.golang.org/protobuf/encoding/protojson"
)

var (
	hostBuildInfo    *buildinfo.BuildInfo
	hostDependencies map[string]*debug.Module
)

func init() {
	info, ok := debug.ReadBuildInfo()
	if ok {
		hostBuildInfo = info
		hostDependencies = make(map[string]*debug.Module)
		for _, dep := range info.Deps {
			if dep.Replace != nil {
				dep = dep.Replace
			}
			hostDependencies[dep.Path] = dep
		}
	} else {
		panic("failed to read host build info") // should never happen.
	}
}

// checkVersionCompatibility checks if the plugin is compatible with the host by comparing their Go versions,
// build modes, and dependencies.
func checkVersionCompatibility(pluginBuildInfo *buildinfo.BuildInfo, buildMode string) error {
	// Check if the Go version of the plugin matches the host Go version.
	if pluginBuildInfo.GoVersion != hostBuildInfo.GoVersion {
		return fmt.Errorf("plugin Go version is different from host Go version")
	}

	// Check if the buildmode of the plugin is "plugin".
	for _, setting := range pluginBuildInfo.Settings {
		if setting.Key == "-buildmode" && setting.Value != buildMode {
			return fmt.Errorf("plugin buildmode is not %q", buildMode)
		}
	}

	// Check if the dependencies of the plugin match the host dependencies.
	for _, pluginDep := range pluginBuildInfo.Deps {
		if pluginDep.Replace != nil {
			pluginDep = pluginDep.Replace
		}
		hostDep, ok := hostDependencies[pluginDep.Path]
		if !ok {
			return fmt.Errorf("plugin dependency is not found in host dependencies")
		}
		if hostDep.Version != pluginDep.Version {
			return fmt.Errorf("plugin dependency: %v has different versions %v/%v",
				pluginDep.Path, pluginDep.Version, hostDep.Version)
		}
		if hostDep.Sum != pluginDep.Sum {
			return fmt.Errorf("plugin dependency: %v has different sums %v/%v",
				pluginDep.Path, pluginDep.Sum, hostDep.Sum)
		}
	}

	return nil
}

// createFactory creates a factory of type T by loading the Go plugin from the given binary path and looking up the symbol.
func createFactory[T any](binaryPath string, symbolName string, pluginName string,
	strictCheck bool,
) (T, error) {
	var goPluginModule T

	if _, err := os.Stat(binaryPath); err != nil {
		return goPluginModule, fmt.Errorf("failed to find a plugin implementation at %v",
			binaryPath)
	}

	pluginBuildInfo, err := buildinfo.ReadFile(binaryPath)
	if err != nil {
		return goPluginModule, fmt.Errorf("failed to read go plugin build info")
	}
	if err = checkVersionCompatibility(pluginBuildInfo, "plugin"); err != nil {
		if strictCheck {
			return goPluginModule, err
		}
		// Log this. This is one line logging and will not cause too much noise even
		// if there are multiple plugins with compatibility issues.
		// TODO(wbpcode): replace fmt.Printf with logger that SDK provides once it's
		// available.
		fmt.Printf("warning: plugin %s failed version compatibility check: %v\n",
			pluginName, err)
	}

	plugin, err := plugin.Open(binaryPath)
	if err != nil {
		return goPluginModule, fmt.Errorf("failed to open plugin as Go plugin module: %w", err)
	}
	sym, err := plugin.Lookup(symbolName)
	if err != nil {
		return goPluginModule, fmt.Errorf("failed to lookup %q: %w", symbolName, err)
	}
	factories, ok := sym.(func() map[string]T)
	if !ok {
		return goPluginModule, fmt.Errorf("unexpected %q type: %w", symbolName, err)
	}
	goPluginModule, ok = factories()[pluginName]
	if !ok {
		plugins := slices.Collect(maps.Keys(factories()))
		return goPluginModule, fmt.Errorf("failed to get config factory from plugin %q. Plugins: %v", pluginName, plugins)
	}
	// Successfully loaded as a Go plugin module.
	return goPluginModule, nil
}

// CreateStreamPluginConfigFactory creates a PluginConfigFactory for the given plugin name.
func CreateStreamPluginConfigFactory(pluginName string, binaryPath string,
	strictCheck bool,
) (shared.HttpFilterConfigFactory, error) {
	return createFactory[shared.HttpFilterConfigFactory](binaryPath,
		"WellKnownHttpFilterConfigFactories", pluginName, strictCheck)
}

func loadGoPlugin(moduleConfig []byte) (*GoPlugin, []byte, error) {
	// load JSON config into GoPlugin
	goPlugin := &GoPlugin{}
	err := protojson.Unmarshal(moduleConfig, goPlugin)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load go plugin config from module config: %w", err)
	}

	// Marshal the inner plugin config back to JSON for the plugin factory.
	innerConfigJSON, marshalErr := protojson.Marshal(goPlugin.GetConfig())
	if marshalErr != nil {
		// This should never happen.
		return nil, nil, fmt.Errorf("failed to marshal inner plugin config to JSON: %w", marshalErr)
	}
	return goPlugin, innerConfigJSON, nil
}

// TODO(wbpcode): when migrating this from extensibility to built-on-envoy, we removed
// remote image fetching support temporarily to keep code tree clean. Re-add it later.
func fetchGoPluginPath(pluginURL string, _ string) (string, error) {
	if strings.HasPrefix(pluginURL, "file://") {
		binaryPath := strings.TrimPrefix(pluginURL, "file://")
		return binaryPath, nil
	}
	return "", fmt.Errorf("unsupported plugin URL: %s", pluginURL)
}

func loadPluginImpl(name, url string, strictCheck bool) (shared.HttpFilterConfigFactory, error) {
	binaryPath, err := fetchGoPluginPath(url, name)
	if err != nil || binaryPath == "" {
		return nil, fmt.Errorf("failed to fetch plugin image: %w", err)
	}

	configFactory, err := CreateStreamPluginConfigFactory(name, binaryPath, strictCheck)
	if err != nil {
		return nil, fmt.Errorf("failed to create config factory for plugin %s: %w", name, err)
	}
	return configFactory, nil
}

// GoPluginLoaderConfigFactory is the config factory for Go plugin loader.
type GoPluginLoaderConfigFactory struct { //nolint:revive
	shared.EmptyHttpFilterConfigFactory

	// Make this a function so we can mock this feature to cover more logic.
	LoadPlugin func(name, url string, strictCheck bool) (shared.HttpFilterConfigFactory, error)
}

// Create creates the HttpFilterFactory from the given unparsed config.
func (f *GoPluginLoaderConfigFactory) Create(handle shared.HttpFilterConfigHandle,
	unparsedConfig []byte,
) (shared.HttpFilterFactory, error) {
	goPlugin, pluginConfig, err := loadGoPlugin(unparsedConfig)
	if err != nil {
		return nil, err
	}
	if goPlugin.GetName() == "" || goPlugin.GetUrl() == "" {
		handle.Log(shared.LogLevelWarn, "plugin name or url is empty: %s/%s", goPlugin.GetName(),
			goPlugin.GetUrl())
		return nil, fmt.Errorf("plugin name or url is empty")
	}

	var strictCheck = true
	if goPlugin.GetStrictCheck() != nil {
		strictCheck = goPlugin.GetStrictCheck().GetValue()
	}

	configFactory, err := f.LoadPlugin(goPlugin.GetName(), goPlugin.GetUrl(),
		strictCheck)
	if err != nil {
		handle.Log(shared.LogLevelWarn, "failed to handle dynamic module plugin: %s/%s/%v",
			goPlugin.GetName(), goPlugin.GetUrl(), err)
		return nil, fmt.Errorf("failed to handle dynamic module plugin %s: %w", goPlugin.GetName(), err)
	}

	pluginFactory, err := configFactory.Create(handle, pluginConfig)
	if err != nil {
		handle.Log(shared.LogLevelWarn, "failed to create plugin factory: %s/%s/%v",
			goPlugin.GetName(), goPlugin.GetUrl(), err)
		return nil, fmt.Errorf("failed to create plugin factory for plugin %s: %w", goPlugin.GetName(), err)
	}

	return pluginFactory, nil
}

// CreatePerRoute creates the per-route config from the given unparsed config.
func (f *GoPluginLoaderConfigFactory) CreatePerRoute(unparsedConfig []byte) (any, error) {
	goPlugin, pluginConfig, err := loadGoPlugin(unparsedConfig)
	if err != nil {
		return nil, err
	}
	if goPlugin.GetName() == "" || goPlugin.GetUrl() == "" {
		return nil, fmt.Errorf("plugin name or url is empty")
	}

	var strictCheck = true
	if goPlugin.GetStrictCheck() != nil {
		strictCheck = goPlugin.GetStrictCheck().GetValue()
	}
	configFactory, err := f.LoadPlugin(goPlugin.GetName(), goPlugin.GetUrl(),
		strictCheck)
	if err != nil {
		return nil, fmt.Errorf("failed to handle dynamic module plugin %s: %w", goPlugin.GetName(), err)
	}
	anyConfig, err := configFactory.CreatePerRoute(pluginConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create per-route config for plugin %s: %w",
			goPlugin.GetName(), err)
	}
	return anyConfig, nil
}

var wellKnownHttpFilterConfigFactories = map[string]shared.HttpFilterConfigFactory{ //nolint:revive
	"goplugin": &GoPluginLoaderConfigFactory{
		LoadPlugin: loadPluginImpl,
	},
}

// WellKnownHttpFilterConfigFactories returns the well-known HttpFilterConfigFactories.
func WellKnownHttpFilterConfigFactories() map[string]shared.HttpFilterConfigFactory { //nolint:revive
	return wellKnownHttpFilterConfigFactories
}

// The go_plugin will always be built into the composer binary.
func init() {
	sdk.RegisterHttpFilterConfigFactories(WellKnownHttpFilterConfigFactories())
}
