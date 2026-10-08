package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/WellWells/agentswap/internal/fsx"
	"github.com/WellWells/agentswap/internal/ui"
)

func (e Env) configPath() string {
	return filepath.Join(e.dir("AGENTSWAP_HOME", ".agentswap"), "config.json")
}

func (e Env) loadConfig() (map[string]any, error) {
	cfg := map[string]any{}
	b, err := os.ReadFile(e.configPath())
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return nil, fmt.Errorf("%s: %w", e.configPath(), err)
	}
	if cfg == nil {
		cfg = map[string]any{}
	}
	return cfg, nil
}

func (e Env) saveConfig(cfg map[string]any) error {
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return fsx.WriteAtomic(e.configPath(), append(b, '\n'), 0o600)
}

func (e Env) savedLang() (ui.Lang, bool) {
	cfg, err := e.loadConfig()
	if err != nil {
		return ui.En, false
	}
	s, _ := cfg["lang"].(string)
	return ui.ParseLang(s)
}

func (e Env) envLang() (string, bool) {
	v := e.Getenv("AGENTSWAP_LANG")
	_, ok := ui.LookupLang(v)
	return v, ok
}

func (e Env) applySavedLang() Env {
	e.autoLang = e.Lang
	if _, ok := e.envLang(); ok {
		return e
	}
	if l, ok := e.savedLang(); ok {
		e.Lang = l
	}
	return e
}

func language(e Env, args []string) error {
	if len(args) > 1 {
		return usageError(e.Lang.T("needsArgs", "lang", 1))
	}
	if len(args) == 0 {
		return showLang(e)
	}
	auto := strings.EqualFold(args[0], "auto")
	l, ok := ui.ParseLang(args[0])
	if n, err := strconv.Atoi(args[0]); err == nil && n >= 1 && n <= len(ui.Langs) {
		l, ok = ui.Langs[n-1], true
	}
	if !auto && !ok {
		return usageError(e.Lang.T("langUnknown", args[0]))
	}
	cfg, err := e.loadConfig()
	if err != nil {
		return err
	}
	if auto {
		delete(cfg, "lang")
		l = e.autoLang
	} else {
		cfg["lang"] = l.Code()
	}
	if err := e.saveConfig(cfg); err != nil {
		return err
	}
	if auto {
		fmt.Fprintln(e.Stdout, l.T("langAuto", l.Name(), l.Code()))
	} else {
		fmt.Fprintln(e.Stdout, l.T("langSet", l.Name(), l.Code()))
	}
	if v, ok := e.envLang(); ok {
		fmt.Fprintln(e.Stdout, l.T("langEnvOverride", v))
	}
	return nil
}

func showLang(e Env) error {
	l := e.Lang
	source := "langFromSystem"
	if _, ok := e.envLang(); ok {
		source = "langFromEnv"
	} else if _, ok := e.savedLang(); ok {
		source = "langFromConfig"
	}
	fmt.Fprintln(e.Stdout, l.T("langCurrent", l.Name(), l.Code(), l.T(source)))
	fmt.Fprintln(e.Stdout)
	ui.LangList(e.Stdout, l, e.Color)
	fmt.Fprintln(e.Stdout)
	fmt.Fprintln(e.Stdout, l.T("langHelp"))
	return nil
}
