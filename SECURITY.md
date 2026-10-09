# Security

agentswap handles login tokens for other tools, so problems that could expose them matter more than usual.

## Reporting

Please report security problems privately through [GitHub's private vulnerability reporting](https://github.com/WellWells/agentswap/security/advisories/new), not in a public issue. Include the version (`agentswap version`), your OS and the steps to reproduce. I will reply as soon as I can and credit you in the release notes unless you prefer otherwise.

Only the latest release gets fixes.

## What agentswap does with your tokens

- Saved accounts are stored under `~/.agentswap` (or `AGENTSWAP_HOME`), encrypted with a key that only the current user on the current machine can use: DPAPI on Windows, the Keychain on macOS, the Secret Service on Linux. Without a Secret Service, Linux falls back to a key file tied to `/etc/machine-id` and prints a warning.
- Tokens are only sent to the services they belong to (OpenAI, Anthropic, Google), to read usage and to refresh them. agentswap has no telemetry. The only other request is the release check against GitHub, which you can turn off with `AGENTSWAP_NO_UPDATE_CHECK=1`.
- Before overwriting a live login, agentswap keeps an encrypted backup of it next to the saved accounts (the five most recent per agent).
