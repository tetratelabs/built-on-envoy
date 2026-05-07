// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package cmd

import (
	"context"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/tetratelabs/func-e/experimental/admin"
)

// Healthcheck is a command to check if Envoy is healthy.
type Healthcheck struct{}

// Run executes the healthcheck command.
func (h *Healthcheck) Run(ctx context.Context) error {
	return healthcheck(ctx, io.Discard, os.Stderr)
}

// healthcheck performs looks up the Envoy subprocess, gets its admin port,
// and returns no error when ready.
func healthcheck(ctx context.Context, _, stderr io.Writer) error {
	// Give up to 1 second for the health check
	ctx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()

	logger := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{}))
	// In docker, pid 1 is the boe process
	return doHealthcheck(ctx, 1, logger)
}

func doHealthcheck(ctx context.Context, boePid int, logger *slog.Logger) error {
	if adminClient, err := admin.NewAdminClient(ctx, boePid); err != nil {
		logger.Error("Failed to find Envoy admin server", "error", err)
		return err
	} else if err = adminClient.IsReady(ctx); err != nil {
		logger.Error("Envoy admin server is not ready", "adminPort", adminClient.Port(), "error", err)
		return err
	}
	return nil
}
