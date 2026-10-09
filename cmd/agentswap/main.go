package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"

	"github.com/WellWells/agentswap/internal/cli"
	"github.com/WellWells/agentswap/internal/links"
	"github.com/WellWells/agentswap/internal/ui"
	"github.com/WellWells/agentswap/internal/update"
)

var (
	version = "dev"
	repo    = "WellWells/agentswap"
)

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
	if runtime.GOOS == "windows" && exe != "" {
		if code, ok := forward(exe); ok {
			os.Exit(code)
		}
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
		Args:        os.Args,
		Stdout:      os.Stdout,
		Stderr:      os.Stderr,
		Getenv:      os.Getenv,
		Home:        home,
		Exe:         exe,
		Version:     version,
		Lang:        ui.DetectLang(os.Getenv, ui.SystemLanguages),
		Color:       color,
		Width:       width,
		Zone:        ui.DetectZone(os.Getenv),
		Exec:        run,
		Output:      output,
		Stdin:       os.Stdin,
		Interactive: ui.IsTerminal(os.Stdin),
		Notify:      ui.IsTerminal(os.Stderr),
		Releases:    update.Source{Repo: repo},
		Endpoints:   endpoints(),
	}))
}

func forward(exe string) (int, bool) {
	target, ok := links.Target(exe)
	if !ok {
		return 0, false
	}
	cmd := exec.Command(target, os.Args[1:]...)
	cmd.Args[0] = os.Args[0]
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		return 0, false
	}
	signal.Notify(make(chan os.Signal, 1), os.Interrupt)
	if err := cmd.Wait(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return exit.ExitCode(), true
		}
		fmt.Fprintln(os.Stderr, "agentswap:", err)
		return 1, true
	}
	return 0, true
}

func run(env []string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

func output(env []string, name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(), env...)
	return cmd.CombinedOutput()
}
