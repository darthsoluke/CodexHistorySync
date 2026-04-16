#!/usr/bin/env bash
set -euo pipefail

repo="darthsoluke/CodexHistorySync"
install_dir="${CODEX_HISTORY_SYNC_INSTALL_DIR:-$HOME/.local/bin}"
version="${CODEX_HISTORY_SYNC_VERSION:-latest}"
base_url="${CODEX_HISTORY_SYNC_BASE_URL:-https://github.com/$repo/releases}"

os_name="$(uname -s 2>/dev/null || echo unknown)"
arch_name="$(uname -m 2>/dev/null || echo unknown)"

case "$os_name" in
  Linux) platform="linux" ;;
  Darwin) platform="darwin" ;;
  MINGW*|MSYS*|CYGWIN*) platform="windows" ;;
  *)
    echo "codex-history-sync: unsupported OS / 不支持的操作系统: $os_name" >&2
    exit 1
    ;;
esac

case "$arch_name" in
  x86_64|amd64) arch="amd64" ;;
  aarch64|arm64) arch="arm64" ;;
  *)
    echo "codex-history-sync: unsupported architecture / 不支持的架构: $arch_name" >&2
    exit 1
    ;;
esac

asset_name="codex-history-sync_${platform}_${arch}"
if [ "$platform" = "windows" ]; then
  asset_name="${asset_name}.exe"
fi

if [ "$version" = "latest" ]; then
  download_url="$base_url/latest/download/$asset_name"
else
  download_url="$base_url/download/$version/$asset_name"
fi

mkdir -p "$install_dir"
tmp_file="$(mktemp "${TMPDIR:-/tmp}/codex-history-sync.XXXXXX")"
trap 'rm -f "$tmp_file"' EXIT

if command -v curl >/dev/null 2>&1; then
  curl -fsSL "$download_url" -o "$tmp_file"
elif command -v wget >/dev/null 2>&1; then
  wget -qO "$tmp_file" "$download_url"
else
  echo "codex-history-sync: need curl or wget to download releases / 需要 curl 或 wget 才能下载发布包" >&2
  exit 1
fi

target_path="$install_dir/codex-history-sync"
if [ "$platform" = "windows" ]; then
  target_path="${target_path}.exe"
fi

mv "$tmp_file" "$target_path"
chmod +x "$target_path"

echo "Installed / 已安装: $target_path"
echo
echo "Try / 试试:"
echo "  codex-history-sync --help"
