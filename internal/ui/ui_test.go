package ui

import (
	"bytes"
	"errors"
	"slices"
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
		system []string
		want   Lang
	}{
		{nil, nil, En},
		{nil, []string{"zh-TW"}, ZhTW},
		{nil, []string{"zh-CN"}, ZhCN},
		{nil, []string{"zh-Hans-SG"}, ZhCN},
		{nil, []string{"zh-Hant-TW", "en-US"}, ZhTW},
		{nil, []string{"zh-HK"}, ZhTW},
		{nil, []string{"en-US"}, En},
		{nil, []string{"ja-JP"}, En},
		{nil, []string{"ja-JP", "zh-Hant-TW"}, ZhTW},
		{nil, []string{"fr-FR", "de-DE"}, En},
		{map[string]string{"LANG": "zh_TW.UTF-8"}, []string{"en-US"}, ZhTW},
		{map[string]string{"LANG": "zh_CN.UTF-8"}, []string{"zh-TW"}, ZhCN},
		{map[string]string{"LANG": "en_US.UTF-8"}, []string{"zh-TW"}, En},
		{map[string]string{"LANG": "C.UTF-8"}, []string{"zh-TW"}, ZhTW},
		{map[string]string{"LANG": "ja_JP.UTF-8"}, []string{"zh-TW"}, ZhTW},
		{map[string]string{"LANGUAGE": "ja:zh_CN:en"}, nil, ZhCN},
		{map[string]string{"LC_ALL": "en_GB", "LANG": "zh_TW"}, nil, En},
		{map[string]string{"AGENTSWAP_LANG": "zh-TW", "LC_ALL": "en_US"}, nil, ZhTW},
		{map[string]string{"AGENTSWAP_LANG": "zhc"}, []string{"zh-TW"}, ZhCN},
		{map[string]string{"AGENTSWAP_LANG": "zht"}, []string{"zh-CN"}, ZhTW},
		{map[string]string{"AGENTSWAP_LANG": "en"}, []string{"zh-TW"}, En},
	}
	for _, c := range cases {
		system := c.system
		if got := DetectLang(env(c.env), func() []string { return system }); got != c.want {
			t.Errorf("%v / %q: got %v want %v", c.env, c.system, got, c.want)
		}
	}
}

func TestDetectLangSkipsSystemWhenEnvDecides(t *testing.T) {
	called := false
	DetectLang(env(map[string]string{"LANG": "en_US.UTF-8"}), func() []string { called = true; return nil })
	if called {
		t.Error("system languages read although LANG decided")
	}
}

func TestParseLang(t *testing.T) {
	cases := map[string]Lang{"en": En, "EN": En, "zh-TW": ZhTW, "zh_tw": ZhTW, "zht": ZhTW, "ZHT": ZhTW, " zh-cn ": ZhCN, "ZH-CN": ZhCN, "zhc": ZhCN}
	for s, want := range cases {
		if got, ok := ParseLang(s); !ok || got != want {
			t.Errorf("%q: got %v %v want %v", s, got, ok, want)
		}
	}
	for _, s := range []string{"", "zh", "zh-HK", "zh-Hant", "zhs", "fr", "auto"} {
		if _, ok := ParseLang(s); ok {
			t.Errorf("%q should be rejected", s)
		}
	}
	for _, l := range Langs {
		if got, ok := ParseLang(l.Code()); !ok || got != l {
			t.Errorf("%s does not round-trip", l.Code())
		}
	}
	if ZhTW.Codes() != "zh-TW/zht" || En.Codes() != "en" {
		t.Errorf("codes: %q %q", ZhTW.Codes(), En.Codes())
	}
}

func TestSystemLanguageParsers(t *testing.T) {
	buf := []uint16{}
	for _, s := range []string{"zh-TW", "en-US"} {
		for _, r := range s {
			buf = append(buf, uint16(r))
		}
		buf = append(buf, 0)
	}
	buf = append(buf, 0)
	if got := strings.Join(splitMultiSZ(buf), ","); got != "zh-TW,en-US" {
		t.Errorf("multi-sz: %q", got)
	}
	apple := "(\n    \"zh-Hant-TW\",\n    en\n)\n"
	if got := strings.Join(parseAppleLanguages(apple), ","); got != "zh-Hant-TW,en" {
		t.Errorf("apple: %q", got)
	}
	conf := "# comment\nLANG=\"zh_CN.UTF-8\"\nexport LANGUAGE=ja:en\n"
	if got := strings.Join(parseLocaleConf(conf), ","); got != "ja,en,zh_CN.UTF-8" {
		t.Errorf("locale.conf: %q", got)
	}
}

