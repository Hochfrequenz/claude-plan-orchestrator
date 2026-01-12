#!/usr/bin/env bash
#
# check-deps.sh - Analyze and install missing dev dependencies for ERP Orchestrator
#
# Usage: ./scripts/check-deps.sh [--install] [--local]
#
# Without flags: reports missing dependencies
# --install: attempts to install missing dependencies (may require sudo)
# --local: install to user directories without sudo (Go, Node via nvm)

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

INSTALL_MODE=false
LOCAL_MODE=false
MISSING_DEPS=()

while [[ $# -gt 0 ]]; do
    case $1 in
        --install)
            INSTALL_MODE=true
            shift
            ;;
        --local)
            LOCAL_MODE=true
            INSTALL_MODE=true
            shift
            ;;
        *)
            echo "Unknown option: $1"
            echo "Usage: $0 [--install] [--local]"
            exit 1
            ;;
    esac
done

log_ok() {
    echo -e "${GREEN}✓${NC} $1"
}

log_missing() {
    echo -e "${RED}✗${NC} $1"
    MISSING_DEPS+=("$2")
}

log_warn() {
    echo -e "${YELLOW}!${NC} $1"
}

log_info() {
    echo -e "${BLUE}→${NC} $1"
}

check_command() {
    local cmd=$1
    local name=${2:-$1}
    local version_flag=${3:---version}

    if command -v "$cmd" &> /dev/null; then
        local version
        version=$($cmd $version_flag 2>&1 | head -n1) || version="(version unknown)"
        log_ok "$name: $version"
        return 0
    else
        log_missing "$name: not found" "$name"
        return 1
    fi
}

detect_package_manager() {
    if command -v apt-get &> /dev/null; then
        echo "apt"
    elif command -v dnf &> /dev/null; then
        echo "dnf"
    elif command -v pacman &> /dev/null; then
        echo "pacman"
    elif command -v brew &> /dev/null; then
        echo "brew"
    else
        echo "unknown"
    fi
}

# User-local Go installation (no sudo)
install_go_local() {
    local GO_VERSION="1.23.4"
    local ARCH
    ARCH=$(uname -m)
    case $ARCH in
        x86_64) ARCH="amd64" ;;
        aarch64|arm64) ARCH="arm64" ;;
        *) echo "Unsupported architecture: $ARCH"; return 1 ;;
    esac

    local GO_TAR="go${GO_VERSION}.linux-${ARCH}.tar.gz"
    local GO_URL="https://go.dev/dl/${GO_TAR}"
    local GO_DIR="$HOME/.local/go"

    log_info "Downloading Go ${GO_VERSION}..."
    mkdir -p "$HOME/.local"
    curl -fsSL "$GO_URL" -o "/tmp/${GO_TAR}"

    log_info "Extracting to ${GO_DIR}..."
    rm -rf "$GO_DIR"
    tar -C "$HOME/.local" -xzf "/tmp/${GO_TAR}"
    rm "/tmp/${GO_TAR}"

    # Add to PATH for current session
    export PATH="$GO_DIR/bin:$HOME/go/bin:$PATH"
    export GOPATH="$HOME/go"

    log_info "Go installed. Add to your shell profile:"
    echo ""
    echo "  export PATH=\"\$HOME/.local/go/bin:\$HOME/go/bin:\$PATH\""
    echo "  export GOPATH=\"\$HOME/go\""
    echo ""
}

install_go_system() {
    local pm=$1
    case $pm in
        apt)
            sudo apt-get update && sudo apt-get install -y golang-go
            ;;
        dnf)
            sudo dnf install -y golang
            ;;
        pacman)
            sudo pacman -S --noconfirm go
            ;;
        brew)
            brew install go
            ;;
        *)
            echo "Please install Go manually from https://go.dev/dl/"
            return 1
            ;;
    esac
}

