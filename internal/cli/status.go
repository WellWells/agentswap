package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/WellWells/agentswap/internal/antigravity"
	"github.com/WellWells/agentswap/internal/claude"
	"github.com/WellWells/agentswap/internal/codex"
	"github.com/WellWells/agentswap/internal/store"
	"github.com/WellWells/agentswap/internal/swap"
	"github.com/WellWells/agentswap/internal/ui"
)

type viewOptions struct {
	json   bool
	cached bool
	list   bool
	query  string
	model  string
}

func beforeSeparator(args []string) []string {
	if i := slices.Index(args, "--"); i >= 0 {
		return args[:i]
	}
	return args
}

func parseView(l ui.Lang, args []string, list, overview bool) (viewOptions, error) {
	o := viewOptions{list: list, json: slices.Contains(args, "--json")}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--json":
		case a == "--cached" && !list:
			o.cached = true
		case a == "--model" && !list:
			i++
			if i >= len(args) || strings.TrimSpace(args[i]) == "" || strings.HasPrefix(args[i], "-") || o.model != "" {
				return o, usageError(l.T("needsArgs", "--model", 1))
			}
			o.model = args[i]
		case len(a) > 1 && strings.HasPrefix(a, "-"):
			return o, usageError(l.T("unknownFlag", a))
		case !list && !overview && o.query == "" && a != "":
			o.query = a
		default:
			return o, usageError(l.T("unexpectedArgument", ui.Clean(a)))
		}
	}
	return o, nil
}

type accountView struct {
	Number       int          `json:"number,omitempty"`
	Alias        string       `json:"alias,omitempty"`
	Email        string       `json:"email"`
	Plan         string       `json:"plan,omitempty"`
	Active       bool         `json:"active"`
	PendingOwner bool         `json:"pendingOwner,omitempty"`
	Saved        bool         `json:"saved"`
	Status       string       `json:"status"`
	Source       string       `json:"source,omitempty"`
	ObservedAt   *time.Time   `json:"observedAt,omitempty"`
	CheckedAt    *time.Time   `json:"checkedAt,omitempty"`
	RetryAt      *time.Time   `json:"retryAt,omitempty"`
	Error        string       `json:"error,omitempty"`
	Windows      []windowView `json:"windows,omitempty"`
	Recommended  bool         `json:"recommended"`
}
type windowView struct {
	Minutes     int        `json:"minutes"`
	Model       string     `json:"model,omitempty"`
	UsedPercent int        `json:"usedPercent"`
	ResetsAt    *time.Time `json:"resetsAt,omitempty"`
}
type providerView struct {
	Provider     string        `json:"provider"`
	Accounts     []accountView `json:"accounts"`
	ActiveSource string        `json:"activeSource,omitempty"`
	LoginWarning string        `json:"loginWarning,omitempty"`
	Error        string        `json:"error,omitempty"`
}
type viewOutput struct {
	SchemaVersion int            `json:"schemaVersion"`
	Providers     []providerView `json:"providers"`
	Model         string         `json:"model,omitempty"`
	Error         string         `json:"error,omitempty"`
}

func jsonTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	utc := t.UTC()
	return &utc
}

func viewError(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, store.ErrNotFound):
		return "not_found"
	case errors.Is(err, store.ErrAmbiguous):
		return "ambiguous"
	case errors.Is(err, swap.ErrRateLimited):
		return "rate_limited"
	case errors.Is(err, swap.ErrNoCachedUsage):
		return "no_cache"
	case errors.Is(err, swap.ErrMismatch):
		return "identity_mismatch"
	case errors.Is(err, swap.ErrUnverified):
		return "identity_unverified"
	case errors.Is(err, swap.ErrStaleLogin):
		return "stale_login"
	case errors.Is(err, swap.ErrUsageLoginExpired), errors.Is(err, claude.ErrLoginExpired), errors.Is(err, codex.ErrLoginExpired), errors.Is(err, antigravity.ErrLoginExpired):
		return "login_expired"
	case errors.Is(err, swap.ErrUsageNetwork):
		return "network"
	case errors.Is(err, codex.ErrNoUsage):
		return "not_available"
	case errors.Is(err, claude.ErrActiveStale):
		return "active_token_expired"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	default:
		return "unavailable"
	}
}

func accountOutput(c ui.Card, list bool) accountView {
	a := accountView{Number: c.Number, Alias: c.Alias, Email: c.Email, Plan: c.Plan, Active: c.Active, Saved: c.Number > 0, PendingOwner: c.PendingOwner, Status: "not_requested", Recommended: c.Suggest != ""}
	if list {
		return a
	}
	a.Error = c.ErrorCode
	a.ObservedAt = jsonTime(c.Usage.At)
	a.CheckedAt = jsonTime(c.CheckedAt)
	a.RetryAt = jsonTime(c.RetryAt)
	switch {
	case c.Cached:
		a.Source = "cache"
	case !c.Usage.Live && !c.Usage.At.IsZero():
		a.Source = "session_log"
	case c.Usage.Live:
		a.Source = "provider"
	}
	switch {
	case len(c.Usage.Windows) > 0 && (c.Stale || !c.Usage.Live || a.Error != ""):
		a.Status = "stale"
	case a.Error != "":
		a.Status = a.Error
	case len(c.Usage.Windows) == 0:
		a.Status = "no_data"
	default:
		a.Status = "ok"
	}
	for _, w := range c.Usage.Windows {
		a.Windows = append(a.Windows, windowView{Minutes: w.Minutes, Model: w.Label, UsedPercent: w.UsedPercent, ResetsAt: jsonTime(w.ResetsAt)})
	}
	return a
}

