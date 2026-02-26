// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package pkg provides shared utilities for composer plugins.
package pkg

import (
	"errors"
	"os"
	"path/filepath"
)

var (
	// ErrDataSourceBothSet is returned when both Inline and File fields are set in DataSource.
	ErrDataSourceBothSet = errors.New("only one of 'inline' or 'file' can be set")
	// ErrDataSourceNeitherSet is returned when neither Inline nor File fields are set in DataSource.
	ErrDataSourceNeitherSet = errors.New("either 'inline' or 'file' must be set")
)

// DataSource represents a data source that can be either inline or from a file.
type DataSource struct {
	// Inline contains the data directly as a string.
	Inline string `yaml:"inline,omitempty"`
	// File contains the path to a file that holds the data.
	File string `yaml:"file,omitempty"`
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
