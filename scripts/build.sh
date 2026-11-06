#!/usr/bin/env bash
#
# Compile the Go bot to WebAssembly with TinyGo and assemble the dist/ folder
# that gets uploaded to Screeps. No Node/npm — just TinyGo + Go:
#
#   dist/main.js       <- js/loader.js    (uploaded as module "main")
#   dist/host.js       <- js/host.js       (uploaded as module "host")
#   dist/host_gen.js   <- js/host_gen.js   (generated; module "host_gen")
#   dist/wasm_exec.js  <- TinyGo glue, patched (uploaded as module "wasm_exec")
#   dist/main.wasm     <- compiled Go bot  (uploaded as binary module "main.wasm")
#
# This only builds. Uploading is scripts/push.sh.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

if ! command -v tinygo >/dev/null 2>&1; then
  echo "ERROR: tinygo not found on PATH. Install: https://tinygo.org/getting-started/install/" >&2
  exit 1
fi

echo "==> TinyGo: $(tinygo version)"

mkdir -p dist

echo "==> Generating layout, JS tables and API coverage from tools/spec_*.go"
( cd "$ROOT/tools" && go run . gen "$ROOT" )

echo "==> Building Go -> WASM"
# -scheduler=none: the loop is synchronous (no goroutines/channels), which keeps
#   exported functions callable after main() returns and shrinks the binary.
# -no-debug: strip DWARF.  -opt=2: optimize for size/speed.
( cd "$ROOT/go" && tinygo build -o "$ROOT/dist/main.wasm" -target=wasm -scheduler=none -no-debug -opt=2 . )

echo "==> Copying + patching TinyGo runtime glue (wasm_exec.js)"
WASM_EXEC="$(tinygo env TINYGOROOT)/targets/wasm_exec.js"
if [ ! -f "$WASM_EXEC" ]; then
  echo "ERROR: could not find wasm_exec.js at $WASM_EXEC" >&2
  exit 1
fi
cp "$WASM_EXEC" dist/wasm_exec.js
# The Screeps isolate's Reflect misbehaves for wasm_exec (Reflect.get returns
# undefined for existing keys), which makes every Go js.Value .Get()/.Call() read
# back undefined. The patch rewrites those to direct access and adds memory-view
# caching + an ASCII fast path. See tools/main.go and CLAUDE.md.
( cd "$ROOT/tools" && go run . patch "$ROOT/dist/wasm_exec.js" )

echo "==> Copying JS modules (plain CommonJS, no bundler)"
cp js/loader.js dist/main.js
cp js/host.js dist/host.js
cp js/host_gen.js dist/host_gen.js

echo "==> Build done. dist/:"
ls -la dist
