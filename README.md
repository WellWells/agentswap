# agentswap

[![ci](https://github.com/WellWells/agentswap/actions/workflows/ci.yml/badge.svg)](https://github.com/WellWells/agentswap/actions/workflows/ci.yml)
[![release](https://img.shields.io/github/v/release/WellWells/agentswap)](https://github.com/WellWells/agentswap/releases/latest)
[![license](https://img.shields.io/github/license/WellWells/agentswap)](LICENSE)

Switch between multiple accounts of AI coding agents from one small binary. No Node, Python or other runtime required. Works on macOS, Linux and Windows.

| Agent | Commands | Status |
|---|---|---|
| OpenAI Codex | `cxswap`, `codexswap`, `agentswap codex` | supported |
| Claude Code | `ccswap`, `claudeswap`, `agentswap claude` | supported |
| Antigravity CLI (`agy`) | `agswap`, `agyswap`, `agentswap antigravity` | supported |

## Install

macOS / Linux:

```sh
curl -fsSL https://github.com/WellWells/agentswap/releases/latest/download/install.sh | sh
```

Windows (PowerShell):

```powershell
irm https://github.com/WellWells/agentswap/releases/latest/download/install.ps1 | iex
```

Homebrew (macOS / Linux):

```sh
brew install wellwells/tap/agentswap
```

None of these need `xattr`, "Open Anyway" or `Unblock-File`: they verify the SHA256 against `checksums.txt` and install a binary that macOS Gatekeeper and Windows SmartScreen do not flag. Set `AGENTSWAP_VERSION` (for example `v1.0.0`) to pin a version `AGENTSWAP_INSTALL_DIR` to change where the script installs, and `AGENTSWAP_DOWNLOAD_URL` to download the archives from a mirror.

Or download an archive from [Releases](https://github.com/WellWells/agentswap/releases) and verify it against `checksums.txt`. Archives opened from a browser download are flagged as untrusted; prefer the commands above.

With Go: `go install github.com/WellWells/agentswap/cmd/agentswap@latest`, then run `agentswap link` to create `cxswap` and the other commands next to it.

`agentswap update` installs the latest release in place (Homebrew: `brew upgrade agentswap`). agentswap also checks for a new release at most every 12 hours and prints a one-line notice; set `AGENTSWAP_NO_UPDATE_CHECK=1` to turn that off.

The installers only ship `agentswap` and run `agentswap link`, which adds the per-agent commands as symlinks (hard links on Windows). `agentswap unlink` removes them.

## Usage

Run any command without arguments for its help. `agentswap status` shows the usage of every saved account of every agent.

```sh
codex login            # log in to the first account
cxswap add work        # save it
cxswap login personal  # log in to another account and save it

cxswap status          # usage of every saved account
cxswap status 2        # usage of one account (number, alias or email)
cxswap list            # account numbers and switch commands
cxswap 2               # switch by number, alias or email
cxswap -               # switch back
cxswap alias 2 home
cxswap rm 2
```

Once an account is saved, do not run `codex login` or `codex logout` yourself: both revoke the tokens of the account that is signed in. `cxswap login` signs in from a temporary `CODEX_HOME`, so saved accounts stay valid.

Restart Codex after switching, including `codex app-server daemon restart`. `CODEX_HOME` is respected. Codex's `cli_auth_credentials_store = "keyring"` mode is not supported yet.

### Claude Code

```sh
ccswap add work        # save the account signed in to Claude Code now
ccswap import          # or import accounts from another tool

ccswap status          # usage of every saved account
ccswap status work     # usage of one account
ccswap list
ccswap 2               # switch; running sessions follow on their next request
```

Sign in to other accounts with Claude Code itself (`/login`), then run `ccswap add`. `CLAUDE_CONFIG_DIR` is respected, MCP and plugin tokens in `.credentials.json` stay with the machine, and only `oauthAccount` in `.claude.json` is replaced. On macOS the credentials are read from and written to the Keychain. Only Claude subscription logins are supported. After `ccswap import`, stop using cswap and run `cswap purge`: its files are only base64.

### Antigravity CLI

```sh
agswap add work        # save the account signed in to agy now
agswap status          # usage of every saved account
agswap list
agswap 2               # switch; offers to close running agy first
agswap 2 --yes         # switch and close running agy without asking
```

To add another account, run `/logout` in agy and sign in to it, then run `agswap add`; logging out does not revoke saved accounts. The login lives in the OS keyring (`gemini:antigravity`: Credential Manager on Windows, Keychain on macOS, Secret Service via `secret-tool` on Linux). agy reads it only when it starts and writes its own account back while running, so `agswap` only switches once every `agy` process is closed, including `agy remote-control`. The desktop app is not supported yet.

### Switching while agents run

When switching would change the account, `cxswap`, `ccswap` and `agswap` list the matching processes that are running (`codex`, `claude` or `agy`, never the one you run them from, and not the Claude desktop app) and ask whether to close them. `--yes` (`-y`) closes them without asking; without a terminal nothing is closed. Declining keeps Codex and Claude Code running on the old account until they restart; for agy the switch is cancelled.

Usage and token requests identify themselves the way the official CLI does: the installed `codex`, `claude` and `agy` versions are detected once and cached in `~/.agentswap/clients.json`, with built-in versions as the fallback.

### Language

Messages follow the system language (English when it is not supported). `agentswap lang` lists the languages with numbers: `1` `en`, `2` `zh-TW`/`zht`, `3` `zh-CN`/`zhc`. `agentswap lang 2` or `agentswap lang zht` sets one, `agentswap lang auto` follows the system again, and `AGENTSWAP_LANG` overrides both.

### Storage

Saved accounts live in `~/.agentswap/` (override with `AGENTSWAP_HOME`) and are encrypted for the current user on the current machine: DPAPI on Windows, a key in the Keychain on macOS, and a key in the Secret Service on Linux (falling back to a key file bound to `/etc/machine-id`, with a warning). A copy of `~/.agentswap` cannot be decrypted on another computer.

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
