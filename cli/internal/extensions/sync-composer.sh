#!/bin/bash
# Copyright Built On Envoy
# SPDX-License-Identifier: Apache-2.0
# The full text of the Apache license is available in the LICENSE file at
# the root of the repo.

# This script compresses all extension files from ./extensions into a single
# tar.gz archive in ./cli/internal/extensions for embedding in the CLI binary.

set -e

ROOT="$(git rev-parse --show-toplevel)"

EXTENSIONS_DIR="${ROOT}/extensions"
OUTPUT_DIR="${ROOT}/cli/internal/extensions"
OUTPUT_FILE="${OUTPUT_DIR}/extensions.tar.gz"

echo "Compressing extensions from ${EXTENSIONS_DIR}..."

# Remove old archive if it exists
rm -f "${OUTPUT_FILE}"

# Create tar.gz archive of all extensions
# Exclude unnecessary files like .git, build artifacts, etc.
tar -czf "${OUTPUT_FILE}" \
    -C "${EXTENSIONS_DIR}" \
    --exclude='*.so' \
    --exclude='*.o' \
    --exclude='.git' \
    --exclude='node_modules' \
    --exclude='__pycache__' \
    .

echo "Created ${OUTPUT_FILE}"
