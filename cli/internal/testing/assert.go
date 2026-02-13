// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package internaltesting

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"testing"
)

// SkipIfTestRegistryNotConfigured skips the test if the TEST_BOE_REGISTRY environment variable is not set.
func SkipIfTestRegistryNotConfigured(t *testing.T) {
	if os.Getenv("TEST_BOE_REGISTRY") == "" {
		t.Skip("TEST_BOE_REGISTRY environment variable not set, skipping test that requires registry")
	}
	t.Setenv("BOE_REGISTRY", os.Getenv("TEST_BOE_REGISTRY"))

	if insecure := os.Getenv("TEST_BOE_REGISTRY_INSECURE"); insecure != "" {
		t.Setenv("BOE_REGISTRY_INSECURE", insecure)
	}
	if username := os.Getenv("TEST_BOE_REGISTRY_USERNAME"); username != "" {
		t.Setenv("BOE_REGISTRY_USERNAME", username)
	}
	if password := os.Getenv("TEST_BOE_REGISTRY_PASSWORD"); password != "" {
		t.Setenv("BOE_REGISTRY_PASSWORD", password)
	}
}

// EqualStatus returns a condition function that checks if the response status code matches the given code.
func EqualStatus(code int) func(r *http.Response) bool {
	return func(r *http.Response) bool {
		return r.StatusCode == code
	}
}

// CheckGet performs an HTTP GET request to the given URL and checks if it satisfies the provided condition.
func CheckGet(ctx context.Context, url string, condition func(r *http.Response) bool) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	return CheckRequest(req, condition)
}

// CheckPost performs an HTTP POST request to the given URL and checks if it satisfies the provided condition.
func CheckPost(ctx context.Context, url string, condition func(r *http.Response) bool) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return err
	}
	return CheckRequest(req, condition)
}

// CheckRequest checks if the given HTTP request succeeds according to the provided condition.
func CheckRequest(req *http.Request, condition func(r *http.Response) bool) error {
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close() // nolint:errcheck
	if !condition(resp) {
		return fmt.Errorf("condition not met (status: %d)", resp.StatusCode)
	}
	return nil
}
