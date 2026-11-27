package ui

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/WellWells/agentswap/internal/swap"
)

const (
	colorFill   = 147
	colorWarn   = 220
	colorCrit   = 203
	colorTrack  = 238
	colorActive = 114
	colorDim    = 245
)

var eighths = []string{"", "▏", "▎", "▍", "▌", "▋", "▊", "▉"}

type State int

const (
	OK State = iota
	LoginExpired
	NoUsage
	Unavailable
)

type Card struct {
	Provider     string
	Number       int
	Alias        string
	Email        string
	Plan         string
	Active       bool
	LastSeen     bool
	PendingOwner bool
	Suggest      string
	Unsaved      string
	Usage        swap.Usage
	State        State
	Detail       string
	Cached       bool
	ShowCache    bool
	Stale        bool
	CheckedAt    time.Time
	RetryAt      time.Time
	ErrorCode    string
}

type Options struct {
	Lang  Lang
	Color bool
	Width int
	Now   time.Time
	Zone  string
	Hint  string
}

func Classify(err error, known map[error]State) (State, string) {
	if err == nil {
		return OK, ""
	}
	for e, s := range known {
		if errors.Is(err, e) {
			return s, ""
		}
	}
	return Unavailable, err.Error()
}

func fg(code int) string { return "\x1b[38;5;" + strconv.Itoa(code) + "m" }

const reset = "\x1b[0m"

type painter bool

func (p painter) paint(prefix, s string) string {
	if !p {
		return s
	}
	return prefix + s + reset
}

func (p painter) bold(s string) string            { return p.paint("\x1b[1m", s) }
func (p painter) color(code int, s string) string { return p.paint(fg(code), s) }

func fillColor(pct int) int {
	switch {
	case pct >= 95:
		return colorCrit
	case pct >= 80:
		return colorWarn
	default:
		return colorFill
	}
}

func Bar(pct, width int, color bool) string {
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	units := (pct*width*8 + 50) / 100
	full, part := units/8, units%8
	rest := width - full
	if part > 0 {
		rest--
	}
	if !color {
		return strings.Repeat("█", full) + eighths[part] + strings.Repeat("░", rest)
	}
	s := fg(fillColor(pct)) + strings.Repeat("█", full)
	if part > 0 {
		s += "\x1b[48;5;" + strconv.Itoa(colorTrack) + "m" + eighths[part]
	}
	s += reset
	if rest > 0 {
		s += fg(colorTrack) + strings.Repeat("█", rest) + reset
	}
	return s
}

func ResetTime(l Lang, t, now time.Time) string {
	t = t.In(now.Location())
	y1, m1, d1 := t.Date()
	y2, m2, d2 := now.Date()
	today := y1 == y2 && m1 == m2 && d1 == d2
	if l != En {
		if today {
			return "今天 " + t.Format("15:04")
		}
		return t.Format("01/02 15:04")
	}
	clock := t.Format("3:04pm")
	if t.Minute() == 0 {
		clock = t.Format("3pm")
	}
	if today {
		return clock
	}
	return t.Format("Jan 2") + " " + clock
}

