#!/bin/sh
# Copyright Built On Envoy
# SPDX-License-Identifier: Apache-2.0
# The full text of the Apache license is available in the LICENSE file at
# the root of the repo.


# This script is used to synchronize extension manifests from the
# extensions/ directory to the cli/internal/extensions/manifests/ directory.
# This is necessary to ensure that the CLI binary has access to the latest
# extension manifests via go:embed.

set -e

CLI_ROOT="$(go list -m -f '{{.Dir}}')"
MANIFESTS_TARGET_DIR="${CLI_ROOT}/internal/extensions/manifests"

echo "Synchronizing extension manifests to ${MANIFESTS_TARGET_DIR}..."

# Synchronize the manifests and schema, preserving directory structure.
rsync -amq --include=*/ \
    --include=manifest.yaml \
    --include=manifest.schema.json \
    --exclude=* \
    "${CLI_ROOT}/../extensions/" "${MANIFESTS_TARGET_DIR}/"
