package claude

import (
	"errors"
	"fmt"
	"io/fs"
	"math/rand"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/WellWells/agentswap/internal/fsx"
)

var ErrBusy = errors.New("Claude Code is updating its login; try again in a few seconds")

var ErrLeaseLost = fmt.Errorf("another process took over the Claude Code login lock: %w", ErrBusy)

type dirLock struct {
	path  string
	dir   *os.File
	stale time.Duration
	once  sync.Once
	stop  chan struct{}
	done  chan struct{}
	mu    sync.Mutex
	mtime time.Time
	lost  bool
}

var leases = struct {
	sync.Mutex
	held map[string][]*dirLock
}{held: map[string][]*dirLock{}}

func hold(l *dirLock) {
	leases.Lock()
	defer leases.Unlock()
	leases.held[l.path] = append(leases.held[l.path], l)
}

func forget(l *dirLock) {
	leases.Lock()
	defer leases.Unlock()
	held := leases.held[l.path]
	for i, h := range held {
		if h == l {
			held = append(held[:i:i], held[i+1:]...)
			break
		}
	}
	if len(held) == 0 {
		delete(leases.held, l.path)
	} else {
		leases.held[l.path] = held
	}
}

func heldLocks(path string) []*dirLock {
	leases.Lock()
	defer leases.Unlock()
	return append([]*dirLock(nil), leases.held[path]...)
}

func acquireDir(path string, stale, wait time.Duration) (*dirLock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(wait)
	for {
		l, err := tryAcquireDir(path, stale)
		if l != nil || err != nil {
			return l, err
		}
		if time.Now().After(deadline) {
			return nil, ErrBusy
		}
		time.Sleep(min(time.Until(deadline), 250*time.Millisecond+time.Duration(rand.Int63n(int64(250*time.Millisecond)))))
	}
}

func tryAcquireDir(path string, stale time.Duration) (*dirLock, error) {
	guard, err := fsx.Acquire(path+".agentswap", 0)
	if errors.Is(err, fsx.ErrLocked) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer guard.Release()
	err = os.Mkdir(path, 0700)
	created := err == nil
	if err != nil && !errors.Is(err, fs.ErrExist) {
		return nil, err
	}
	dir, err := openLockDir(path)
	if err != nil {
		if created {
			return nil, err
		}
		return nil, nil
	}
	l := &dirLock{path: path, stale: stale, dir: dir, stop: make(chan struct{}), done: make(chan struct{})}
	if (created && l.current()) || (!created && l.claimStale()) {
		if !l.recordLease() {
			dir.Close()
			return nil, ErrBusy
		}
		go l.touch()
		return l, nil
	}
	dir.Close()
	return nil, nil
}

func (l *dirLock) current() bool {
	held, err := l.dir.Stat()
	if err != nil || !held.IsDir() {
		return false
	}
	st, err := os.Lstat(l.path)
	if err != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return false
	}
	current, err := openLockDir(l.path)
	if err != nil {
		return false
	}
	defer current.Close()
	st, err = current.Stat()
	return err == nil && os.SameFile(held, st)
}

func (l *dirLock) claimStale() bool {
	st, err := l.dir.Stat()
	if err != nil || !st.IsDir() || time.Since(st.ModTime()) <= l.stale {
		return false
	}
	entries, err := l.dir.Readdirnames(-1)
	if err != nil || len(entries) != 0 || !l.current() {
		return false
	}
	latest, err := l.dir.Stat()
	return err == nil && latest.ModTime().Equal(st.ModTime()) && touchLockDir(l.dir) == nil && l.current()
}

func (l *dirLock) recordLease() bool {
	st, err := l.dir.Stat()
	if err != nil {
		return false
	}
	l.mtime = st.ModTime()
	return true
}

func (l *dirLock) ownsLease() bool {
	st, err := l.dir.Stat()
	return err == nil && time.Since(l.mtime) < l.stale && st.ModTime().Equal(l.mtime) && l.current()
}

func (l *dirLock) renew() bool { return l.extend() == nil }

func (l *dirLock) extend() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.lost {
		return ErrLeaseLost
	}
	guard, err := fsx.Acquire(l.path+".agentswap", min(time.Second, l.stale/3))
	if err != nil {
		if l.expired() {
			l.lost = true
			return ErrLeaseLost
		}
		return fmt.Errorf("%w: %v", ErrBusy, err)
	}
	defer guard.Release()
	if !l.ownsLease() || touchLockDir(l.dir) != nil || !l.recordLease() {
		l.lost = true
		return ErrLeaseLost
	}
	return nil
}

func (l *dirLock) expired() bool { return time.Since(l.mtime) >= l.stale }

func (l *dirLock) touch() {
	t := time.NewTicker(3 * time.Second)
	defer t.Stop()
	defer close(l.done)
	for {
		select {
		case <-l.stop:
			return
		case <-t.C:
			if errors.Is(l.extend(), ErrLeaseLost) {
				return
			}
		}
	}
}

func (l *dirLock) release() {
	l.once.Do(func() {
		forget(l)
		close(l.stop)
		<-l.done
		defer l.dir.Close()
		l.mu.Lock()
		defer l.mu.Unlock()
		guard, err := fsx.Acquire(l.path+".agentswap", min(time.Second, l.stale/3))
		if err != nil {
			return
		}
		defer guard.Release()
		if l.ownsLease() {
			removeLockDir(l.dir)
		}
	})
}

type lockSpec struct {
	path  string
	stale time.Duration
}

func (p Provider) lockSpecs() []lockSpec {
	return []lockSpec{
		{filepath.Join(p.ConfigDir, ".oauth_refresh.lock"), 60 * time.Second},
		{realDir(p.ConfigDir) + ".lock", 60 * time.Second},
		{p.GlobalConfig + ".lock", 10 * time.Second},
	}
}

func (p Provider) storageLockSpec() lockSpec {
	return lockSpec{filepath.Join(p.ConfigDir, ".storage-write.lock"), 15 * time.Second}
}

func realDir(dir string) string {
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		return real
	}
	return filepath.Clean(dir)
}

func (p Provider) fence() error {
	specs := append(p.lockSpecs(), p.storageLockSpec())
	if alias := filepath.Clean(p.ConfigDir) + ".lock"; alias != specs[1].path {
		specs = append(specs, lockSpec{alias, specs[1].stale})
	}
	for _, s := range specs {
		for _, l := range heldLocks(s.path) {
			if err := l.extend(); err != nil {
				return err
			}
		}
	}
	return nil
}

func (p Provider) lockWait() time.Duration {
	if p.LockWait == 0 {
		return 9 * time.Second
	}
	return p.LockWait
}

func (p Provider) lockStorage() (func(), error) {
	s := p.storageLockSpec()
	l, err := acquireDir(s.path, s.stale, p.lockWait())
	if err != nil {
		return nil, err
	}
	hold(l)
	return l.release, nil
}

func (p Provider) LockLive() (func(), error) {
	if p.Keychain == nil && p.Credman == nil {
		if _, err := os.Stat(p.ConfigDir); errors.Is(err, fs.ErrNotExist) {
			return func() {}, nil
		}
	}
	if err := os.MkdirAll(p.ConfigDir, 0o700); err != nil {
		return nil, err
	}
	wait := p.lockWait()
	var held []*dirLock
	release := func() {
		for i := len(held) - 1; i >= 0; i-- {
			held[i].release()
		}
	}
	for _, s := range p.lockSpecs() {
		l, err := acquireDir(s.path, s.stale, wait)
		if err != nil {
			release()
			return nil, err
		}
		hold(l)
		held = append(held, l)
	}
	return release, nil
}
