#!/usr/bin/env bash
#
# Build and upload to a destination defined in screeps.json. No Node/npm.
#
#   ./scripts/push.sh main       # or: sim, pserver, season, ...
#
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DEST="${1:-}"
if [ -z "$DEST" ]; then
  echo "usage: ./scripts/push.sh <destination>   (a key in screeps.json)" >&2
  exit 1
fi

bash "$ROOT/scripts/build.sh"

echo "==> Uploading to '$DEST'"
( cd "$ROOT/tools" && go run . upload "$ROOT" "$DEST" )
