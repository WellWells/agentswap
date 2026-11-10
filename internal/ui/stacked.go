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
		for _, tag := range cardTags(c, o) {
			add(tag.text, tag.color)
		}
		if msg, code := cardMessage(c, o.Lang); msg != "" {
			add(msg, code)
			continue
		}
		for _, col := range cols {
			for _, win := range c.Usage.Windows {
				if win.Minutes != col.key.minutes || win.Label != col.key.label {
					continue
				}
				add(col.head, colorDim)
				pct := win.Percent(o.Now)
				text := padLeft(strconv.Itoa(pct)+"%", 4)
				if pct >= 80 {
					text = p.color(fillColor(pct), text)
				}
				bar := min(8, width-5)
				if bar > 0 {
					text = Bar(pct, bar, o.Color) + " " + text
				}
				when := resetsAt(win, o)
				if when != "" && visibleWidth(text)+2+displayWidth(when) <= width {
					text += "  " + p.color(colorDim, when)
					when = ""
				}
				if visibleWidth(text) <= width {
					lines = append(lines, indent+text)
				} else {
					add(strconv.Itoa(pct)+"%", fillColor(pct))
				}
				if when != "" {
					add(when, colorDim)
				}
				shown = shown || resetsAt(win, o) != ""
				break
			}
		}
	}
	return lines, shown
}
