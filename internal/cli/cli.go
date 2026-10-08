package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/WellWells/agentswap/internal/codex"
	"github.com/WellWells/agentswap/internal/store"
	"github.com/WellWells/agentswap/internal/swap"
)

type Env struct {
	Args    []string
	Stdout  io.Writer
	Stderr  io.Writer
	Getenv  func(string) string
	Home    string
	Version string
	Now     func() time.Time
}

const usageTimeout = 10 * time.Second

func (e Env) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

type provider struct {
	name      string
	supported bool
	login     string
	open      func(Env) swap.Provider
}

var providers = map[string]provider{
	"codex": {name: "codex", supported: true, login: "codex login", open: func(e Env) swap.Provider {
		return codex.Provider{
			Home:       e.dir("CODEX_HOME", ".codex"),
			RefreshURL: e.Getenv("CODEX_REFRESH_TOKEN_URL_OVERRIDE"),
			UserAgent:  "agentswap/" + e.Version,
		}
	}},
	"claude": {name: "claude"},
}

var aliases = map[string]string{
	"cxswap": "codex", "codexswap": "codex", "codex": "codex", "cx": "codex",
	"ccswap": "claude", "claudeswap": "claude", "claude": "claude", "cc": "claude",
}

func (e Env) dir(envVar, fallback string) string {
	if v := e.Getenv(envVar); v != "" {
		return v
	}
	return filepath.Join(e.Home, fallback)
}

