#!/usr/bin/env bash
set -e

DAEMON_ARGS=("$@")

if [[ -n "$FILECOIN_SNAPSHOT" ]]; then
    GATE="$LOTUS_PATH/date_initialized"
    if [[ ! -f "$GATE" ]]; then
        echo "Importing snapshot from $FILECOIN_SNAPSHOT"
        /usr/local/bin/lotus daemon --import-snapshot "$FILECOIN_SNAPSHOT" --halt-after-import
        date > "$GATE"
    fi
fi

exec /usr/local/bin/lotus daemon "${DAEMON_ARGS[@]}"
