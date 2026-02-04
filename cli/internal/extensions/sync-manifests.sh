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
LIBCOMPOSER_VERSION_SRC="${ROOT}/extensions/internal/libcomposer/Makefile"


echo "Synchronizing extension manifests to ${MANIFESTS_TARGET_DIR}..."

rm -rf "${MANIFESTS_TARGET_DIR}"

# Synchronize the manifests and schema, preserving directory structure.
rsync -amq --include=*/ \
    --include=manifest.yaml \
    --include=manifest.schema.json \
    --exclude=* \
    "${ROOT}/extensions/" "${MANIFESTS_TARGET_DIR}/"

# Extract libcomposer version from its Makefile, cleaning up whitespaces, etc.
echo "Extracting libcomposer version..."

grep -E "^VERSION[[:space:]]*[:?]?=" "${LIBCOMPOSER_VERSION_SRC}" | sed 's/.*=[[:space:]]*//' | tr -d '\n' > "${MANIFESTS_TARGET_DIR}/libcomposer-version.txt"

if [ ! -s "${MANIFESTS_TARGET_DIR}/libcomposer-version.txt" ]; then
    echo "Failed to extract libcomposer version from ${LIBCOMPOSER_VERSION_SRC}"
    exit 1
fi
