package cli

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/WellWells/agentswap/internal/antigravity"
	"github.com/WellWells/agentswap/internal/claude"
	"github.com/WellWells/agentswap/internal/swap"
)

func rootLogin(e Env, args []string) int {
	var choice string
	if len(args) != 0 {
		choice, args = args[0], args[1:]
		if isHelp(choice) {
			fmt.Fprint(e.Stdout, e.usage("agentswap"))
			return 0
		}
	} else if e.Interactive && e.Stdin != nil {
		fmt.Fprintln(e.Stdout, e.Lang.T("chooseLogin"))
		for i, name := range providerOrder {
			fmt.Fprintf(e.Stdout, "  %d) %s\n", i+1, providers[name].display)
		}
		fmt.Fprint(e.Stdout, "> ")
		line, _ := bufio.NewReader(e.Stdin).ReadString('\n')
		choice = strings.TrimSpace(line)
		if choice == "" {
			return 0
		}
		if n, err := strconv.Atoi(choice); err == nil && n >= 1 && n <= len(providerOrder) {
			choice = providerOrder[n-1]
		}
	}
	if choice == "" {
		fmt.Fprintln(e.Stderr, e.Lang.T("loginProviderRequired"))
		return 2
	}
	name, ok := lookup(strings.ToLower(choice))
	if !ok {
		fmt.Fprintln(e.Stderr, e.Lang.T("unknownProvider", choice))
		return 2
	}
	return run(e, "agentswap", append([]string{name, "login"}, args...))
}

func login(e Env, prog string, p provider, m *swap.Manager, alias string) (result error) {
	if e.Exec == nil {
		return errors.New(e.Lang.T("noExec"))
	}
	if p.name == "antigravity" {
		return loginAgy(e, prog, p, m, alias)
	}
	if _, err := m.P.ReadLive(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	base, err := filepath.Abs(e.dir("AGENTSWAP_HOME", ".agentswap"))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(base, 0o700); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(base, p.name+"-login-")
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, os.RemoveAll(tmp)) }()
	live := e.dir(p.homeEnv, p.homeDir)
	for _, name := range p.homeCopy {
		b, err := os.ReadFile(filepath.Join(live, name))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(tmp, name), b, 0o600); err != nil {
			return err
		}
	}
	env := []string{p.homeEnv + "=" + tmp}
	claudeLogin := p.name == "claude"
	if claudeLogin {
		env = append(env, "CLAUDE_SECURESTORAGE_CONFIG_DIR="+tmp)
	}
	isolated := e
	isolated.Getenv = func(k string) string {
		if k == p.homeEnv || (claudeLogin && k == "CLAUDE_SECURESTORAGE_CONFIG_DIR") {
			return tmp
		}
		return e.Getenv(k)
	}
	if claudeLogin {
		defer func() {
			result = errors.Join(result, claude.New(isolated.Getenv, e.Home, e.goos(), e.Run).DeleteSecure())
		}()
	}
	fmt.Fprintln(e.Stdout, e.Lang.T("loginStart"))
	if p.name == "codex" {
		fmt.Fprintln(e.Stdout, e.Lang.T("codexLoginWarning", prog))
	}
	if err := e.Exec(env, p.loginCmd[0], p.loginCmd[1:]...); err != nil {
		return fmt.Errorf("%s: %w", p.login, err)
	}
	raw, err := p.open(isolated).ReadLive()
	if errors.Is(err, os.ErrNotExist) {
		return errors.New(e.Lang.T("loginNoAuth"))
	}
	if err != nil {
		return err
	}
	a, err := m.Import(raw, alias)
	if err != nil {
		return err
	}
	if a.PendingOwner {
		fmt.Fprintln(e.Stdout, e.Lang.T("saved", savedLabel(e.Lang, a)))
		return nil
	}
	st, err := m.Status()
	if err != nil {
		return err
	}
	n := st.Registry.Index(a.Key) + 1
	fmt.Fprintln(e.Stdout, e.Lang.T("loginSaved", savedLabel(e.Lang, a), n, prog))
	return nil
}

func loginAgy(e Env, prog string, p provider, m *swap.Manager, alias string) error {
	if !e.Interactive {
		return errors.New(e.Lang.T("agyLoginTerminal"))
	}
	if err := closeRunning(e, prog, p, "login"); err != nil {
		return err
	}
	a, err := m.LoginLive(alias, func() error {
		fmt.Fprintln(e.Stdout, e.Lang.T("agyLoginStart"))
		return e.Exec(nil, "agy")
	})
	switch {
	case errors.Is(err, antigravity.ErrLoginChanged):
		return errors.New(e.Lang.T("agyLoginChanged"))
	case errors.Is(err, swap.ErrSameAccount):
		return errors.New(e.Lang.T("agyLoginUnchanged"))
	case errors.Is(err, swap.ErrNoLive):
		return errors.New(e.Lang.T("loginNoAuth"))
	case err != nil:
		return err
	}
	fmt.Fprintln(e.Stdout, e.Lang.T("saved", label(a)))
	return nil
}
