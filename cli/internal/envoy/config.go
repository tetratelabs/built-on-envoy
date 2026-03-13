// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package envoy

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"strconv"

	accesslogv3 "github.com/envoyproxy/go-control-plane/envoy/config/accesslog/v3"
	bootstrapv3 "github.com/envoyproxy/go-control-plane/envoy/config/bootstrap/v3"
	clusterv3 "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpointv3 "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	listenerv3 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	routev3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	streamv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/access_loggers/stream/v3"
	routerv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/router/v3"
	hcmv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	tlsv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"sigs.k8s.io/yaml"

	"github.com/tetratelabs/built-on-envoy/cli/internal/extensions"
	"github.com/tetratelabs/built-on-envoy/cli/internal/xdg"
)

// ConfigGenerationParams holds parameters for generating the Envoy config.
type ConfigGenerationParams struct {
	// Logger is used for logging during config generation.
	Logger *slog.Logger
	// AdminPort is the port for Envoy admin interface.
	AdminPort uint32
	// ListenerPort is the port where Envoy listens for incoming traffic.
	ListenerPort uint32
	// Dirs provides access to XDG directories for locating extension resources.
	Dirs *xdg.Directories
	// Extensions to generate the config for
	Extensions []*extensions.Manifest
	// Configs specifies optional JSON config strings for each extension (by index).
	Configs []string
	// Clusters specifies additional Envoy cluster (with TLS) from short names to include in the configuration.
	Clusters []string
	// ClustersInsecure specifies additional Envoy cluster (without TLS) from short names to include in the configuration.
	ClustersInsecure []string
	// ClustersJSON specifies additional Envoy cluster JSON strings to include in the configuration.
	ClustersJSON []string
	// TestUpstreamHost specifies the hostname for the test upstream cluster. Defaults to "httpbin.org".
	// Mutually exclusive with TestUpstreamCluster.
	TestUpstreamHost string
	// TestUpstreamCluster specifies the name of an existing configured cluster to use as the test upstream.
	// The cluster must exist in the generated configuration (from extensions or --cluster/--cluster-insecure/--cluster-json flags).
	// Mutually exclusive with TestUpstreamHost.
	TestUpstreamCluster string
}

const defaultTestUpstreamHost = "httpbin.org"

// GeneratedConfigResources holds the generated Envoy resources for an extension.
type GeneratedConfigResources struct {
	// HTTPFilters are the generated HTTP filters to be included in the Envoy configuration.
	HTTPFilters []*hcmv3.HttpFilter
	// Clusters are the generated clusters to be included in the Envoy configuration.
	Clusters []*clusterv3.Cluster
}

// ConfigRenderer is a function type that renders the Envoy configuration based on the provided parameters and generated resources.
type ConfigRenderer func(*ConfigGenerationParams, GeneratedConfigResources) (string, error)

// RenderConfig renders the Envoy configuration with the given parameters.
// The ouyput is a YAML string that is passed to func-e to run Envoy.
func RenderConfig(params *ConfigGenerationParams, renderer ConfigRenderer) (string, error) {
	gen, err := generateConfig(params)
	if err != nil {
		return "", fmt.Errorf("failed to generate config resources: %w", err)
	}
	rendered, err := renderer(params, gen)
	if err != nil {
		return "", fmt.Errorf("failed to render config: %w", err)
	}
	params.Logger.Debug("generated Envoy config", "config", rendered)
	return rendered, nil
}

// FullConfigRenderer is a default ConfigRenderer that generates the full Envoy configuration with listeners, clusters, and admin interface.
func FullConfigRenderer(params *ConfigGenerationParams, gen GeneratedConfigResources) (string, error) {
	params.Logger.Info("rendering full Envoy config")

	var (
		clusterName string
		hostRewrite string
		newCluster  *clusterv3.Cluster
	)

	if params.TestUpstreamCluster != "" {
		// Validate the named cluster exists in the generated resources.
		found := false
		for _, c := range gen.Clusters {
			if c.Name == params.TestUpstreamCluster {
				found = true
				break
			}
		}
		if !found {
			return "", fmt.Errorf("cluster %q specified via --test-upstream-cluster does not exist; configure it with --cluster, --cluster-insecure, or --cluster-json", params.TestUpstreamCluster)
		}
		clusterName = params.TestUpstreamCluster
	} else {
		testUpstreamHost := params.TestUpstreamHost
		if testUpstreamHost == "" {
			testUpstreamHost = defaultTestUpstreamHost
		}
		var err error
		newCluster, err = buildTestUpstreamCluster("test-upstream", testUpstreamHost, 443, true)
		if err != nil {
			return "", fmt.Errorf("failed to build test upstream cluster: %w", err)
		}
		clusterName = "test-upstream"
		hostRewrite = testUpstreamHost
	}

	cfg, err := buildFullConfig(params.AdminPort, params.ListenerPort, clusterName, hostRewrite, newCluster, gen.HTTPFilters, gen.Clusters)
	if err != nil {
		return "", fmt.Errorf("failed to build config: %w", err)
	}
	cfgYaml, err := ProtoToYaml(cfg)
	if err != nil {
		return "", fmt.Errorf("failed to convert config to YAML: %w", err)
	}
	return string(cfgYaml), nil
}

