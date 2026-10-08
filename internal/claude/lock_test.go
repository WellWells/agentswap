package claude

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLockLiveCreatesAndRemovesClaudeLocks(t *testing.T) {
	p, home := setup(t)
	os.MkdirAll(p.ConfigDir, 0o700)
	p.LockWait = 200 * time.Millisecond
	unlock, err := p.LockLive()
	if err != nil {
		t.Fatal(err)
	}
	dirs := []string{filepath.Join(home, ".claude", ".oauth_refresh.lock"), filepath.Join(home, ".claude.lock"), filepath.Join(home, ".claude.json.lock")}
	for _, d := range dirs {
		if st, err := os.Stat(d); err != nil || !st.IsDir() {
			t.Fatalf("%s: %v", d, err)
		}
	}
	unlock()
	for _, d := range dirs {
		if _, err := os.Stat(d); !os.IsNotExist(err) {
			t.Fatalf("%s left behind", d)
		}
	}
}

func TestLockLiveWaitsForHeldLockAndTakesOverStale(t *testing.T) {
	p, home := setup(t)
	os.MkdirAll(p.ConfigDir, 0o700)
	p.LockWait = 200 * time.Millisecond
	held := filepath.Join(home, ".claude.json.lock")
	os.MkdirAll(held, 0o700)
	if _, err := p.LockLive(); !errors.Is(err, ErrBusy) {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude.lock")); !os.IsNotExist(err) {
		t.Fatal("partial locks not released")
	}
	old := time.Now().Add(-time.Minute)
	os.Chtimes(held, old, old)
	unlock, err := p.LockLive()
	if err != nil {
		t.Fatalf("stale takeover: %v", err)
	}
	unlock()
}

func TestLockLiveTimesOutOnUnremovableStaleLock(t *testing.T) {
	p, home := setup(t)
	os.MkdirAll(p.ConfigDir, 0o700)
	p.LockWait = 100 * time.Millisecond
	held := filepath.Join(home, ".claude", ".oauth_refresh.lock")
	os.MkdirAll(held, 0o700)
	os.WriteFile(filepath.Join(held, "x"), []byte("x"), 0o600)
	old := time.Now().Add(-time.Hour)
	os.Chtimes(held, old, old)
	done := make(chan error, 1)
	go func() {
		unlock, err := p.LockLive()
		if err == nil {
			unlock()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, ErrBusy) {
			t.Fatalf("err = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("LockLive ignored LockWait")
	}
}

func TestLockLiveDoesNotCreateClaudeDirForCodexOnlyUsers(t *testing.T) {
	p, home := setup(t)
	unlock, err := p.LockLive()
	if err != nil {
		t.Fatal(err)
	}
	unlock()
	if _, err := os.Stat(filepath.Join(home, ".claude")); !os.IsNotExist(err) {
		t.Fatal("~/.claude created")
	}
}
