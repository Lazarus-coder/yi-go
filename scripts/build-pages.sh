#!/bin/sh
set -eu

cd "$(dirname "$0")/.."
mkdir -p dist
cp -R web/. dist/
sed 's/data-engine="http"/data-engine="wasm"/' web/index.html > dist/index.html
GOOS=js GOARCH=wasm go build -trimpath -ldflags="-s -w" -o dist/game.wasm ./cmd/wasm
runtime="$(go env GOROOT)/lib/wasm/wasm_exec.js"
if [ ! -f "$runtime" ]; then
  runtime="$(go env GOROOT)/misc/wasm/wasm_exec.js"
fi
cp "$runtime" dist/wasm_exec.js
touch dist/.nojekyll
echo "GitHub Pages files are ready in dist/"
