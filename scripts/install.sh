#!/bin/bash
set -euo pipefail

# ============================================================================
# Configuration
# ============================================================================
DAYZ_HOME="${DAYZ_HOME:-/srv/dayz}"
STEAM_USER="${STEAM_USER:-kqkklan}"
REINSTALL="${REINSTALL:-0}"
CONFIG_PATH="/etc/dayzctl/config.yaml"

prompt_for_values() {
    if [ ! -t 0 ]; then
        return 0
    fi

    log "Interactive setup — press Enter to accept the default in brackets"
    local input=""
    read -r -p "Installation directory (DAYZ_HOME) [${DAYZ_HOME}]: " input
    if [ -n "${input}" ]; then
        DAYZ_HOME="$input"
    fi

    input=""
    read -r -p "Steam username (STEAM_USER) [${STEAM_USER}]: " input
    if [ -n "${input}" ]; then
        STEAM_USER="$input"
    fi
}

# ============================================================================
# Colors for output
# ============================================================================
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m'

log() { echo -e "${GREEN}[install]${NC} $*" || true; }
warn() { echo -e "${YELLOW}[install] WARNING:${NC} $*" >&2 || true; }
error() { echo -e "${RED}[install] ERROR:${NC} $*" >&2; exit 1; }

# ============================================================================
# Check root privileges
# ============================================================================
if [ "$EUID" -ne 0 ]; then
    error "Please run as root"
fi

# ============================================================================
# Detect distribution
# ============================================================================
detect_distro() {
    if [ ! -f /etc/os-release ]; then
        error "Cannot detect distribution: /etc/os-release not found"
    fi

    . /etc/os-release

    DISTRO_ID="${ID:-unknown}"

    if [ -n "${ID_LIKE:-}" ]; then
        DISTRO_FAMILY="$ID_LIKE"
    else
        case "$ID" in
            ubuntu|debian) DISTRO_FAMILY="apt" ;;
            centos|rhel|fedora) DISTRO_FAMILY="yum" ;;
            alpine) DISTRO_FAMILY="apk" ;;
            *) DISTRO_FAMILY="unknown" ;;
        esac
    fi

    if [ -z "$DISTRO_FAMILY" ] || [ "$DISTRO_FAMILY" = "unknown" ]; then
        case "$ID" in
            ubuntu|debian) DISTRO_FAMILY="apt" ;;
            centos|rhel|fedora) DISTRO_FAMILY="yum" ;;
            alpine) DISTRO_FAMILY="apk" ;;
            *) error "Unsupported distribution: $ID (family: $DISTRO_FAMILY)" ;;
        esac
    fi

    log "detected distro: $ID (family $DISTRO_FAMILY)"
}

# ============================================================================
# Create directory structure
# ============================================================================
create_structure() {
    log "creating structure in $DAYZ_HOME"
    mkdir -p "$DAYZ_HOME" \
             "$DAYZ_HOME/server" \
             "$DAYZ_HOME/backups" \
             "$DAYZ_HOME/workshop" \
             "$DAYZ_HOME/state" \
             "$DAYZ_HOME/steamcmd" || error "Failed to create directory structure"
}

# ============================================================================
# Create dayz user
# ============================================================================
create_user() {
    if id "dayz" &>/dev/null; then
        log "user dayz already exists"
        return 0
    fi

    log "creating user dayz"
    useradd -m -d "$DAYZ_HOME" -s /bin/bash dayz || error "Failed to create dayz user"
    chown -R dayz:dayz "$DAYZ_HOME" || error "Failed to set ownership for dayz user"
}