// MinimalConfigRenderer is a ConfigRenderer that generates a minimal Envoy configuration containing only the generated HTTP filters and clusters.
func MinimalConfigRenderer(params *ConfigGenerationParams, gen GeneratedConfigResources) (string, error) {
	params.Logger.Info("rendering minimal Envoy config")

	filterConfigs, err := protoListToAny(gen.HTTPFilters)
	if err != nil {
		return "", fmt.Errorf("failed to serialize filter configs: %w", err)
	}

	payload := map[string]any{"http_filters": filterConfigs}
	clusterConfigs, err := protoListToAny(gen.Clusters)
	if err != nil {
		return "", fmt.Errorf("failed to serialize cluster configs: %w", err)
	}
	if len(clusterConfigs) > 0 {
		payload["clusters"] = clusterConfigs
	}
	cfgYaml, err := yaml.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal config to YAML: %w", err)
	}

	return string(cfgYaml), nil
}

// generateConfig generates the Envoy configuration resources for the given extensions and parameters.
func generateConfig(params *ConfigGenerationParams) (GeneratedConfigResources, error) {
	filters := make([]*hcmv3.HttpFilter, 0, len(params.Extensions))
	clusters := make([]*clusterv3.Cluster, 0)
	for i, ext := range params.Extensions {
		var config string
		if i < len(params.Configs) {
			config = params.Configs[i]
		}
		resources, err := GenerateFilterConfig(params.Logger, ext, params.Dirs, config)
		if err != nil {
			return GeneratedConfigResources{}, fmt.Errorf("failed to generate filter config for extension %q: %w", ext.Name, err)
		}
		filters = append(filters, resources.HTTPFilters...)
		clusters = append(clusters, resources.Clusters...)
	}

	for i, clusterSpec := range params.Clusters {
		cluster, err := parseCluster(clusterSpec, true)
		if err != nil {
			return GeneratedConfigResources{}, fmt.Errorf("failed to parse --cluster[%d]: %w", i, err)
		}
		clusters = append(clusters, cluster)
	}

	for i, clusterSpec := range params.ClustersInsecure {
		cluster, err := parseCluster(clusterSpec, false)
		if err != nil {
			return GeneratedConfigResources{}, fmt.Errorf("failed to parse --cluster-insecure[%d]: %w", i, err)
		}
		clusters = append(clusters, cluster)
	}

	for i, clusterSpec := range params.ClustersJSON {
		cluster, err := parseJSONCluster(clusterSpec)
		if err != nil {
			return GeneratedConfigResources{}, fmt.Errorf("failed to parse --cluster-json[%d]: %w", i, err)
		}
		clusters = append(clusters, cluster)
	}

	return GeneratedConfigResources{
		HTTPFilters: filters,
		Clusters:    clusters,
	}, nil
}

// parseJSONCluster parses a cluster specification in JSON format.
func parseJSONCluster(spec string) (*clusterv3.Cluster, error) {
	var cluster clusterv3.Cluster
	if err := protojson.Unmarshal([]byte(spec), &cluster); err != nil {
		return nil, fmt.Errorf("invalid JSON cluster spec: %w", err)
	}
	return &cluster, nil
}