func savedCard(provider string, number int, a store.Account, st swap.Status) ui.Card {
	active := a.Key == st.Registry.Active
	return ui.Card{Provider: provider, Number: number, Alias: a.Alias, Email: a.Email, Plan: a.Plan, PendingOwner: a.PendingOwner, Active: active, LastSeen: active && !st.LiveOK}
}

func readView(e Env, prog string, p provider, o viewOptions) (providerView, []ui.Card, error) {
	m := e.manager(p)
	g := providerView{Provider: p.name, Accounts: []accountView{}}
	ctx, cancel := context.WithTimeout(context.Background(), usageTimeout)
	defer cancel()
	var st swap.Status
	var readings map[string]swap.UsageResult
	var err error
	switch {
	case o.list:
		st, err = m.Status()
	case o.cached:
		st, readings, err = m.UsageCached(ctx, o.query)
	case o.query != "":
		st, readings, err = m.UsageOf(ctx, o.query)
	default:
		st, readings, err = m.Usage(ctx, true)
	}
	if err != nil {
		return g, nil, withQuery(o.query, err)
	}
	g.ActiveSource = "unknown"
	if st.LiveOK {
		g.ActiveSource = "live"
	} else if st.Registry.Active != "" {
		g.ActiveSource = "last_seen"
	}
	g.LoginWarning = viewError(st.LiveErr)
	var cards []ui.Card
	add := func(c ui.Card, key string) {
		if !o.list {
			fill(&c, readings[key])
			c.ShowCache = o.cached
		}
		cards = append(cards, c)
	}
	selected := -1
	if o.query != "" {
		selected, err = st.Registry.Find(o.query)
		if err != nil {
			return g, nil, withQuery(o.query, err)
		}
	}
	for i, a := range st.Registry.Accounts {
		if selected >= 0 && i != selected {
			continue
		}
		add(savedCard(p.display, i+1, a, st), a.Key)
	}
	if selected < 0 && (!o.list || len(st.Registry.Accounts) > 0) && st.LiveOK && st.Registry.Index(st.Live.Key) < 0 {
		add(ui.Card{Provider: p.display, Email: st.Live.Email, Plan: st.Live.Plan, Active: true, Unsaved: prog + " add"}, st.Live.Key)
	}
	if !o.list && selected < 0 {
		ui.MarkModelSuggestions(cards, e.now(), switchCmd(prog), o.model)
	}
	for _, c := range cards {
		g.Accounts = append(g.Accounts, accountOutput(c, o.list))
	}
	return g, cards, nil
}

func viewCommand(e Env, prog string, names []string, listMode bool, args []string) error {
	multi := len(names) > 1
	o, err := parseView(e.Lang, args, listMode, multi)
	out := viewOutput{SchemaVersion: 1, Providers: []providerView{}, Model: o.model}
	if err != nil {
		if o.json {
			out.Error = "invalid_arguments"
			if writeErr := json.NewEncoder(e.Stdout).Encode(out); writeErr != nil {
				return writeErr
			}
		}
		return err
	}
	if listMode && !o.json {
		p := providers[names[0]]
		return list(e, prog, p, e.manager(p))
	}
	var cards []ui.Card
	var failures []error
	for _, name := range names {
		p := providers[name]
		command := prog
		if multi {
			command += " " + name
		}
		group, rows, readErr := readView(e, command, p, o)
		if readErr != nil {
			group.Error = viewError(readErr)
			if group.Error == "unavailable" {
				group.Error = "provider_error"
			}
			failures = append(failures, fmt.Errorf("%s: %w", p.display, readErr))
		} else if !o.json && group.LoginWarning != "" {
			fmt.Fprintln(e.Stderr, e.Lang.T("liveWarning", p.display, group.LoginWarning))
		}
		out.Providers = append(out.Providers, group)
		cards = append(cards, rows...)
	}
	if o.json {
		if err := json.NewEncoder(e.Stdout).Encode(out); err != nil {
			return err
		}
	} else if len(cards) > 0 {
		options := e.options()
		if !multi && len(cards) > 1 {
			options.Hint = e.Lang.T("usageHint", prog)
		}
		ui.Render(e.Stdout, cards, options)
	} else if len(failures) == 0 {
		if multi {
			fmt.Fprintln(e.Stdout, e.Lang.T("emptyAll"))
		} else {
			p := providers[names[0]]
			fmt.Fprintln(e.Stdout, e.Lang.T(p.empty, p.name, p.login, prog))
		}
	}
	return errors.Join(failures...)
}
