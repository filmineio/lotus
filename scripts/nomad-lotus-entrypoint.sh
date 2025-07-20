#!/usr/bin/env bash
set -e

SNAPSHOT_URL="https://forest-archive.chainsafe.dev/latest/calibnet/"

DAEMON_ARGS=("$@")
GATE="$LOTUS_PATH/date_initialized"

if [[ ! -f "$GATE" ]]; then
    echo "Fetching snapshot from $SNAPSHOT_URL"
    curl -sL "$SNAPSHOT_URL" | /usr/local/bin/lotus daemon --import-snapshot - --halt-after-import
    date > "$GATE"
fi

exec /usr/local/bin/lotus daemon "${DAEMON_ARGS[@]}"
