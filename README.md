# CodexHistorySync

`CodexHistorySync` / `对话历史同步器` is a standalone CLI for retagging local Codex history across providers.

It is implemented as a single Go binary, so runtime use does not require Python, conda, or a separate SQLite installation.

Install:

```bash
./install.sh
```

Build:

```bash
go build -o "$HOME/.local/bin/codex-history-sync"
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

Pushing a `v*` tag triggers the GitHub Actions release workflow and publishes binaries under GitHub Releases.
