#!/bin/sh
set -eu

cd "$(dirname "$0")/.."
mkdir -p dist
cp AGENTS.md dist/llms.txt
cp web/*.html web/*.js web/*.css web/*.jpg dist/
cp web/arena/index.html web/arena/arena.css web/arena/arena.js web/arena/report.json dist/
for page in play join; do
  sed 's/data-engine="http"/data-engine="wasm"/' "web/$page.html" > "dist/$page.html"
done
GOOS=js GOARCH=wasm go build -trimpath -ldflags="-s -w" -o dist/game.wasm ./cmd/wasm
runtime="$(go env GOROOT)/lib/wasm/wasm_exec.js"
if [ ! -f "$runtime" ]; then
  runtime="$(go env GOROOT)/misc/wasm/wasm_exec.js"
fi
cp "$runtime" dist/wasm_exec.js
touch dist/.nojekyll
echo "Arena and practice board are ready in dist/"
