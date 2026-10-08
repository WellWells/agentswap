package ui

import (
	"errors"
	"fmt"
	"io"
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

var zhWeekdays = []string{"日", "一", "二", "三", "四", "五", "六"}

func ResetLine(l Lang, t, now time.Time) string {
	t = t.In(now.Location())
	y1, m1, d1 := t.Date()
	y2, m2, d2 := now.Date()
	today := y1 == y2 && m1 == m2 && d1 == d2
	if l != En {
		clock := t.Format("15:04")
		if today {
			return "今天 " + clock + " 重置"
		}
		return t.Format("01/02") + "（" + zhWeekdays[t.Weekday()] + "）" + clock + " 重置"
	}
	clock := t.Format("3:04pm")
	if t.Minute() == 0 {
		clock = t.Format("3pm")
	}
	if today {
		return "Resets " + clock
	}
	return "Resets " + t.Format("Jan 2") + ", " + clock
}

const (
	minCardWidth = 40
	maxCardWidth = 60
	gutter       = " │ "
)

func grid(n, total int) (int, int) {
	cols := (total + 3) / (minCardWidth + 3)
	cols = max(1, min(cols, n))
	width := (total - 3*(cols-1)) / cols
	return cols, max(24, min(width, maxCardWidth))
}

func Render(w io.Writer, cards []Card, o Options) {
	p := painter(o.Color)
	resets := false
	for i, group := range byProvider(cards) {
		if i > 0 {
			fmt.Fprintln(w)
		}
		cols, width := grid(len(group), o.Width)
		fmt.Fprintln(w, rule(p, group[0].Provider, cols, width, "┬"))
		for r := 0; r < len(group); r += cols {
			if r > 0 {
				fmt.Fprintln(w, rule(p, "", cols, width, "┼"))
			}
			row := group[r:min(r+cols, len(group))]
			heads, bodies := make([][]string, len(row)), make([][]string, len(row))
			top := 0
			for j, c := range row {
				var rs bool
				heads[j], bodies[j], rs = block(c, width, o, p)
				resets = resets || rs
				top = max(top, len(heads[j]))
			}
			blocks := make([][]string, cols)
			height := 0
			for j := range row {
				for len(heads[j]) < top {
					heads[j] = append(heads[j], "")
				}
				blocks[j] = append(append(heads[j], ""), bodies[j]...)
				height = max(height, len(blocks[j]))
			}
			for y := 0; y < height; y++ {
				cells := make([]string, cols)
				for j, b := range blocks {
					if y < len(b) {
						cells[j] = b[y]
					}
					cells[j] = padVisible(cells[j], width)
				}
				fmt.Fprintln(w, strings.TrimRight(strings.Join(cells, p.color(colorDim, gutter)), " "))
			}
		}
		fmt.Fprintln(w, rule(p, "", cols, width, "┴"))
	}
	if resets {
		fmt.Fprintln(w, p.color(colorDim, o.Lang.T("zoneNote", ZoneLabel(o.Zone, o.Now))))
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

func rule(p painter, title string, cols, width int, joint string) string {
	segs := make([]string, cols)
	for i := range segs {
		segs[i] = strings.Repeat("─", width)
	}
	line := []rune(strings.Join(segs, "─"+joint+"─"))
	if title == "" {
		return p.color(colorDim, string(line))
	}
	n := min(displayWidth(title)+1, len(line))
	return p.bold(title) + " " + p.color(colorDim, string(line[n:]))
}

func block(c Card, width int, o Options, p painter) ([]string, []string, bool) {
	l := o.Lang
	title := truncate(cardTitle(c), width)
	head := []string{p.bold(title)}
	var badges []string
	if c.Active {
		badges = append(badges, p.color(colorActive, l.T("active")))
	}
	if c.Suggest != "" {
		badges = append(badges, p.color(colorWarn, l.T("suggest", c.Suggest)))
	}
	if c.Unsaved != "" {
		badges = append(badges, p.color(colorDim, l.T("unsaved", c.Unsaved)))
	}
	joined := strings.Join(badges, "  ")
	switch gap := width - displayWidth(title) - visibleWidth(joined); {
	case joined == "":
	case gap >= 2:
		head[0] += strings.Repeat(" ", gap) + joined
	case visibleWidth(joined) <= width:
		head = append(head, joined)
	default:
		head = append(head, badges...)
	}
	var lines []string
	note := func(code int, s string) []string { return append(lines, p.color(code, truncate(s, width))) }
	switch c.State {
	case LoginExpired:
		return head, note(colorCrit, l.T("loginExpired")), false
	case NoUsage:
		return head, note(colorDim, l.T("noUsage")), false
	case Unavailable:
		return head, note(colorDim, l.T("unavailable", c.Detail)), false
	}
	if len(c.Usage.Windows) == 0 {
		lines = note(colorDim, l.T("noData"))
	}
	resets := false
	pctWidth := displayWidth(l.T("used", 100))
	for j, win := range c.Usage.Windows {
		if j > 0 {
			lines = append(lines, "")
		}
		pct := win.Percent(o.Now)
		name := truncate(l.WindowTitle(win.Minutes, win.Label), width)
		label := p.bold(name)
		if !win.ResetsAt.IsZero() && win.ResetsAt.After(o.Now) {
			resets = true
			when := ResetLine(l, win.ResetsAt, o.Now)
			if gap := width - displayWidth(name) - displayWidth(when); gap >= 2 {
				label += strings.Repeat(" ", gap) + p.color(colorDim, when)
			} else {
				lines = append(lines, label)
				label = p.color(colorDim, truncate(when, width))
			}
		}
		used := l.T("used", pct)
		if pct >= 80 {
			used = p.color(fillColor(pct), used)
		}
		lines = append(lines, label, Bar(pct, width-pctWidth-2, o.Color)+"  "+padLeft(used, pctWidth))
	}
	if !c.Usage.Live && !c.Usage.At.IsZero() {
		lines = append(lines, "")
		lines = note(colorDim, l.T("fallback", l.Ago(o.Now.Sub(c.Usage.At))))
	}
	return head, lines, resets
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
	return h
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
