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

func ResetLine(l Lang, t, now time.Time, zone string) string {
	t = t.In(now.Location())
	label := ZoneLabel(zone, t)
	y1, m1, d1 := t.Date()
	y2, m2, d2 := now.Date()
	today := y1 == y2 && m1 == m2 && d1 == d2
	if l == ZhTW {
		clock := t.Format("15:04")
		if today {
			return "今天 " + clock + " 重置（" + label + "）"
		}
		return t.Format("01/02") + "（" + zhWeekdays[t.Weekday()] + "）" + clock + " 重置（" + label + "）"
	}
	clock := t.Format("3:04pm")
	if t.Minute() == 0 {
		clock = t.Format("3pm")
	}
	if today {
		return "Resets " + clock + " (" + label + ")"
	}
	return "Resets " + t.Format("Jan 2") + ", " + clock + " (" + label + ")"
}

func Render(w io.Writer, cards []Card, o Options) {
	p := painter(o.Color)
	barWidth := o.Width - 16
	if barWidth > 50 {
		barWidth = 50
	}
	if barWidth < 10 {
		barWidth = 10
	}
	for i, c := range cards {
		if i > 0 {
			fmt.Fprintln(w)
		}
		fmt.Fprintln(w, header(c, o.Lang, p))
		fmt.Fprintln(w)
		switch c.State {
		case LoginExpired:
			fmt.Fprintln(w, p.color(colorCrit, o.Lang.T("loginExpired")))
			continue
		case NoUsage:
			fmt.Fprintln(w, p.color(colorDim, o.Lang.T("noUsage")))
			continue
		case Unavailable:
			fmt.Fprintln(w, p.color(colorDim, o.Lang.T("unavailable", c.Detail)))
			continue
		}
		if len(c.Usage.Windows) == 0 {
			fmt.Fprintln(w, p.color(colorDim, o.Lang.T("noData")))
		}
		for j, win := range c.Usage.Windows {
			if j > 0 {
				fmt.Fprintln(w)
			}
			pct := win.Percent(o.Now)
			fmt.Fprintln(w, p.bold(o.Lang.WindowTitle(win.Minutes, win.Label)))
			fmt.Fprintln(w, Bar(pct, barWidth, o.Color)+"  "+o.Lang.T("used", pct))
			if !win.ResetsAt.IsZero() && win.ResetsAt.After(o.Now) {
				fmt.Fprintln(w, p.color(colorDim, ResetLine(o.Lang, win.ResetsAt, o.Now, o.Zone)))
			}
		}
		if !c.Usage.Live && !c.Usage.At.IsZero() {
			fmt.Fprintln(w)
			fmt.Fprintln(w, p.color(colorDim, o.Lang.T("fallback", o.Lang.Ago(o.Now.Sub(c.Usage.At)))))
		}
	}
}

func header(c Card, l Lang, p painter) string {
	h := c.Provider + " · "
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
	s := p.bold(h)
	if c.Active {
		s += "  " + p.color(colorActive, l.T("active"))
	}
	if c.Suggest != "" {
		s += "  " + p.color(colorWarn, l.T("suggest", c.Suggest))
	}
	if c.Unsaved != "" {
		s += "  " + p.color(colorDim, l.T("unsaved", c.Unsaved))
	}
	return s
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
