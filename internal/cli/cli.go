package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/WellWells/agentswap/internal/antigravity"
	"github.com/WellWells/agentswap/internal/claude"
	"github.com/WellWells/agentswap/internal/codex"
	"github.com/WellWells/agentswap/internal/execx"
	"github.com/WellWells/agentswap/internal/fsx"
	"github.com/WellWells/agentswap/internal/links"
	"github.com/WellWells/agentswap/internal/official"
	"github.com/WellWells/agentswap/internal/procs"
	"github.com/WellWells/agentswap/internal/store"
	"github.com/WellWells/agentswap/internal/swap"
	"github.com/WellWells/agentswap/internal/transfer"
	"github.com/WellWells/agentswap/internal/ui"
	"github.com/WellWells/agentswap/internal/update"
	"github.com/WellWells/agentswap/internal/vault"
)

type Env struct {
	Args    []string
	Stdout  io.Writer
	Stderr  io.Writer
	Getenv  func(string) string
	Lookup  func(string) (string, bool)
	Home    string
	Exe     string
	Version string
	Now     func() time.Time
	Lang    ui.Lang
	Color   bool
	Width   int
	Zone    string
	Exec    func(env []string, name string, args ...string) error
	Output  func(env []string, name string, args ...string) ([]byte, error)
	Stdin   io.Reader
	Secret  func() (string, error)
	GOOS    string
	Run     execx.Runner
	Vault   vault.Vault
	Clients *official.Detector
	Procs   Processes

	Releases  update.Source
	Endpoints Endpoints

	Interactive bool
	Notify      bool

	yes      bool
	autoLang ui.Lang
}

type Endpoints struct {
	CodexAPI, CodexToken   string
	ClaudeAPI, ClaudeToken string
	AgyAPI, AgyToken       string
}

type Processes interface {
	Find(names, skipPaths []string) ([]procs.Proc, error)
	Stop([]procs.Proc) error
}

func (e Env) processes() Processes {
	if e.Procs != nil {
		return e.Procs
	}
	return procs.System{Run: e.Run, ClaudeDir: filepath.Join(e.dir("AGENTSWAP_HOME", ".agentswap"), "claude")}
}

func (e Env) clients() *official.Detector {
	if e.Clients != nil {
		return e.Clients
	}
	return &official.Detector{GOOS: e.goos(), GOARCH: runtime.GOARCH, Getenv: e.Getenv, Run: e.Run}
}

func (e Env) goos() string {
	if e.GOOS != "" {
		return e.GOOS
	}
	return runtime.GOOS
}

func (e Env) vault() vault.Vault {
	if e.Vault != nil {
		return e.Vault
	}
	return vault.Default(e.dir("AGENTSWAP_HOME", ".agentswap"), e.Stderr)
}

const usageTimeout = 10 * time.Second

func (e Env) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

func (e Env) lookup(k string) (string, bool) {
	if v := e.Getenv(k); v != "" {
		return v, true
	}
	if e.Lookup != nil {
		if v, ok := e.Lookup(k); ok && v == "" {
			return "", true
		}
	}
	return "", false
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
	loginCmd  []string
	homeEnv   string
	homeDir   string
	homeCopy  []string
	daemon    []string
	hint      string
	usage     string
	empty     string
	procNames []string
	procSkip  []string
	mustStop  bool
	rotates   bool
	open      func(Env) swap.Provider
}

var providerOrder = []string{"codex", "claude", "antigravity"}

