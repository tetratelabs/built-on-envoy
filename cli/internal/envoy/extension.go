// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package envoy

import (
	"fmt"
	"log/slog"
	"os"
	"path"
	"time"

	clusterv3 "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpointv3 "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	listenerv3 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	dymv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/dynamic_modules/v3"
	dymhttpv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/dynamic_modules/v3"
	extprocv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/ext_proc/v3"
	luav3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/lua/v3"
	wasmhttpv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/wasm/v3"
	dymlistv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/listener/dynamic_modules/v3"
	dymnetv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/dynamic_modules/v3"
	hcmv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	dymudpv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/udp/dynamic_modules/v3"
	httpv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/upstreams/http/v3"
	wasmv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/wasm/v3"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/tetratelabs/built-on-envoy/cli/internal/extensions"
	"github.com/tetratelabs/built-on-envoy/cli/internal/xdg"
)

type (
	// ExtensionFilterGenerator defines an interface for generating filter configurations
	ExtensionFilterGenerator interface {
		// GenerateFilterConfig generates the filter configuration for the given extension manifest.
		GenerateFilterConfig(manifest *extensions.Manifest, dirs *xdg.Directories, config string) (*ExtensionResources, error)
	}

	// LuaFilterGenerator generates filter configuration for Lua extensions.
	LuaFilterGenerator struct{ Logger *slog.Logger }
	// WasmFilterGenerator generates filter configuration for Wasm extensions.
	WasmFilterGenerator struct{ Logger *slog.Logger }
	// DynamicModuleFilterGenerator generates filter configuration for Dynamic Module extensions.
	DynamicModuleFilterGenerator struct{ Logger *slog.Logger }
	// ComposerFilterGenerator generates filter configuration for Composer extensions.
	ComposerFilterGenerator struct{ Logger *slog.Logger }
	// ExtProcFilterGenerator generates filter configuration for ext_proc extensions.
	ExtProcFilterGenerator struct{ Logger *slog.Logger }

	// ExtensionResources holds the resources created by an extension.
	ExtensionResources struct {
		HTTPFilters     []*hcmv3.HttpFilter
		Clusters        []*clusterv3.Cluster
		NetworkFilters  []*listenerv3.Filter
		ListenerFilters []*listenerv3.ListenerFilter
		// UDPListenerFilters are listener filters that must be hosted on a UDP listener
		// (i.e. one whose socket address protocol is UDP), kept separate from ListenerFilters
		// (TCP listener filters) so the config renderer can build the right kind of listener.
		UDPListenerFilters []*listenerv3.ListenerFilter
		// TODO(huabing): may need to add more resources
	}
)

var (
	// ErrUnsupportedExtensionType is returned when an extension type is not supported.
	ErrUnsupportedExtensionType = fmt.Errorf("unsupported extension type")
	// ErrLuaLoadFile is returned when loading Lua code from file fails.
	ErrLuaLoadFile = fmt.Errorf("failed to load Lua file")
)

// GenerateFilterConfig generates the filter configuration for the given extension manifest.
func GenerateFilterConfig(logger *slog.Logger, manifest *extensions.Manifest, dirs *xdg.Directories, config string) (*ExtensionResources, error) {
	var generator ExtensionFilterGenerator

	switch manifest.Type {
	case extensions.TypeLua:
		generator = LuaFilterGenerator{Logger: logger}
	case extensions.TypeWasm:
		generator = WasmFilterGenerator{Logger: logger}
	case extensions.TypeRust:
		generator = DynamicModuleFilterGenerator{Logger: logger}
	case extensions.TypeGo:
		if manifest.CShared {
			generator = DynamicModuleFilterGenerator{Logger: logger}
		} else {
			generator = ComposerFilterGenerator{Logger: logger}
		}
	case extensions.TypeExtProc:
		generator = ExtProcFilterGenerator{Logger: logger}
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedExtensionType, manifest.Type)
	}

	logger.Debug("generating filter config for extension", "name",
		manifest.Name, "type", manifest.Type, "generator", fmt.Sprintf("%T", generator))

	return generator.GenerateFilterConfig(manifest, dirs, config)
}

