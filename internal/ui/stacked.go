package ui

import (
	"strconv"
	"strings"
)

func cardMessage(c Card, l Lang) (string, int) {
	switch c.State {
	case LoginExpired:
		return l.T("loginExpired"), colorCrit
	case NoUsage:
		return l.T("noUsage"), colorDim
	case Unavailable:
		return l.T("unavailable", c.Detail), colorDim
	default:
		if len(c.Usage.Windows) == 0 {
			return l.T("noData"), colorDim
		}
	}
	return "", colorDim
}

func wrapText(text string, width int) []string {
	width = max(2, width)
	var lines []string
	for displayWidth(text) > width {
		cut, space, cells := 0, 0, 0
		for i, r := range text {
			cells += displayWidth(string(r))
			if cells > width {
				cut = i
				break
			}
			if r == ' ' {
				space = i
			}
		}
		if space > 0 {
			cut = space
		}
		lines = append(lines, strings.TrimRight(text[:cut], " "))
		text = strings.TrimLeft(text[cut:], " ")
	}
	if text != "" {
		lines = append(lines, text)
	}
	return lines
}

func stacked(group []Card, cols []column, o Options, p painter) ([]string, bool) {
	indent := strings.Repeat(" ", min(tagIndent, max(0, o.Width-16)))
	width := o.Width - len(indent)
	var lines []string
	add := func(text string, color int) {
		for _, line := range wrapText(text, width) {
			lines = append(lines, indent+p.color(color, line))
		}
	}
	shown := false
	for i, c := range group {
		if i > 0 {
			lines = append(lines, "")
		}
		for _, line := range wrapText(cardTitle(c), o.Width) {
			lines = append(lines, p.bold(line))
		}
		if c.Active {
			add(c.activeTag(o.Lang), colorActive)
		}
		for _, tag := range cardTags(c, o) {
			add(tag.text, tag.color)
		}
		if c.staleReading() {
			add(o.Lang.T("staleReading"), colorWarn)
		}
		if msg, code := cardMessage(c, o.Lang); msg != "" {
			add(msg, code)
			continue
		}
		for _, col := range cols {
			win := c.window(col.key)
			if win == nil {
				continue
			}
			add(col.head, colorDim)
			pct := displayedPercent(c, *win, o.Now)
			text := usageMeter(pct, min(8, width-5), o.Color)
			if visibleWidth(text) <= width {
				lines = append(lines, indent+text)
			} else {
				add(strconv.Itoa(pct)+"%", fillColor(pct))
			}
			if when := resetsAt(*win, o); when != "" {
				add(when, colorDim)
				shown = true
			}
		}
	}
	return lines, shown
}
