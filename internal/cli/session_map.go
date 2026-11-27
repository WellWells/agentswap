package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/WellWells/agentswap/internal/fsx"
	"github.com/WellWells/agentswap/internal/swap"
	"github.com/WellWells/agentswap/internal/ui"
)

type sessionMappings map[string]string

const mappingFile = "directory-mappings.json"

func readSessionMappings(dir string) (sessionMappings, error) {
	result := sessionMappings{}
	path := filepath.Join(dir, mappingFile)
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &result); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if result == nil {
		return nil, fmt.Errorf("%s: expected a JSON object", path)
	}
	return result, nil
}

func sessionDirectory(path string, mustExist bool) (string, error) {
	dir, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(dir)
	if !mustExist && errors.Is(err, os.ErrNotExist) {
		return dir, nil
	}
	if err != nil {
		return "", err
	}
	st, err := os.Stat(real)
	if err != nil {
		return "", err
	}
	if !st.IsDir() {
		return "", &os.PathError{Op: "map", Path: real, Err: os.ErrInvalid}
	}
	return real, nil
}

func sameDirectory(a, b string) bool {
	rel, err := filepath.Rel(a, b)
	return err == nil && rel == "."
}

func (paths sessionMappings) exact(dir string) (string, bool) {
	if _, ok := paths[dir]; ok {
		return dir, true
	}
	for path := range paths {
		if sameDirectory(path, dir) {
			return path, true
		}
	}
	return "", false
}

func sessionMapCommand(e Env, prog string, m *swap.Manager, cmd string, args []string) error {
	if cmd == "map" && len(args) > 2 || cmd == "unmap" && len(args) > 1 {
		return usageError(e.Lang.T("sessionMapUsage", prog))
	}
	lock, err := fsx.Acquire(m.S.LockPath(), 15*time.Second)
	if err != nil {
		return err
	}
	defer lock.Release()
	paths, err := readSessionMappings(m.S.Dir)
	if err != nil {
		return err
	}
	var target, message string
	if cmd == "map" {
		r, err := m.S.Load()
		if err != nil {
			return err
		}
		if len(args) == 0 {
			if len(paths) == 0 {
				fmt.Fprintln(e.Stdout, e.Lang.T("sessionNoMaps"))
			}
			dirs := make([]string, 0, len(paths))
			for dir := range paths {
				dirs = append(dirs, dir)
			}
			sort.Strings(dirs)
			for _, dir := range dirs {
				name := e.Lang.T("sessionRemoved")
				if i := r.Index(paths[dir]); i >= 0 {
					name = label(r.Accounts[i])
				}
				fmt.Fprintf(e.Stdout, "%s  →  %s\n", ui.Clean(dir), name)
			}
			return nil
		}
		i, err := r.Find(args[0])
		if err != nil {
			return withQuery(args[0], err)
		}
		account := r.Accounts[i]
		target, message = account.Key, label(account)
		args = args[1:]
	}
	dir := "."
	if len(args) > 0 {
		dir = args[0]
	}
	dir, err = sessionDirectory(dir, cmd == "map")
	if err != nil {
		return err
	}
	previous, found := paths.exact(dir)
	if cmd == "unmap" {
		if !found {
			return errors.New(e.Lang.T("sessionMapMissing", ui.Clean(dir)))
		}
		delete(paths, previous)
		message = e.Lang.T("sessionUnmapped", ui.Clean(dir))
	} else {
		if found {
			delete(paths, previous)
		}
		paths[dir] = target
		message = e.Lang.T("sessionMapped", ui.Clean(dir), message)
	}
	b, err := json.MarshalIndent(paths, "", "  ")
	if err != nil {
		return err
	}
	if err := fsx.WriteAtomic(filepath.Join(m.S.Dir, mappingFile), append(b, '\n'), 0600); err != nil {
		return err
	}
	fmt.Fprintln(e.Stderr, message)
	return nil
}