func Render(w io.Writer, cards []Card, o Options) {
	if o.Width <= 0 {
		o.Width = 80
	}
	p := painter(o.Color)
	l := o.Lang
	groups := byProvider(cards)
	cols := columns(cards, o)
	nameWidth := displayWidth(l.T("colAccount"))
	statusWidth := 0
	tagWidth := 0
	for _, c := range cards {
		nameWidth = max(nameWidth, displayWidth(cardTitle(c)))
		if c.Active {
			statusWidth = max(statusWidth, displayWidth(c.activeTag(l))+1, displayWidth(l.T("colStatus"))+1)
		}
		if t := rowTags(c, o, p); t != "" {
			tagWidth = max(tagWidth, visibleWidth(t)+2)
		}
	}
	nameWidth += statusWidth
	size := func(lay layout) int {
		for i := range cols {
			cols[i].width = max(lay.bar+5, cols[i].reset, cols[i].headWidth(lay.split))
		}
		cells := 0
		for _, group := range groups {
			cells = max(cells, cellsWidth(sectionColumns(group, cols)))
		}
		return cells
	}
	chooseLayout := func(names, tags int) (layout, bool) {
		for _, c := range layouts {
			if names+size(c)+tags <= o.Width {
				return c, true
			}
		}
		return layouts[len(layouts)-1], false
	}
	lay, aligned := chooseLayout(nameWidth, tagWidth)
	preferredName := min(nameWidth, statusWidth+24)
	if !aligned {
		lay, aligned = chooseLayout(preferredName, tagWidth)
	}
	if !aligned {
		lay, _ = chooseLayout(preferredName, 0)
	}
	cells := size(lay)
	stack := o.Width < 60 || min(nameWidth, 16)+cells > o.Width || tagWidth+tagIndent-2 > o.Width
	available := o.Width - cells
	if aligned {
		available -= tagWidth
	}
	nameWidth = min(nameWidth, available)

	var sections [][]string
	resets, missing := false, false
	for _, group := range groups {
		sc := sectionColumns(group, cols)
		width := cells
		if !aligned {
			width = cellsWidth(sc)
		}
		var lines []string
		var rs bool
		if stack {
			lines, rs = stacked(group, sc, o, p)
		} else {
			var gaps bool
			lines, rs, gaps = table(group, sc, lay, statusWidth, nameWidth, width, o, p)
			missing = missing || gaps
		}
		sections = append(sections, lines)
		resets = resets || rs
	}
	ruleWidth := 0
	for _, lines := range sections {
		for _, line := range lines {
			ruleWidth = max(ruleWidth, visibleWidth(line))
		}
	}
	ruleWidth = min(ruleWidth, o.Width)
	for i, lines := range sections {
		if i > 0 {
			fmt.Fprintln(w)
		}
		fmt.Fprintln(w, rule(p, groups[i][0].Provider, ruleWidth))
		for _, line := range lines {
			fmt.Fprintln(w, line)
		}
		fmt.Fprintln(w, rule(p, "", ruleWidth))
	}
	footer := func(note string) {
		for _, line := range wrapText(note, o.Width) {
			fmt.Fprintln(w, p.color(colorDim, line))
		}
	}
	if resets {
		footer(l.T("zoneNote", ZoneLabel(o.Zone, o.Now)))
	}
	if missing {
		footer(l.T("missingNote"))
	}
	if o.Hint != "" {
		fmt.Fprintln(w)
		footer(o.Hint)
	}
}

func byProvider(cards []Card) [][]Card {
	var groups [][]Card
	for i, c := range cards {
		if i == 0 || c.Provider != cards[i-1].Provider {
			groups = append(groups, nil)
		}
		groups[len(groups)-1] = append(groups[len(groups)-1], c)
	}
	return groups
}

func rule(p painter, title string, width int) string {
	line := []rune(strings.Repeat("─", width))
	if title == "" {
		return p.color(colorDim, string(line))
	}
	if displayWidth(title)+1 >= width {
		return p.bold(truncate(title, width))
	}
	n := min(displayWidth(title)+1, len(line))
	return p.bold(title) + " " + p.color(colorDim, string(line[n:]))
}

type windowKey struct {
	minutes int
	label   string
}

type column struct {
	key   windowKey
	head  string
	name  string
	width int
	reset int
}

func (c column) headWidth(split bool) int {
	if split && c.key.label != "" {
		return max(displayWidth(c.name), displayWidth(c.key.label))
	}
	return displayWidth(c.head)
}

type layout struct {
	split bool
	bar   int
}

const tagIndent = 3

var layouts = []layout{
	{bar: 8},
	{split: true, bar: 8},
	{split: true, bar: 6},
	{split: true, bar: 4},
}

func resetsAt(win swap.Window, o Options) string {
	if win.ResetsAt.IsZero() || !win.ResetsAt.After(o.Now) {
		return ""
	}
	return ResetTime(o.Lang, win.ResetsAt, o.Now)
}

func windowKeys(cards []Card) []windowKey {
	var keys []windowKey
	for _, c := range cards {
		if c.State != OK {
			continue
		}
		for _, win := range c.Usage.Windows {
			if k := (windowKey{win.Minutes, win.Label}); !slices.Contains(keys, k) {
				keys = append(keys, k)
			}
		}
	}
	return keys
}

func columns(cards []Card, o Options) []column {
	var cols []column
	for _, k := range windowKeys(cards) {
		cols = append(cols, column{key: k, head: o.Lang.WindowTitle(k.minutes, k.label), name: o.Lang.WindowName(k.minutes)})
	}
	slices.SortFunc(cols, func(a, b column) int {
		if c := cmp.Compare(min(len(a.key.label), 1), min(len(b.key.label), 1)); c != 0 {
			return c
		}
		if c := cmp.Compare(a.key.minutes, b.key.minutes); c != 0 {
			return c
		}
		return strings.Compare(a.key.label, b.key.label)
	})
	for i := range cols {
		for _, c := range cards {
			for _, win := range c.Usage.Windows {
				if c.State == OK && win.Minutes == cols[i].key.minutes && win.Label == cols[i].key.label {
					cols[i].reset = max(cols[i].reset, displayWidth(resetsAt(win, o)))
				}
			}
		}
	}
	return cols
}

