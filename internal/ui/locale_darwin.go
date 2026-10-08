//go:build darwin

package ui

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

func SystemLanguages() []string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var langs []string
	if out, err := exec.CommandContext(ctx, "/usr/bin/defaults", "read", "-g", "AppleLanguages").Output(); err == nil {
		langs = parseAppleLanguages(string(out))
	}
	if out, err := exec.CommandContext(ctx, "/usr/bin/defaults", "read", "-g", "AppleLocale").Output(); err == nil {
		if v := strings.TrimSpace(string(out)); v != "" {
			langs = append(langs, v)
		}
	}
	return langs
}
