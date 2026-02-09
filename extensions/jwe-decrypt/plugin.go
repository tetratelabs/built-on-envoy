package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"

	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwe"
)

// Config represents the JSON configuration for this filter.
type jweDecryptConfig struct {
	// KeyFile is the path to the file containing the decryption key.
	KeyFile string `json:"key_file"`
	// InlineKey is the decryption key provided directly in the configuration.
	InlineKey string `json:"inline_key"`
	// InputHeader is the name of the header that contains the JWE string to be decrypted.
	InputHeader string `json:"input_header"`
	// OutputHeader is the name of the header where the decrypted payload will be stored.
	OutputHeader string `json:"output_header"`
	// OutputMetadataKey is the key under which the decrypted payload will be stored in the filter state for later use.
	OutputMetadataKey string `json:"output_metadata_key"`
}

// This is the implementation of the HTTP filter.
type jweDecryptHttpFilter struct {
	shared.HttpFilter
	handle shared.HttpFilterHandle
	config *jweDecryptConfig
}

// DecryptJWE decrypts the given JWE string using the provided key and returns the decrypted payload.
func decryptJWE(jweString string, key []byte) ([]byte, error) {
	return jwe.Decrypt([]byte(jweString), jwe.WithKey(jwa.RSA_OAEP(), key))
}

// getKey retrieves the decryption key based on the filter configuration. It can read the key from a file or use an inline key provided in the configuration.
func getKey(config *jweDecryptConfig) ([]byte, error) {
	// Prefer inline key if provided
	if config.InlineKey != "" {
		return []byte(config.InlineKey), nil
	}

	// Otherwise, read from file
	if config.KeyFile != "" {
		key, err := os.ReadFile(config.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read key file %s: %w", config.KeyFile, err)
		}
		return key, nil
	}

	return nil, fmt.Errorf("no key provided: either key_file or inline_key must be specified")
}

func (f *jweDecryptHttpFilter) OnRequestHeaders(headers shared.HeaderMap, endStream bool) shared.HeadersStatus {
	jweHeaderValues := headers.Get(f.config.InputHeader)
	if len(jweHeaderValues) == 0 {
		f.handle.Log(shared.LogLevelInfo, "jwe-decrypt: no JWE found in header "+f.config.InputHeader)
		return shared.HeadersStatusContinue
	}

	key, err := getKey(f.config)
	if err != nil {
		f.handle.Log(shared.LogLevelError, "jwe-decrypt: failed to get decryption key: "+err.Error())
		return shared.HeadersStatusContinue
	}

	for _, jwe := range jweHeaderValues {
		payload, err := decryptJWE(jwe, key)
		if err != nil {
			f.handle.Log(shared.LogLevelError, "jwe-decrypt: failed to decrypt JWE: "+err.Error())
			continue
		}

		if f.config.OutputHeader != "" {
			headers.Add(f.config.OutputHeader, string(payload))
		}
		if f.config.OutputMetadataKey != "" {
			f.handle.SetMetadata("jwe-decrypt", f.config.OutputMetadataKey, payload)
		}
	}

	return shared.HeadersStatusContinue
}

func (f *jweDecryptHttpFilter) OnRequestBody(body shared.BodyBuffer, endStream bool) shared.BodyStatus {
	return shared.BodyStatusContinue
}

func (f *jweDecryptHttpFilter) OnRequestTrailers(trailers shared.HeaderMap) shared.TrailersStatus {
	return shared.TrailersStatusContinue
}

func (f *jweDecryptHttpFilter) OnResponseHeaders(headers shared.HeaderMap, endStream bool) shared.HeadersStatus {
	return shared.HeadersStatusContinue
}

func (f *jweDecryptHttpFilter) OnResponseBody(body shared.BodyBuffer, endStream bool) shared.BodyStatus {
	return shared.BodyStatusContinue
}

func (f *jweDecryptHttpFilter) OnResponseTrailers(trailers shared.HeaderMap) shared.TrailersStatus {
	return shared.TrailersStatusContinue
}

// This is the factory for the HTTP filter.
type jweDecryptHttpFilterFactory struct {
	config *jweDecryptConfig
}

func (f *jweDecryptHttpFilterFactory) Create(handle shared.HttpFilterHandle) shared.HttpFilter {
	return &jweDecryptHttpFilter{handle: handle, config: f.config}
}

// This is the configuration factory for the HTTP filter.
type jweDecryptHttpFilterConfigFactory struct {
	shared.EmptyHttpFilterConfigFactory
}

func (f *jweDecryptHttpFilterConfigFactory) Create(handle shared.HttpFilterConfigHandle, config []byte) (shared.HttpFilterFactory, error) {
	// Parse JSON configuration
	// TODO: To implement your own configuration parsing and validation logic here.
	if len(config) == 0 {
		handle.Log(shared.LogLevelError, "jwe-decrypt: empty config")
		return nil, fmt.Errorf("empty config")
	}

	cfg := jweDecryptConfig{}
	if err := json.Unmarshal(config, &cfg); err != nil {
		handle.Log(shared.LogLevelError, "jwe-decrypt: failed to parse config: "+err.Error())
		return nil, err
	}

	return &jweDecryptHttpFilterFactory{config: &cfg}, nil
}

func WellKnownHttpFilterConfigFactories() map[string]shared.HttpFilterConfigFactory {
	return map[string]shared.HttpFilterConfigFactory{
		"jwe-decrypt": &jweDecryptHttpFilterConfigFactory{},
	}
}
