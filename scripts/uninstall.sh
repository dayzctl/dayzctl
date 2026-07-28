#!/bin/bash
set -euo pipefail

DAYZ_HOME="${DAYZ_HOME:-/srv/dayz}"
CONFIG_PATH="/etc/dayzctl/config.yaml"

GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m'

log() { echo -e "${GREEN}[uninstall]${NC} $*" || true; }
warn() { echo -e "${YELLOW}[uninstall] WARNING:${NC} $*" >&2 || true; }
error() { echo -e "${RED}[uninstall] ERROR:${NC} $*" >&2; exit 1; }

if [ "$EUID" -ne 0 ]; then
    error "Please run as root"
fi

# Ask for confirmation for destructive operations
confirm() {
    local prompt="$1"
    if [ ! -t 0 ]; then
        # non-interactive: require explicit YES environment
        if [ "${YES:-}" = "1" ] || [ "${YES:-}" = "true" ]; then
            return 0
        fi
        echo "$prompt"
        echo "Run with YES=1 to skip interactive confirmation. Aborting." >&2
        exit 1
    fi

    read -r -p "$prompt [y/N]: " ans
    case "$ans" in
        [Yy]|[Yy][Ee][Ss]) return 0 ;;
        *) return 1 ;;
    esac
}

# Stop and disable systemd units created by dayzctl
remove_systemd_units() {
    if ! command -v systemctl >/dev/null 2>&1; then
        warn "systemctl not present; skipping systemd unit cleanup"
        return 0
    fi

    log "Stopping and disabling dayz@*.service units"
    # Stop any instance units
    INSTANCES=$(systemctl list-units --type=service --no-legend | awk '{print $1}' | grep '^dayz@' || true)
    for u in $INSTANCES; do
        log "Stopping $u"
        systemctl stop "$u" || true
        log "Disabling $u"
        systemctl disable "$u" || true
    done

    # Stop/disable update/prune timers and services
    for s in dayz-update.service dayz-update.timer dayz-prune.service dayz-prune.timer; do
        if systemctl list-unit-files | grep -q "^$s"; then
            log "Stopping/disabling $s"
            systemctl stop "$s" || true
            systemctl disable "$s" || true
        fi
    done

    # Remove unit files in /etc/systemd/system if any were written there
    for f in /etc/systemd/system/dayz@*.service /etc/systemd/system/dayz-update.* /etc/systemd/system/dayz-prune.*; do
        if ls $f 2>/dev/null >/dev/null; then
            for ff in $f; do
                log "Removing $ff"
                rm -f "$ff" || true
            done
        fi
    done

    log "Reloading systemd daemon"
    systemctl daemon-reload || true
}

remove_binary_and_config() {
    if [ -f /usr/local/bin/dayzctl ]; then
        log "Removing /usr/local/bin/dayzctl"
        rm -f /usr/local/bin/dayzctl || warn "Failed to remove /usr/local/bin/dayzctl"
    else
        log "No /usr/local/bin/dayzctl present"
    fi

    if [ -f "$CONFIG_PATH" ]; then
        if confirm "Remove config at $CONFIG_PATH?"; then
            rm -f "$CONFIG_PATH" || warn "Failed to remove $CONFIG_PATH"
            rmdir --ignore-fail-on-non-empty /etc/dayzctl 2>/dev/null || true
            log "Config removed"
        else
            log "Preserving config at $CONFIG_PATH"
        fi
    else
        log "No config found at $CONFIG_PATH"
    fi
}

remove_data_and_user() {
    if confirm "Remove server files at $DAYZ_HOME (this will delete server files, workshop, backups)?"; then
        log "Removing $DAYZ_HOME"
        rm -rf "$DAYZ_HOME" || warn "Failed to remove some files under $DAYZ_HOME"
    else
        log "Preserving $DAYZ_HOME"
    fi

    if id "dayz" &>/dev/null; then
        if confirm "Remove 'dayz' user and its home?"; then
            log "Removing user 'dayz'"
            userdel -r dayz || warn "Failed to remove user 'dayz' cleanly"
        else
            log "Preserving user 'dayz'"
        fi
    else
        log "User 'dayz' not present"
    fi
}

main() {
    echo
    log "=== DayZ Server Uninstall ==="
    echo

    # Basic sanity: if dayzctl binary exists, attempt to stop running instances using it
    if command -v /usr/local/bin/dayzctl >/dev/null 2>&1; then
        log "Attempting to stop instances via dayzctl"
        # try to list instances and stop them
        if /usr/local/bin/dayzctl list >/dev/null 2>&1; then
            INSTS=$(/usr/local/bin/dayzctl list | awk 'NR>1 {print $1}' || true)
            for i in $INSTS; do
                log "Stopping instance $i"
                /usr/local/bin/dayzctl stop "$i" || true
            done
        fi
    fi

    if confirm "Proceed with uninstall?"; then
        remove_systemd_units
        remove_binary_and_config
        remove_data_and_user
        log "Uninstall complete"
    else
        log "Aborted by user"
        exit 0
    fi
}

main "$@"
