package claude

import (
	"errors"
	"io/fs"
	"math/rand"
	"os"
	"path/filepath"
	"time"
)

var ErrBusy = errors.New("Claude Code is updating its login; try again in a few seconds")

type dirLock struct {
	path string
	stop chan struct{}
	done chan struct{}
}

func acquireDir(path string, stale, wait time.Duration) (*dirLock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(wait)
	for {
		err := os.Mkdir(path, 0o700)
		if err == nil {
			l := &dirLock{path: path, stop: make(chan struct{}), done: make(chan struct{})}
			go l.touch()
			return l, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, err
		}
		if st, serr := os.Stat(path); serr == nil && time.Since(st.ModTime()) > stale {
			os.Remove(path)
			continue
		}
		if time.Now().After(deadline) {
			return nil, ErrBusy
		}
		time.Sleep(250*time.Millisecond + time.Duration(rand.Int63n(int64(250*time.Millisecond))))
	}
}

func (l *dirLock) touch() {
	t := time.NewTicker(3 * time.Second)
	defer t.Stop()
	defer close(l.done)
	for {
		select {
		case <-l.stop:
			return
		case <-t.C:
			now := time.Now()
			os.Chtimes(l.path, now, now)
		}
	}
}

func (l *dirLock) release() {
	close(l.stop)
	<-l.done
	os.Remove(l.path)
}

func (p Provider) LockLive() (func(), error) {
	if p.Keychain == nil {
		if _, err := os.Stat(p.ConfigDir); errors.Is(err, fs.ErrNotExist) {
			return func() {}, nil
		}
	}
	wait := p.LockWait
	if wait == 0 {
		wait = 9 * time.Second
	}
	specs := []struct {
		path  string
		stale time.Duration
	}{
		{filepath.Join(p.ConfigDir, ".oauth_refresh.lock"), 60 * time.Second},
		{filepath.Clean(p.ConfigDir) + ".lock", 60 * time.Second},
		{p.GlobalConfig + ".lock", 10 * time.Second},
	}
	var held []*dirLock
	release := func() {
		for i := len(held) - 1; i >= 0; i-- {
			held[i].release()
		}
	}
	for _, s := range specs {
		l, err := acquireDir(s.path, s.stale, wait)
		if err != nil {
			release()
			return nil, err
		}
		held = append(held, l)
	}
	return release, nil
}
