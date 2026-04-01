#!/bin/sh
# Copyright Built On Envoy
# SPDX-License-Identifier: Apache-2.0
# The full text of the Apache license is available in the LICENSE file at
# the root of the repo.

set -e

REPO="tetratelabs/built-on-envoy"
BINARY_NAME="boe"
INSTALL_DIR="${BOE_INSTALL_DIR:-/usr/local/bin}"
VERSION=""

# Colors for output
BOLD='\033[1m'
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color


info() {
    printf "${GREEN}[INFO]${NC} %s\n" "$1"
}

warn() {
    printf "${YELLOW}[WARN]${NC} %s\n" "$1"
}

error() {
    printf "${RED}[ERROR]${NC} %s\n" "$1" >&2
    exit 1
}

bold() {
    printf "${BOLD}%s${NC}\n" "$1"
}

# Ensure version has 'v' prefix
normalize_version() {
    _ver="$1"
    case "$_ver" in
        v*) echo "$_ver" ;;
        *)  echo "v${_ver}" ;;
    esac
}

# Detect the operating system
detect_os() {
    _os="$(uname -s | tr '[:upper:]' '[:lower:]')"
    case "$_os" in
        linux*)  echo "linux" ;;
        darwin*) echo "darwin" ;;
        *)       error "Unsupported operating system: $_os" ;;
    esac
}

# Detect the architecture
detect_arch() {
    _arch="$(uname -m)"
    case "$_arch" in
        x86_64|amd64)  echo "amd64" ;;
        aarch64|arm64) echo "arm64" ;;
        *)             error "Unsupported architecture: $_arch" ;;
    esac
}

# Get the latest release version from GitHub
get_latest_version() {
    _version=$(curl -sL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')
    if [ -z "$_version" ]; then
        error "Failed to fetch the latest release version. Please check your internet connection or try again later."
    fi
    echo "$_version"
}

# Download and install the binary
install_binary() {
    _os="$1"
    _arch="$2"
    _version="$3"
    _binary_name="${BINARY_NAME}-${_os}-${_arch}"

    _tmp_dir=$(mktemp -d)
    trap 'rm -rf "$_tmp_dir"' EXIT

    info "Downloading ${BINARY_NAME} ${_version} for ${_os}/${_arch}..."
    _download_url="https://github.com/tetratelabs/built-on-envoy/releases/download/${_version}/${_binary_name}"
    
    if ! curl --fail -sL -H "Accept: application/octet-stream" -o "${_tmp_dir}/${BINARY_NAME}" "$_download_url"; then
        error "Failed to download ${BINARY_NAME} from ${_download_url}"
    fi

    chmod +x "${_tmp_dir}/${BINARY_NAME}"

    info "Installing ${BINARY_NAME} to ${INSTALL_DIR}..."

    # Check if we need sudo
    if [ -w "$INSTALL_DIR" ]; then
        mv "${_tmp_dir}/${BINARY_NAME}" "${INSTALL_DIR}/${BINARY_NAME}"
    else
        warn "Elevated permissions required to install to ${INSTALL_DIR}"
        sudo mv "${_tmp_dir}/${BINARY_NAME}" "${INSTALL_DIR}/${BINARY_NAME}"
    fi

    info "Successfully installed ${BINARY_NAME} ${_version} to ${INSTALL_DIR}/${BINARY_NAME}"
}

# Verify installation
verify_installation() {
    if command -v "$BINARY_NAME" > /dev/null 2>&1; then
        info "Verification successful! Run '${BINARY_NAME} --help' to get started."
    else
        warn "${BINARY_NAME} was installed but may not be in your PATH."
        warn "Add ${INSTALL_DIR} to your PATH or run: ${INSTALL_DIR}/${BINARY_NAME}"
        info "Run '${INSTALL_DIR}/${BINARY_NAME} --help' to get started."
    fi
}

main() {
    # Parse arguments
    while [ $# -gt 0 ]; do
        case "$1" in
            --debug)
                set -x
                shift
                ;;
            --install-dir)
                if [ -n "$2" ] && [ "${2#-}" = "$2" ]; then
                    INSTALL_DIR="$2"
                    shift 2
                else
                    error "Option --install-dir requires a directory path"
                fi
                ;;
            --version)
                if [ -n "$2" ] && [ "${2#-}" = "$2" ]; then
                    VERSION=$(normalize_version "$2")
                    shift 2
                else
                    error "Option --version requires a version number"
                fi
                ;;
            *)
                shift
                ;;
        esac
    done

    printf "\n"
    bold "Installing Built On Envoy..."
    printf "\n"

    _os=$(detect_os)
    _arch=$(detect_arch)

    info "Detected platform: ${_os}/${_arch}"

    if [ -n "$VERSION" ]; then
        _version="$VERSION"
        info "Using version: ${_version}"
    else
        _version=$(get_latest_version)
        info "Latest version: ${_version}"
    fi

    install_binary "$_os" "$_arch" "$_version"
    verify_installation

    printf "\n"
    bold "Installation complete!"
    printf "\n"
}

main "$@"
