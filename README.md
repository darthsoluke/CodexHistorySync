# CodexHistorySync

`CodexHistorySync` / `对话历史同步器` is a standalone CLI for retagging local Codex history across providers.

It is implemented as a single Go binary, so runtime use does not require Python, conda, or a separate SQLite installation.

Install from release:

```bash
curl -fsSL https://raw.githubusercontent.com/darthsoluke/CodexHistorySync/main/install.sh | bash
```

Windows PowerShell:

```bash
irm https://raw.githubusercontent.com/darthsoluke/CodexHistorySync/main/install.ps1 | iex
```

Run:

```bash
codex-history-sync --help
codex-history-sync --apply
```

Release:

```bash
git tag v0.1.0
git push origin v0.1.0
```

Pushing a `v*` tag triggers the GitHub Actions release workflow and publishes prebuilt binaries under GitHub Releases.

To pin a version, set `CODEX_HISTORY_SYNC_VERSION=v0.1.0` before running the installer.