var providers = map[string]provider{
	"codex": {name: "codex", display: "Codex", supported: true, login: "codex login", loginCmd: []string{"codex", "login"}, homeEnv: "CODEX_HOME", homeDir: ".codex", homeCopy: []string{"config.toml"}, daemon: []string{"codex", "app-server", "daemon"}, hint: "hintCodex", usage: "usage", empty: "emptyProvider", procNames: []string{"codex"}, rotates: true, open: func(e Env) swap.Provider {
		return codex.Provider{
			Home:       e.dir("CODEX_HOME", ".codex"),
			BaseURL:    e.Endpoints.CodexAPI,
			RefreshURL: e.Endpoints.CodexToken,
			UserAgent:  e.clients().Codex,
			Now:        e.Now,
		}
	}},
	"claude": {name: "claude", display: "Claude Code", supported: true, login: "claude auth login", loginCmd: []string{"claude", "auth", "login"}, homeEnv: "CLAUDE_CONFIG_DIR", hint: "hintClaude", usage: "usageClaude", empty: "emptyClaude", procNames: []string{"claude"}, procSkip: []string{"AnthropicClaude", "Claude.app/Contents/MacOS/Claude"}, rotates: true, open: func(e Env) swap.Provider {
		e = sessionManagementEnv(e)
		p := claude.NewLookup(e.lookup, e.Home, e.goos(), e.Run)
		p.BaseURL, p.RefreshURL = e.Endpoints.ClaudeAPI, e.Endpoints.ClaudeToken
		p.Now = e.Now
		p.UserAgent = e.clients().Claude
		return p
	}},
	"antigravity": {name: "antigravity", display: "Antigravity", supported: true, login: "agy", hint: "hintAntigravity", usage: "usageAntigravity", empty: "emptyAntigravity", procNames: []string{"agy"}, mustStop: true, open: func(e Env) swap.Provider {
		p := antigravity.New(e.goos(), e.Run)
		p.BaseURL, p.RefreshURL = e.Endpoints.AgyAPI, e.Endpoints.AgyToken
		p.Now = e.Now
		p.UserAgent = e.clients().Agy
		return p
	}},
}

var commands = []struct{ name, provider string }{
	{"cxswap", "codex"}, {"codexswap", "codex"},
	{"ccswap", "claude"}, {"claudeswap", "claude"},
	{"agswap", "antigravity"}, {"agyswap", "antigravity"},
}

var aliases = map[string]string{
	"codex": "codex", "cx": "codex",
	"claude": "claude", "cc": "claude",
	"antigravity": "antigravity", "agy": "antigravity", "ag": "antigravity",
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
	return &swap.Manager{P: p.open(e), S: store.Store{Dir: filepath.Join(e.dir("AGENTSWAP_HOME", ".agentswap"), p.name), Vault: e.vault()}, Now: e.Now}
}

func isNumber(s string) bool {
	_, err := strconv.Atoi(strings.TrimSpace(s))
	return err == nil
}

func isHelp(s string) bool    { return s == "help" || s == "-h" || s == "--help" }
func isVersion(s string) bool { return s == "version" || s == "--version" || s == "-v" }

func isProvider(s string) bool {
	_, ok := lookup(strings.ToLower(s))
	return ok
}

func helpFlag(args []string) bool {
	for _, a := range args {
		if a == "-h" || a == "--help" {
			return true
		}
	}
	return false
}

func flagArg(args []string) string {
	for _, a := range args {
		if len(a) > 1 && strings.HasPrefix(a, "-") {
			return a
		}
	}
	return ""
}

