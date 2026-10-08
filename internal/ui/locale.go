package ui

import (
	"strings"
	"unicode/utf16"
)

func splitMultiSZ(buf []uint16) []string {
	var out []string
	start := 0
	for i, c := range buf {
		if c != 0 {
			continue
		}
		if i == start {
			break
		}
		out = append(out, string(utf16.Decode(buf[start:i])))
		start = i + 1
	}
	return out
}

func parseAppleLanguages(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		v := strings.Trim(strings.TrimSpace(line), `,"`)
		if v == "" || v == "(" || v == ")" {
			continue
		}
		out = append(out, v)
	}
	return out
}

func parseLocaleConf(s string) []string {
	var out []string
	for _, key := range []string{"LANGUAGE", "LC_ALL", "LC_MESSAGES", "LANG"} {
		for _, line := range strings.Split(s, "\n") {
			k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
			if !ok || strings.TrimPrefix(k, "export ") != key {
				continue
			}
			for _, part := range strings.Split(strings.Trim(v, `"'`), ":") {
				if part != "" {
					out = append(out, part)
				}
			}
		}
	}
	return out
}
