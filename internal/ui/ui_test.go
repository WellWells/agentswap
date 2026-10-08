package ui

import (
	"bytes"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/WellWells/agentswap/internal/swap"
)

var tpe = time.FixedZone("", 8*3600)

func at(month time.Month, day, hour, min int) time.Time {
	return time.Date(2026, month, day, hour, min, 0, 0, tpe)
}

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestDetectLang(t *testing.T) {
	cases := []struct {
		env    map[string]string
		system string
		want   Lang
	}{
		{nil, "", En},
		{nil, "zh-TW", ZhTW},
		{nil, "zh-CN", ZhTW},
		{nil, "en-US", En},
		{map[string]string{"LANG": "zh_TW.UTF-8"}, "en-US", ZhTW},
		{map[string]string{"LANG": "en_US.UTF-8"}, "zh-TW", En},
		{map[string]string{"LANG": "C.UTF-8"}, "zh-TW", ZhTW},
		{map[string]string{"LC_ALL": "en_GB", "LANG": "zh_TW"}, "", En},
		{map[string]string{"AGENTSWAP_LANG": "zh-TW", "LC_ALL": "en_US"}, "", ZhTW},
		{map[string]string{"AGENTSWAP_LANG": "en"}, "zh-TW", En},
	}
	for _, c := range cases {
		if got := DetectLang(env(c.env), c.system); got != c.want {
			t.Errorf("%v / %q: got %v want %v", c.env, c.system, got, c.want)
		}
	}
}

func TestMessagesExistInBothLanguages(t *testing.T) {
	for key, m := range messages {
		if m[En] == "" || m[ZhTW] == "" {
			t.Errorf("%s missing a translation", key)
		}
	}
	if En.T("used", 7) != "7% used" || ZhTW.T("used", 7) != "已用 7%" {
		t.Fatalf("got %q / %q", En.T("used", 7), ZhTW.T("used", 7))
	}
}