func Run(e Env) int {
	prog := strings.ToLower(filepath.Base(strings.ReplaceAll(e.Args[0], `\`, "/")))
	prog = strings.TrimSuffix(prog, ".exe")
	args := e.Args[1:]
	name, ok := aliases[prog]
	if !ok {
		if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
			fmt.Fprint(e.Stderr, usage("agentswap <provider>"))
			return 2
		}
		if isVersion(args[0]) {
			printVersion(e)
			return 0
		}
		if name, ok = aliases[strings.ToLower(args[0])]; !ok {
			fmt.Fprintf(e.Stderr, "agentswap: unknown provider %q (available: codex, claude)\n", args[0])
			return 2
		}
		prog = "agentswap " + name
		args = args[1:]
	}
	p := providers[name]
	if !p.supported {
		fmt.Fprintf(e.Stderr, "%s: %s is not supported yet\n", prog, p.name)
		return 2
	}
	m := &swap.Manager{P: p.open(e), S: store.Store{Dir: filepath.Join(e.dir("AGENTSWAP_HOME", ".agentswap"), p.name)}}
	err := dispatch(e, prog, p, m, args)
	var ue usageError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &ue):
		fmt.Fprintf(e.Stderr, "%s: %v\n\n%s", prog, err, usage(prog))
		return 2
	case errors.Is(err, swap.ErrNoLive):
		fmt.Fprintf(e.Stderr, "%s: %v (run `%s`)\n", prog, err, p.login)
	default:
		fmt.Fprintf(e.Stderr, "%s: %v\n", prog, err)
	}
	return 1
}

type usageError string

func (u usageError) Error() string { return string(u) }

func printVersion(e Env) {
	fmt.Fprintf(e.Stdout, "agentswap %s\nhttps://github.com/WellWells/agentswap\nby WellsTsai · https://wellstsai.com\n", e.Version)
}

func isVersion(s string) bool { return s == "version" || s == "--version" || s == "-v" }

func dispatch(e Env, prog string, p provider, m *swap.Manager, args []string) error {
	cmd := ""
	if len(args) > 0 {
		cmd, args = args[0], args[1:]
	}
	need := func(n int) error {
		if len(args) < n {
			return usageError(fmt.Sprintf("%s needs %d argument(s)", cmd, n))
		}
		return nil
	}
	switch {
	case cmd == "" || cmd == "list" || cmd == "ls":
		return list(e, prog, p, m)
	case cmd == "status" || cmd == "current":
		return status(e, prog, m)
	case cmd == "add":
		alias := ""
		if len(args) > 0 {
			alias = args[0]
		}
		a, err := m.Add(alias)
		if err != nil {
			return err
		}
		fmt.Fprintf(e.Stdout, "Saved %s\n", label(a))
		return nil
	case cmd == "switch" || cmd == "use":
		if err := need(1); err != nil {
			return err
		}
		return doSwitch(e, m, args[0])
	case cmd == "remove" || cmd == "rm":
		if err := need(1); err != nil {
			return err
		}
		a, err := m.Remove(args[0])
		if err != nil {
			return err
		}
		fmt.Fprintf(e.Stdout, "Removed %s\n", label(a))
		return nil
	case cmd == "alias":
		if err := need(1); err != nil {
			return err
		}
		alias := ""
		if len(args) > 1 {
			alias = args[1]
		}
		return m.SetAlias(args[0], alias)
	case isVersion(cmd):
		printVersion(e)
		return nil
	case cmd == "help" || cmd == "-h" || cmd == "--help":
		fmt.Fprint(e.Stdout, usage(prog))
		return nil
	case strings.HasPrefix(cmd, "--"):
		return usageError("unknown flag " + cmd)
	default:
		return doSwitch(e, m, cmd)
	}
}

func doSwitch(e Env, m *swap.Manager, q string) error {
	a, changed, err := m.Switch(q)
	if err != nil {
		return err
	}
	if !changed {
		fmt.Fprintf(e.Stdout, "Already using %s\n", label(a))
		return nil
	}
	fmt.Fprintf(e.Stdout, "Switched to %s\n%s\n", label(a), m.P.ApplyHint())
	return nil
}

func list(e Env, prog string, p provider, m *swap.Manager) error {
	ctx, cancel := context.WithTimeout(context.Background(), usageTimeout)
	defer cancel()
	st, usage, err := m.Usage(ctx, true)
	if err != nil {
		return err
	}
	r := st.Registry
	if len(r.Accounts) == 0 {
		fmt.Fprintf(e.Stdout, "No saved %s accounts. Log in with `%s`, then run `%s add`.\n", p.name, p.login, prog)
		return nil
	}
	now := e.now()
	w := tabwriter.NewWriter(e.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, " \t#\tALIAS\tACCOUNT\tPLAN\t5H\tWEEK\tUPDATED")
	for i, a := range r.Accounts {
		mark := " "
		if a.Key == r.Active {
			mark = "*"
		}
		res, ok := usage[a.Key]
		plan := firstNonEmpty(res.Usage.Plan, a.Plan, "-")
		short, long := buckets(res.Usage, now)
		fmt.Fprintf(w, "%s\t%d\t%s\t%s\t%s\t%s\t%s\t%s\n", mark, i+1, a.Alias, a.Email, plan, short, long, freshness(res, ok, now))
	}
	return w.Flush()
}

func status(e Env, prog string, m *swap.Manager) error {
	ctx, cancel := context.WithTimeout(context.Background(), usageTimeout)
	defer cancel()
	st, usage, err := m.Usage(ctx, false)
	if err != nil {
		return err
	}
	if !st.LiveOK {
		fmt.Fprintln(e.Stdout, "Not logged in.")
		return nil
	}
	r := st.Registry
	if i := r.Index(st.Live.Key); i >= 0 {
		fmt.Fprintf(e.Stdout, "Using #%d %s\n", i+1, label(r.Accounts[i]))
	} else {
		fmt.Fprintf(e.Stdout, "Using %s (not saved; run `%s add`)\n", st.Live.Email, prog)
	}
	res, ok := usage[st.Live.Key]
	if !ok {
		return nil
	}
	if res.Err != nil {
		fmt.Fprintf(e.Stdout, "Usage unavailable: %v\n", res.Err)
		return nil
	}
	now := e.now()
	for _, win := range res.Usage.Windows {
		line := fmt.Sprintf("%-6s %3d%%", windowName(win.Minutes)+":", win.Percent(now))
		if !win.ResetsAt.IsZero() && win.ResetsAt.After(now) {
			line += "  resets " + resetTime(win.ResetsAt, now)
		}
		fmt.Fprintln(e.Stdout, line)
	}
	if !res.Usage.Live {
		fmt.Fprintf(e.Stdout, "(from the local session log, %s)\n", ago(now.Sub(res.Usage.At)))
	}
	return nil
}

func buckets(u swap.Usage, now time.Time) (string, string) {
	short, long := "-", "-"
	for _, w := range u.Windows {
		v := fmt.Sprintf("%d%%", w.Percent(now))
		if w.Minutes > 0 && w.Minutes <= 24*60 {
			short = v
		} else {
			long = v
		}
	}
	return short, long
}

func freshness(r swap.UsageResult, ok bool, now time.Time) string {
	switch {
	case !ok || errors.Is(r.Err, codex.ErrNoUsage):
		return "-"
	case errors.Is(r.Err, codex.ErrLoginExpired):
		return "login-expired"
	case r.Err != nil:
		return "unavailable"
	case r.Usage.Live:
		return "now"
	default:
		return ago(now.Sub(r.Usage.At))
	}
}

func ago(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func windowName(minutes int) string {
	switch {
	case minutes == 7*24*60:
		return "week"
	case minutes > 0 && minutes%(24*60) == 0:
		return fmt.Sprintf("%dd", minutes/(24*60))
	case minutes > 0 && minutes%60 == 0:
		return fmt.Sprintf("%dh", minutes/60)
	default:
		return fmt.Sprintf("%dm", minutes)
	}
}

func resetTime(t, now time.Time) string {
	t, now = t.Local(), now.Local()
	if t.YearDay() == now.YearDay() && t.Year() == now.Year() {
		return t.Format("15:04")
	}
	return t.Format("01/02 15:04")
}

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
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

func usage(prog string) string {
	return fmt.Sprintf(`Usage:
  %[1]s                    list saved accounts (* = active)
  %[1]s add [alias]        save the currently logged-in account
  %[1]s <n|alias|email>    switch account (also: switch <q>)
  %[1]s -                  switch to the previous account
  %[1]s status             show the live account
  %[1]s alias <q> [name]   set or clear an alias
  %[1]s rm <q>             forget a saved account
  %[1]s version

Commands: agentswap <codex|claude> ..., cxswap/codexswap, ccswap/claudeswap
`, prog)
}
