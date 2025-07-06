#!/usr/bin/env bash

set -euo pipefail

# fetch Filecoin proof parameters and pre-seal sectors ahead of time

export TRUST_PARAMS=1

SECTOR_SIZE="${SECTOR_SIZE:-512MiB}"
NUM_SECTORS=16
SECTOR_DIR="${SECTOR_DIR:-./presealed}"

mkdir -p "${SECTOR_DIR}"

# retrieve parameters for the desired sector size
./lotus fetch-params "${SECTOR_SIZE}"

# generate pre-sealed sectors
./lotus-seed --sector-dir="${SECTOR_DIR}" pre-seal --sector-size="${SECTOR_SIZE}" --num-sectors="${NUM_SECTORS}"

