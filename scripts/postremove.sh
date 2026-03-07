#!/bin/sh
# $1 == 0: This is a permanent removal (not an upgrade)
# $1 == 1: This is an upgrade (don't stop the service, or handled by post-install)

if [ "$1" -eq 0 ]; then
    # Stop and disable the service
    systemctl stop eventsproc >/dev/null 2>&1 || :
    systemctl disable eventsproc >/dev/null 2>&1 || :

    # Reload systemd to clear the "missing" unit file from memory
    systemctl daemon-reload >/dev/null 2>&1 || :
fi
