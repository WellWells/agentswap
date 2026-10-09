# Contributing

Bug reports, questions and pull requests are all welcome, in English or Chinese. If something in the README was unclear, that is worth an issue too.

## Issues

Use the [bug report](https://github.com/WellWells/agentswap/issues/new?template=bug_report.yml) or [feature request](https://github.com/WellWells/agentswap/issues/new?template=feature_request.yml) form. Remove emails and tokens before pasting output. Security problems go through [private reporting](https://github.com/WellWells/agentswap/security/advisories/new) instead; see [SECURITY.md](SECURITY.md).

## Pull requests

For anything bigger than a small fix, open an issue first so we can agree on the approach before you spend time on it.

You need Go 1.22 or newer and nothing else. There are no third-party modules, so it builds offline.

```sh
go vet ./...
go test ./...
gofmt -l .
go build -o agentswap ./cmd/agentswap
go run ./tools/dist -version v0.1.0    # every platform, as in a release, into dist/
```

CI runs the same checks on macOS, Linux and Windows, plus the tests on Go 1.22. To check that other platforms still compile from your machine:

```sh
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go vet ./...
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go vet ./...
```

A few conventions:

- Standard library only, and `CGO_ENABLED=0`. A new dependency needs a good reason.
- The code has no comments. Put the reasoning in the pull request description, where it can be discussed.
- Commit messages follow [Conventional Commits](https://www.conventionalcommits.org/): `fix: ...`, `feat: ...`, `docs: ...`.
- User-facing text lives in `internal/ui/lang.go` in English, Traditional Chinese and Simplified Chinese. If you can only write English, leave the other two in English and say so; someone will translate them.

## Trying changes without touching your real accounts

Point every location at a temporary directory:

```sh
export AGENTSWAP_HOME=/tmp/as/data
export CODEX_HOME=/tmp/as/codex
export CLAUDE_CONFIG_DIR=/tmp/as/claude
go run ./cmd/agentswap codex status
```

The Antigravity CLI keeps its login in the OS keyring, which has no such override, so switching agy accounts always writes to the real keyring. Keep that in mind before running `agswap` from a development build.

## Adding an agent

Each agent is a `swap.Provider` in its own package under `internal/` (see `internal/codex` for the simplest one). It reads and writes the live login as raw bytes, derives a stable account key, and optionally fetches usage. Then register it in `providers` and `commands` in `internal/cli/cli.go`. Opening a feature request first with what you know about where the agent stores its login is a good start.
