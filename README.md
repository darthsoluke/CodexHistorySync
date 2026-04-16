# CodexHistorySync

`CodexHistorySync` / `对话历史同步器` is a standalone CLI for retagging local Codex history across providers.
`CodexHistorySync` / `对话历史同步器` 是一个独立 CLI，用于在不同 provider 之间重标记本地 Codex 历史。

It is implemented as a single Go binary, so runtime use does not require Python, conda, or a separate SQLite installation.
它以单个 Go 二进制发布，因此运行时不需要 Python、conda 或单独安装 SQLite。

## Install / 安装

### macOS / Linux

```bash
curl -fsSL https://raw.githubusercontent.com/darthsoluke/CodexHistorySync/main/install.sh | bash
```

### Windows PowerShell

```bash
irm https://raw.githubusercontent.com/darthsoluke/CodexHistorySync/main/install.ps1 | iex
```

## Usage / 用法

```bash
codex-history-sync --help
codex-history-sync --apply
```

`--help` shows bilingual usage text and flag descriptions.
`--help` 会显示中英文双语的用法和参数说明。

`--apply` writes changes; without it, the tool only previews.
`--apply` 会写入更改；不加时只会预览。

```bash
codex-history-sync --provider openai --from-provider anthropic --apply
```

This retags history from `anthropic` to `openai`.
这会把历史从 `anthropic` 重标记到 `openai`。

## Release / 发布

Pushing a `v*` tag triggers the GitHub Actions release workflow and publishes prebuilt binaries under GitHub Releases.
推送 `v*` tag 会触发 GitHub Actions 发布流程，并在 GitHub Releases 中发布预编译二进制。

To pin a version, set `CODEX_HISTORY_SYNC_VERSION=v0.1.1` before running the installer.
如需固定版本，在运行安装脚本前设置 `CODEX_HISTORY_SYNC_VERSION=v0.1.1`。
