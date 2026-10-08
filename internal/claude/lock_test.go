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
