#!/bin/sh
# Build unstripped Linux binaries for crash diagnosis (symbols intact).
# Output: dist/qqbot-debug-linux-amd64, dist/dicegirl-debug-linux-amd64
set -eu
export PATH="$HOME/.local/go/bin:$PATH"
cd "$(dirname "$0")/.."
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o dist/qqbot-debug-linux-amd64 ./cmd/qqbot
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o dist/dicegirl-debug-linux-amd64 ./cmd/dicegirl
echo "built: dist/qqbot-debug-linux-amd64 dist/dicegirl-debug-linux-amd64"
