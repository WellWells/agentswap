package main

import (
	"fmt"
	"os"

	"github.com/WellWells/agentswap/internal/cli"
)

var version = "dev"

func main() {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "agentswap:", err)
		os.Exit(1)
	}
	os.Exit(cli.Run(cli.Env{
		Args:    os.Args,
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
		Getenv:  os.Getenv,
		Home:    home,
		Version: version,
	}))
}
