package ui

import (
	"fmt"
	"io"
	"strconv"
	"strings"
)

func displayWidth(s string) int {
	w := 0
	for _, r := range s {
		if r >= 0x1100 && (r <= 0x115f || (r >= 0x2e80 && r <= 0xa4cf) || (r >= 0xac00 && r <= 0xd7a3) || (r >= 0xf900 && r <= 0xfaff) || (r >= 0xfe30 && r <= 0xfe4f) || (r >= 0xff00 && r <= 0xff60) || (r >= 0xffe0 && r <= 0xffe6) || (r >= 0x20000 && r <= 0x3fffd)) {
			w += 2
		} else {
			w++
		}
	}
	return w
}

func pad(s string, width int) string {
	if n := width - displayWidth(s); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}

func List(w io.Writer, provider string, cards []Card, cmd func(Card) string, o Options) {
	p := painter(o.Color)
	l := o.Lang
	type row struct {
		cells  [3]string
		action string
		color  int
	}
	rows := []row{{cells: [3]string{l.T("colNumber"), l.T("colAccount"), l.T("colPlan")}, action: l.T("colSwitch"), color: colorDim}}
	for _, c := range cards {
		r := row{cells: [3]string{"-", "<" + c.Email + ">", c.Plan}}
		if c.Number > 0 {
			r.cells[0] = strconv.Itoa(c.Number)
		}
		if c.Alias != "" {
			r.cells[1] = c.Alias + " " + r.cells[1]
		}
		switch {
		case c.Unsaved != "":
			r.action, r.color = l.T("unsaved", c.Unsaved), colorDim
		case c.Active:
			r.action, r.color = l.T("active"), colorActive
		default:
			r.action = cmd(c)
		}
		rows = append(rows, r)
	}
	var widths [3]int
	for _, r := range rows {
		for i, cell := range r.cells {
			if n := displayWidth(cell); n > widths[i] {
				widths[i] = n
			}
		}
	}
	fmt.Fprintln(w, p.bold(l.T("listTitle", provider)))
	fmt.Fprintln(w)
	for i, r := range rows {
		line := "  "
		for j, cell := range r.cells {
			line += pad(cell, widths[j]) + "  "
		}
		if i == 0 {
			fmt.Fprintln(w, p.color(colorDim, line+r.action))
			continue
		}
		action := r.action
		if r.color != 0 {
			action = p.color(r.color, action)
		}
		fmt.Fprintln(w, line+action)
	}
}

func LangList(w io.Writer, cur Lang, color bool) {
	p := painter(color)
	numW, codeW, nameW := len(strconv.Itoa(len(Langs))), 0, 0
	for _, l := range Langs {
		codeW = max(codeW, displayWidth(l.Codes()))
		nameW = max(nameW, displayWidth(l.Name()))
	}
	for i, l := range Langs {
		line := "  " + pad(strconv.Itoa(i+1), numW) + "  " + pad(l.Codes(), codeW) + "  "
		if l == cur {
			fmt.Fprintln(w, line+pad(l.Name(), nameW)+"  "+p.color(colorActive, cur.T("active")))
		} else {
			fmt.Fprintln(w, line+l.Name())
		}
	}
}
