#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TARGET_DIR="${1:-$HOME/.local/bin}"
TARGET_PATH="$TARGET_DIR/codex-history-sync"

if ! command -v go >/dev/null 2>&1; then
  echo "codex-history-sync: missing go in PATH" >&2
  exit 1
fi

mkdir -p "$TARGET_DIR"
(cd "$SCRIPT_DIR" && go build -o "$TARGET_PATH" .)

echo "Installed: $TARGET_PATH"
echo
echo "Try:"
echo "  codex-history-sync --help"
