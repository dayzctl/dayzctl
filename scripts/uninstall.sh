#!/usr/bin/env bash
#
# dayzops uninstaller. Idempotent (running again does not cause errors).
# Requires root.
#
# Usage:
#   sudo ./scripts/uninstall.sh           Removes service, units, and package.
#                                          PRESERVES ${DAYZ_HOME} (config,
#                                          server, mods, backups, state).
#   sudo ./scripts/uninstall.sh --purge   Everything above + DELETES ${DAYZ_HOME}
#                                          and the system user (total removal).
#   --yes / -y                            Does not ask for confirmation on --purge
#                                          (non-interactive / scripts).
#
# Overridable variables (use the same as during installation):
#   DAYZ_HOME  (default /srv/dayz or set via environment)
#   DAYZ_USER  (default dayz)
#
set -euo pipefail
DAYZ_HOME="${DAYZ_HOME:-/srv/dayz}"
DAYZ_USER="${DAYZ_USER:-dayz}"
VENV="${DAYZ_HOME}/.venv"
BIN_LINK="/usr/local/bin/dayzops"
PURGE=0
ASSUME_YES=0
log() { printf '[uninstall] %s\n' "$*"; }
usage() {
    sed -n '3,18p' "$0" | sed 's/^# \{0,1\}//'
}
for arg in "$@"; do
    case "${arg}" in
        --purge)   PURGE=1 ;;
        --yes|-y)  ASSUME_YES=1 ;;
        -h|--help) usage; exit 0 ;;
        *) echo "unknown argument: ${arg}" >&2; usage; exit 2 ;;
    esac
done
require_root() {
    if [[ "${EUID}" -ne 0 ]]; then
        echo "This uninstaller needs root. Run: sudo ./scripts/uninstall.sh" >&2
        exit 1
    fi
}
stop_services() {
    log "stopping and disabling services/timers"
    systemctl disable --now dayz dayz-update.timer dayz-prune.timer >/dev/null 2>&1 || true
}
remove_units() {
    log "removing systemd units"
    rm -f /etc/systemd/system/dayz.service \
          /etc/systemd/system/dayz-update.service \
          /etc/systemd/system/dayz-update.timer \
          /etc/systemd/system/dayz-prune.service \
          /etc/systemd/system/dayz-prune.timer
    systemctl daemon-reload >/dev/null 2>&1 || true
    systemctl reset-failed >/dev/null 2>&1 || true
}
remove_package() {
    # The current installer places dayzops in a virtualenv (${VENV}) and exposes
    # the command via a symlink at ${BIN_LINK}. We remove both here. We keep a
    # fallback of 'pip uninstall' for older installations (system-wide).
    log "removing dayzops command"
    if [[ -L "${BIN_LINK}" || -e "${BIN_LINK}" ]]; then
        log "removing symlink ${BIN_LINK}"
        rm -f "${BIN_LINK}"
    fi
    # The venv is only deleted here in normal mode; on --purge it disappears
    # along with ${DAYZ_HOME}. Explicitly removing it avoids leaving the venv
    # orphaned when data is preserved.
    if [[ "${PURGE}" -ne 1 && -d "${VENV}" ]]; then
        log "removing virtualenv ${VENV}"
        rm -rf "${VENV}"
    fi
    # Fallback: older installation via global/system-wide pip.
    if command -v pip >/dev/null 2>&1 || command -v pip3 >/dev/null 2>&1; then
        local pip_bin
        pip_bin="$(command -v pip || command -v pip3)"
        if "${pip_bin}" uninstall -y dayzops >/dev/null 2>&1 \
           || "${pip_bin}" uninstall -y --break-system-packages dayzops >/dev/null 2>&1; then
            log "dayzops package (pip system-wide) removed"
        fi
    fi
}
purge_data() {
    if [[ "${ASSUME_YES}" -ne 1 ]]; then
        echo
        echo "ATTENTION: --purge will PERMANENTLY DELETE:"
        echo "  - ${DAYZ_HOME} (config, server, mods, backups, state, SteamCMD cache)"
        echo "  - the system user '${DAYZ_USER}'"
        echo
        read -r -p "Are you sure? type 'yes' to confirm: " answer
        if [[ "${answer}" != "yes" ]]; then
            log "purge cancelled; data preserved in ${DAYZ_HOME}"
            return
        fi
    fi
    if [[ -d "${DAYZ_HOME}" ]]; then
        log "deleting ${DAYZ_HOME}"
        rm -rf "${DAYZ_HOME}"
    fi
    if id "${DAYZ_USER}" &>/dev/null; then
        log "removing user ${DAYZ_USER}"
        userdel "${DAYZ_USER}" >/dev/null 2>&1 \
            || log "WARNING: could not remove ${DAYZ_USER} (active processes?)"
    fi
}
main() {
    require_root
    stop_services
    remove_units
    remove_package
    if [[ "${PURGE}" -eq 1 ]]; then
        purge_data
    else
        log "data in ${DAYZ_HOME} preserved (use --purge to remove everything)"
    fi
    log "done."
}
main "$@"
