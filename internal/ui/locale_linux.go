//go:build linux

package ui

import "os"

func SystemLanguages() []string {
	var langs []string
	for _, p := range []string{"/etc/locale.conf", "/etc/default/locale"} {
		if b, err := os.ReadFile(p); err == nil {
			langs = append(langs, parseLocaleConf(string(b))...)
		}
	}
	return langs
}