// GenerateFilterConfig generates the filter configuration for Lua extensions.
func (l LuaFilterGenerator) GenerateFilterConfig(manifest *extensions.Manifest, _ *xdg.Directories, _ string) (*ExtensionResources, error) {
	var code string
	if manifest.Lua.Path != "" {
		l.Logger.Info("loading Lua code from file for extension", "extension", manifest.Name, "path", manifest.Lua.Path)
		absPath := path.Join(path.Dir(manifest.Path), manifest.Lua.Path)
		bytes, err := os.ReadFile(path.Clean(absPath))
		if err != nil {
			return nil, fmt.Errorf("%w %q: %w", ErrLuaLoadFile, manifest.Lua.Path, err)
		}
		code = string(bytes)
	} else if manifest.Lua.Inline != "" {
		l.Logger.Info("using inline Lua code for extension", "extension", manifest.Name)
		code = manifest.Lua.Inline
	}

	luaFilter := &luav3.Lua{
		DefaultSourceCode: &corev3.DataSource{
			Specifier: &corev3.DataSource_InlineString{
				InlineString: code,
			},
		},
	}
	luaAny, err := anypb.New(luaFilter)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal Lua filter to Any: %w", err)
	}

	filter := &hcmv3.HttpFilter{
		Name: manifest.Name,
		ConfigType: &hcmv3.HttpFilter_TypedConfig{
			TypedConfig: luaAny,
		},
	}

	return &ExtensionResources{
		HTTPFilters: []*hcmv3.HttpFilter{filter},
	}, nil
}

// wasmRuntimeV8 is the Envoy Wasm runtime used to execute the compiled module. V8 is the
// default runtime shipped in official Envoy builds.
const wasmRuntimeV8 = "envoy.wasm.runtime.v8"

// GenerateFilterConfig generates the filter configuration for Wasm extensions.
//
// It emits an envoy.filters.http.wasm HTTP filter whose VM loads the compiled module from the
// local cache (extensions/wasm/<name>/<version>/plugin.wasm) via vm_config.code.local.filename.
// The user-supplied --config string is passed to the plugin as its configuration and surfaced to
// proxy-wasm modules through proxy_on_configure (e.g. proxywasm.GetPluginConfiguration in Go).
func (w WasmFilterGenerator) GenerateFilterConfig(manifest *extensions.Manifest,
	dirs *xdg.Directories, config string,
) (*ExtensionResources, error) {
	w.Logger.Info("generating wasm filter config for extension", "name", manifest.Name, "config", config)

	wasmPath := extensions.LocalCacheExtension(dirs, manifest)

	var pluginConfiguration *anypb.Any
	if config != "" {
		// The plugin receives the raw config string. proxy-wasm delivers a StringValue's bytes
		// directly to the module (without the wrapper), so the plugin sees exactly `config`.
		var err error
		pluginConfiguration, err = anypb.New(wrapperspb.String(config))
		if err != nil {
			return nil, fmt.Errorf("failed to marshal wasm plugin configuration to Any: %w", err)
		}
	}

	wasmFilter := &wasmhttpv3.Wasm{
		Config: &wasmv3.PluginConfig{
			Name:   manifest.Name,
			RootId: manifest.Name,
			Vm: &wasmv3.PluginConfig_VmConfig{
				VmConfig: &wasmv3.VmConfig{
					VmId:    manifest.Name,
					Runtime: wasmRuntimeV8,
					Code: &corev3.AsyncDataSource{
						Specifier: &corev3.AsyncDataSource_Local{
							Local: &corev3.DataSource{
								Specifier: &corev3.DataSource_Filename{
									Filename: wasmPath,
								},
							},
						},
					},
				},
			},
			Configuration: pluginConfiguration,
			// Reload the VM on a fatal error instead of failing closed, so a crashing
			// plugin recovers rather than serving 503s indefinitely.
			FailurePolicy: wasmv3.FailurePolicy_FAIL_RELOAD,
			// Allow the plugin's onRequestHeaders/onResponseHeaders callbacks to pause
			// filter iteration (StopIteration).
			AllowOnHeadersStopIteration: wrapperspb.Bool(true),
		},
	}

	wasmAny, err := anypb.New(wasmFilter)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal Wasm filter to Any: %w", err)
	}

	return &ExtensionResources{
		HTTPFilters: []*hcmv3.HttpFilter{
			{
				Name:       manifest.Name,
				ConfigType: &hcmv3.HttpFilter_TypedConfig{TypedConfig: wasmAny},
			},
		},
	}, nil
}