// parseCluster parses a cluster specification:
// - in short format "host:tlsPort" that generates a STRICT_DNS cluster with TLS.
// - in short format "host:port" that generates a STRICT_DNS cluster without TLS.
// The cluster name is derived as "host:tlsPort" or "host:port".
func parseCluster(shortSpec string, tls bool) (*clusterv3.Cluster, error) {
	host, portStr, err := net.SplitHostPort(shortSpec)
	if err != nil {
		portFormat := "tlsPort"
		if !tls {
			portFormat = "port"
		}
		return nil, fmt.Errorf("invalid cluster spec %q: must be in the format host:%s", shortSpec, portFormat)
	}

	port, err := strconv.ParseUint(portStr, 10, 32)
	if err != nil {
		return nil, fmt.Errorf("invalid port in cluster short format: %w", err)
	}
	// The cluster name is the host and port combined, e.g. "example.com:443"
	// we can reuse shortSpec as the name since it is already in the correct format.
	return buildTestUpstreamCluster(shortSpec, host, uint32(port), tls)
}

// buildFullConfig creates the EnvoyConfiguration based on the provided parameters.
//
// testUpstreamClusterName is the name of the cluster to route traffic to.
// testUpstreamHostRewrite, if non-empty, is used for the host header rewrite in the route.
// newCluster, if non-nil, is a new cluster to prepend to the static cluster list.
//
// Note we won't generate a "bootstrap" configuration but normal Envoy config. However,
// using the Bootstrap struct is convenient as a wrapper as it is already a `proto.Message`
// and allows us to use the proto marshalling functions. Otherwise, we would have to create a wrapper
// proto on our own, or marshal the config manually.
// TODO(nacx): Is there a wrapper for `admin` and `static_resources` we could use other than Bootstrap?
func buildFullConfig(adminPort, listenerPort uint32, testUpstreamClusterName, testUpstreamHostRewrite string, newCluster *clusterv3.Cluster, filters []*hcmv3.HttpFilter, clusters []*clusterv3.Cluster) (*bootstrapv3.Bootstrap, error) {
	hcm, err := buildHTTPConnectionManager(filters, testUpstreamClusterName, testUpstreamHostRewrite)
	if err != nil {
		return nil, fmt.Errorf("failed to build HTTP connection manager: %w", err)
	}

	hcmAny, err := anypb.New(hcm)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal HTTP connection manager to Any: %w", err)
	}

	listener := &listenerv3.Listener{
		Name: "main",
		Address: &corev3.Address{
			Address: &corev3.Address_SocketAddress{
				SocketAddress: &corev3.SocketAddress{
					Address: "0.0.0.0",
					PortSpecifier: &corev3.SocketAddress_PortValue{
						PortValue: listenerPort,
					},
				},
			},
		},
		FilterChains: []*listenerv3.FilterChain{
			{
				Filters: []*listenerv3.Filter{
					{
						Name: "envoy.filters.network.http_connection_manager",
						ConfigType: &listenerv3.Filter_TypedConfig{
							TypedConfig: hcmAny,
						},
					},
				},
			},
		},
	}

	admin := &bootstrapv3.Admin{
		Address: &corev3.Address{
			Address: &corev3.Address_SocketAddress{
				SocketAddress: &corev3.SocketAddress{
					Address: "127.0.0.1",
					PortSpecifier: &corev3.SocketAddress_PortValue{
						PortValue: adminPort,
					},
				},
			},
		},
	}

	return &bootstrapv3.Bootstrap{
		Admin: admin,
		StaticResources: &bootstrapv3.Bootstrap_StaticResources{
			Listeners: []*listenerv3.Listener{listener},
			Clusters:  prependCluster(newCluster, clusters),
		},
	}, nil
}

// buildHTTPConnectionManager creates the HTTP connection manager configuration.
// testUpstreamClusterName is the cluster to route all traffic to.
// testUpstreamHostRewrite, if non-empty, sets the host header rewrite for the route.
func buildHTTPConnectionManager(filters []*hcmv3.HttpFilter, testUpstreamClusterName, testUpstreamHostRewrite string) (*hcmv3.HttpConnectionManager, error) {
	// Build the router filter
	router := &routerv3.Router{}
	routerAny, err := anypb.New(router)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal router filter to Any: %w", err)
	}

	// Build the stdout access log
	stdoutLog := &streamv3.StdoutAccessLog{}
	stdoutLogAny, err := anypb.New(stdoutLog)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal stdout access log to Any: %w", err)
	}

	httpFilters := append([]*hcmv3.HttpFilter{}, filters...)
	httpFilters = append(httpFilters, &hcmv3.HttpFilter{
		Name: "envoy.filters.http.router",
		ConfigType: &hcmv3.HttpFilter_TypedConfig{
			TypedConfig: routerAny,
		},
	})

	return &hcmv3.HttpConnectionManager{
		StatPrefix: "ingress_http",
		AccessLog: []*accesslogv3.AccessLog{
			{
				Name: "envoy.access_loggers.stdout",
				ConfigType: &accesslogv3.AccessLog_TypedConfig{
					TypedConfig: stdoutLogAny,
				},
			},
		},
		HttpFilters: httpFilters,
		RouteSpecifier: &hcmv3.HttpConnectionManager_RouteConfig{
			RouteConfig: &routev3.RouteConfiguration{
				Name: "default_route",
				VirtualHosts: []*routev3.VirtualHost{
					{
						Name:    "default_service",
						Domains: []string{"*"},
						Routes: []*routev3.Route{
							{
								Match: &routev3.RouteMatch{
									PathSpecifier: &routev3.RouteMatch_Prefix{
										Prefix: "/",
									},
								},
								Action: buildRouteAction(testUpstreamClusterName, testUpstreamHostRewrite),
							},
						},
					},
				},
			},
		},
	}, nil
}

