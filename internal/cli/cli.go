package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/WellWells/agentswap/internal/codex"
	"github.com/WellWells/agentswap/internal/fsx"
	"github.com/WellWells/agentswap/internal/links"
	"github.com/WellWells/agentswap/internal/store"
	"github.com/WellWells/agentswap/internal/swap"
	"github.com/WellWells/agentswap/internal/ui"
)

type Env struct {
	Args    []string
	Stdout  io.Writer
	Stderr  io.Writer
	Getenv  func(string) string
	Home    string
	Exe     string
	Version string
	Now     func() time.Time
	Lang    ui.Lang
	Color   bool
	Width   int
	Zone    string
}

const usageTimeout = 10 * time.Second

func (e Env) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

func (e Env) dir(envVar, fallback string) string {
	if v := e.Getenv(envVar); v != "" {
		return v
	}
	return filepath.Join(e.Home, fallback)
}

type provider struct {
	name      string
	display   string
	supported bool
	login     string
	hint      string
	open      func(Env) swap.Provider
}

var providerOrder = []string{"codex", "claude"}

var providers = map[string]provider{
	"codex": {name: "codex", display: "Codex", supported: true, login: "codex login", hint: "hintCodex", open: func(e Env) swap.Provider {
		return codex.Provider{
			Home:       e.dir("CODEX_HOME", ".codex"),
			RefreshURL: e.Getenv("CODEX_REFRESH_TOKEN_URL_OVERRIDE"),
			UserAgent:  "agentswap/" + e.Version,
		}
	}},
	"claude": {name: "claude", display: "Claude Code"},
}

var commands = []struct{ name, provider string }{
	{"cxswap", "codex"}, {"codexswap", "codex"},
	{"ccswap", "claude"}, {"claudeswap", "claude"},
}

var aliases = map[string]string{
	"codex": "codex", "cx": "codex",
	"claude": "claude", "cc": "claude",
}

func lookup(s string) (string, bool) {
	for _, c := range commands {
		if c.name == s {
			return c.provider, true
		}
	}
	name, ok := aliases[s]
	return name, ok
}

func commandNames() []string {
	names := make([]string, len(commands))
	for i, c := range commands {
		names[i] = c.name
	}
	return names
}

func (e Env) manager(p provider) *swap.Manager {
	return &swap.Manager{P: p.open(e), S: store.Store{Dir: filepath.Join(e.dir("AGENTSWAP_HOME", ".agentswap"), p.name)}}
}

func isHelp(s string) bool    { return s == "help" || s == "-h" || s == "--help" }
func isVersion(s string) bool { return s == "version" || s == "--version" || s == "-v" }