// GenerateFilterConfig generates the filter configuration for Dynamic Module extensions.
func (d DynamicModuleFilterGenerator) GenerateFilterConfig(manifest *extensions.Manifest,
	_ *xdg.Directories, config string,
) (*ExtensionResources, error) {
	d.Logger.Info("generating dynamic module filter config for extension", "name", manifest.Name, "config", config)

	var anyConfig *anypb.Any

	if config != "" {
		// Convert JSON string to StringValue.
		// Ideally we suggest that `config` should be JSON string. But Envoy's DynamicModuleFilter
		// take a string value anyway. And it's possible that a user wants to pass a non-JSON string.
		// We pass the string as-is to Envoy anyway and let the dynamic module handle the content.
		configStringValue := wrapperspb.String(config)
		// Covert the StringValue to Any.
		anyConfig, _ = anypb.New(configStringValue)
	}

	// Use the library name (with underscores) as the dynamic module config name.
	// This is the identifier Envoy uses to reference the loaded module.
	//
	// Bundle-hosted extensions (e.g. the goplugin-loader) are instead loaded
	// through a shared bundle module (e.g. libcomposer.so) named after the bundle.
	moduleName := extensions.ModuleName(manifest)
	// The composer bundle must be loaded globally: it embeds a single Go runtime that
	// has to be shared across all of its hosted filters. Other modules load per-filter.
	// Both the full composer and the independent composer-lite host share this behavior.
	loadGlobally := moduleName == extensions.ComposerBundle || moduleName == extensions.ComposerLiteBundle

	moduleConfig := &dymv3.DynamicModuleConfig{
		Name:             moduleName,
		LoadGlobally:     loadGlobally,
		MetricsNamespace: "builtonenvoy",
	}

	var httpFilters []*hcmv3.HttpFilter
	var networkFilters []*listenerv3.Filter
	var listenerFilters []*listenerv3.ListenerFilter
	var udpListenerFilters []*listenerv3.ListenerFilter

	for _, filterType := range manifest.FilterTypes {
		switch filterType {
		case extensions.FilterTypeNetwork:
			protoConfig := &dymnetv3.DynamicModuleNetworkFilter{
				DynamicModuleConfig: moduleConfig,
				FilterName:          manifest.Name,
				FilterConfig:        anyConfig,
			}
			dynamicModuleAny, err := anypb.New(protoConfig)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal dynamic module network filter to Any: %w", err)
			}
			networkFilters = append(networkFilters, &listenerv3.Filter{
				Name:       manifest.Name,
				ConfigType: &listenerv3.Filter_TypedConfig{TypedConfig: dynamicModuleAny},
			})
		case extensions.FilterTypeListener:
			protoConfig := &dymlistv3.DynamicModuleListenerFilter{
				DynamicModuleConfig: moduleConfig,
				FilterName:          manifest.Name,
				FilterConfig:        anyConfig,
			}
			dynamicModuleAny, err := anypb.New(protoConfig)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal dynamic module listener filter to Any: %w", err)
			}
			listenerFilters = append(listenerFilters, &listenerv3.ListenerFilter{
				Name:       manifest.Name,
				ConfigType: &listenerv3.ListenerFilter_TypedConfig{TypedConfig: dynamicModuleAny},
			})
		case extensions.FilterTypeUDPListener:
			protoConfig := &dymudpv3.DynamicModuleUdpListenerFilter{
				DynamicModuleConfig: moduleConfig,
				FilterName:          manifest.Name,
				FilterConfig:        anyConfig,
			}
			dynamicModuleAny, err := anypb.New(protoConfig)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal dynamic module UDP listener filter to Any: %w", err)
			}
			udpListenerFilters = append(udpListenerFilters, &listenerv3.ListenerFilter{
				Name:       manifest.Name,
				ConfigType: &listenerv3.ListenerFilter_TypedConfig{TypedConfig: dynamicModuleAny},
			})
		default: // FilterTypeHTTP or empty
			protoConfig := &dymhttpv3.DynamicModuleFilter{
				DynamicModuleConfig: moduleConfig,
				FilterName:          manifest.Name,
				FilterConfig:        anyConfig,
			}
			dynamicModuleAny, err := anypb.New(protoConfig)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal dynamic module filter to Any: %w", err)
			}
			httpFilters = append(httpFilters, &hcmv3.HttpFilter{
				Name:       manifest.Name,
				ConfigType: &hcmv3.HttpFilter_TypedConfig{TypedConfig: dynamicModuleAny},
			})
		}
	}

	return &ExtensionResources{
		HTTPFilters:        httpFilters,
		NetworkFilters:     networkFilters,
		ListenerFilters:    listenerFilters,
		UDPListenerFilters: udpListenerFilters,
	}, nil
}

