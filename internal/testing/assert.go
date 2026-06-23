// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package internaltesting

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MaybeSkipLongRunningTest skips the test if the -short flag is set.
func MaybeSkipLongRunningTest(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping long-running test in short mode")
	}
}

// SkipIfTestRegistryNotConfigured skips the test if the TEST_BOE_REGISTRY environment variable is not set.
func SkipIfTestRegistryNotConfigured(t *testing.T) {
	if !TestBoeRegistry.Configured() {
		t.Skip("TEST_BOE_REGISTRY environment variable not set, skipping test that requires registry")
	}
	t.Setenv("BOE_REGISTRY", TestBoeRegistry.Get())

	if TestBoeRegistryInsecure.Configured() {
		t.Setenv("BOE_REGISTRY_INSECURE", strconv.FormatBool(TestBoeRegistryInsecure.Get()))
	}
	if TestBoeRegistryUsername.Configured() {
		t.Setenv("BOE_REGISTRY_USERNAME", TestBoeRegistryUsername.Get())
	}
	if TestBoeRegistryPassword.Configured() {
		t.Setenv("BOE_REGISTRY_PASSWORD", TestBoeRegistryPassword.Get())
	}
}

// EqualStatus returns a condition function that checks if the response status code matches the given code.
func EqualStatus(code int) func(r *http.Response) bool {
	return func(r *http.Response) bool {
		return r.StatusCode == code
	}
}

// RequireEventuallyGet performs an HTTP GET request to the given URL and checks if
// it satisfies the provided condition, retrying until it does or a timeout is reached.
func RequireEventuallyGet(t *testing.T, url string, condition func(r *http.Response) bool) {
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()

		assert.NoError(c, checkGet(ctx, url, condition))
	}, time.Minute, 200*time.Millisecond)
}

// RequireEventuallyPost performs an HTTP POST request to the given URL and checks if
// it satisfies the provided condition, retrying until it does or a timeout is reached.
func RequireEventuallyPost(t *testing.T, url string, condition func(r *http.Response) bool) {
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()

		assert.NoError(c, checkPost(ctx, url, condition))
	}, time.Minute, 200*time.Millisecond)
}

// checkGet performs an HTTP GET request to the given URL and checks if it satisfies the provided condition.
func checkGet(ctx context.Context, url string, condition func(r *http.Response) bool) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	return checkRequest(req, condition)
}

// checkPost performs an HTTP POST request to the given URL and checks if it satisfies the provided condition.
func checkPost(ctx context.Context, url string, condition func(r *http.Response) bool) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return err
	}
	return checkRequest(req, condition)
}

// RequireEventuallyRequest performs the given HTTP request and checks if it satisfies the provided condition,
// retrying until it does or a timeout is reached.
func RequireEventuallyRequest(t *testing.T, req *http.Request, condition func(r *http.Response) bool) {
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.NoError(c, checkRequest(req, condition))
	}, time.Minute, 200*time.Millisecond)
}

// checkRequest checks if the given HTTP request succeeds according to the provided condition.
func checkRequest(req *http.Request, condition func(r *http.Response) bool) error {
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
