#!/usr/bin/env sh
set -eu

# Check required tools early.
if ! command -v yq >/dev/null 2>&1; then
  echo "error: 'yq' is required but was not found in PATH" >&2
  exit 1
fi

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
README_PATH="$SCRIPT_DIR/README.catalog.md"
MANIFEST_PATH="$SCRIPT_DIR/manifest.yaml"

if [ ! -f "$README_PATH" ]; then
  echo "error: README not found at $README_PATH" >&2
  exit 1
fi


if [ ! -f "$MANIFEST_PATH" ]; then
  echo "error: manifest not found at $MANIFEST_PATH" >&2
  exit 1
fi

sed -i -e 's/[[:space:]]*$//g' "${README_PATH}"

yq eval -i 'del(.longDescription)' "$MANIFEST_PATH"
yq eval -i '.longDescription |= (load_str("'${README_PATH}'"))' "$MANIFEST_PATH"

echo "Updated long description in $MANIFEST_PATH from $README_PATH"
cat $MANIFEST_PATH
