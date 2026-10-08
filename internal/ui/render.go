package ui

import (
	"cmp"
	"errors"
	"fmt"
	"io"
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
	Provider string
	Number   int
	Alias    string
	Email    string
	Plan     string
	Active   bool
	Suggest  string
	Unsaved  string
	Usage    swap.Usage
	State    State
	Detail   string
}

type Options struct {
	Lang  Lang
	Color bool
	Width int
	Now   time.Time
	Zone  string
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
	p := painter(o.Color)
	l := o.Lang
	cols := columns(cards, o)
	nameWidth := displayWidth(l.T("colAccount"))
	tagWidth := 0
	for _, c := range cards {
		nameWidth = max(nameWidth, displayWidth(cardTitle(c)))
		if t := rowTags(c, o, p); t != "" {
			tagWidth = max(tagWidth, visibleWidth(t)+2)
		}
	}
	layout := func(withResets bool) int {
		for i := range cols {
			cols[i].width = usageWidth
			if withResets && cols[i].reset > 0 {
				cols[i].width += 2 + cols[i].reset
			}
			cols[i].width = max(cols[i].width, displayWidth(cols[i].head))
		}
		cells := 0
		for _, group := range byProvider(cards) {
			cells = max(cells, cellsWidth(sectionColumns(group, cols)))
		}
		return cells
	}
	cells := layout(true)
	showResets := true
	if min(nameWidth, 24)+cells+tagWidth > o.Width {
		cells, showResets = layout(false), false
	}
	nameWidth = min(nameWidth, max(24, o.Width-cells-tagWidth))

	var sections [][]string
	resets := false
	for _, group := range byProvider(cards) {
		lines, rs := table(group, sectionColumns(group, cols), nameWidth, cells, showResets, o, p)
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
		fmt.Fprintln(w, rule(p, byProvider(cards)[i][0].Provider, ruleWidth))
		for _, line := range lines {
			fmt.Fprintln(w, line)
		}
		fmt.Fprintln(w, rule(p, "", ruleWidth))
	}
	if resets {
		fmt.Fprintln(w, p.color(colorDim, l.T("zoneNote", ZoneLabel(o.Zone, o.Now))))
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
	width int
	reset int
}

const usageWidth = 13

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
		cols = append(cols, column{key: k, head: o.Lang.WindowTitle(k.minutes, k.label)})
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

func table(group []Card, cols []column, nameWidth, cells int, showResets bool, o Options, p painter) ([]string, bool) {
	l := o.Lang
	has := windowKeys(group)
	header := pad(l.T("colAccount"), nameWidth)
	for _, col := range cols {
		head := ""
		if slices.Contains(has, col.key) {
			head = col.head
		}
		header += "  " + pad(head, col.width)
	}
	lines := []string{p.color(colorDim, strings.TrimRight(header, " "))}
	shown := false
	for _, c := range group {
		line := pad(truncate(cardTitle(c), nameWidth), nameWidth)
		var msg string
		code := colorDim
		switch c.State {
		case LoginExpired:
			msg, code = l.T("loginExpired"), colorCrit
		case NoUsage:
			msg = l.T("noUsage")
		case Unavailable:
			msg = l.T("unavailable", c.Detail)
		default:
			if len(c.Usage.Windows) == 0 {
				msg = l.T("noData")
			}
		}
		if msg != "" {
			line += "  " + padVisible(p.color(code, truncate(msg, max(o.Width-nameWidth-2, 16))), cells-2)
		} else {
			for _, col := range cols {
				cell := ""
				if slices.Contains(has, col.key) {
					var rs bool
					cell, rs = usageCell(c, col, showResets, o, p)
					shown = shown || rs
				}
				line += "  " + padVisible(cell, col.width)
			}
			line = padVisible(line, nameWidth+cells)
		}
		if tags := rowTags(c, o, p); tags != "" {
			line += "  " + tags
		}
		lines = append(lines, strings.TrimRight(line, " "))
	}
	return lines, shown
}

func rowTags(c Card, o Options, p painter) string {
	l := o.Lang
	var tags []string
	if c.Active {
		tags = append(tags, p.color(colorActive, l.T("active")))
	}
	if c.Suggest != "" {
		tags = append(tags, p.color(colorWarn, l.T("suggest", c.Suggest)))
	}
	if c.Unsaved != "" {
		tags = append(tags, p.color(colorDim, l.T("unsaved", c.Unsaved)))
	}
	if c.State == OK && !c.Usage.Live && !c.Usage.At.IsZero() {
		tags = append(tags, p.color(colorDim, l.T("fallback", l.Ago(o.Now.Sub(c.Usage.At)))))
	}
	return strings.Join(tags, "  ")
}

func usageCell(c Card, col column, showResets bool, o Options, p painter) (string, bool) {
	for _, win := range c.Usage.Windows {
		if win.Minutes != col.key.minutes || win.Label != col.key.label {
			continue
		}
		pct := win.Percent(o.Now)
		text := padLeft(strconv.Itoa(pct)+"%", 4)
		if pct >= 80 {
			text = p.color(fillColor(pct), text)
		}
		bar := col.width - 5
		if !showResets || col.reset == 0 {
			return Bar(pct, bar, o.Color) + " " + text, false
		}
		when := resetsAt(win, o)
		cell := Bar(pct, bar-2-col.reset, o.Color) + " " + text + "  " + p.color(colorDim, when)
		return cell, when != ""
	}
	return p.color(colorDim, "—"), false
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
	if c.State != OK || !c.Usage.Live || len(c.Usage.Windows) == 0 {
		return 0, false
	}
	worst := 0
	for _, w := range c.Usage.Windows {
		if w.Label != "" {
			continue
		}
		if p := w.Percent(now); p > worst {
			worst = p
		}
	}
	return 100 - worst, true
}

func MarkSuggestions(cards []Card, now time.Time, cmd func(Card) string) {
	var order []string
	groups := map[string][]int{}
	for i, c := range cards {
		if _, ok := groups[c.Provider]; !ok {
			order = append(order, c.Provider)
		}
		groups[c.Provider] = append(groups[c.Provider], i)
	}
	for _, prov := range order {
		activeRoom, best, bestRoom := -1, -1, -1
		for _, i := range groups[prov] {
			room, ok := headroom(cards[i], now)
			if !ok {
				continue
			}
			if cards[i].Active {
				activeRoom = room
			} else if room > bestRoom {
				best, bestRoom = i, room
			}
		}
		if best >= 0 && bestRoom > activeRoom {
			cards[best].Suggest = cmd(cards[best])
		}
	}
}
