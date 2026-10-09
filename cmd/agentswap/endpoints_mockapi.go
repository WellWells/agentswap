//go:build mockapi

package main

import (
	"os"

	"github.com/WellWells/agentswap/internal/cli"
)

func endpoints() cli.Endpoints {
	return cli.Endpoints{
		CodexAPI:    os.Getenv("AGENTSWAP_CODEX_API_URL"),
		CodexToken:  os.Getenv("AGENTSWAP_CODEX_TOKEN_URL"),
		ClaudeAPI:   os.Getenv("AGENTSWAP_CLAUDE_API_URL"),
		ClaudeToken: os.Getenv("AGENTSWAP_CLAUDE_TOKEN_URL"),
		AgyAPI:      os.Getenv("AGENTSWAP_ANTIGRAVITY_API_URL"),
		AgyToken:    os.Getenv("AGENTSWAP_ANTIGRAVITY_TOKEN_URL"),
	}
}
