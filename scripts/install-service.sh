#!/usr/bin/env bash
#
# Install reolink-onvif-shim as a macOS LaunchAgent so it starts at login and
# is automatically restarted if it exits. No root required (the shim uses high
# ports). Re-running this is safe — it reinstalls and restarts.
#
# Usage: make install-service   (or: scripts/install-service.sh)
set -euo pipefail

LABEL="com.statico-alt.reolink-onvif-shim"
REPO="$(cd "$(dirname "$0")/.." && pwd)"
BIN="$REPO/reolink-onvif-shim"
CONFIG="$REPO/config.json"
LOG="$REPO/reolink-onvif-shim.log"
TEMPLATE="$REPO/launchd/$LABEL.plist.template"
PLIST_DIR="$HOME/Library/LaunchAgents"
PLIST="$PLIST_DIR/$LABEL.plist"
UID_NUM="$(id -u)"

# --- preflight ---------------------------------------------------------------
if [ ! -f "$CONFIG" ]; then
	echo "error: $CONFIG not found." >&2
	echo "       Copy config.example.json to config.json and fill in your camera." >&2
	exit 1
fi
if [ ! -x "$BIN" ]; then
	echo "building $BIN ..."
	( cd "$REPO" && go build -o reolink-onvif-shim . )
fi

# Warn if the configured ports are already held (e.g. a manual run in tmux),
# since KeepAlive would just fail to bind in a loop.
PORT="$(sed -n 's/.*"[Ll]isten"[^:]*:[^"]*"\(:[0-9]*\)".*/\1/p' "$CONFIG" | head -1)"
PORT="${PORT#:}"
if [ -n "${PORT:-}" ] && lsof -nP -iTCP:"$PORT" -sTCP:LISTEN >/dev/null 2>&1; then
	echo "warning: something is already listening on port $PORT." >&2
	echo "         Stop any manual/tmux run of the shim first, or the agent" >&2
	echo "         will fail to bind. Continuing anyway." >&2
fi

# --- render the plist from the template --------------------------------------
mkdir -p "$PLIST_DIR"
sed \
	-e "s#__LABEL__#$LABEL#g" \
	-e "s#__BINARY__#$BIN#g" \
	-e "s#__CONFIG__#$CONFIG#g" \
	-e "s#__LOG__#$LOG#g" \
	-e "s#__WORKDIR__#$REPO#g" \
	"$TEMPLATE" > "$PLIST"
echo "wrote $PLIST"

# --- (re)load and start ------------------------------------------------------
# bootout first so a re-install cleanly replaces any running instance.
launchctl bootout "gui/$UID_NUM/$LABEL" 2>/dev/null || true
launchctl bootstrap "gui/$UID_NUM" "$PLIST"
launchctl kickstart -k "gui/$UID_NUM/$LABEL"

echo
echo "installed and started $LABEL"
echo "status:  launchctl print gui/$UID_NUM/$LABEL | grep -E 'state|pid'"
echo "logs:    tail -f $LOG"
sleep 1
launchctl print "gui/$UID_NUM/$LABEL" 2>/dev/null | grep -E '^\s*(state|pid) =' || true