func TestZoneLabel(t *testing.T) {
	cases := map[string]time.Time{
		"Asia/Taipei": time.Date(2026, 1, 1, 0, 0, 0, 0, tpe),
		"UTC+8":       time.Date(2026, 1, 1, 0, 0, 0, 0, tpe),
		"UTC+5:30":    time.Date(2026, 1, 1, 0, 0, 0, 0, time.FixedZone("", 5*3600+1800)),
		"UTC-3":       time.Date(2026, 1, 1, 0, 0, 0, 0, time.FixedZone("", -3*3600)),
		"UTC":         time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	for want, tm := range cases {
		name := ""
		if strings.Contains(want, "/") {
			name = want
		}
		if got := ZoneLabel(name, tm); got != want {
			t.Errorf("got %q want %q", got, want)
		}
	}
}

func TestZoneFromTZ(t *testing.T) {
	if got := DetectZone(env(map[string]string{"TZ": ":Asia/Taipei"})); got != "Asia/Taipei" {
		t.Fatalf("got %q", got)
	}
}

func TestBar(t *testing.T) {
	cases := []struct {
		pct   int
		width int
		want  string
	}{
		{0, 4, "░░░░"},
		{50, 10, "█████░░░░░"},
		{55, 10, "█████▌░░░░"},
		{100, 4, "████"},
		{130, 4, "████"},
		{-5, 4, "░░░░"},
	}
	for _, c := range cases {
		if got := Bar(c.pct, c.width, false); got != c.want {
			t.Errorf("Bar(%d,%d) = %q want %q", c.pct, c.width, got, c.want)
		}
	}
	if s := Bar(10, 10, true); !strings.Contains(s, "38;5;147m") || strings.Contains(s, "░") {
		t.Errorf("normal color: %q", s)
	}
	if !strings.Contains(Bar(85, 10, true), "38;5;220m") || !strings.Contains(Bar(96, 10, true), "38;5;203m") {
		t.Error("threshold colors missing")
	}
}

func TestResetLine(t *testing.T) {
	now := at(10, 8, 10, 0)
	cases := []struct {
		lang Lang
		t    time.Time
		zone string
		want string
	}{
		{En, at(10, 8, 15, 20), "", "Resets 3:20pm (UTC+8)"},
		{En, at(10, 8, 15, 0), "", "Resets 3pm (UTC+8)"},
		{En, at(10, 14, 17, 41), "Asia/Taipei", "Resets Oct 14, 5:41pm (Asia/Taipei)"},
		{ZhTW, at(10, 8, 15, 20), "", "今天 15:20 重置（UTC+8）"},
		{ZhTW, at(10, 14, 17, 41), "", "10/14（三）17:41 重置（UTC+8）"},
	}
	for _, c := range cases {
		if got := ResetLine(c.lang, c.t, now, c.zone); got != c.want {
			t.Errorf("got %q want %q", got, c.want)
		}
	}
}

func TestAgo(t *testing.T) {
	cases := map[time.Duration][2]string{
		30 * time.Second: {"just now", "剛剛"},
		5 * time.Minute:  {"5m ago", "5 分鐘前"},
		3 * time.Hour:    {"3h ago", "3 小時前"},
		72 * time.Hour:   {"3d ago", "3 天前"},
	}
	for d, want := range cases {
		if En.Ago(d) != want[0] || ZhTW.Ago(d) != want[1] {
			t.Errorf("%v: %q / %q", d, En.Ago(d), ZhTW.Ago(d))
		}
	}
}

func TestWindowName(t *testing.T) {
	cases := map[int][2]string{
		300:   {"5-hour limit", "5 小時額度"},
		60:    {"1-hour limit", "1 小時額度"},
		1440:  {"1-day limit", "1 天額度"},
		10080: {"Weekly limit", "本週額度"},
		4320:  {"3-day limit", "3 天額度"},
		90:    {"90-minute limit", "90 分鐘額度"},
	}
	for m, want := range cases {
		if En.WindowName(m) != want[0] || ZhTW.WindowName(m) != want[1] {
			t.Errorf("%d: %q / %q", m, En.WindowName(m), ZhTW.WindowName(m))
		}
	}
}

func sampleCards(now time.Time) []Card {
	return []Card{
		{Provider: "Codex", Number: 1, Alias: "main", Email: "jora@x.com", Plan: "prolite", Active: true,
			Usage: swap.Usage{Live: true, Windows: []swap.Window{{UsedPercent: 76, Minutes: 10080, ResetsAt: at(10, 14, 17, 41)}}}},
		{Provider: "Codex", Number: 2, Alias: "work", Email: "boss@x.com", Plan: "plus",
			Usage: swap.Usage{Live: true, Windows: []swap.Window{
				{UsedPercent: 42, Minutes: 300, ResetsAt: at(10, 8, 13, 20)},
				{UsedPercent: 10, Minutes: 10080, ResetsAt: at(10, 15, 9, 12)},
			}}},
		{Provider: "Codex", Number: 3, Email: "old@x.com", State: LoginExpired},
		{Provider: "Codex", Number: 4, Email: "cache@x.com",
			Usage: swap.Usage{At: now.Add(-3 * time.Hour), Windows: []swap.Window{{UsedPercent: 5, Minutes: 300}}}},
		{Provider: "Codex", Number: 5, Email: "key@x.com", State: NoUsage},
		{Provider: "Codex", Number: 6, Email: "down@x.com", State: Unavailable, Detail: "HTTP 500"},
	}
}

func TestRenderPlainCards(t *testing.T) {
	now := at(10, 8, 10, 0)
	cards := sampleCards(now)
	cards[1].Suggest = "cxswap 2"
	var buf bytes.Buffer
	Render(&buf, cards, Options{Lang: En, Width: 80, Now: now})
	out := buf.String()
	for _, want := range []string{
		"Codex · #1 main <jora@x.com> · prolite  ● active\n\nWeekly limit\n",
		"76% used\nResets Oct 14, 5:41pm (UTC+8)\n",
		"Codex · #2 work <boss@x.com> · plus  ★ suggested: cxswap 2\n\n5-hour limit\n",
		"42% used\nResets 1:20pm (UTC+8)\n\nWeekly limit\n",
		"Codex · #3 <old@x.com>\n\nLogin expired",
		"From local session log · 3h ago",
		"No usage data for API key logins",
		"Usage unavailable (HTTP 500)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "\x1b[") {
		t.Error("escape codes without color")
	}
	bar := strings.Split(strings.Split(out, "Weekly limit\n")[1], "  ")[0]
	if n := len([]rune(bar)); n != 50 {
		t.Errorf("bar width %d: %q", n, bar)
	}
}

func TestRenderChineseAndColor(t *testing.T) {
	now := at(10, 8, 10, 0)
	var buf bytes.Buffer
	Render(&buf, sampleCards(now)[:1], Options{Lang: ZhTW, Width: 40, Color: true, Now: now})
	out := buf.String()
	for _, want := range []string{"● 使用中", "本週額度", "已用 76%", "10/14（三）17:41 重置（UTC+8）", "\x1b[1m"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestRenderUnsavedCard(t *testing.T) {
	var buf bytes.Buffer
	Render(&buf, []Card{{Provider: "Codex", Email: "new@x.com", Active: true, Unsaved: "cxswap add"}}, Options{Lang: En, Width: 80, Now: time.Now()})
	if !strings.Contains(buf.String(), "Codex · <new@x.com>  ● active  not saved, run `cxswap add`") {
		t.Fatalf("got %q", buf.String())
	}
}

func TestMarkSuggestions(t *testing.T) {
	now := at(10, 8, 10, 0)
	cards := sampleCards(now)
	MarkSuggestions(cards, now, func(c Card) string { return "cxswap " + string(rune('0'+c.Number)) })
	if cards[1].Suggest != "cxswap 2" {
		t.Fatalf("best inactive not suggested: %+v", cards[1])
	}
	for i, c := range cards {
		if i != 1 && c.Suggest != "" {
			t.Errorf("card %d unexpectedly suggested", c.Number)
		}
	}

	cards = sampleCards(now)
	cards[0].Usage.Windows[0].UsedPercent = 5
	MarkSuggestions(cards, now, func(Card) string { return "x" })
	for _, c := range cards {
		if c.Suggest != "" {
			t.Errorf("active account already best, but %d suggested", c.Number)
		}
	}
}

func TestStateFromError(t *testing.T) {
	expired, noUsage := errors.New("expired"), errors.New("nousage")
	classify := func(err error) (State, string) {
		return Classify(err, map[error]State{expired: LoginExpired, noUsage: NoUsage})
	}
	if s, _ := classify(nil); s != OK {
		t.Error("nil")
	}
	if s, _ := classify(expired); s != LoginExpired {
		t.Error("expired")
	}
	if s, d := classify(errors.New("HTTP 500")); s != Unavailable || d != "HTTP 500" {
		t.Errorf("other: %v %q", s, d)
	}
}

func TestUsageColumnsAligned(t *testing.T) {
	for _, l := range []Lang{En, ZhTW} {
		col := -1
		for _, line := range strings.Split(l.T("usage", "cxswap"), "\n") {
			if !strings.HasPrefix(line, "  cxswap") || strings.HasSuffix(line, "version") {
				continue
			}
			i := strings.LastIndex(line, "  ")
			w := displayWidth(line[:i+2])
			if col == -1 {
				col = w
			} else if w != col {
				t.Errorf("lang %d: %q starts at column %d, want %d", l, line, w, col)
			}
		}
	}
}

func TestRenderList(t *testing.T) {
	cards := []Card{
		{Provider: "Codex", Number: 1, Alias: "工作", Email: "jora@x.com", Plan: "prolite", Active: true},
		{Provider: "Codex", Number: 2, Email: "boss@x.com", Plan: "plus"},
		{Provider: "Codex", Email: "new@x.com", Unsaved: "cxswap add"},
	}
	cmd := func(c Card) string { return "cxswap " + strconv.Itoa(c.Number) }
	var buf bytes.Buffer
	List(&buf, "Codex", cards, cmd, Options{Lang: En, Width: 80})
	want := strings.Join([]string{
		"Codex accounts",
		"",
		"  #  Account            Plan     Switch with",
		"  1  工作 <jora@x.com>  prolite  ● active",
		"  2  <boss@x.com>       plus     cxswap 2",
		"  -  <new@x.com>                 not saved, run `cxswap add`",
		"",
	}, "\n")
	if buf.String() != want {
		t.Fatalf("got:\n%s\nwant:\n%s", buf.String(), want)
	}
	buf.Reset()
	List(&buf, "Codex", cards[:2], cmd, Options{Lang: ZhTW, Width: 80, Color: true})
	out := buf.String()
	for _, want := range []string{"Codex 帳號", "編號", "切換指令", "● 使用中", "\x1b["} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	buf.Reset()
	List(&buf, "Codex", cards[:2], cmd, Options{Lang: ZhTW, Width: 80})
	col := -1
	for _, line := range strings.Split(buf.String(), "\n")[2:5] {
		i := strings.LastIndex(line, "  ")
		if w := displayWidth(line[:i+2]); col == -1 {
			col = w
		} else if w != col {
			t.Errorf("%q: last column at %d, want %d", line, w, col)
		}
	}
}

func TestDisplayWidth(t *testing.T) {
	for s, want := range map[string]int{"abc": 3, "工作": 4, "編號 1": 6, "（三）": 6} {
		if got := displayWidth(s); got != want {
			t.Errorf("displayWidth(%q) = %d, want %d", s, got, want)
		}
	}
}
