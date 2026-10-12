package cli

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/WellWells/agentswap/internal/fsx"
	"github.com/WellWells/agentswap/internal/transfer"
	"github.com/WellWells/agentswap/internal/ui"
)

func exportAccounts(e Env, names []string, args []string) error {
	l := e.Lang
	plain := false
	var paths []string
	for _, a := range args {
		switch {
		case a == "--plain":
			plain = true
		case strings.HasPrefix(a, "--"):
			return usageError(l.T("unknownFlag", a))
		default:
			paths = append(paths, a)
		}
	}
	if len(paths) != 1 {
		return usageError(l.T("needsArgs", "export", 1))
	}
	path := paths[0]
	if _, err := os.Lstat(path); err == nil {
		return errors.New(l.T("exportExists", path))
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	bundle := transfer.Bundle{}
	var lines []string
	rotates := false
	for _, name := range names {
		p := providers[name]
		m := e.manager(p)
		r, err := m.S.Load()
		if err != nil {
			return err
		}
		if len(r.Accounts) == 0 {
			continue
		}
		saved, err := m.Snapshots()
		if err != nil {
			return fmt.Errorf("%s: %w", p.display, err)
		}
		for _, s := range saved {
			bundle[name] = append(bundle[name], transfer.Entry{Alias: s.Account.Alias, Snapshot: s.Raw})
			lines = append(lines, l.T("exportedAccount", p.display, label(s.Account)))
		}
		rotates = rotates || (p.rotates && len(saved) > 0)
	}
	if len(lines) == 0 {
		return errors.New(l.T("exportEmpty"))
	}
	var data []byte
	var err error
	if plain {
		ok, err := confirmPlain(e)
		if err != nil {
			return err
		}
		if !ok {
			fmt.Fprintln(e.Stdout, l.T("importCancelled"))
			return nil
		}
		data, err = transfer.Plain(bundle)
		if err != nil {
			return err
		}
	} else {
		pass, err := newPassphrase(e)
		if err != nil {
			return err
		}
		if data, err = transfer.Seal(bundle, []byte(pass)); err != nil {
			return err
		}
	}
	if err = fsx.WriteAtomic(path, data, 0o600); err != nil {
		return err
	}
	for _, line := range lines {
		fmt.Fprintln(e.Stdout, line)
	}
	fmt.Fprintln(e.Stdout, l.T("exported", len(lines), path))
	if plain {
		fmt.Fprintln(e.Stdout, l.T("plainWritten"))
	} else {
		fmt.Fprintln(e.Stdout, l.T("exportKeep"))
	}
	if rotates {
		fmt.Fprintln(e.Stdout, l.T("exportRotation"))
	}
	return nil
}

func confirmPlain(e Env) (bool, error) {
	l := e.Lang
	if !e.Interactive || e.Stdin == nil {
		return false, errors.New(l.T("plainNeedsTerminal"))
	}
	in := bufio.NewReader(e.Stdin)
	fmt.Fprint(e.Stdout, l.T("plainWarning"))
	line, _ := in.ReadString('\n')
	if a := strings.ToLower(strings.TrimSpace(line)); a != "y" && a != "yes" {
		return false, nil
	}
	fmt.Fprint(e.Stdout, l.T("plainType"))
	line, _ = in.ReadString('\n')
	return strings.TrimSpace(line) == "plaintext", nil
}

func readSecret(e Env, prompt string) (string, error) {
	if !e.Interactive || e.Secret == nil {
		return "", errors.New(e.Lang.T("passphraseTerminal"))
	}
	fmt.Fprint(e.Stdout, prompt)
	s, err := e.Secret()
	fmt.Fprintln(e.Stdout)
	return s, err
}

func newPassphrase(e Env) (string, error) {
	l := e.Lang
	pass, err := readSecret(e, l.T("passphraseNew", transfer.MinPassphrase))
	if err != nil {
		return "", err
	}
	if utf8.RuneCountInString(pass) < transfer.MinPassphrase {
		return "", errors.New(l.T("passphraseShort", transfer.MinPassphrase))
	}
	again, err := readSecret(e, l.T("passphraseRepeat"))
	if err != nil {
		return "", err
	}
	if pass != again {
		return "", errors.New(l.T("passphraseMismatch"))
	}
	return pass, nil
}

func importAccounts(e Env, names []string, args []string) error {
	l := e.Lang
	if len(args) != 1 {
		return usageError(l.T("needsArgs", "import", 1))
	}
	path := args[0]
	if strings.HasPrefix(path, "--") {
		return usageError(l.T("unknownFlag", path))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	f, err := transfer.Parse(data)
	if err != nil {
		return err
	}
	var pass string
	if f.Encrypted() {
		if pass, err = readSecret(e, l.T("passphraseAsk")); err != nil {
			return err
		}
	}
	bundle, err := f.Open([]byte(pass))
	if err != nil {
		return err
	}
	wanted := map[string]bool{}
	for _, name := range names {
		wanted[name] = true
	}
	total, failed := 0, 0
	for _, name := range providerOrder {
		entries := bundle[name]
		if !wanted[name] || len(entries) == 0 {
			continue
		}
		p := providers[name]
		m := e.manager(p)
		for i, en := range entries {
			total++
			who := fmt.Sprintf("%s #%d", p.display, i+1)
			email := who
			if id, err := m.P.Identify(en.Snapshot); err == nil {
				email = ui.Clean(id.Email)
				who = email
			}
			if en.Alias != "" {
				who = ui.Clean(en.Alias) + " <" + email + ">"
			}
			if !adopt(e, m, en.Snapshot, en.Alias, who, email) {
				failed++
			}
		}
	}
	if len(names) > 1 {
		var unknown []string
		for name := range bundle {
			if _, ok := providers[name]; !ok && len(bundle[name]) > 0 {
				unknown = append(unknown, name)
			}
		}
		sort.Strings(unknown)
		for _, name := range unknown {
			fmt.Fprintln(e.Stdout, l.T("importUnknownAgent", len(bundle[name]), ui.Clean(name)))
			total += len(bundle[name])
			failed += len(bundle[name])
		}
	}
	if total == 0 {
		fmt.Fprintln(e.Stdout, l.T("importNone"))
		return nil
	}
	if f.Encrypted() {
		fmt.Fprintln(e.Stdout, l.T("importDelete", path))
	} else {
		fmt.Fprintln(e.Stdout, l.T("importPlainDelete", path))
	}
	if failed > 0 {
		return errors.New(l.T("importFileIncomplete", failed))
	}
	return nil
}
