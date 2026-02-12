// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package impl

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"

	"github.com/lestrrat-go/jwx/v3/jwe"
	"github.com/lestrrat-go/jwx/v3/jwk"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
)

// Config represents the JSON configuration for this filter.
type jweDecryptConfig struct {
	// KeyFile is the path to the file containing the decryption key.
	KeyFile string `json:"key_file"`
	// InlineKey is the decryption key provided directly in the configuration, in PEM format, base64 encoded.
	InlineKey string `json:"inline_key"`
	// InputHeader is the name of the header that contains the JWE string to be decrypted. Defaults to Authorization if not specified.
	InputHeader string `json:"input_header"`
	// Prefix is an optional prefix to remove from the input header value before decryption (e.g., "Bearer ").
	Prefix string `json:"prefix"`
	// OutputHeader is the name of the header where the decrypted payload will be stored.
	OutputHeader string `json:"output_header"`
	// OutputMetadataKey is the key under which the decrypted payload will be stored in the request metadata for later use.
	OutputMetadataKey string `json:"output_metadata_key"`

	privateJwks jwk.Set
}

func (c *jweDecryptConfig) getKeySet() (jwk.Set, error) {
	// Key file
	if c.KeyFile != "" {
		bytes, err := os.ReadFile(c.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read key file: %w", err)
		}
		k, err := jwk.ParseString(string(bytes))
		return k, nil
	}

	// Inline key
	// base64 decode the inline key before parsing
	decodedKey, err := base64.StdEncoding.DecodeString(c.InlineKey)
	if err != nil {
		return nil, fmt.Errorf("failed to base64 decode inline key: %w", err)
	}
	k, err := jwk.ParseString(string(decodedKey))
	if err != nil {
		return nil, fmt.Errorf("failed to parse inline key: %w", err)
	}
	return k, nil
}

// This is the implementation of the HTTP filter.
type jweDecryptHttpFilter struct {
	shared.EmptyHttpFilter
	handle shared.HttpFilterHandle
	config *jweDecryptConfig
}

func (f *jweDecryptHttpFilter) OnRequestHeaders(headers shared.HeaderMap, endStream bool) shared.HeadersStatus {
	jweHeaderValues := headers.Get(f.config.InputHeader)
	if len(jweHeaderValues) == 0 {
		f.handle.Log(shared.LogLevelInfo, "jwe-decrypt: no JWE found in header "+f.config.InputHeader)
		return shared.HeadersStatusContinue
	}

	for _, jweValue := range jweHeaderValues {
		f.handle.Log(shared.LogLevelInfo, "Decrypting: "+jweValue)

		// Handle prefix if specified
		encrypted := jweValue
		if f.config.Prefix != "" && len(jweValue) > len(f.config.Prefix) && jweValue[:len(f.config.Prefix)] == f.config.Prefix {
			encrypted = jweValue[len(f.config.Prefix):]
		}

		payload, err := jwe.Decrypt([]byte(encrypted), jwe.WithKeySet(f.config.privateJwks, jwe.WithRequireKid(false)))
		if err != nil {
			f.handle.Log(shared.LogLevelError, "jwe-decrypt: failed to decrypt JWE: "+err.Error())
			continue
		}

		// Put prefix back if it was removed
		if f.config.Prefix != "" {
			payload = append([]byte(f.config.Prefix), payload...)
		}

		if f.config.OutputHeader != "" {
			f.handle.RequestHeaders().Add(f.config.OutputHeader, string(payload))
		}
		if f.config.OutputMetadataKey != "" {
			f.handle.SetMetadata("jwe-decrypt", f.config.OutputMetadataKey, payload)
		}
	}

	return shared.HeadersStatusContinue
}

// This is the factory for the HTTP filter.
type jweDecryptHttpFilterFactory struct {
	config *jweDecryptConfig
}

func (f *jweDecryptHttpFilterFactory) Create(handle shared.HttpFilterHandle) shared.HttpFilter {
	return &jweDecryptHttpFilter{handle: handle, config: f.config}
}

// This is the configuration factory for the HTTP filter.
type JWEDecryptHttpFilterConfigFactory struct {
	shared.EmptyHttpFilterConfigFactory
}

// Create parses the JSON configuration and creates a factory for the HTTP filter.
func (f *JWEDecryptHttpFilterConfigFactory) Create(handle shared.HttpFilterConfigHandle, config []byte) (shared.HttpFilterFactory, error) {
	if len(config) == 0 {
		handle.Log(shared.LogLevelError, "jwe-decrypt: empty config")
		return nil, fmt.Errorf("empty config")
	}

	cfg := jweDecryptConfig{}
	if err := json.Unmarshal(config, &cfg); err != nil {
		handle.Log(shared.LogLevelError, "jwe-decrypt: failed to parse config: "+err.Error())
		return nil, err
	}

	// Parse private key from config (either from file or inline)
	k, err := cfg.getKeySet()
	if err != nil {
		handle.Log(shared.LogLevelError, "jwe-decrypt: failed to get decryption key set: "+err.Error())
		return nil, err
	}
	cfg.privateJwks = k

	// Default input header to "Authorization" if not specified
	if cfg.InputHeader == "" {
		cfg.InputHeader = "Authorization"
	}

	return &jweDecryptHttpFilterFactory{config: &cfg}, nil
}

// WellKnownHttpFilterConfigFactories is used to load the plugin.
func WellKnownHttpFilterConfigFactories() map[string]shared.HttpFilterConfigFactory {
	return map[string]shared.HttpFilterConfigFactory{
		"jwe-decrypt": &JWEDecryptHttpFilterConfigFactory{},
	}
}
