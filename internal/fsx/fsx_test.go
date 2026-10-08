package fsx

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestWriteAtomicCreatesNestedFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a", "b", "f.json")
	if err := WriteAtomic(p, []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(p)
	if err != nil || string(got) != "one" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestWriteAtomicReplacesWithoutLeftovers(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.json")
	if err := WriteAtomic(p, []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomic(p, []byte("two"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	if string(got) != "two" {
		t.Fatalf("got %q", got)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("leftover files: %v", entries)
	}
}

func TestWriteAtomicSetsPrivateMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permissions")
	}
	p := filepath.Join(t.TempDir(), "f.json")
	if err := WriteAtomic(p, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(p)
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode().Perm())
	}
}

func TestLockBlocksSecondHolder(t *testing.T) {
	p := filepath.Join(t.TempDir(), "lock")
	l, err := Acquire(p, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Acquire(p, 100*time.Millisecond); !errors.Is(err, ErrLocked) {
		t.Fatalf("want ErrLocked, got %v", err)
	}
	if err := l.Release(); err != nil {
		t.Fatal(err)
	}
	l2, err := Acquire(p, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("reacquire: %v", err)
	}
	l2.Release()
}

func TestLockReusesLeftoverFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "lock")
	os.WriteFile(p, []byte("1"), 0o600)
	l, err := Acquire(p, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("leftover lock file blocked: %v", err)
	}
	l.Release()
}

func TestLockOldMtimeDoesNotStealHeldLock(t *testing.T) {
	p := filepath.Join(t.TempDir(), "lock")
	l, err := Acquire(p, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Release()
	old := time.Now().Add(-time.Hour)
	os.Chtimes(p, old, old)
	if l2, err := Acquire(p, 100*time.Millisecond); !errors.Is(err, ErrLocked) {
		if l2 != nil {
			l2.Release()
		}
		t.Fatalf("want ErrLocked, got %v", err)
	}
}

func TestLockRejectsDirectory(t *testing.T) {
	p := filepath.Join(t.TempDir(), "lock")
	os.MkdirAll(filepath.Join(p, "x"), 0o700)
	start := time.Now()
	if _, err := Acquire(p, 5*time.Second); err == nil || errors.Is(err, ErrLocked) {
		t.Fatalf("got %v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("did not fail fast")
	}
}

func TestLockReleasedWhenHolderExits(t *testing.T) {
	if p := os.Getenv("FSX_LOCK_CHILD"); p != "" {
		if _, err := Acquire(p, time.Second); err != nil {
			os.Exit(3)
		}
		os.Exit(0)
	}
	p := filepath.Join(t.TempDir(), "lock")
	cmd := exec.Command(os.Args[0], "-test.run=^TestLockReleasedWhenHolderExits$")
	cmd.Env = append(os.Environ(), "FSX_LOCK_CHILD="+p)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("child: %v %s", err, out)
	}
	l, err := Acquire(p, time.Second)
	if err != nil {
		t.Fatalf("lock not released after holder exited: %v", err)
	}
	l.Release()
}
