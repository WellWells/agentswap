package links

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func fakeExe(t *testing.T) (string, string) {
	dir := t.TempDir()
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	exe := filepath.Join(dir, "agentswap"+ext)
	if err := os.WriteFile(exe, []byte("v1"), 0o755); err != nil {
		t.Fatal(err)
	}
	return exe, ext
}

func sameFile(t *testing.T, a, b string) bool {
	fa, err := os.Stat(a)
	if err != nil {
		return false
	}
	fb, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(fa, fb)
}

func TestLinkCreatesNamesNextToExe(t *testing.T) {
	exe, ext := fakeExe(t)
	if err := Link(exe, []string{"cxswap", "ccswap"}); err != nil {
		t.Fatal(err)
	}
	if err := Link(exe, []string{"cxswap", "ccswap"}); err != nil {
		t.Fatalf("second link: %v", err)
	}
	for _, n := range []string{"cxswap", "ccswap"} {
		p := filepath.Join(filepath.Dir(exe), n+ext)
		if !sameFile(t, exe, p) {
			t.Errorf("%s does not point to agentswap", p)
		}
		if _, err := os.Lstat(p + ".new"); err == nil {
			t.Errorf("leftover %s.new", p)
		}
	}
}

func TestLinkReplacesStaleFile(t *testing.T) {
	exe, ext := fakeExe(t)
	stale := filepath.Join(filepath.Dir(exe), "cxswap"+ext)
	os.WriteFile(stale, []byte("old"), 0o755)
	if err := Link(exe, []string{"cxswap"}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(stale); string(b) != "v1" || !sameFile(t, exe, stale) {
		t.Fatalf("stale file not replaced: %q", b)
	}
}

func TestLinkFollowsUpdatedExe(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hardlinks on windows are re-created by running link again")
	}
	exe, _ := fakeExe(t)
	if err := Link(exe, []string{"cxswap"}); err != nil {
		t.Fatal(err)
	}
	tmp := exe + ".tmp"
	os.WriteFile(tmp, []byte("v2"), 0o755)
	os.Rename(tmp, exe)
	if b, _ := os.ReadFile(filepath.Join(filepath.Dir(exe), "cxswap")); string(b) != "v2" {
		t.Fatalf("got %q", b)
	}
}

func TestLinkRefusesDirectory(t *testing.T) {
	exe, ext := fakeExe(t)
	os.Mkdir(filepath.Join(filepath.Dir(exe), "cxswap"+ext), 0o755)
	if err := Link(exe, []string{"cxswap"}); err == nil {
		t.Fatal("expected error")
	}
}

func TestUnlinkRemovesOnlyOwnLinks(t *testing.T) {
	exe, ext := fakeExe(t)
	dir := filepath.Dir(exe)
	if err := Link(exe, []string{"cxswap"}); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(dir, "ccswap"+ext)
	os.WriteFile(foreign, []byte("someone else"), 0o755)
	removed, err := Unlink(exe, []string{"cxswap", "ccswap", "claudeswap"})
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 1 || removed[0] != "cxswap" {
		t.Fatalf("removed %v", removed)
	}
	if _, err := os.Lstat(filepath.Join(dir, "cxswap"+ext)); !os.IsNotExist(err) {
		t.Fatalf("cxswap still exists: %v", err)
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Fatalf("foreign file removed: %v", err)
	}
	if _, err := os.Stat(exe); err != nil {
		t.Fatalf("exe removed: %v", err)
	}
}
