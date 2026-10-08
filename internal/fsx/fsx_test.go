package fsx

import (
	"errors"
	"os"
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
	l, err := Acquire(p, time.Second, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Acquire(p, 100*time.Millisecond, time.Minute); !errors.Is(err, ErrLocked) {
		t.Fatalf("want ErrLocked, got %v", err)
	}
	if err := l.Release(); err != nil {
		t.Fatal(err)
	}
	l2, err := Acquire(p, 100*time.Millisecond, time.Minute)
	if err != nil {
		t.Fatalf("reacquire: %v", err)
	}
	l2.Release()
}

func TestLockTakesOverStaleLock(t *testing.T) {
	p := filepath.Join(t.TempDir(), "lock")
	os.WriteFile(p, []byte("1"), 0o600)
	old := time.Now().Add(-time.Hour)
	os.Chtimes(p, old, old)
	l, err := Acquire(p, 100*time.Millisecond, time.Minute)
	if err != nil {
		t.Fatalf("stale lock not taken over: %v", err)
	}
	l.Release()
}
