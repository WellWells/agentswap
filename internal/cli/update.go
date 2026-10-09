package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/WellWells/agentswap/internal/fsx"
	"github.com/WellWells/agentswap/internal/links"
	"github.com/WellWells/agentswap/internal/update"
)

const (
	updateEvery   = 12 * time.Hour
	updateTimeout = 3 * time.Second
	updateGrace   = time.Second
)

func isUpdate(s string) bool { return s == "update" || s == "upgrade" }

func (e Env) updateCache() string {
	return filepath.Join(e.dir("AGENTSWAP_HOME", ".agentswap"), "update.json")
}

func (e Env) upgradeCmd() string {
	if update.Homebrew(e.Exe) {
		return "brew upgrade agentswap"
	}
	return "agentswap update"
}

func selfUpdate(e Env) error {
	l := e.Lang
	if !update.Valid(e.Version) {
		return errors.New(l.T("updateDev", e.Version))
	}
	exe, err := e.exe()
	if err != nil {
		return err
	}
	if update.Homebrew(exe) {
		return errors.New(l.T("updateBrew"))
	}
	lock, err := fsx.Acquire(filepath.Join(e.dir("AGENTSWAP_HOME", ".agentswap"), "update.lock"), 2*time.Second)
	if err != nil {
		return err
	}
	defer lock.Release()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	tag, err := e.Releases.Latest(ctx)
	if err != nil {
		return err
	}
	update.SaveCache(e.updateCache(), update.Cache{Checked: e.now(), Latest: tag})
	if !update.Newer(tag, e.Version) {
		fmt.Fprintln(e.Stdout, l.T("upToDate", e.Version))
		return nil
	}
	fmt.Fprintln(e.Stdout, l.T("updating", e.Version, tag))
	name := update.Asset(e.goos(), runtime.GOARCH)
	sums, err := e.Releases.Download(ctx, tag, "checksums.txt")
	if err != nil {
		return err
	}
	archive, err := e.Releases.Download(ctx, tag, name)
	if err != nil {
		return err
	}
	if err := update.Verify(sums, name, archive); err != nil {
		return err
	}
	bin, err := update.Extract(archive, e.goos())
	if err != nil {
		return err
	}
	err = update.Install(exe, bin, func() error { return e.runNew(exe, "version") })
	if errors.Is(err, os.ErrPermission) {
		return errors.New(l.T("updatePerm", filepath.Dir(exe)))
	}
	if err != nil {
		return err
	}
	if err := e.relink(exe); err != nil {
		return errors.New(l.T("updateLinkFailed", tag, err))
	}
	e.removeOld()
	fmt.Fprintln(e.Stdout, l.T("updated", e.Version, tag))
	return nil
}

func (e Env) runNew(exe string, args ...string) error {
	if e.Output == nil {
		return nil
	}
	out, err := e.Output(nil, exe, args...)
	if err != nil {
		return fmt.Errorf("%s %s: %w: %s", filepath.Base(exe), args[0], err, bytes.TrimSpace(out))
	}
	return nil
}

func (e Env) relink(exe string) error {
	if e.Output == nil {
		return links.Link(exe, commandNames())
	}
	return e.runNew(exe, "link")
}

func (e Env) removeOld() {
	if e.Exe == "" {
		return
	}
	old := e.Exe + ".old"
	if _, err := os.Stat(old); err != nil || links.Shared(e.Exe, old, commandNames()) {
		return
	}
	os.Remove(old)
}

func (e Env) checkUpdate(skip bool) func() {
	if skip || !e.Notify || e.Releases.Repo == "" || !update.Valid(e.Version) || e.Getenv("AGENTSWAP_NO_UPDATE_CHECK") != "" {
		return func() {}
	}
	path := e.updateCache()
	cache := update.LoadCache(path)
	now := e.now()
	var done chan update.Cache
	if now.Sub(cache.Checked) >= updateEvery || cache.Checked.After(now) {
		done = make(chan update.Cache, 1)
		go func(c update.Cache) {
			ctx, cancel := context.WithTimeout(context.Background(), updateTimeout)
			defer cancel()
			if tag, err := e.Releases.Latest(ctx); err == nil {
				c.Latest = tag
			}
			c.Checked = now
			done <- c
		}(cache)
	}
	return func() {
		if done != nil {
			select {
			case c := <-done:
				cache = c
			case <-time.After(updateGrace):
				cache.Checked = now
			}
			update.SaveCache(path, cache)
		}
		if update.Newer(cache.Latest, e.Version) {
			fmt.Fprintf(e.Stderr, "\n%s\n", e.Lang.T("updateAvailable", cache.Latest, e.Version, e.upgradeCmd()))
		}
	}
}