# ============================================================================
# Install system dependencies
# ============================================================================
install_deps() {
    log "enabling i386 architecture and installing dependencies ($DISTRO_FAMILY)"

    case "$DISTRO_FAMILY" in
        apt|debian)
            dpkg --add-architecture i386 || error "Failed to add i386 architecture"
            apt-get update -qq || error "Failed to update package lists"
            apt-get install -y -qq \
                curl \
                tar \
                gzip \
                rsync \
                ca-certificates \
                lib32gcc-s1 \
                util-linux \
                jq || error "Failed to install dependencies"
            ;;
        yum|fedora|rhel|centos)
            yum install -y -q \
                curl \
                tar \
                gzip \
                rsync \
                ca-certificates \
                glibc.i686 \
                util-linux \
                jq || error "Failed to install dependencies"
            ;;
        apk|alpine)
            apk add --no-cache \
                curl \
                tar \
                gzip \
                rsync \
                ca-certificates \
                libgcc \
                util-linux \
                jq || error "Failed to install dependencies"
            ;;
        *)
            error "Unsupported package manager: $DISTRO_FAMILY"
            ;;
    esac
}

# ============================================================================
# Install SteamCMD
# ============================================================================
install_steamcmd() {
    log "installing SteamCMD in $DAYZ_HOME/steamcmd"

    if [ -f "$DAYZ_HOME/steamcmd/steamcmd.sh" ] && [ "$REINSTALL" != "1" ]; then
        log "SteamCMD already installed"
        return 0
    fi

    mkdir -p "$DAYZ_HOME/steamcmd" || error "Failed to create steamcmd directory"
    chown dayz:dayz "$DAYZ_HOME/steamcmd" || error "Failed to set ownership on steamcmd directory"

    cd "$DAYZ_HOME/steamcmd" || error "Failed to cd to steamcmd directory"

    runuser -u dayz -- sh -c "curl -sSL https://steamcdn-a.akamaihd.net/client/installer/steamcmd_linux.tar.gz | tar -xz" \
        || error "Failed to download/extract SteamCMD"

    runuser -u dayz -- "$DAYZ_HOME/steamcmd/steamcmd.sh" +quit \
        || error "SteamCMD installation test failed"
}