func Run(e Env) int {
	prog := strings.ToLower(filepath.Base(strings.ReplaceAll(e.Args[0], `\`, "/")))
	prog = strings.TrimSuffix(prog, ".exe")
	var args []string
	for i, a := range e.Args[1:] {
		if a == "--" {
			args = append(args, e.Args[i+1:]...)
			break
		}
		if a == "-y" || a == "--yes" {
			e.yes = true
			continue
		}
		args = append(args, a)
	}
	e = e.applySavedLang()
	if e.Clients == nil {
		e.Clients = e.clients()
		e.Clients.Cache = filepath.Join(e.dir("AGENTSWAP_HOME", ".agentswap"), "clients.json")
	}
	if e.goos() == "windows" {
		e.removeOld()
	}
	_, isProvider := lookup(prog)
	done := e.checkUpdate(slices.Contains(args, "--cached") || (!isProvider && len(args) > 0 && isUpdate(args[0])))
	code := run(e, prog, args)
	done()
	return code
}

func run(e Env, prog string, args []string) int {
	name, ok := lookup(prog)
	if !ok {
		switch {
		case len(args) > 0 && args[0] == "login":
			return rootLogin(e, args[1:])
		case len(args) == 0 || isHelp(args[0]) || helpFlag(args[1:]) && !isProvider(args[0]):
			fmt.Fprint(e.Stdout, e.usage("agentswap <provider>"))
			return 0
		case isVersion(args[0]):
			printVersion(e)
			return 0
		case args[0] == "status" || args[0] == "current" || args[0] == "usage":
			return e.report("agentswap", provider{}, viewCommand(e, "agentswap", providerOrder, false, args[1:]))
		case args[0] == "link":
			return e.report("agentswap", provider{}, link(e))
		case args[0] == "unlink":
			return e.report("agentswap", provider{}, unlink(e))
		case isUpdate(args[0]):
			return e.report("agentswap", provider{}, selfUpdate(e))
		case args[0] == "export":
			return e.report("agentswap", provider{}, exportAccounts(e, providerOrder, args[1:]))
		case args[0] == "import":
			return e.report("agentswap", provider{}, importAccounts(e, providerOrder, args[1:]))
		case args[0] == "lang" || args[0] == "language":
			return e.report("agentswap", provider{}, language(e, args[1:]))
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
	var child sessionExit
	if errors.As(err, &child) {
		return child.code
	}
	l := e.Lang
	var ue usageError
	if errors.As(err, &ue) {
		fmt.Fprintf(e.Stderr, "%s: %v\n\n%s", prog, err, e.usageFor(p, prog))
		return 2
	}
	msg := err.Error()
	var qe queryError
	var ii importIncomplete
	switch {
	case errors.As(err, &qe) && qe.query == "-" && errors.Is(err, store.ErrNotFound):
		msg = l.T("noPrevious")
	case errors.As(err, &qe) && errors.Is(err, store.ErrNotFound) && isNumber(qe.query):
		msg = l.T("noNumber", qe.query, prog)
	case errors.As(err, &qe) && errors.Is(err, store.ErrNotFound):
		msg = l.T("notFound", qe.query)
	case errors.As(err, &qe) && errors.Is(err, store.ErrAmbiguous):
		msg = l.T("ambiguous", qe.query)
	case errors.Is(err, swap.ErrNoLive):
		msg = l.T("noLive", prog+" login")
	case errors.Is(err, fsx.ErrLocked):
		msg = l.T("locked")
	case errors.Is(err, codex.ErrKeyringStore):
		msg = l.T("keyring")
	case errors.Is(err, swap.ErrMismatch):
		msg = l.T("mismatch")
	case errors.Is(err, swap.ErrUnverified):
		msg = l.T("usageReason_identity_unverified")
	case errors.Is(err, swap.ErrStaleLogin):
		msg = l.T("usageReason_stale_login")
	case errors.Is(err, swap.ErrRepairLive):
		msg = l.T("repairActive")
	case errors.Is(err, claude.ErrBusy):
		msg = l.T("claudeBusy")
	case errors.Is(err, claude.ErrUnsupportedLogin):
		msg = l.T("claudeUnsupported")
	case errors.Is(err, claude.ErrWiped):
		msg = l.T("claudeWiped")
	case errors.Is(err, antigravity.ErrRunning):
		msg = l.T("agyRunning")
	case errors.Is(err, antigravity.ErrUnsupportedLogin):
		msg = l.T("agyUnsupported")
	case errors.Is(err, update.ErrChecksum):
		msg = l.T("updateChecksum")
	case errors.Is(err, vault.ErrKey):
		msg = l.T("vaultKey")
	case errors.Is(err, vault.ErrCorrupt):
		msg = l.T("vaultCorrupt")
	case errors.Is(err, transfer.ErrFormat):
		msg = l.T("importNotExport")
	case errors.Is(err, transfer.ErrVersion):
		msg = l.T("importNewer")
	case errors.Is(err, transfer.ErrPassphrase):
		msg = l.T("passphraseWrong")
	case errors.As(err, &ii):
		msg = l.T("cswapIncomplete", ii.failed, ii.dir, prog)
	}
	fmt.Fprintf(e.Stderr, "%s: %s\n", prog, msg)
	return 1
}

func printVersion(e Env) {
	fmt.Fprint(e.Stdout, e.Lang.T("version", e.Version))
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
	case p.name == "claude" && (cmd == "run" || cmd == "map" || cmd == "unmap") && !helpFlag(beforeSeparator(args)):
		return sessionCommand(e, prog, p, m, cmd, args)
	case cmd == "" || isHelp(cmd) || helpFlag(args):
		fmt.Fprint(e.Stdout, e.usageFor(p, prog))
		return nil
	case cmd == "list" || cmd == "ls":
		return viewCommand(e, prog, []string{p.name}, true, args)
	case cmd == "status" || cmd == "current" || cmd == "usage":
		return viewCommand(e, prog, []string{p.name}, false, args)
	case cmd != "export" && cmd != "import" && flagArg(args) != "":
		return usageError(l.T("unknownFlag", flagArg(args)))
	case cmd == "add":
		alias := ""
		if len(args) > 0 {
			alias = args[0]
		}
		a, added, err := m.AddWithResult(alias)
		if err != nil {
			return err
		}
		if added {
			fmt.Fprintln(e.Stdout, l.T("saved", savedLabel(l, a)))
		} else {
			fmt.Fprintln(e.Stdout, l.T("updatedAccount", savedLabel(l, a)))
			fmt.Fprintln(e.Stdout, l.T("listAddMore", prog))
		}
		if p.name == "codex" && a.Mode == "chatgpt" {
			fmt.Fprintln(e.Stdout, l.T("codexLoginWarning", prog))
		}
		return nil
	case cmd == "login":
		if len(args) > 1 {
			return usageError(l.T("loginArgs", prog))
		}
		alias := ""
		if len(args) > 0 {
			alias = args[0]
		}
		return login(e, prog, p, m, alias)
	case cmd == "export":
		return exportAccounts(e, []string{p.name}, args)
	case cmd == "repair":
		return repairAccount(e, p, m, args)
	case cmd == "import":
		if p.name == "claude" && (len(args) == 0 || args[0] == "1") {
			choice := ""
			if len(args) > 0 {
				choice = args[0]
			}
			return importMenu(e, m, choice)
		}
		return importAccounts(e, []string{p.name}, args)
	case cmd == "switch" || cmd == "use":
		if err := need(1); err != nil {
			return err
		}
		return doSwitch(e, prog, p, m, args[0])
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
	case strings.HasPrefix(cmd, "--"):
		return usageError(l.T("unknownFlag", cmd))
	default:
		return doSwitch(e, prog, p, m, cmd)
	}
}

func stopRunning(e Env, prog string, p provider, m *swap.Manager, q string) error {
	if len(p.procNames) == 0 {
		return nil
	}
	st, err := m.Status()
	if err != nil {
		return nil
	}
	i, err := st.Registry.Find(q)
	if err != nil || (st.LiveOK && st.Registry.Accounts[i].Key == st.Live.Key) {
		return nil
	}
	return closeRunning(e, prog, p, q)
}

func closeRunning(e Env, prog string, p provider, q string) error {
	found, err := e.processes().Find(p.procNames, p.procSkip)
	if err != nil && p.mustStop {
		return err
	}
	if err != nil || len(found) == 0 {
		return nil
	}
	l := e.Lang
	name := p.procNames[0]
	fmt.Fprintln(e.Stdout, l.T("procsFound", name))
	for _, f := range found {
		where := f.Path
		if where == "" {
			where = f.Name
		}
		fmt.Fprintf(e.Stdout, "  PID %-7d %s\n", f.PID, ui.Clean(where))
	}
	yes := e.yes
	if !yes && e.Interactive && e.Stdin != nil {
		fmt.Fprint(e.Stdout, l.T("procsAsk"))
		line, _ := bufio.NewReader(e.Stdin).ReadString('\n')
		answer := strings.ToLower(strings.TrimSpace(line))
		yes = answer == "y" || answer == "yes"
	}
	if !yes {
		if p.mustStop {
			return errors.New(l.T("procsCancelled", name, prog, q))
		}
		fmt.Fprintln(e.Stdout, l.T("procsKept"))
		return nil
	}
	if err := e.processes().Stop(found); err != nil {
		return err
	}
	fmt.Fprintln(e.Stdout, l.T("procsStopped", len(found)))
	return nil
}

func doSwitch(e Env, prog string, p provider, m *swap.Manager, q string) error {
	if err := stopRunning(e, prog, p, m, q); err != nil {
		return err
	}
	a, changed, err := m.Switch(q)
	if err != nil {
		return withQuery(q, err)
	}
	if !changed {
		fmt.Fprintln(e.Stdout, e.Lang.T("already", label(a)))
		return nil
	}
	fmt.Fprintln(e.Stdout, e.Lang.T("switched", label(a)))
	restartDaemon(e, p)
	fmt.Fprintln(e.Stdout, e.Lang.T(p.hint))
	return nil
}

func restartDaemon(e Env, p provider) {
	if e.Output == nil || len(p.daemon) == 0 {
		return
	}
	daemonCmd := func(sub string) []string { return append(append([]string{}, p.daemon...), sub) }
	version := daemonCmd("version")
	b, err := e.Output(nil, version[0], version[1:]...)
	var st struct {
		Status string `json:"status"`
	}
	if err != nil || json.Unmarshal(bytes.TrimSpace(b), &st) != nil || st.Status != "running" {
		return
	}
	restart := daemonCmd("restart")
	if _, err := e.Output(nil, restart[0], restart[1:]...); err != nil {
		fmt.Fprintln(e.Stdout, e.Lang.T("restartFailed", err, strings.Join(restart, " ")))
		return
	}
	fmt.Fprintln(e.Stdout, e.Lang.T("restarted"))
}

func (e Env) options() ui.Options {
	return ui.Options{Lang: e.Lang, Color: e.Color, Width: e.Width, Now: e.now(), Zone: e.Zone}
}

func fill(c *ui.Card, res swap.UsageResult) {
	c.ErrorCode = viewError(res.Err)
	c.Cached, c.CheckedAt, c.RetryAt = res.Cached, res.CheckedAt, res.RetryAt
	c.Stale = res.Stale
	c.State, c.Detail = ui.Classify(res.Err, map[error]ui.State{
		swap.ErrUsageLoginExpired:   ui.LoginExpired,
		codex.ErrLoginExpired:       ui.LoginExpired,
		codex.ErrNoUsage:            ui.NoUsage,
		claude.ErrLoginExpired:      ui.LoginExpired,
		antigravity.ErrLoginExpired: ui.LoginExpired,
	})
	if c.State == ui.Unavailable {
		c.Detail = shortError(res.Err)
	}
	c.Usage = res.Usage
	if c.Cached && len(c.Usage.Windows) > 0 && res.Err != nil {
		c.State = ui.OK
		c.Usage.Live = false
	}
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

func switchCmd(prog string) func(ui.Card) string {
	return func(c ui.Card) string { return fmt.Sprintf("%s %d", prog, c.Number) }
}

func list(e Env, prog string, p provider, m *swap.Manager) error {
	st, err := m.Status()
	if err != nil {
		return err
	}
	r := st.Registry
	if st.LiveErr != nil {
		fmt.Fprintln(e.Stderr, e.Lang.T("liveWarning", p.display, viewError(st.LiveErr)))
	}
	if len(r.Accounts) == 0 {
		fmt.Fprintln(e.Stdout, e.Lang.T(p.empty, p.name, p.login, prog))
		return nil
	}
	var cards []ui.Card
	for i, a := range r.Accounts {
		cards = append(cards, savedCard(p.display, i+1, a, st))
	}
	if st.LiveOK && r.Index(st.Live.Key) < 0 {
		cards = append(cards, ui.Card{Provider: p.display, Email: st.Live.Email, Plan: st.Live.Plan, Active: true, Unsaved: prog + " add"})
	}
	ui.List(e.Stdout, p.display, cards, switchCmd(prog), e.options())
	fmt.Fprintln(e.Stdout)
	fmt.Fprintln(e.Stdout, e.Lang.T("listHelp", prog))
	if len(r.Accounts) == 1 {
		fmt.Fprintln(e.Stdout, e.Lang.T("listAddMore", prog))
		if p.name == "codex" {
			fmt.Fprintln(e.Stdout, e.Lang.T("codexLoginWarning", prog))
		}
	}
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

func savedLabel(l ui.Lang, a store.Account) string {
	text := label(a)
	if a.PendingOwner {
		text += " · " + l.T("pendingOwner")
	}
	return text
}

func label(a store.Account) string {
	s := a.Email
	if a.Alias != "" {
		s = a.Alias + " <" + a.Email + ">"
	}
	if a.Plan != "" {
		s += " [" + a.Plan + "]"
	}
	return ui.Clean(s)
}

func (e Env) usage(prog string) string {
	return e.Lang.T("usageAgentswap") + "\n" + e.Lang.T("statusFlags") + "\n"
}

func (e Env) usageFor(p provider, prog string) string {
	if p.usage == "" {
		return e.usage(prog)
	}
	help := e.Lang.T(p.usage, prog) + "\n" + e.Lang.T("statusFlags") + "\n" + e.Lang.T("repairHelp", prog) + "\n"
	if p.name == "claude" {
		help += e.Lang.T("sessionHelp", prog) + "\n"
	} else if p.name == "codex" {
		help += e.Lang.T("codexLoginWarning", prog) + "\n"
	}
	return help
}

func importMenu(e Env, m *swap.Manager, choice string) error {
	l := e.Lang
	dirs := claude.CswapDirs(e.Home, e.goos(), e.Getenv)
	found := ""
	for _, d := range dirs {
		if _, err := os.Stat(filepath.Join(d, "sequence.json")); err == nil {
			found = d
			break
		}
	}
	if choice == "" {
		where := l.T("importNotFound")
		if found != "" {
			where = found
		}
		fmt.Fprintln(e.Stdout, l.T("importMenu", where))
		if e.Stdin != nil {
			line, _ := bufio.NewReader(e.Stdin).ReadString('\n')
			choice = strings.TrimSpace(line)
		}
	}
	if choice != "1" {
		fmt.Fprintln(e.Stdout, l.T("importCancelled"))
		return nil
	}
	if found == "" {
		return errors.New(l.T("cswapMissing", strings.Join(dirs, ", ")))
	}
	return importCswap(e, m, found)
}

type importIncomplete struct {
	failed int
	dir    string
}

func (e importIncomplete) Error() string {
	return fmt.Sprintf("%d account(s) not imported", e.failed)
}

func importCswap(e Env, m *swap.Manager, dir string) error {
	l := e.Lang
	var keychain func(int, string) ([]byte, error)
	if e.goos() == "darwin" {
		keychain = claude.CswapKeychain(e.Run)
	}
	accts, err := claude.ReadCswap(dir, keychain)
	if err != nil {
		return err
	}
	failed := 0
	for _, a := range accts {
		name := a.Email
		if a.Alias != "" {
			name = a.Alias + " <" + a.Email + ">"
		}
		name = ui.Clean(name)
		if a.Err != nil {
			fmt.Fprintln(e.Stdout, l.T("importFailed", name, importReason(l, a.Err)))
			if !errors.Is(a.Err, claude.ErrUnsupportedLogin) {
				failed++
			}
			continue
		}
		if !adopt(e, m, a.Snapshot, a.Alias, name, ui.Clean(a.Email)) {
			failed++
		}
	}
	fmt.Fprintln(e.Stdout)
	if failed > 0 {
		return importIncomplete{failed, dir}
	}
	fmt.Fprintln(e.Stdout, l.T("cswapAfter", dir))
	return nil
}

func adopt(e Env, m *swap.Manager, raw []byte, alias, name, email string) bool {
	l := e.Lang
	acc, err := m.Adopt(raw, alias)
	if err != nil && alias != "" && errors.Is(err, swap.ErrAlias) {
		fmt.Fprintln(e.Stdout, l.T("importAliasDropped", alias, email))
		acc, err = m.Adopt(raw, "")
	}
	if errors.Is(err, swap.ErrExists) {
		fmt.Fprintln(e.Stdout, l.T("importSkipped", name))
		return true
	}
	if err != nil {
		fmt.Fprintln(e.Stdout, l.T("importFailed", name, importReason(l, err)))
		return errors.Is(err, claude.ErrUnsupportedLogin)
	}
	fmt.Fprintln(e.Stdout, l.T("imported", savedLabel(l, acc)))
	return true
}

func importReason(l ui.Lang, err error) string {
	if errors.Is(err, claude.ErrUnsupportedLogin) {
		return l.T("claudeUnsupportedShort")
	}
	return shortError(err)
}
