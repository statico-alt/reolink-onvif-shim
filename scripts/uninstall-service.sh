#!/usr/bin/env bash
#
# Stop and remove the reolink-onvif-shim LaunchAgent.
#
# Usage: make uninstall-service   (or: scripts/uninstall-service.sh)
set -euo pipefail

LABEL="com.statico-alt.reolink-onvif-shim"
PLIST="$HOME/Library/LaunchAgents/$LABEL.plist"
UID_NUM="$(id -u)"

launchctl bootout "gui/$UID_NUM/$LABEL" 2>/dev/null && echo "stopped $LABEL" || echo "$LABEL was not loaded"
if [ -f "$PLIST" ]; then
	rm -f "$PLIST"
	echo "removed $PLIST"
fi
echo "done. (the binary, config, and log are left in place.)"
