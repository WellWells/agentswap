//go:build !windows && !linux && !darwin

package ui

import "os"

func systemZone() string { return "" }

func Terminal(f *os.File) (bool, int) { return false, 0 }

func IsTerminal(f *os.File) bool { return false }
