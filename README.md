# agentswap

[![ci](https://github.com/WellWells/agentswap/actions/workflows/ci.yml/badge.svg)](https://github.com/WellWells/agentswap/actions/workflows/ci.yml)
[![release](https://img.shields.io/github/v/release/WellWells/agentswap)](https://github.com/WellWells/agentswap/releases/latest)
[![license](https://img.shields.io/github/license/WellWells/agentswap)](LICENSE)

Switch between multiple accounts of AI coding agents from one small binary. No Node, Python or other runtime required. Works on macOS, Linux and Windows.

| Agent | Commands | Status |
|---|---|---|
| OpenAI Codex | `cxswap`, `codexswap`, `agentswap codex` | supported |
| Claude Code | `ccswap`, `claudeswap`, `agentswap claude` | planned |
| Antigravity | | planned |

## Install

macOS / Linux:

```sh
curl -fsSL https://github.com/WellWells/agentswap/releases/latest/download/install.sh | sh
```

Windows (PowerShell):

```powershell
irm https://github.com/WellWells/agentswap/releases/latest/download/install.ps1 | iex
```

Or download an archive from [Releases](https://github.com/WellWells/agentswap/releases) and verify it against `checksums.txt`.

With Go: `go install github.com/WellWells/agentswap/cmd/agentswap@latest`, then run it as `agentswap codex ...`.

## Usage

```sh
codex login            # log in to the first account
cxswap add work        # save it
codex login            # log in to another account (do not log out first)
cxswap add personal

cxswap                 # list, * marks the active account
cxswap work            # switch by alias, number or email
cxswap -               # switch back
cxswap status
cxswap alias 2 home
cxswap rm 2
```

Restart Codex after switching. Saved accounts live in `~/.agentswap/` (override with `AGENTSWAP_HOME`); `CODEX_HOME` is respected. Codex's `cli_auth_credentials_store = "keyring"` mode is not supported yet.

These files contain login tokens. Keep them private.

## Build from source

Only Go 1.22 or newer is needed. There are no third-party modules, so builds work fully offline.

```sh
go test ./...
go build -o agentswap ./cmd/agentswap
go run ./tools/dist -version v0.1.0
```

`tools/dist` cross-compiles every platform and writes the release archives and `checksums.txt` to `dist/`, exactly as the release workflow does. Use `-targets windows/amd64` to build a subset. Set `SOURCE_DATE_EPOCH` for reproducible archives.

## License

Copyright 2026 [WellsTsai](https://wellstsai.com)

Licensed under the [MIT License](LICENSE). See [NOTICE](NOTICE).