// GenerateFilterConfig generates the filter configuration for Composer extensions.
func (c ComposerFilterGenerator) GenerateFilterConfig(manifest *extensions.Manifest, dirs *xdg.Directories, config string) (*ExtensionResources, error) {
	c.Logger.Info("generating composer filter config for extension", "name", manifest.Name, "config", config)

	var pluginURL string
	if manifest.Remote && manifest.SourceRegistry != "" {
		// For remote extensions, use oci:// URL so config is portable.
		pluginURL = "oci://" + extensions.RepositoryName(manifest.SourceRegistry, manifest.Name) + ":" + manifest.SourceTag
	} else {
		cachedPluginPath := extensions.LocalCacheExtension(dirs, manifest)
		pluginURL = "file://" + cachedPluginPath
	}

	// Covert the config to struct first. For go plugin/composer extensions, we ensure the
	// config is always a valid JSON string (could be converted to google.protobuf.Struct).
	var configValue *structpb.Value
	if config != "" {
		innerStruct := &structpb.Struct{}
		err := protojson.Unmarshal([]byte(config), innerStruct)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal config JSON string to Struct: %w", err)
		}
		configValue = structpb.NewStructValue(innerStruct)
	} else {
		configValue = structpb.NewNullValue()
	}

	// Create New proto struct for Composer go plugin filter.
	configStruct := &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"name":   structpb.NewStringValue(manifest.Name),
			"url":    structpb.NewStringValue(pluginURL),
			"config": configValue,
			// TODO(wbpcode): this could be false always in local testing/development.
			// Should we support to configure this or give different default value for
			// `run` and `genconfig` command?
			"strict_check": structpb.NewBoolValue(false),
		},
	}

	// Covert to JSON string.
	configJSON, _ := protojson.Marshal(configStruct)

	// Convert JSON string to StringValue.
	configStringValue := wrapperspb.String(string(configJSON))

	// Covert the StringValue to Any.
	anyConfig, _ := anypb.New(configStringValue)

	// Standalone Go plugins (CShared=false) are loaded dynamically via the goplugin-loader, which
	// is hosted by the independent composer-lite module (libcomposer-lite.so). Bundle-hosted
	// children are CShared=true and go through DynamicModuleFilterGenerator instead, so they never
	// reach here.
	protoConfig := &dymhttpv3.DynamicModuleFilter{
		DynamicModuleConfig: &dymv3.DynamicModuleConfig{
			Name:             extensions.ComposerLiteBundle,
			LoadGlobally:     true,
			MetricsNamespace: "builtonenvoy",
		},
		FilterName:   extensions.GoPluginLoaderName,
		FilterConfig: anyConfig,
	}
	composerAny, err := anypb.New(protoConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal Composer filter to Any: %w", err)
	}

	return &ExtensionResources{
		HTTPFilters: []*hcmv3.HttpFilter{
			{
				Name: manifest.Name,
				ConfigType: &hcmv3.HttpFilter_TypedConfig{
					TypedConfig: composerAny,
				},
			},
		},
	}, nil
}

const extProcClusterSuffix = "-ext-proc"