func sectionColumns(group []Card, cols []column) []column {
	has := windowKeys(group)
	var out []column
	for _, col := range cols {
		if col.key.label == "" || slices.Contains(has, col.key) {
			out = append(out, col)
		}
	}
	return out
}

func cellsWidth(cols []column) int {
	n := 0
	for _, col := range cols {
		n += col.width + 2
	}
	return n
}

func table(group []Card, cols []column, lay layout, statusWidth, nameWidth, cells int, o Options, p painter) ([]string, bool, bool) {
	l := o.Lang
	has := windowKeys(group)
	accountWidth := nameWidth - statusWidth
	header := ""
	if statusWidth > 0 {
		header = pad(l.T("colStatus"), statusWidth)
	}
	header += pad(l.T("colAccount"), accountWidth)
	sub := strings.Repeat(" ", nameWidth)
	for _, col := range cols {
		head, label := "", ""
		if slices.Contains(has, col.key) {
			head = col.head
			if lay.split && col.key.label != "" {
				head, label = col.name, col.key.label
			}
		}
		header += "  " + pad(head, col.width)
		sub += "  " + pad(label, col.width)
	}
	lines := []string{p.color(colorDim, strings.TrimRight(header, " "))}
	if sub = strings.TrimRight(sub, " "); sub != "" {
		lines = append(lines, p.color(colorDim, sub))
	}
	shown, missing := false, false
	for _, c := range group {
		status := ""
		if c.Active {
			status = p.color(colorActive, c.activeTag(l))
		}
		line := padVisible(status, statusWidth) + pad(truncate(cardTitle(c), accountWidth), accountWidth)
		times := strings.Repeat(" ", nameWidth)
		msg, code := cardMessage(c, l)
		if msg != "" {
			if nameWidth+2+displayWidth(msg) <= o.Width {
				line += "  " + padVisible(p.color(code, msg), cells-2)
				msg = ""
			}
		} else {
			for _, col := range cols {
				cell, when := "", ""
				if slices.Contains(has, col.key) {
					if win := c.window(col.key); win != nil {
						cell = usageMeter(displayedPercent(c, *win, o.Now), lay.bar, o.Color)
						when = resetsAt(*win, o)
					} else {
						cell = p.color(colorDim, "—")
						missing = true
					}
				}
				line += "  " + padVisible(cell, col.width)
				times += "  " + pad(when, col.width)
			}
			line = padVisible(line, nameWidth+cells)
		}
		tags := rowTags(c, o, p)
		if tags != "" && visibleWidth(line)+2+visibleWidth(tags) <= o.Width {
			line, tags = line+"  "+tags, ""
		}
		lines = append(lines, strings.TrimRight(line, " "))
		for _, part := range wrapText(msg, o.Width-tagIndent) {
			lines = append(lines, strings.Repeat(" ", tagIndent)+p.color(code, part))
		}
		if times = strings.TrimRight(times, " "); times != "" {
			lines = append(lines, p.color(colorDim, times))
			shown = true
		}
		if tags != "" {
			lines = append(lines, strings.Repeat(" ", tagIndent)+tags)
		}
		if c.staleReading() {
			lines = append(lines, strings.Repeat(" ", tagIndent)+p.color(colorWarn, l.T("staleReading")))
		}
	}
	return lines, shown, missing
}

func (c Card) window(key windowKey) *swap.Window {
	for i := range c.Usage.Windows {
		win := &c.Usage.Windows[i]
		if win.Minutes == key.minutes && win.Label == key.label {
			return win
		}
	}
	return nil
}

func rowTags(c Card, o Options, p painter) string {
	var tags []string
	for _, tag := range cardTags(c, o) {
		tags = append(tags, p.color(tag.color, tag.text))
	}
	return strings.Join(tags, "  ")
}

type cardTag struct {
	text  string
	color int
}

func (c Card) activeTag(l Lang) string {
	if c.LastSeen {
		return l.T("lastSeen")
	}
	return l.T("active")
}