func TestMessagesExistInEveryLanguage(t *testing.T) {
	for key, m := range messages {
		for _, l := range Langs {
			if m[l] == "" {
				t.Errorf("%s missing %s", key, l.Code())
			}
		}
	}
	if ZhCN.T("week") != "本周额度" {
		t.Fatalf("got %q", ZhCN.T("week"))
	}
	if En.T("zoneNote", "UTC+8") != "Times show when each limit resets (UTC+8)" || ZhTW.T("zoneNote", "UTC+8") != "時間為各額度的重置時間（UTC+8）" {
		t.Fatalf("got %q / %q", En.T("zoneNote", "UTC+8"), ZhTW.T("zoneNote", "UTC+8"))
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
	for _, tz := range []string{"UTC", "EST5EDT"} {
		if got := DetectZone(env(map[string]string{"TZ": tz})); got != "" {
			t.Fatalf("%s: got %q", tz, got)
		}
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

func TestResetTime(t *testing.T) {
	now := at(10, 8, 10, 0)
	cases := []struct {
		lang Lang
		t    time.Time
		want string
	}{
		{En, at(10, 8, 15, 20), "3:20pm"},
		{En, at(10, 8, 15, 0), "3pm"},
		{En, at(10, 14, 17, 41), "Oct 14 5:41pm"},
		{ZhTW, at(10, 8, 15, 20), "今天 15:20"},
		{ZhCN, at(10, 14, 17, 41), "10/14 17:41"},
		{ZhTW, at(10, 14, 17, 41), "10/14 17:41"},
	}
	for _, c := range cases {
		if got := ResetTime(c.lang, c.t, now); got != c.want {
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

func TestRenderTable(t *testing.T) {
	now := at(10, 8, 10, 0)
	cards := sampleCards(now)
	cards[1].Suggest = "cxswap 2"
	var buf bytes.Buffer
	Render(&buf, cards, Options{Lang: En, Width: 140, Now: now})
	body := []string{
		"Account                         5-hour limit           Weekly limit",
		"#1 main <jora@x.com> · prolite  —                      ██████▏░  76%  Oct 14 5:41pm  ● active",
		"#2 work <boss@x.com> · plus     ███▍░░░░  42%  1:20pm  ▊░░░░░░░  10%  Oct 15 9:12am  ★ suggested: cxswap 2",
		"#3 <old@x.com>                  Login expired, log in to this account again",
		"#4 <cache@x.com>                ▍░░░░░░░   5%          —                             From local session log · 3h ago",
		"#5 <key@x.com>                  No usage data for API key logins",
		"#6 <down@x.com>                 Usage unavailable (HTTP 500)",
	}
	width := 0
	for _, line := range body {
		width = max(width, displayWidth(line))
	}
	want := "Codex " + strings.Repeat("─", width-6) + "\n" + strings.Join(body, "\n") + "\n" + strings.Repeat("─", width) + "\nTimes show when each limit resets (UTC+8)\n"
	if buf.String() != want {
		t.Fatalf("got:\n%s\nwant:\n%s", buf.String(), want)
	}
}

func TestRenderAlignsProviders(t *testing.T) {
	now := at(10, 8, 10, 0)
	cards := []Card{
		{Provider: "Claude Code", Number: 1, Email: "longer.name@example.com", Plan: "max", Active: true, Usage: swap.Usage{Live: true, Windows: []swap.Window{
			{UsedPercent: 12, Minutes: 10080, Label: "Fable", ResetsAt: at(10, 9, 0, 0)},
			{UsedPercent: 48, Minutes: 10080, ResetsAt: at(10, 9, 0, 0)},
			{UsedPercent: 2, Minutes: 300, ResetsAt: at(10, 8, 13, 0)},
		}}},
		{Provider: "Codex", Number: 1, Email: "a@x.com", Plan: "plus", Active: true, Usage: swap.Usage{Live: true, Windows: []swap.Window{
			{UsedPercent: 10, Minutes: 10080, ResetsAt: at(10, 15, 9, 0)},
			{UsedPercent: 42, Minutes: 300, ResetsAt: at(10, 8, 13, 20)},
		}}},
	}
	var buf bytes.Buffer
	Render(&buf, cards, Options{Lang: En, Width: 160, Now: now})
	lines := strings.Split(buf.String(), "\n")
	var heads []string
	for _, line := range lines {
		if strings.HasPrefix(line, "Account") {
			heads = append(heads, line)
		}
	}
	if len(heads) != 2 {
		t.Fatalf("want 2 headers:\n%s", buf.String())
	}
	for _, name := range []string{"5-hour limit", "Weekly limit"} {
		a, b := strings.Index(heads[0], name), strings.Index(heads[1], name)
		if a < 0 || a != b {
			t.Errorf("%s at %d and %d:\n%s", name, a, b, buf.String())
		}
	}
	if !strings.Contains(heads[0], "Weekly limit (Fable)") || strings.Contains(heads[1], "Fable") {
		t.Errorf("Fable column:\n%s", buf.String())
	}
	if strings.Index(heads[0], "5-hour") > strings.Index(heads[0], "Weekly limit") {
		t.Errorf("5-hour column should come first:\n%s", buf.String())
	}
}

func TestRenderLabeledColumnsAfterPlain(t *testing.T) {
	now := at(10, 8, 10, 0)
	cards := []Card{
		{Provider: "Codex", Number: 1, Email: "a@x.com", Active: true, Usage: swap.Usage{Live: true, Windows: []swap.Window{
			{UsedPercent: 42, Minutes: 300, ResetsAt: at(10, 8, 13, 20)},
			{UsedPercent: 10, Minutes: 10080, ResetsAt: at(10, 15, 9, 0)},
		}}},
		{Provider: "Claude Code", Number: 1, Email: "c@x.com", Active: true, Usage: swap.Usage{Live: true, Windows: []swap.Window{
			{UsedPercent: 5, Minutes: 300, ResetsAt: at(10, 8, 13, 0)},
			{UsedPercent: 6, Minutes: 10080, ResetsAt: at(10, 15, 9, 0)},
			{UsedPercent: 7, Minutes: 10080, Label: "Fable", ResetsAt: at(10, 15, 9, 0)},
		}}},
		{Provider: "Antigravity", Number: 1, Email: "b@x.com", Active: true, Usage: swap.Usage{Live: true, Windows: []swap.Window{
			{UsedPercent: 1, Minutes: 300, ResetsAt: at(10, 8, 13, 0)},
			{UsedPercent: 2, Minutes: 300, Label: "Claude and GPT", ResetsAt: at(10, 8, 13, 0)},
			{UsedPercent: 3, Minutes: 10080, ResetsAt: at(10, 15, 9, 0)},
			{UsedPercent: 4, Minutes: 10080, Label: "Claude and GPT", ResetsAt: at(10, 15, 9, 0)},
		}}},
	}
	var buf bytes.Buffer
	Render(&buf, cards, Options{Lang: En, Width: 200, Now: now})
	var heads []string
	for _, line := range strings.Split(buf.String(), "\n") {
		if strings.HasPrefix(line, "Account") {
			heads = append(heads, line)
		}
	}
	if len(heads) != 3 {
		t.Fatalf("want 3 headers:\n%s", buf.String())
	}
	h := heads[2]
	order := []int{
		strings.Index(h, "5-hour limit  "),
		strings.Index(h, "Weekly limit  "),
		strings.Index(h, "5-hour limit (Claude and GPT)"),
		strings.Index(h, "Weekly limit (Claude and GPT)"),
	}
	if order[0] < 0 || !slices.IsSorted(order) {
		t.Errorf("column order %v:\n%s", order, buf.String())
	}
	if a := strings.Index(heads[0], "Weekly limit"); a != order[1] {
		t.Errorf("Weekly limit at %d and %d:\n%s", a, order[1], buf.String())
	}
	if strings.Contains(heads[0], "Claude and GPT") || strings.Contains(heads[1], "Claude and GPT") {
		t.Errorf("labeled heads leak into other providers:\n%s", buf.String())
	}
	if a := strings.Index(heads[1], "Weekly limit (Fable)"); a != order[2] {
		t.Errorf("Fable at %d, first labeled column at %d:\n%s", a, order[2], buf.String())
	}
	var active []int
	for _, line := range strings.Split(buf.String(), "\n") {
		if i := strings.Index(line, "● active"); i >= 0 {
			active = append(active, displayWidth(line[:i]))
		}
	}
	if len(active) != 3 || active[0] != active[1] || active[1] != active[2] {
		t.Errorf("active tags at %v:\n%s", active, buf.String())
	}
}

func TestRenderTableFitsNarrowTerminal(t *testing.T) {
	now := at(10, 8, 10, 0)
	cards := sampleCards(now)
	cards[1].Suggest = "cxswap 2"
	var buf bytes.Buffer
	Render(&buf, cards, Options{Lang: En, Width: 80, Now: now})
	out := buf.String()
	for _, line := range strings.Split(out, "\n") {
		if displayWidth(line) > 80 && !strings.Contains(line, "From local session log") {
			t.Errorf("line wider than 80: %q", line)
		}
	}
	if !strings.Contains(out, "#2 work <boss@x.com> · …  ") {
		t.Errorf("account column not shortened to 24:\n%s", out)
	}
	if strings.Contains(out, "Oct 14") || strings.Contains(out, "Times show") {
		t.Errorf("reset times kept although they do not fit:\n%s", out)
	}
	if !strings.Contains(out, "76%") || !strings.Contains(out, "★ suggested: cxswap 2") {
		t.Errorf("usage or tags dropped:\n%s", out)
	}
}

func TestRenderSingleAccountChineseColor(t *testing.T) {
	now := at(10, 8, 10, 0)
	var buf bytes.Buffer
	Render(&buf, sampleCards(now)[:1], Options{Lang: ZhTW, Width: 100, Color: true, Now: now})
	out := buf.String()
	for _, want := range []string{"帳號", "● 使用中", "本週額度", "76%", "10/14 17:41", "時間為各額度的重置時間（UTC+8）", "\x1b[1m"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestRenderUnsavedCard(t *testing.T) {
	var buf bytes.Buffer
	Render(&buf, []Card{{Provider: "Codex", Email: "new@x.com", Active: true, Unsaved: "cxswap add"}}, Options{Lang: En, Width: 80, Now: time.Now()})
	if !strings.Contains(buf.String(), "<new@x.com>  No usage data  ● active  not saved, run `cxswap add`") {
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
	for _, l := range Langs {
		for _, key := range []string{"usage", "usageClaude", "usageAntigravity"} {
			checkUsageColumns(t, l, l.T(key, "cxswap"), "  cxswap")
		}
		checkUsageColumns(t, l, l.T("usageAgentswap"), "  agentswap")
	}
}

func checkUsageColumns(t *testing.T, l Lang, text, prefix string) {
	t.Helper()
	col := -1
	lines := 0
	for _, line := range strings.Split(text, "\n") {
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		lines++
		i := strings.LastIndex(line, "  ")
		w := displayWidth(line[:i+2])
		if col == -1 {
			col = w
		} else if w != col {
			t.Errorf("lang %d: %q starts at column %d, want %d", l, line, w, col)
		}
	}
	if lines < 4 {
		t.Errorf("lang %d: only %d command lines in %q", l, lines, text)
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

func TestModelWindowTitleAndHeadroom(t *testing.T) {
	if got := En.WindowTitle(7*24*60, "Sonnet"); got != En.WindowName(7*24*60)+" (Sonnet)" {
		t.Fatalf("en: %q", got)
	}
	if got := ZhTW.WindowTitle(7*24*60, "Opus"); got != ZhTW.WindowName(7*24*60)+"（Opus）" {
		t.Fatalf("zh: %q", got)
	}
	if got := En.WindowTitle(300, ""); got != En.WindowName(300) {
		t.Fatalf("plain: %q", got)
	}
	now := time.Unix(1_800_000_000, 0)
	c := Card{State: OK, Usage: swap.Usage{Live: true, Windows: []swap.Window{
		{UsedPercent: 20, Minutes: 300},
		{UsedPercent: 30, Minutes: 7 * 24 * 60},
		{UsedPercent: 99, Minutes: 7 * 24 * 60, Label: "Opus"},
	}}}
	if room, ok := headroom(c, now); !ok || room != 70 {
		t.Fatalf("headroom = %d %v", room, ok)
	}
	var b strings.Builder
	Render(&b, []Card{c}, Options{Lang: En, Width: 80, Now: now})
	if !strings.Contains(b.String(), "(Opus)") {
		t.Fatalf("render: %s", b.String())
	}
}

func TestLangListNumbersAndMarksCurrent(t *testing.T) {
	var buf bytes.Buffer
	LangList(&buf, ZhTW, false)
	want := "  1  en         English\n  2  zh-TW/zht  正體中文  ● 使用中\n  3  zh-CN/zhc  简体中文\n"
	if buf.String() != want {
		t.Errorf("got\n%s\nwant\n%s", buf.String(), want)
	}
}
