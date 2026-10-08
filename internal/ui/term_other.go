//go:build !windows && !linux && !darwin

package ui

import "os"

func SystemLocale() string { return "" }

func systemZone() string { return "" }

func Terminal(f *os.File) (bool, int) { return false, 0 }
