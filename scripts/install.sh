#!/bin/bash
set -euo pipefail

# ============================================================================
# Configuration
# ============================================================================
DAYZ_HOME="${DAYZ_HOME:-/srv/dayz}"
STEAM_USER="${STEAM_USER:-kqkklan}"
REINSTALL="${REINSTALL:-0}"
CONFIG_PATH="/etc/dayzctl/config.yaml"
# Map imagery for the log analyzer (tiles + place names).
MAPS_DIR="${MAPS_DIR:-/usr/local/share/dayzctl/maps}"
# Set to 1 once map data is on disk; the config is only pointed at it then.
MAPS_READY=0

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
    # install_maps() looks for the matching maps archive.
    RELEASE_VERSION="$VERSION"

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
# Install map data for the log analyzer
# ============================================================================
# The web UI needs tiles and place names. A source checkout already has them
# under web/log-analyzer/maps; a release install pulls the maps archive that is
# published next to the binary. Without it the analyzer still works, it just
# draws a coordinate grid instead of the terrain.
install_maps() {
    log "installing map data for the log analyzer"

    mkdir -p "$(dirname "$MAPS_DIR")" || error "Failed to create $(dirname "$MAPS_DIR")"

    local local_maps="./web/log-analyzer/maps"
    if [ -d "$local_maps" ] && [ -n "$(find "$local_maps" -name meta.json -print -quit 2>/dev/null)" ]; then
        log "found local map data: $local_maps"
        rm -rf "$MAPS_DIR" || error "Failed to clear $MAPS_DIR"
        mkdir -p "$MAPS_DIR" || error "Failed to create $MAPS_DIR"
        cp -a "$local_maps"/. "$MAPS_DIR"/ || error "Failed to copy map data"
    else
        local version="${RELEASE_VERSION:-}"
        local url
        if [ -n "$version" ]; then
            url="https://github.com/dayzctl/dayzctl/releases/download/v${version}/dayzctl-maps.tar.gz"
        else
            url="https://github.com/dayzctl/dayzctl/releases/latest/download/dayzctl-maps.tar.gz"
        fi

        log "downloading map data: $url"
        local tmp
        tmp=$(mktemp) || error "Failed to create temporary file"

        if curl -fsSL "$url" -o "$tmp" 2>/dev/null; then
            unpack_maps "$tmp" || {
                rm -f "$tmp"
                error "Failed to install the downloaded map data"
            }
        else
            warn "map data is not published for this release"
            warn "the web UI will draw a grid; fetch maps later with 'make maps-all' and copy them to $MAPS_DIR"
        fi
        rm -f "$tmp"
    fi

    chmod -R a+rX "$MAPS_DIR" 2>/dev/null || true

    local count
    count=$(find "$MAPS_DIR" -name meta.json 2>/dev/null | wc -l | tr -d ' ')
    if [ "${count:-0}" -gt 0 ]; then
        MAPS_READY=1
        log "map data ready: ${count} map(s) in $MAPS_DIR"
    else
        warn "no maps installed (the analyzer will use its built-in copy or a coordinate grid)"
    fi
}

# ============================================================================
# Unpack a maps archive into $MAPS_DIR
# ============================================================================
# The archive may nest the maps (maps/<id>/meta.json, or
# web/log-analyzer/maps/<id>/meta.json depending on how it was produced), so the
# directory holding the map folders is located first instead of trusting a fixed
# prefix.
unpack_maps() {
    local archive="$1"
    local staging root

    staging=$(mktemp -d) || return 1
    if ! tar -xzf "$archive" -C "$staging" 2>/dev/null; then
        rm -rf "$staging"
        return 1
    fi

    root=$(find "$staging" -type f -name meta.json 2>/dev/null | head -n 1)
    if [ -n "$root" ]; then
        root=$(dirname "$(dirname "$root")")   # <maps>/<id>/meta.json
    fi

    if [ -z "$root" ] || [ ! -d "$root" ]; then
        rm -rf "$staging"
        warn "the map archive did not contain any maps"
        return 1
    fi

    rm -rf "$MAPS_DIR"
    mkdir -p "$MAPS_DIR" || { rm -rf "$staging"; return 1; }
    cp -a "$root"/. "$MAPS_DIR"/ || { rm -rf "$staging"; return 1; }

    rm -rf "$staging"
    return 0
}