install_sqlite_system() {
    local pm=$1
    case $pm in
        apt)
            sudo apt-get update && sudo apt-get install -y sqlite3 libsqlite3-dev
            ;;
        dnf)
            sudo dnf install -y sqlite sqlite-devel
            ;;
        pacman)
            sudo pacman -S --noconfirm sqlite
            ;;
        brew)
            brew install sqlite
            ;;
        *)
            echo "Please install SQLite manually"
            return 1
            ;;
    esac
}

# SQLite from source (user-local, no sudo)
install_sqlite_local() {
    local SQLITE_VERSION="3470200"
    local SQLITE_YEAR="2024"
    local SQLITE_URL="https://www.sqlite.org/${SQLITE_YEAR}/sqlite-autoconf-${SQLITE_VERSION}.tar.gz"
    local INSTALL_DIR="$HOME/.local"

    log_info "Downloading SQLite..."
    curl -fsSL "$SQLITE_URL" -o "/tmp/sqlite.tar.gz"

    log_info "Building SQLite (this may take a minute)..."
    cd /tmp
    tar -xzf sqlite.tar.gz
    cd "sqlite-autoconf-${SQLITE_VERSION}"
    ./configure --prefix="$INSTALL_DIR" --quiet
    make -j"$(nproc)" --quiet
    make install --quiet
    cd -
    rm -rf "/tmp/sqlite.tar.gz" "/tmp/sqlite-autoconf-${SQLITE_VERSION}"

    export PATH="$INSTALL_DIR/bin:$PATH"
    export LD_LIBRARY_PATH="$INSTALL_DIR/lib:${LD_LIBRARY_PATH:-}"

    log_info "SQLite installed. Add to your shell profile:"
    echo ""
    echo "  export PATH=\"\$HOME/.local/bin:\$PATH\""
    echo "  export LD_LIBRARY_PATH=\"\$HOME/.local/lib:\$LD_LIBRARY_PATH\""
    echo ""
}

install_gh_system() {
    local pm=$1
    case $pm in
        apt)
            type -p curl >/dev/null || sudo apt install curl -y
            curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg | sudo dd of=/usr/share/keyrings/githubcli-archive-keyring.gpg 2>/dev/null
            sudo chmod go+r /usr/share/keyrings/githubcli-archive-keyring.gpg
            echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" | sudo tee /etc/apt/sources.list.d/github-cli.list > /dev/null
            sudo apt-get update && sudo apt-get install -y gh
            ;;
        dnf)
            sudo dnf install -y gh
            ;;
        pacman)
            sudo pacman -S --noconfirm github-cli
            ;;
        brew)
            brew install gh
            ;;
        *)
            echo "Please install GitHub CLI manually from https://cli.github.com/"
            return 1
            ;;
    esac
}

install_gh_local() {
    local GH_VERSION="2.63.2"
    local ARCH
    ARCH=$(uname -m)
    case $ARCH in
        x86_64) ARCH="amd64" ;;
        aarch64|arm64) ARCH="arm64" ;;
        *) echo "Unsupported architecture: $ARCH"; return 1 ;;
    esac

    local GH_TAR="gh_${GH_VERSION}_linux_${ARCH}.tar.gz"
    local GH_URL="https://github.com/cli/cli/releases/download/v${GH_VERSION}/${GH_TAR}"

    log_info "Downloading GitHub CLI ${GH_VERSION}..."
    mkdir -p "$HOME/.local/bin"
    curl -fsSL "$GH_URL" -o "/tmp/${GH_TAR}"
    tar -xzf "/tmp/${GH_TAR}" -C /tmp
    cp "/tmp/gh_${GH_VERSION}_linux_${ARCH}/bin/gh" "$HOME/.local/bin/"
    rm -rf "/tmp/${GH_TAR}" "/tmp/gh_${GH_VERSION}_linux_${ARCH}"

    export PATH="$HOME/.local/bin:$PATH"
    log_info "GitHub CLI installed to ~/.local/bin"
}

install_golangci_lint() {
    if command -v go &> /dev/null; then
        log_info "Installing golangci-lint via go install..."
        go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
    else
        echo "Go must be installed first to install golangci-lint"
        return 1
    fi
}