# ============================================================================
# Install dayzctl binary
# ============================================================================
install_dayzctl() {
    log "installing dayzctl binary (latest release)"

    ARCH=$(uname -m | sed 's/x86_64/amd64/' | sed 's/aarch64/arm64/')
    OS=$(uname -s | tr '[:upper:]' '[:lower:]')

    log "Detected architecture: $ARCH, OS: $OS"

    rm -f /usr/local/bin/dayzctl

    LOCAL_BINARY="./build/dayzctl-${OS}-${ARCH}"
    if [ -f "$LOCAL_BINARY" ]; then
        log "found local binary: $LOCAL_BINARY"
        cp "$LOCAL_BINARY" /usr/local/bin/dayzctl || error "Failed to copy local binary"
        chmod 755 /usr/local/bin/dayzctl || error "Failed to make dayzctl executable"
        log "dayzctl installed successfully from local build"
        return 0
    elif [ -f "./build/dayzctl" ]; then
        log "found local binary: ./build/dayzctl"
        cp ./build/dayzctl /usr/local/bin/dayzctl || error "Failed to copy local binary"
        chmod 755 /usr/local/bin/dayzctl || error "Failed to make dayzctl executable"
        log "dayzctl installed successfully from local build"
        return 0
    fi

    log "Fetching latest release from GitHub API..."
    API_URL="https://api.github.com/repos/dayzctl/dayzctl/releases/latest"

    TMP_RESPONSE=$(mktemp)
    TMP_ERROR=$(mktemp)

    # Run curl and fail immediately on any curl error (no fallback).
    HTTP_CODE=$(curl -sSL -H "Accept: application/vnd.github.v3+json" -w "%{http_code}" -o "$TMP_RESPONSE" "$API_URL" 2>"$TMP_ERROR")
    CURL_EXIT=$?

    if [ "$CURL_EXIT" -ne 0 ]; then
        ERROR_MSG=$(cat "$TMP_ERROR" 2>/dev/null || echo "curl failed with exit $CURL_EXIT")
        rm -f "$TMP_RESPONSE" "$TMP_ERROR"
        error "curl failed: $ERROR_MSG"
    fi

    log "HTTP status code: $HTTP_CODE"

    if [ "$HTTP_CODE" != "200" ]; then
        ERROR_MSG=$(cat "$TMP_ERROR" 2>/dev/null || echo "HTTP $HTTP_CODE")
        RESPONSE=$(cat "$TMP_RESPONSE" 2>/dev/null | head -20)
        rm -f "$TMP_RESPONSE" "$TMP_ERROR"
        log "HTTP error details: $ERROR_MSG"
        log "Response preview: $RESPONSE"
        error "GitHub API returned HTTP $HTTP_CODE. Check network connectivity and repository access."
    fi
    
    RELEASE_JSON=$(cat "$TMP_RESPONSE")
    rm -f "$TMP_RESPONSE" "$TMP_ERROR"
    
    if [ -z "$RELEASE_JSON" ]; then
        error "Empty response from GitHub API"
    fi
    
    log "API response received successfully"

    # jq should have been installed by install_deps(); fail fast if missing.
    if ! command -v jq >/dev/null 2>&1; then
        error "jq is required but not installed. Please run the installer with network access so packages can be installed, or install jq manually."
    fi

    VERSION=$(echo "$RELEASE_JSON" | jq -r '.tag_name' | sed 's/^v//')

    if [ -z "$VERSION" ] || [ "$VERSION" = "null" ]; then
        log "Full GitHub API response: $RELEASE_JSON"
        error "Failed to extract version from GitHub API response (jq returned empty)"
    fi

    log "Extracted version: $VERSION"

    ASSET="dayzctl_${VERSION}_${OS}_${ARCH}.tar.gz"
    DL_URL="https://github.com/dayzctl/dayzctl/releases/download/v${VERSION}/${ASSET}"
    CHECKSUM_URL="https://github.com/dayzctl/dayzctl/releases/download/v${VERSION}/checksums.txt"

    log "Installing dayzctl v${VERSION}"
    log "Asset: $ASSET"
    log "Download URL: $DL_URL"

    log "Downloading checksums..."
    CHECKSUMS=$(curl -fsSL "$CHECKSUM_URL" 2>/dev/null) || {
        error "Failed to download checksums from $CHECKSUM_URL"
    }

    if [ -z "$CHECKSUMS" ]; then
        error "Empty checksums file from $CHECKSUM_URL"
    fi

    EXPECTED_CHECKSUM=$(echo "$CHECKSUMS" | grep " ${ASSET}$" | awk '{print $1}')

    if [ -z "$EXPECTED_CHECKSUM" ]; then
        error "Checksum not found for ${ASSET} in checksums file"
    fi

    log "Expected checksum: $EXPECTED_CHECKSUM"

    TMP_DIR=$(mktemp -d)
    TMP_FILE="${TMP_DIR}/${ASSET}"

    log "Downloading binary from $DL_URL..."
    HTTP_CODE=$(curl -fsSL -w "%{http_code}" -o "$TMP_FILE" "$DL_URL" 2>/dev/null || echo "000")
    
    if [ "$HTTP_CODE" != "200" ]; then
        rm -rf "$TMP_DIR"
        error "Failed to download ${ASSET} (HTTP $HTTP_CODE) from $DL_URL"
    fi

    if [ ! -f "$TMP_FILE" ]; then
        rm -rf "$TMP_DIR"
        error "Downloaded file not found: $TMP_FILE"
    fi

    ACTUAL_CHECKSUM=$(sha256sum "$TMP_FILE" | awk '{print $1}')
    log "Actual checksum: $ACTUAL_CHECKSUM"

    if [ "$ACTUAL_CHECKSUM" != "$EXPECTED_CHECKSUM" ]; then
        rm -rf "$TMP_DIR"
        error "Checksum verification failed. Expected: $EXPECTED_CHECKSUM, Got: $ACTUAL_CHECKSUM"
    fi

    log "Checksum verified successfully"

    log "Extracting archive..."
    tar -xzf "$TMP_FILE" -C "$TMP_DIR" 2>/dev/null || {
        rm -rf "$TMP_DIR"
        error "Failed to extract archive"
    }

    if [ ! -f "${TMP_DIR}/dayzctl" ]; then
        rm -rf "$TMP_DIR"
        error "Binary not found in archive"
    fi

    mv "${TMP_DIR}/dayzctl" /usr/local/bin/dayzctl
    chmod 755 /usr/local/bin/dayzctl

    rm -rf "$TMP_DIR"

    log "Verifying installation..."
    if ! /usr/local/bin/dayzctl version > /dev/null 2>&1; then
        error "dayzctl verification failed"
    fi

    log "dayzctl v${VERSION} installed successfully: $(/usr/local/bin/dayzctl version)"
}