// buildRouteAction creates a route action that forwards traffic to the given cluster.
// If hostRewrite is non-empty, it sets the host header rewrite for the route.
func buildRouteAction(clusterName, hostRewrite string) *routev3.Route_Route {
	routeAction := &routev3.RouteAction{
		ClusterSpecifier: &routev3.RouteAction_Cluster{
			Cluster: clusterName,
		},
	}
	if hostRewrite != "" {
		routeAction.HostRewriteSpecifier = &routev3.RouteAction_HostRewriteLiteral{
			HostRewriteLiteral: hostRewrite,
		}
	}
	return &routev3.Route_Route{Route: routeAction}
}

// prependCluster prepends c to clusters if c is non-nil, otherwise returns clusters unchanged.
func prependCluster(c *clusterv3.Cluster, clusters []*clusterv3.Cluster) []*clusterv3.Cluster {
	if c == nil {
		return clusters
	}
	return append([]*clusterv3.Cluster{c}, clusters...)
}

func buildTestUpstreamCluster(name string, hostname string, port uint32, tls bool) (*clusterv3.Cluster, error) {
	cluster := &clusterv3.Cluster{
		Name: name,
		ClusterDiscoveryType: &clusterv3.Cluster_Type{
			Type: clusterv3.Cluster_STRICT_DNS,
		},
		DnsLookupFamily: clusterv3.Cluster_V4_ONLY,
		LoadAssignment: &endpointv3.ClusterLoadAssignment{
			ClusterName: name,
			Endpoints: []*endpointv3.LocalityLbEndpoints{
				{
					LbEndpoints: []*endpointv3.LbEndpoint{
						{
							HostIdentifier: &endpointv3.LbEndpoint_Endpoint{
								Endpoint: &endpointv3.Endpoint{
									Address: &corev3.Address{
										Address: &corev3.Address_SocketAddress{
											SocketAddress: &corev3.SocketAddress{
												Address: hostname,
												PortSpecifier: &corev3.SocketAddress_PortValue{
													PortValue: port,
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

	if !tls {
		return cluster, nil
	}

	// Add TLS context
	tlsContext := &tlsv3.UpstreamTlsContext{Sni: hostname}
	tlsContextAny, err := anypb.New(tlsContext)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal TLS context to Any: %w", err)
	}
	cluster.TransportSocket = &corev3.TransportSocket{
		Name: "envoy.transport_sockets.tls",
		ConfigType: &corev3.TransportSocket_TypedConfig{
			TypedConfig: tlsContextAny,
		},
	}
	return cluster, nil
}

// ProtoToYaml converts a proto Message to YAML
func ProtoToYaml(msg proto.Message) ([]byte, error) {
	marshaler := protojson.MarshalOptions{
		UseProtoNames:   true,
		EmitUnpopulated: false,
	}

	jsonBytes, err := marshaler.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal bootstrap to JSON: %w", err)
	}

	yamlBytes, err := yaml.JSONToYAML(jsonBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to convert JSON to YAML: %w", err)
	}

	return yamlBytes, nil
}

// protoListToAny converts a list of proto messages to a list of interface{} by marshaling to JSON and unmarshaling back.
func protoListToAny[T proto.Message](items []T) ([]any, error) {
	marshaler := protojson.MarshalOptions{UseProtoNames: true}
	out := make([]any, 0, len(items))
	for _, item := range items {
		raw, err := marshaler.Marshal(item)
		if err != nil {
			return nil, err
		}
		var decoded any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return nil, err
		}
		out = append(out, decoded)
	}

	return out, nil
}