func Run(e Env) int {
	prog := strings.ToLower(filepath.Base(strings.ReplaceAll(e.Args[0], `\`, "/")))
	prog = strings.TrimSuffix(prog, ".exe")
	args := e.Args[1:]
	name, ok := lookup(prog)
	if !ok {
		switch {
		case len(args) == 0:
			return e.report("agentswap", provider{}, overview(e))
		case isHelp(args[0]):
			fmt.Fprint(e.Stdout, e.usage("agentswap <provider>"))
			return 0
		case isVersion(args[0]):
			printVersion(e)
			return 0
		case args[0] == "link":
			return e.report("agentswap", provider{}, link(e))
		case args[0] == "unlink":
			return e.report("agentswap", provider{}, unlink(e))
		}
		if name, ok = lookup(strings.ToLower(args[0])); !ok {
			fmt.Fprintln(e.Stderr, e.Lang.T("unknownProvider", args[0]))
			return 2
		}
		prog = "agentswap " + name
		args = args[1:]
	}
	p := providers[name]
	if !p.supported {
		fmt.Fprintln(e.Stderr, e.Lang.T("notSupported", prog, p.name))
		return 2
	}
	return e.report(prog, p, dispatch(e, prog, p, e.manager(p), args))
}

type usageError string

func (u usageError) Error() string { return string(u) }

type queryError struct {
	query string
	err   error
}

func (q queryError) Error() string { return q.err.Error() }
func (q queryError) Unwrap() error { return q.err }

func withQuery(q string, err error) error {
	if err == nil {
		return nil
	}
	return queryError{q, err}
}

func (e Env) report(prog string, p provider, err error) int {
	if err == nil {
		return 0
	}
	l := e.Lang
	var ue usageError
	if errors.As(err, &ue) {
		fmt.Fprintf(e.Stderr, "%s: %v\n\n%s", prog, err, e.usage(prog))
		return 2
	}
	msg := err.Error()
	var qe queryError
	switch {
	case errors.As(err, &qe) && qe.query == "-" && errors.Is(err, store.ErrNotFound):
		msg = l.T("noPrevious")
	case errors.As(err, &qe) && errors.Is(err, store.ErrNotFound):
		msg = l.T("notFound", qe.query)
	case errors.As(err, &qe) && errors.Is(err, store.ErrAmbiguous):
		msg = l.T("ambiguous", qe.query)
	case errors.Is(err, swap.ErrNoLive):
		msg = l.T("noLive", p.login)
	case errors.Is(err, fsx.ErrLocked):
		msg = l.T("locked")
	case errors.Is(err, codex.ErrKeyringStore):
		msg = l.T("keyring")
	}
	fmt.Fprintf(e.Stderr, "%s: %s\n", prog, msg)
	return 1
}

func printVersion(e Env) {
	fmt.Fprintf(e.Stdout, "agentswap %s\nhttps://github.com/WellWells/agentswap\nby WellsTsai · https://wellstsai.com\n", e.Version)
}

func dispatch(e Env, prog string, p provider, m *swap.Manager, args []string) error {
	l := e.Lang
	cmd := ""
	if len(args) > 0 {
		cmd, args = args[0], args[1:]
	}
	need := func(n int) error {
		if len(args) < n {
			return usageError(l.T("needsArgs", cmd, n))
		}
		return nil
	}
	switch {
	case cmd == "" || cmd == "list" || cmd == "ls":
		return list(e, prog, p, m)
	case cmd == "status" || cmd == "current":
		return status(e, prog, p, m)
	case cmd == "add":
		alias := ""
		if len(args) > 0 {
			alias = args[0]
		}
		a, err := m.Add(alias)
		if err != nil {
			return err
		}
		fmt.Fprintln(e.Stdout, l.T("saved", label(a)))
		return nil
	case cmd == "switch" || cmd == "use":
		if err := need(1); err != nil {
			return err
		}
		return doSwitch(e, p, m, args[0])
	case cmd == "remove" || cmd == "rm":
		if err := need(1); err != nil {
			return err
		}
		a, err := m.Remove(args[0])
		if err != nil {
			return withQuery(args[0], err)
		}
		fmt.Fprintln(e.Stdout, l.T("removed", label(a)))
		return nil
	case cmd == "alias":
		if err := need(1); err != nil {
			return err
		}
		alias := ""
		if len(args) > 1 {
			alias = args[1]
		}
		return withQuery(args[0], m.SetAlias(args[0], alias))
	case isVersion(cmd):
		printVersion(e)
		return nil
	case isHelp(cmd):
		fmt.Fprint(e.Stdout, e.usage(prog))
		return nil
	case strings.HasPrefix(cmd, "--"):
		return usageError(l.T("unknownFlag", cmd))
	default:
		return doSwitch(e, p, m, cmd)
	}
}

func doSwitch(e Env, p provider, m *swap.Manager, q string) error {
	a, changed, err := m.Switch(q)
	if err != nil {
		return withQuery(q, err)
	}
	if !changed {
		fmt.Fprintln(e.Stdout, e.Lang.T("already", label(a)))
		return nil
	}
	fmt.Fprintln(e.Stdout, e.Lang.T("switched", label(a)))
	fmt.Fprintln(e.Stdout, e.Lang.T(p.hint))
	return nil
}

func (e Env) options() ui.Options {
	return ui.Options{Lang: e.Lang, Color: e.Color, Width: e.Width, Now: e.now(), Zone: e.Zone}
}

func (e Env) cards(prog string, p provider, m *swap.Manager, all bool) ([]ui.Card, error) {
	ctx, cancel := context.WithTimeout(context.Background(), usageTimeout)
	defer cancel()
	st, usage, err := m.Usage(ctx, all)
	if err != nil {
		return nil, err
	}
	r := st.Registry
	var cards []ui.Card
	if all {
		for i, a := range r.Accounts {
			c := ui.Card{Provider: p.display, Number: i + 1, Alias: a.Alias, Email: a.Email, Plan: a.Plan, Active: a.Key == r.Active}
			fill(&c, usage[a.Key])
			cards = append(cards, c)
		}
		return cards, nil
	}
	if !st.LiveOK {
		return nil, nil
	}
	c := ui.Card{Provider: p.display, Email: st.Live.Email, Plan: st.Live.Plan, Active: true, Unsaved: prog + " add"}
	if i := r.Index(st.Live.Key); i >= 0 {
		a := r.Accounts[i]
		c.Number, c.Alias, c.Email, c.Unsaved = i+1, a.Alias, a.Email, ""
	}
	fill(&c, usage[st.Live.Key])
	return []ui.Card{c}, nil
}

func fill(c *ui.Card, res swap.UsageResult) {
	c.State, c.Detail = ui.Classify(res.Err, map[error]ui.State{
		codex.ErrLoginExpired: ui.LoginExpired,
		codex.ErrNoUsage:      ui.NoUsage,
	})
	if c.State == ui.Unavailable {
		c.Detail = shortError(res.Err)
	}
	c.Usage = res.Usage
	if res.Usage.Plan != "" {
		c.Plan = res.Usage.Plan
	}
}

func shortError(err error) string {
	var ue *url.Error
	if errors.As(err, &ue) {
		if ue.Timeout() {
			return "timeout"
		}
		return "network error"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	return err.Error()
}

func list(e Env, prog string, p provider, m *swap.Manager) error {
	cards, err := e.cards(prog, p, m, true)
	if err != nil {
		return err
	}
	if len(cards) == 0 {
		fmt.Fprintln(e.Stdout, e.Lang.T("emptyProvider", p.name, p.login, prog))
		return nil
	}
	ui.MarkSuggestions(cards, e.now(), func(c ui.Card) string { return fmt.Sprintf("%s %d", prog, c.Number) })
	ui.Render(e.Stdout, cards, e.options())
	return nil
}

func status(e Env, prog string, p provider, m *swap.Manager) error {
	cards, err := e.cards(prog, p, m, false)
	if err != nil {
		return err
	}
	if len(cards) == 0 {
		fmt.Fprintln(e.Stdout, e.Lang.T("notLoggedIn"))
		return nil
	}
	ui.Render(e.Stdout, cards, e.options())
	return nil
}

func overview(e Env) error {
	var all []ui.Card
	for _, name := range providerOrder {
		p := providers[name]
		if !p.supported {
			continue
		}
		cards, err := e.cards("agentswap "+name, p, e.manager(p), true)
		if err != nil {
			return err
		}
		ui.MarkSuggestions(cards, e.now(), func(c ui.Card) string { return fmt.Sprintf("agentswap %s %d", name, c.Number) })
		all = append(all, cards...)
	}
	if len(all) == 0 {
		fmt.Fprintln(e.Stdout, e.Lang.T("emptyAll"))
		return nil
	}
	ui.Render(e.Stdout, all, e.options())
	return nil
}

func (e Env) exe() (string, error) {
	if e.Exe == "" {
		return "", errors.New(e.Lang.T("noExe"))
	}
	return e.Exe, nil
}

func link(e Env) error {
	exe, err := e.exe()
	if err != nil {
		return err
	}
	names := commandNames()
	if err := links.Link(exe, names); err != nil {
		return err
	}
	fmt.Fprintln(e.Stdout, e.Lang.T("linked", strings.Join(names, ", "), filepath.Dir(exe)))
	return nil
}

func unlink(e Env) error {
	exe, err := e.exe()
	if err != nil {
		return err
	}
	removed, err := links.Unlink(exe, commandNames())
	if len(removed) > 0 {
		fmt.Fprintln(e.Stdout, e.Lang.T("unlinked", strings.Join(removed, ", "), filepath.Dir(exe)))
	} else if err == nil {
		fmt.Fprintln(e.Stdout, e.Lang.T("nothingLinked"))
	}
	return err
}

func label(a store.Account) string {
	s := a.Email
	if a.Alias != "" {
		s = a.Alias + " <" + a.Email + ">"
	}
	if a.Plan != "" {
		s += " [" + a.Plan + "]"
	}
	return s
}

func (e Env) usage(prog string) string {
	return e.Lang.T("usage", prog)
}
