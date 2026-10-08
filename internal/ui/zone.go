package ui

import (
	"fmt"
	"strings"
	"time"
)

func DetectZone(getenv func(string) string) string {
	tz := strings.TrimPrefix(getenv("TZ"), ":")
	if i := strings.Index(tz, "zoneinfo/"); i >= 0 {
		tz = tz[i+len("zoneinfo/"):]
	}
	if strings.Contains(tz, "/") {
		return tz
	}
	return systemZone()
}

func ZoneLabel(name string, t time.Time) string {
	if strings.Contains(name, "/") {
		return name
	}
	_, off := t.Zone()
	if off == 0 {
		return "UTC"
	}
	sign := "+"
	if off < 0 {
		sign, off = "-", -off
	}
	if m := off % 3600 / 60; m != 0 {
		return fmt.Sprintf("UTC%s%d:%02d", sign, off/3600, m)
	}
	return fmt.Sprintf("UTC%s%d", sign, off/3600)
}
