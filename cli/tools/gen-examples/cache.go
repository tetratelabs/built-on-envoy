// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// This cache keeps repeated example checks fast while extension binaries and runtime state remain isolated.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/mod/semver"
)

func prepareEnvoyCache(root, dataHome, version string) error {
	cache := filepath.Join(root, "extensions", "out", "example-envoy-cache", "envoy-versions")
	if err := os.MkdirAll(cache, 0o700); err != nil {
		return fmt.Errorf("create example Envoy cache: %w", err)
	}
	if semver.IsValid("v" + version) {
		if err := seedInstalledEnvoy(cache, version); err != nil {
			return err
		}
	}
	if err := os.Symlink(cache, filepath.Join(dataHome, "envoy-versions")); err != nil {
		return fmt.Errorf("connect isolated example run to Envoy cache: %w", err)
	}
	return nil
}

func seedInstalledEnvoy(cache, version string) error {
	target := filepath.Join(cache, version, "bin", "envoy")
	if _, err := os.Stat(target); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect cached Envoy: %w", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve BOE cache home: %w", err)
	}
	installed := os.Getenv("BOE_DATA_HOME")
	if installed == "" {
		installed = filepath.Join(home, ".local", "share", "boe")
	} else {
		installed = os.ExpandEnv(installed)
		if installed == "~" {
			installed = home
		} else if strings.HasPrefix(installed, "~/") {
			installed = filepath.Join(home, installed[2:])
		}
	}
	installedRoot, err := os.OpenRoot(installed)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open installed BOE data directory: %w", err)
	}
	defer func() { _ = installedRoot.Close() }()
	input, err := installedRoot.Open(filepath.Join("envoy-versions", version, "bin", "envoy"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open installed Envoy: %w", err)
	}
	defer func() { _ = input.Close() }()
	if mkdirErr := os.MkdirAll(filepath.Dir(target), 0o700); mkdirErr != nil {
		return fmt.Errorf("create cached Envoy directory: %w", mkdirErr)
	}
	output, err := os.CreateTemp(filepath.Dir(target), ".envoy-*")
	if err != nil {
		return fmt.Errorf("stage cached Envoy: %w", err)
	}
	defer func() { _ = os.Remove(output.Name()) }()
	if _, err := io.Copy(output, input); err != nil {
		_ = output.Close()
		return fmt.Errorf("copy installed Envoy: %w", err)
	}
	if err := output.Chmod(0o700); err != nil {
		_ = output.Close()
		return fmt.Errorf("make cached Envoy executable: %w", err)
	}
	if err := output.Close(); err != nil {
		return fmt.Errorf("close cached Envoy: %w", err)
	}
	if err := os.Rename(output.Name(), target); err != nil {
		return fmt.Errorf("install cached Envoy: %w", err)
	}
	return nil
}