install_go_tools() {
    if command -v go &> /dev/null; then
        log_info "Installing Go development tools..."
        go install golang.org/x/tools/cmd/goimports@latest
        go install github.com/air-verse/air@latest
    else
        echo "Go must be installed first"
        return 1
    fi
}

print_shell_config() {
    echo ""
    echo "========================================"
    echo -e "${BLUE}Add these lines to your ~/.bashrc or ~/.zshrc:${NC}"
    echo "========================================"
    echo ""
    echo "# Go"
    echo "export PATH=\"\$HOME/.local/go/bin:\$HOME/go/bin:\$PATH\""
    echo "export GOPATH=\"\$HOME/go\""
    echo ""
    echo "# Local binaries"
    echo "export PATH=\"\$HOME/.local/bin:\$PATH\""
    echo ""
    echo "Then run: source ~/.bashrc"
    echo "========================================"
}

echo "========================================"
echo "ERP Orchestrator - Dependency Checker"
echo "========================================"
echo ""

PM=$(detect_package_manager)
echo "Detected package manager: $PM"
if [[ "$LOCAL_MODE" == true ]]; then
    echo -e "${BLUE}Mode: user-local install (no sudo)${NC}"
fi
echo ""

echo "--- Core Dependencies ---"
check_command go "Go" "version" || true
check_command node "Node.js" "--version" || true
check_command npm "npm" "--version" || true
check_command sqlite3 "SQLite" "-version" || true
check_command git "Git" "--version" || true

echo ""
echo "--- Development Tools ---"
check_command golangci-lint "golangci-lint" "--version" || true
check_command gh "GitHub CLI" "--version" || true
check_command air "Air (hot reload)" "-v" || true

echo ""
echo "--- Optional Tools ---"
if command -v goimports &> /dev/null; then
    log_ok "goimports: installed"
else
    log_warn "goimports: not installed (optional)"
fi
check_command make "Make" "--version" || log_warn "Make: not installed (optional)"

echo ""
echo "========================================"

if [[ ${#MISSING_DEPS[@]} -eq 0 ]]; then
    echo -e "${GREEN}All required dependencies are installed!${NC}"
    exit 0
fi

echo -e "${YELLOW}Missing dependencies: ${MISSING_DEPS[*]}${NC}"
echo ""

if [[ "$INSTALL_MODE" == true ]]; then
    echo "Installing missing dependencies..."
    echo ""

    INSTALLED_LOCAL=false

    for dep in "${MISSING_DEPS[@]}"; do
        echo "----------------------------------------"
        echo "Installing $dep..."
        echo "----------------------------------------"
        case $dep in
            Go)
                if [[ "$LOCAL_MODE" == true ]]; then
                    install_go_local
                    INSTALLED_LOCAL=true
                else
                    install_go_system "$PM"
                fi
                ;;
            SQLite)
                if [[ "$LOCAL_MODE" == true ]]; then
                    install_sqlite_local
                    INSTALLED_LOCAL=true
                else
                    install_sqlite_system "$PM"
                fi
                ;;
            golangci-lint)
                install_golangci_lint || true
                ;;
            "GitHub CLI")
                if [[ "$LOCAL_MODE" == true ]]; then
                    install_gh_local
                    INSTALLED_LOCAL=true
                else
                    install_gh_system "$PM"
                fi
                ;;
            "Air (hot reload)")
                install_go_tools || true
                ;;
            *)
                log_warn "No automatic installer for $dep"
                ;;
        esac
        echo ""
    done

    if [[ "$INSTALLED_LOCAL" == true ]]; then
        print_shell_config
    fi

    echo ""
    echo "Installation complete. Re-running check..."
    echo ""
    exec "$0"
else
    echo "Run with --install to attempt automatic installation:"
    echo "  ./scripts/check-deps.sh --install        # System install (may need sudo)"
    echo "  ./scripts/check-deps.sh --local          # User-local install (no sudo)"
fi
