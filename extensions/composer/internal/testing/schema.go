// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package internaltesting provides utilities for testing extensions.
package internaltesting

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/stretchr/testify/require"
)

// AssertSchemaValid validates that jsonStr is valid according to the JSON schema
// at schemaPath (relative to the caller's directory).
func AssertSchemaValid(t *testing.T, schemaPath string, jsonStr string) {
	t.Helper()
	err := validateSchema(schemaPath, jsonStr)
	require.NoError(t, err, "expected config to be valid")
}

// AssertSchemaInvalid validates that jsonStr is NOT valid according to the JSON schema
// at schemaPath (relative to the caller's directory).
func AssertSchemaInvalid(t *testing.T, schemaPath string, jsonStr string) {
	t.Helper()
	err := validateSchema(schemaPath, jsonStr)
	require.Error(t, err, "expected config to be invalid")
}

func validateSchema(schemaPath string, jsonStr string) error {
	_, callerFile, _, _ := runtime.Caller(2)
	dir := filepath.Dir(callerFile)
	absPath := filepath.Join(dir, schemaPath)

	schemaBytes, err := os.ReadFile(filepath.Clean(absPath))
	if err != nil {
		return err
	}

	var schemaDoc any
	if err = json.Unmarshal(schemaBytes, &schemaDoc); err != nil {
		return err
	}

	c := jsonschema.NewCompiler()
	if err = c.AddResource("schema.json", schemaDoc); err != nil {
		return err
	}
	sch, err := c.Compile("schema.json")
	if err != nil {
		return err
	}

	var instance any
	if err = json.Unmarshal([]byte(jsonStr), &instance); err != nil {
		return err
	}

	return sch.Validate(instance)
}
