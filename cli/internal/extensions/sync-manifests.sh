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

ROOT="$(git rev-parse --show-toplevel)"
MANIFESTS_TARGET_DIR="${ROOT}/cli/internal/extensions/manifests"
LIBGOBUNDLE_VERSION_SRC="${ROOT}/extensions/go-bundle/manifest.yaml"


echo "Synchronizing extension manifests to ${MANIFESTS_TARGET_DIR}..."

rm -rf "${MANIFESTS_TARGET_DIR}"

# Synchronize the manifests and schema, preserving directory structure.
rsync -amq --include=*/ \
    --include=manifest.yaml \
    --include=manifest.schema.json \
    --exclude=* \
    "${ROOT}/extensions/" "${MANIFESTS_TARGET_DIR}/"

# Extract libgobundle version from its manifest.yaml, cleaning up whitespaces, etc.
echo "Setting embedded libgobundle version..."

grep -E "^version:" "${LIBGOBUNDLE_VERSION_SRC}" | sed 's/[^:]*:[[:space:]]*//g' | tr -d '\n' > "${MANIFESTS_TARGET_DIR}/libgobundle-version.txt"

if [ ! -s "${MANIFESTS_TARGET_DIR}/libgobundle-version.txt" ]; then
    echo "Failed to extract libgobundle version from ${LIBGOBUNDLE_VERSION_SRC}"
    exit 1
fi