func (c Card) staleReading() bool {
	return c.Stale && !c.Cached && c.Usage.Live && len(c.Usage.Windows) > 0
}

func cardTags(c Card, o Options) []cardTag {
	l := o.Lang
	var tags []cardTag
	if c.PendingOwner {
		tags = append(tags, cardTag{l.T("pendingOwner"), colorWarn})
	}
	if c.Suggest != "" {
		tags = append(tags, cardTag{l.T("suggest", c.Suggest), colorWarn})
	}
	if c.Unsaved != "" {
		tags = append(tags, cardTag{l.T("unsaved", c.Unsaved), colorDim})
	}
	if c.Cached && (c.ShowCache || !c.Usage.Live) && !c.Usage.At.IsZero() {
		tags = append(tags, cardTag{l.T("cacheAge", l.Ago(o.Now.Sub(c.Usage.At))), colorDim})
	} else if c.State == OK && !c.Usage.Live && !c.Usage.At.IsZero() {
		tags = append(tags, cardTag{l.T("fallback", l.Ago(o.Now.Sub(c.Usage.At))), colorDim})
	}
	if c.Cached && c.ErrorCode != "" && len(c.Usage.Windows) > 0 {
		tags = append(tags, cardTag{l.T("usageReason_" + c.ErrorCode), colorWarn})
	}
	if c.RetryAt.After(o.Now) {
		tags = append(tags, cardTag{l.T("retryAt", ResetTime(l, c.RetryAt, o.Now)), colorDim})
	}
	return tags
}

func usageMeter(pct, bar int, color bool) string {
	text := padLeft(strconv.Itoa(pct)+"%", 4)
	if pct >= 80 {
		text = painter(color).color(fillColor(pct), text)
	}
	if bar <= 0 {
		return text
	}
	return Bar(pct, bar, color) + " " + text
}

func cardTitle(c Card) string {
	h := ""
	if c.Number > 0 {
		h += "#" + strconv.Itoa(c.Number) + " "
	}
	if c.Alias != "" {
		h += c.Alias + " "
	}
	h += "<" + c.Email + ">"
	if c.Plan != "" {
		h += " · " + c.Plan
	}
	return Clean(h)
}

func visibleWidth(s string) int {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b {
			for i < len(s) && s[i] != 'm' {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return displayWidth(b.String())
}

func padVisible(s string, width int) string {
	if n := width - visibleWidth(s); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}

func padLeft(s string, width int) string {
	if n := width - visibleWidth(s); n > 0 {
		return strings.Repeat(" ", n) + s
	}
	return s
}

func truncate(s string, width int) string {
	if displayWidth(s) <= width {
		return s
	}
	r := []rune(s)
	for len(r) > 0 && displayWidth(string(r))+1 > width {
		r = r[:len(r)-1]
	}
	return string(r) + "…"
}

func headroom(c Card, now time.Time) (int, bool) {
	if c.PendingOwner || c.State != OK || !c.Usage.Live {
		return 0, false
	}
	var score float64
	haveWindow := false
	for _, w := range c.Usage.Windows {
		if w.Label != "" {
			continue
		}
		remaining := float64(100 - min(100, max(0, w.Percent(now))))
		if w.Minutes > 0 && w.ResetsAt.After(now) {
			untilReset := w.ResetsAt.Sub(now).Minutes()
			elapsedFraction := max(0, 1-untilReset/float64(w.Minutes))
			remaining *= 1 + elapsedFraction
		}
		if !haveWindow || remaining < score {
			score = remaining
			haveWindow = true
		}
	}
	return int(math.Round(score)), haveWindow
}

func MarkSuggestions(cards []Card, now time.Time, cmd func(Card) string) {
	MarkModelSuggestions(cards, now, cmd, "")
}

func rankSuggestions(cards []Card, now time.Time, cmd func(Card) string) {
	var order []string
	groups := map[string][]int{}
	for i, c := range cards {
		if _, ok := groups[c.Provider]; !ok {
			order = append(order, c.Provider)
		}
		groups[c.Provider] = append(groups[c.Provider], i)
	}
	for _, prov := range order {
		best, bestScore := -1, 0
		for _, i := range groups[prov] {
			score, ok := headroom(cards[i], now)
			if !ok {
				continue
			}
			if score > bestScore || (score == bestScore && cards[i].Active) {
				best, bestScore = i, score
			}
		}
		if best >= 0 && !cards[best].Active {
			cards[best].Suggest = cmd(cards[best])
		}
	}
}
