#!/usr/bin/env bash
#
# Build Agent Installer
# Downloads and installs the build-agent binary for distributed build pool workers
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/hochfrequenz/claude-plan-orchestrator/main/scripts/install-build-agent.sh | bash
#
# Or with a specific version:
#   curl -fsSL https://raw.githubusercontent.com/hochfrequenz/claude-plan-orchestrator/main/scripts/install-build-agent.sh | bash -s -- v1.0.0
#

set -euo pipefail

REPO="hochfrequenz/claude-plan-orchestrator"
BINARY_NAME="build-agent"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

info() { echo -e "${BLUE}==>${NC} $1"; }
success() { echo -e "${GREEN}==>${NC} $1"; }
warn() { echo -e "${YELLOW}==>${NC} $1"; }
error() { echo -e "${RED}Error:${NC} $1" >&2; exit 1; }

# Detect OS and architecture
detect_platform() {
    local os arch

    case "$(uname -s)" in
        Linux*)  os="linux" ;;
        Darwin*) os="darwin" ;;
        *) error "Unsupported operating system: $(uname -s). Build agents run on Linux or macOS." ;;
    esac

    case "$(uname -m)" in
        x86_64|amd64) arch="amd64" ;;
        arm64|aarch64) arch="arm64" ;;
        *) error "Unsupported architecture: $(uname -m)" ;;
    esac

    echo "${os}_${arch}"
}

# Get latest version from GitHub
get_latest_version() {
    curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" |
        grep '"tag_name":' |
        sed -E 's/.*"([^"]+)".*/\1/'
}

# Download and install
install() {
    local version="${1:-}"
    local platform
    platform=$(detect_platform)

    info "Detected platform: $platform"

    # Get version
    if [[ -z "$version" ]]; then
        info "Fetching latest version..."
        version=$(get_latest_version)
        if [[ -z "$version" ]]; then
            error "Could not determine latest version. Please specify a version."
        fi
    fi

    info "Installing ${BINARY_NAME} ${version}..."

    # Construct download URL
    local filename="${BINARY_NAME}_${version#v}_${platform}"
    local url="https://github.com/${REPO}/releases/download/${version}/${filename}.tar.gz"

    # Create temp directory
    local tmp_dir
    tmp_dir=$(mktemp -d)
    trap "rm -rf $tmp_dir" EXIT

    # Download
    info "Downloading from ${url}..."
    if ! curl -fsSL "$url" -o "${tmp_dir}/archive.tar.gz"; then
        error "Failed to download. Check if version ${version} exists."
    fi

    # Extract
    info "Extracting..."
    tar -xzf "${tmp_dir}/archive.tar.gz" -C "$tmp_dir"

    # Install
    if [[ ! -w "$INSTALL_DIR" ]]; then
        warn "Need sudo to install to ${INSTALL_DIR}"
        sudo mkdir -p "$INSTALL_DIR"
    else
        mkdir -p "$INSTALL_DIR"
    fi

    local binary="${tmp_dir}/${BINARY_NAME}"
    if [[ ! -f "$binary" ]]; then
        # Try finding binary in extracted directory
        binary=$(find "$tmp_dir" -name "${BINARY_NAME}*" -type f -executable | head -1)
    fi

    if [[ ! -w "$INSTALL_DIR" ]]; then
        sudo mv "$binary" "${INSTALL_DIR}/${BINARY_NAME}"
        sudo chmod +x "${INSTALL_DIR}/${BINARY_NAME}"
    else
        mv "$binary" "${INSTALL_DIR}/${BINARY_NAME}"
        chmod +x "${INSTALL_DIR}/${BINARY_NAME}"
    fi

    success "Installed ${BINARY_NAME} to ${INSTALL_DIR}/${BINARY_NAME}"

    # Check if in PATH
    if ! command -v "$BINARY_NAME" &> /dev/null; then
        warn "${INSTALL_DIR} may not be in your PATH"
    else
        success "${BINARY_NAME} is ready to use"
    fi

    echo ""
    info "Next steps:"
    echo "  1. Create config file at /etc/build-agent/config.toml:"
    echo ""
    echo "     [server]"
    echo "     url = \"ws://coordinator-host:8081/ws\""
    echo ""
    echo "     [worker]"
    echo "     id = \"$(hostname)\""
    echo "     max_jobs = 4"
    echo ""
    echo "     [storage]"
    echo "     git_cache_dir = \"/var/cache/build-agent/repos\""
    echo "     worktree_dir = \"/tmp/build-agent/jobs\""
    echo ""
    echo "  2. Run directly:"
    echo "     ${BINARY_NAME} --server ws://coordinator:8081/ws --id worker-1 --jobs 4"
    echo ""
    echo "  3. Or set up as systemd service (see README for template)"
}

# Main
main() {
    echo ""
    echo "  Build Agent Installer"
    echo "  ====================="
    echo ""

    install "$@"
}

main "$@"
