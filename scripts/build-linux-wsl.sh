#!/bin/sh
# Build the Linux distribution archive inside WSL.
# The Go toolchain is expected in ~/.local/go (see docs/guide if missing).
set -eu
export PATH="$HOME/.local/go/bin:$PATH"
cd "$(dirname "$0")/.."
exec go run ./scripts/package -targets linux/amd64 "$@"