# ============================================================================
# Create default config.yaml
# ============================================================================
create_config() {
    if [ -f "$CONFIG_PATH" ] && [ "$REINSTALL" != "1" ]; then
        log "config already exists in $CONFIG_PATH (preserved)"
        return 0
    fi

    log "creating default config at $CONFIG_PATH"

    # Create /etc/dayzctl directory
    mkdir -p /etc/dayzctl || error "Failed to create /etc/dayzctl"

    TEMPLATE_URL="https://raw.githubusercontent.com/dayzctl/dayzctl/main/configs/config.yaml.tmpl"
    TMP_TEMPLATE=$(mktemp)

    if [ -z "$TMP_TEMPLATE" ]; then
        error "Failed to create temporary file"
    fi

    curl -fsSL "$TEMPLATE_URL" -o "$TMP_TEMPLATE" 2>/dev/null || {
        rm -f "$TMP_TEMPLATE"
        error "Failed to download template from $TEMPLATE_URL"
    }

    log "Template downloaded successfully"

    sed "s|%%DAYZ_HOME%%|$DAYZ_HOME|g; s|%%STEAM_USER%%|$STEAM_USER|g" "$TMP_TEMPLATE" > "$CONFIG_PATH" || {
        rm -f "$TMP_TEMPLATE"
        error "Failed to render config template"
    }

    rm -f "$TMP_TEMPLATE"

    chmod 644 "$CONFIG_PATH" || error "Failed to set permissions on config"
    log "config created at $CONFIG_PATH"
}

# ============================================================================
# Set ownership
# ============================================================================
set_ownership() {
    log "adjusting ownership of $DAYZ_HOME to dayz:dayz"
    chown -R dayz:dayz "$DAYZ_HOME" 2>/dev/null || warn "Failed to set ownership on some files (continuing)"
}

# ============================================================================
# Main installation
# ============================================================================
main() {
    log "=== DayZ Server Installation ==="
    log "dayzctl: root tool for server management"
    log "dayz user: runs steamcmd and server processes"
    log ""

    prompt_for_values

    detect_distro
    create_structure
    create_user
    install_deps
    install_steamcmd
    install_dayzctl
    create_config
    set_ownership

    log "Applying configuration..."
    # Capture and print output on failure so callers see the underlying error
    APPLY_OUT=$(/usr/local/bin/dayzctl apply 2>&1) || {
        echo "$APPLY_OUT" >&2
        error "dayzctl apply failed - see output above"
    }
    log "Configuration applied successfully"
    log "Configuration applied successfully"

    log "Downloading/updating DayZ server (this may take a while)..."
    UPDATE_OUT=$(/usr/local/bin/dayzctl update 2>&1) || {
        echo "$UPDATE_OUT" >&2
        error "dayzctl update failed - see output above"
    }
    log "DayZ server downloaded/updated successfully"
    log "DayZ server downloaded/updated successfully"

    log ""
    log "=== Installation Complete ==="
    log ""
    log "📁 Configuration: $CONFIG_PATH"
    log "📁 Server files: $DAYZ_HOME/server"
    log ""
    log "🔧 dayzctl commands (run as root):"
    log "  dayzctl apply              # Generate systemd units"
    log "  dayzctl status             # Check server status"
    log "  dayzctl update             # Update server"
    log "  dayzctl start main         # Start main instance"
    log "  dayzctl stop main          # Stop main instance"
    log "  dayzctl mods list          # List mods"
    log "  dayzctl rcon main send status  # Send RCON command (usage: dayzctl rcon <instance> <command>)"
    log ""
    log "🔑 Steam login (run as dayz user):"
    log "  sudo -u dayz /usr/local/bin/dayzctl steam-login"
    log ""
    log "📋 View logs:"
    log "  journalctl -u dayz@main -f"
    log "  journalctl -u dayz-update -f"
    log ""
    log "done."
}

main "$@"
