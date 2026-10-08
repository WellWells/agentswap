//go:build !windows && !linux && !darwin

package ui

func SystemLanguages() []string { return nil }