# ============================================================================
# Point the config at the installed map data
# ============================================================================
configure_maps_path() {
    if [ ! -f "$CONFIG_PATH" ]; then
        return 0
    fi

    local tmp
    tmp=$(mktemp) || error "Failed to create temporary file"

    if grep -qE '^[[:space:]]*maps:' "$CONFIG_PATH"; then
        sed -E "s|^[[:space:]]*maps:.*|  maps: \"${MAPS_DIR}\"|" "$CONFIG_PATH" > "$tmp"
    else
        # Insert the setting straight after the top-level paths: key.
        awk -v dir="$MAPS_DIR" '
            { print }
            /^paths:[[:space:]]*$/ && !done { printf "  maps: \"%s\"\n", dir; done = 1 }
        ' "$CONFIG_PATH" > "$tmp"
    fi

    if [ -s "$tmp" ]; then
        cat "$tmp" > "$CONFIG_PATH" || error "Failed to update $CONFIG_PATH"
        chmod 644 "$CONFIG_PATH" || true
        log "configured paths.maps=$MAPS_DIR in $CONFIG_PATH"
    else
        warn "could not update $CONFIG_PATH; set paths.maps: $MAPS_DIR manually"
    fi
    rm -f "$tmp"
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
    install_maps
    if [ "${MAPS_READY:-0}" = "1" ]; then
        configure_maps_path
    fi
    set_ownership

    log "Applying configuration..."
    # Capture and print output on failure so callers see the underlying error
    APPLY_OUT=$(/usr/local/bin/dayzctl apply 2>&1) || {
        echo "$APPLY_OUT" >&2
        error "dayzctl apply failed - see output above"
    }
    log "Configuration applied successfully"

    log ""
    log "Steam login is required to download the DayZ server and mods."
    if [ -t 0 ]; then
        log "Enter your Steam password and, if Steam Guard is enabled, the Steam Guard code when prompted."
        /usr/local/bin/dayzctl steam-login || error "dayzctl steam-login failed - see output above"
    else
        warn "No interactive terminal attached; skipping Steam login."
        warn "Run 'dayzctl steam-login' as root before 'dayzctl update' if Steam reports 'Invalid Password'."
    fi

    log "Downloading/updating DayZ server (this may take a while)..."
    UPDATE_OUT=$(/usr/local/bin/dayzctl update 2>&1) || {
        echo "$UPDATE_OUT" >&2
        error "dayzctl update failed - see output above"
    }
    log "DayZ server downloaded/updated successfully"

    log ""
    log "=== Installation Complete ==="
    log ""
    log "📁 Configuration: $CONFIG_PATH"
    log "📁 Server files: $DAYZ_HOME/server"
    log "📁 Map data: $MAPS_DIR"
    log ""
    log "🔧 dayzctl commands (run as root):"
    log "  dayzctl list                        # List configured instances"
    log "  dayzctl status [instance|all]       # Show service status and timers"
    log "  dayzctl start <instance|all>        # Start instance(s)"
    log "  dayzctl stop <instance|all>         # Stop instance(s) (--timeout N, --force)"
    log "  dayzctl restart <instance|all>      # Restart instance(s) (--timeout N, --force)"
    log "  dayzctl logs <instance|all>         # Show journald logs (-f follow, -n N lines)"
    log "  dayzctl apply                       # Generate configs and systemd units"
    log "  dayzctl update                      # Update server and sync mods"
    log "  dayzctl mods <instance> list        # Mod management: list|add|add-server|remove|delete|sync"
    log "  dayzctl rcon <instance> players     # RCON: players|send|kick|ban|say|shutdown"
    log "  dayzctl render-config <instance>    # Render server config (dry-run)"
    log "  dayzctl validate-config             # Validate the configuration file"
    log "  dayzctl check-space                 # Check disk space for the install dir"
    log "  dayzctl version                     # Print version information"
    log ""
    log "🔑 Steam login (run as root; SteamCMD is executed as the dayz user internally):"
    log "  dayzctl steam-login"
    log ""
    log "📋 View logs:"
    log "  dayzctl logs                        # command help"
    log "  dayzctl logs main                   # last 100 entries for 'main'"
    log "  dayzctl logs main -f                # follow live output (Ctrl+C to stop)"
    log "  dayzctl logs -n all all             # entire journal for every instance"
    log "  dayzctl logs main --since \"1 hour ago\""
    log "  journalctl -u dayz@main -f          # raw systemd unit logs"
    log "  journalctl -u dayz-update -f        # update timer/service logs"
    log ""
    log "done."
}

main "$@"