// GenerateFilterConfig generates the filter and upstream cluster configuration for ext_proc extensions.
func (e ExtProcFilterGenerator) GenerateFilterConfig(manifest *extensions.Manifest, _ *xdg.Directories, _ string) (*ExtensionResources, error) {
	e.Logger.Info("generating ext_proc filter config for extension", "name", manifest.Name)

	cfg := manifest.ExtProc
	clusterName := manifest.Name + extProcClusterSuffix
	port := cfg.GRPCPort
	if port == 0 {
		port = 50051
	}

	extProcFilter := &extprocv3.ExternalProcessor{
		GrpcService: &corev3.GrpcService{
			TargetSpecifier: &corev3.GrpcService_EnvoyGrpc_{
				EnvoyGrpc: &corev3.GrpcService_EnvoyGrpc{
					ClusterName: clusterName,
				},
			},
		},
		FailureModeAllow: cfg.FailureModeAllow,
		ProcessingMode:   extProcProcessingMode(cfg.ProcessingMode),
	}

	if cfg.MessageTimeout != "" {
		d, err := time.ParseDuration(cfg.MessageTimeout)
		if err != nil {
			return nil, fmt.Errorf("invalid messageTimeout %q: %w", cfg.MessageTimeout, err)
		}
		extProcFilter.MessageTimeout = durationpb.New(d)
	}

	filterAny, err := anypb.New(extProcFilter)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal ext_proc filter to Any: %w", err)
	}

	httpProtocolOptions := &httpv3.HttpProtocolOptions{
		UpstreamProtocolOptions: &httpv3.HttpProtocolOptions_ExplicitHttpConfig_{
			ExplicitHttpConfig: &httpv3.HttpProtocolOptions_ExplicitHttpConfig{
				ProtocolConfig: &httpv3.HttpProtocolOptions_ExplicitHttpConfig_Http2ProtocolOptions{
					Http2ProtocolOptions: &corev3.Http2ProtocolOptions{},
				},
			},
		},
	}
	httpProtocolOptionsAny, err := anypb.New(httpProtocolOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal HTTP protocol options to Any: %w", err)
	}

	cluster := &clusterv3.Cluster{
		Name: clusterName,
		ClusterDiscoveryType: &clusterv3.Cluster_Type{
			Type: clusterv3.Cluster_STATIC,
		},
		TypedExtensionProtocolOptions: map[string]*anypb.Any{
			"envoy.extensions.upstreams.http.v3.HttpProtocolOptions": httpProtocolOptionsAny,
		},
		LoadAssignment: &endpointv3.ClusterLoadAssignment{
			ClusterName: clusterName,
			Endpoints: []*endpointv3.LocalityLbEndpoints{
				{
					LbEndpoints: []*endpointv3.LbEndpoint{
						{
							HostIdentifier: &endpointv3.LbEndpoint_Endpoint{
								Endpoint: &endpointv3.Endpoint{
									Address: &corev3.Address{
										Address: &corev3.Address_SocketAddress{
											SocketAddress: &corev3.SocketAddress{
												Address: "127.0.0.1",
												PortSpecifier: &corev3.SocketAddress_PortValue{
													PortValue: uint32(port), //nolint:gosec
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	return &ExtensionResources{
		HTTPFilters: []*hcmv3.HttpFilter{
			{
				Name:       manifest.Name,
				ConfigType: &hcmv3.HttpFilter_TypedConfig{TypedConfig: filterAny},
			},
		},
		Clusters: []*clusterv3.Cluster{cluster},
	}, nil
}

// extProcProcessingMode converts the manifest processing mode config to the Envoy proto type.
func extProcProcessingMode(m *extensions.ExtProcProcessingMode) *extprocv3.ProcessingMode {
	if m == nil {
		return nil
	}
	return &extprocv3.ProcessingMode{
		RequestHeaderMode:  extProcHeaderSendMode(m.RequestHeaderMode),
		ResponseHeaderMode: extProcHeaderSendMode(m.ResponseHeaderMode),
		RequestBodyMode:    extProcBodySendMode(m.RequestBodyMode),
		ResponseBodyMode:   extProcBodySendMode(m.ResponseBodyMode),
	}
}

func extProcHeaderSendMode(s string) extprocv3.ProcessingMode_HeaderSendMode {
	switch s {
	case "SEND":
		return extprocv3.ProcessingMode_SEND
	case "SKIP":
		return extprocv3.ProcessingMode_SKIP
	default:
		return extprocv3.ProcessingMode_DEFAULT
	}
}

func extProcBodySendMode(s string) extprocv3.ProcessingMode_BodySendMode {
	switch s {
	case "BUFFERED":
		return extprocv3.ProcessingMode_BUFFERED
	case "STREAMED":
		return extprocv3.ProcessingMode_STREAMED
	case "BUFFERED_PARTIAL":
		return extprocv3.ProcessingMode_BUFFERED_PARTIAL
	default:
		return extprocv3.ProcessingMode_NONE
	}
}
