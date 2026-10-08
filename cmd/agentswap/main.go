package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/WellWells/agentswap/internal/cli"
	"github.com/WellWells/agentswap/internal/ui"
)

var version = "dev"

func main() {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "agentswap:", err)
		os.Exit(1)
	}
	exe, err := os.Executable()
	if err == nil {
		exe, err = filepath.EvalSymlinks(exe)
	}
	if err != nil {
		exe = ""
	}
	color, width := ui.Terminal(os.Stdout)
	if os.Getenv("NO_COLOR") != "" {
		color = false
	}
	if n, err := strconv.Atoi(os.Getenv("COLUMNS")); err == nil && n > 0 {
		width = n
	}
	if width <= 0 {
		width = 80
	}
	os.Exit(cli.Run(cli.Env{
		Args:    os.Args,
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
		Getenv:  os.Getenv,
		Home:    home,
		Exe:     exe,
		Version: version,
		Lang:    ui.DetectLang(os.Getenv, ui.SystemLocale()),
		Color:   color,
		Width:   width,
		Zone:    ui.DetectZone(os.Getenv),
	}))
}
