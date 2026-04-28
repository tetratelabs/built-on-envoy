// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package pkg provides shared utilities for composer plugins.
package pkg

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
)

var (
	// ErrDataSourceBothSet is returned when both Inline and File fields are set in DataSource.
	ErrDataSourceBothSet = errors.New("only one of 'inline' or 'file' can be set")
	// ErrDataSourceNeitherSet is returned when neither Inline nor File fields are set in DataSource.
	ErrDataSourceNeitherSet = errors.New("either 'inline' or 'file' must be set")

	// ErrInvalidHTTPStatus is returned when a LocalResponse has an invalid HTTP status code.
	ErrInvalidHTTPStatus = errors.New("invalid HTTP status code: must be between 100 and 599")

	// ErrMetadataKeyInvalid is returned when a MetadataKey is missing the Namespace or Key.
	ErrMetadataKeyInvalid = errors.New("metadata key must have both namespace and key")
)

// MetadataKey identifies a location in Envoy's dynamic metadata by combining a
// namespace with a key. Use this in extension configs to write metadata entries
// that downstream filters (e.g. JWT authn, OPA, ext_authz) can read.
type MetadataKey struct {
	// Namespace is the filter-state namespace for the metadata entry.
	Namespace string `json:"namespace"`
	// Key is the key under which the value is stored within the namespace.
	Key string `json:"key"`
}

// Validate the MetadataKey configuration. If default namespace or key is provided by extensions,
// set it before calling Validate.
func (k *MetadataKey) Validate() error {
	if k.Namespace == "" || k.Key == "" {
		return ErrMetadataKeyInvalid
	}
	return nil
}

// DataSource represents a data source that can be either inline or from a file.
type DataSource struct {
	// Inline contains the data directly as a string.
	Inline string `yaml:"inline,omitempty" json:"inline,omitempty"`
	// File contains the path to a file that holds the data.
	File string `yaml:"file,omitempty" json:"file,omitempty"`
}

// Validate the DataSource configuration
func (d *DataSource) Validate() error {
	if d.Inline != "" && d.File != "" {
		return ErrDataSourceBothSet
	}
	if d.Inline == "" && d.File == "" {
		return ErrDataSourceNeitherSet
	}
	return nil
}

// Content returns the content of the DataSource, either from the inline string or by reading the file.
func (d *DataSource) Content() ([]byte, error) {
	if d.Inline != "" {
		return []byte(d.Inline), nil
	}
	if d.File != "" {
		return os.ReadFile(filepath.Clean(d.File))
	}
	return nil, ErrDataSourceNeitherSet
}

// LocalResponse represents a local HTTP response to send to the client.
type LocalResponse struct {
	// Status is the HTTP status code to return. If 0, the plugin uses its default.
	Status int `json:"status,omitempty"`
	// Body is the response body. If empty, the plugin uses its default.
	Body string `json:"body,omitempty"`
	// Headers are additional headers to include in the response.
	Headers map[string]string `json:"headers,omitempty"`
}

// Validate checks that the LocalResponse has a valid HTTP status code if one is set.
func (r *LocalResponse) Validate() error {
	if r.Status < 100 || r.Status > 599 {
		return ErrInvalidHTTPStatus
	}
	return nil
}

// StringMatcher holds a path-matching rule expressed as one of three strategies.
// Exactly one of Prefix, Suffix, or Regex must be set.
//
// JSON representation:
//
//	{"prefix": "/v1/chat/completions"}
//	{"suffix": "/completions"}
//	{"regex":  "^/v1/(chat/completions|custom)$"}
type StringMatcher struct {
	// Prefix matches paths that start with the given string.
	Prefix string `json:"prefix,omitempty"`
	// Suffix matches paths that end with the given string.
	Suffix string `json:"suffix,omitempty"`
	// Regex matches paths that satisfy the compiled regular expression.
	Regex string `json:"regex,omitempty"`

	// compiled form of Regex; set during UnmarshalJSON
	re *regexp.Regexp `json:"-"`
}

// ValidateAndParse checks that exactly one of Prefix, Suffix, or Regex is set and that any provided Regex
// is valid. It also compiles the Regex if provided.
func (m *StringMatcher) ValidateAndParse() error {
	var count int
	if m.Prefix != "" {
		count++
	}
	if m.Suffix != "" {
		count++
	}
	if m.Regex != "" {
		count++
		re, err := regexp.Compile(m.Regex)
		if err != nil {
			return fmt.Errorf("invalid regex %q: %w", m.Regex, err)
		}
		m.re = re
	}
	if count != 1 {
		return fmt.Errorf("exactly one of prefix/suffix/regex must be set, got %d", count)
	}
	return nil
}

// Matches reports whether path satisfies this matcher.
func (m *StringMatcher) Matches(path string) bool {
	if m.Prefix != "" {
		return strings.HasPrefix(path, m.Prefix)
	}
	if m.Suffix != "" {
		return strings.HasSuffix(path, m.Suffix)
	}
	if m.re != nil {
		return m.re.MatchString(path)
	}
	return false
}

// GetMostSpecificConfig is a helper function to get the most specific config of type T from
// the filter handle.
func GetMostSpecificConfig[T any](handle shared.HttpFilterHandle) T {
	var zero T
	mostSpecificConfig := handle.GetMostSpecificConfig()
	if mostSpecificConfig == nil {
		return zero
	}

	config, ok := mostSpecificConfig.(T)
	if !ok {
		handle.Log(shared.LogLevelDebug, "most specific config is not of expected type: %T", mostSpecificConfig)
		return zero
	}

	return config
}
