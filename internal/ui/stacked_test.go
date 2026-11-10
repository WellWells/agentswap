package ui

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/WellWells/agentswap/internal/swap"
)

func TestRenderExtremelyNarrowTerminal(t *testing.T) {
	now := at(10, 8, 10, 0)
	ansi := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	normalize := func(s string) string { return strings.Join(strings.Fields(ansi.ReplaceAllString(s, "")), "") }
	for _, l := range Langs {
		for _, width := range []int{2, 8, 12, 16, 20, 24, 32, 40, 48, 60, 64, 70, 72, 79, 80, 88, 100, 120} {
			for _, color := range []bool{false, true} {
				cards := overviewCards(now)
				cards[1].Usage.Windows = []swap.Window{{UsedPercent: 17, Minutes: 43200, ResetsAt: at(11, 7, 10, 0)}}
				cards = append(cards,
					Card{Provider: "Antigravity", Number: 3, Alias: "工作用的帳號別名很長", Email: "very.long.email.address@example.com", State: LoginExpired},
					Card{Provider: "Antigravity", Number: 4, Email: "key@example.com", State: NoUsage},
					Card{Provider: "Antigravity", Number: 5, Email: "down@example.com", State: Unavailable, Detail: "HTTP 503: temporarily unavailable"},
					Card{Provider: "Antigravity", Number: 6, Email: "empty@example.com", Active: true, Unsaved: "agentswap antigravity add"},
					Card{Provider: "Antigravity", Number: 7, Email: "cache@example.com", Usage: swap.Usage{At: now.Add(-time.Hour), Windows: []swap.Window{{UsedPercent: 13, Minutes: 300, ResetsAt: now.Add(time.Hour)}}}},
				)
				var buf bytes.Buffer
				Render(&buf, cards, Options{Lang: l, Width: width, Color: color, Now: now})
				out := buf.String()
				for _, line := range strings.Split(out, "\n") {
					if visibleWidth(line) > width {
						t.Fatalf("lang %d width %d color %v: line width %d: %q", l, width, color, visibleWidth(line), line)
					}
				}
				if width < 60 || strings.HasPrefix(ansi.ReplaceAllString(strings.Split(out, "\n")[1], ""), "#1") {
					plain := normalize(out)
					for _, c := range cards {
						if !strings.Contains(plain, normalize(cardTitle(c))) {
							t.Fatalf("lang %d width %d: missing account %q:\n%s", l, width, cardTitle(c), out)
						}
						for _, tag := range cardTags(c, Options{Lang: l, Now: now}) {
							if !strings.Contains(plain, normalize(tag.text)) {
								t.Fatalf("lang %d width %d: missing tag %q:\n%s", l, width, tag.text, out)
							}
						}
						if msg, _ := cardMessage(c, l); msg != "" && !strings.Contains(plain, normalize(msg)) {
							t.Fatalf("lang %d width %d: missing message %q:\n%s", l, width, msg, out)
						}
						for _, win := range c.Usage.Windows {
							for _, want := range []string{l.WindowTitle(win.Minutes, win.Label), ResetTime(l, win.ResetsAt, now), strconv.Itoa(win.Percent(now)) + "%"} {
								if !strings.Contains(plain, normalize(want)) {
									t.Fatalf("lang %d width %d: missing usage %q:\n%s", l, width, want, out)
								}
							}
						}
					}
					if !strings.Contains(plain, normalize(l.T("zoneNote", "UTC+8"))) {
						t.Fatalf("lang %d width %d: missing time zone:\n%s", l, width, out)
					}
				}
			}
		}
	}
}

func TestRenderNarrowAccountWithoutUsage(t *testing.T) {
	c := Card{Provider: "Codex", Number: 1, Email: "long.account.name@example.com", State: Unavailable, Detail: "HTTP 503: temporarily unavailable"}
	for _, l := range Langs {
		var buf bytes.Buffer
		Render(&buf, []Card{c}, Options{Lang: l, Width: 40})
		out := strings.Join(strings.Fields(buf.String()), "")
		for _, want := range []string{cardTitle(c), l.T("unavailable", c.Detail)} {
			if !strings.Contains(out, strings.Join(strings.Fields(want), "")) {
				t.Fatalf("lang %d: missing %q:\n%s", l, want, buf.String())
			}
		}
	}
}
