package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewer(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		{"v1.0.1", "v1.0.0", true},
		{"v1.1.0", "v1.0.9", true},
		{"v2.0.0", "v1.9.9", true},
		{"v1.0.0", "v1.0.0", false},
		{"v1.0.0", "v1.0.1", false},
		{"v1.0.0", "v1.0.0-rc.3", true},
		{"v1.0.0-rc.3", "v1.0.0", false},
		{"v1.0.0-rc.10", "v1.0.0-rc.9", true},
		{"v1.0.0-rc.2", "v1.0.0-rc.10", false},
		{"v1.0.0-rc.1", "v1.0.0-beta.9", true},
		{"v1.0.0-rc.1.1", "v1.0.0-rc.1", true},
		{"v1.0.0-alpha", "v1.0.0-1", true},
		{"v1.0.1", "dev", false},
		{"v1.0.1", "dev-abc123", false},
		{"latest", "v1.0.0", false},
		{"v1.0", "v0.9.0", false},
		{"v01.0.0", "v0.9.0", false},
		{"1.0.1", "v1.0.0", false},
		{"v1.0.1+build", "v1.0.0", true},
		{"v1.0.1+build.2-x", "v1.0.0", true},
		{"v9.9.9-\x1b]0;x\x07", "v1.0.0", false},
		{"v9.9.9-rc.1\x1b[2J", "v1.0.0", false},
		{"v9.9.9+\x1b[31m", "v1.0.0", false},
		{"v9.9.9-rc 1", "v1.0.0", false},
		{"v9.9.9-rc/../x", "v1.0.0", false},
		{"v9.9.9-rc..1", "v1.0.0", false},
		{"v9.9.9+", "v1.0.0", false},
	}
	for _, c := range cases {
		if got := Newer(c.latest, c.current); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.latest, c.current, got, c.want)
		}
	}
}

func TestLatest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/o/ok/releases/latest":
			http.Redirect(w, r, "/o/ok/releases/tag/v1.2.3-rc.1", http.StatusFound)
		case "/o/abs/releases/latest":
			http.Redirect(w, r, "https://github.com/o/abs/releases/tag/v2.0.0", http.StatusFound)
		case "/o/none/releases/latest":
			http.Redirect(w, r, "/o/none/releases", http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	for repo, want := range map[string]string{"o/ok": "v1.2.3-rc.1", "o/abs": "v2.0.0"} {
		got, err := Source{Base: srv.URL, Repo: repo}.Latest(context.Background())
		if err != nil || got != want {
			t.Errorf("%s: got %q, %v; want %q", repo, got, err, want)
		}
	}
	for _, repo := range []string{"o/none", "o/missing"} {
		if _, err := (Source{Base: srv.URL, Repo: repo}).Latest(context.Background()); !errors.Is(err, ErrNoTag) {
			t.Errorf("%s: err = %v, want ErrNoTag", repo, err)
		}
	}
}

func TestDownload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/o/r/releases/download/v1.0.0/a.zip":
			http.Redirect(w, r, "/blob", http.StatusFound)
		case "/blob":
			w.Write([]byte("data"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	s := Source{Base: srv.URL, Repo: "o/r"}
	b, err := s.Download(context.Background(), "v1.0.0", "a.zip")
	if err != nil || string(b) != "data" {
		t.Fatalf("got %q, %v", b, err)
	}
	if _, err := s.Download(context.Background(), "v1.0.0", "b.zip"); err == nil {
		t.Fatal("missing asset: want error")
	}
}

func sumLine(name string, data []byte) string {
	s := sha256.Sum256(data)
	return hex.EncodeToString(s[:]) + "  " + name + "\n"
}

func TestVerify(t *testing.T) {
	data := []byte("archive")
	sums := []byte(sumLine("other.zip", []byte("x")) + sumLine("a.zip", data))
	if err := Verify(sums, "a.zip", data); err != nil {
		t.Fatal(err)
	}
	if err := Verify(sums, "a.zip", []byte("tampered")); !errors.Is(err, ErrChecksum) {
		t.Fatalf("tampered: %v", err)
	}
	if err := Verify(sums, "b.zip", data); !errors.Is(err, ErrChecksum) {
		t.Fatalf("unlisted: %v", err)
	}
}

func tarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg})
		tw.Write([]byte(body))
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func zipOf(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, _ := zw.Create(name)
		w.Write([]byte(body))
	}
	zw.Close()
	return buf.Bytes()
}

func TestExtract(t *testing.T) {
	b, err := Extract(tarGz(t, map[string]string{"README.md": "r", "agentswap": "bin"}), "linux")
	if err != nil || string(b) != "bin" {
		t.Fatalf("tar: %q, %v", b, err)
	}
	b, err = Extract(zipOf(t, map[string]string{"LICENSE": "l", "agentswap.exe": "exe"}), "windows")
	if err != nil || string(b) != "exe" {
		t.Fatalf("zip: %q, %v", b, err)
	}
	if _, err := Extract(tarGz(t, map[string]string{"README.md": "r"}), "darwin"); err == nil {
		t.Fatal("missing binary: want error")
	}
	if _, err := Extract([]byte("junk"), "windows"); err == nil {
		t.Fatal("junk zip: want error")
	}
}

func TestInstall(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "agentswap")
	os.WriteFile(exe, []byte("old"), 0o755)
	checked := false
	if err := Install(exe, []byte("new"), func() error { checked = true; return nil }); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "new" || !checked {
		t.Fatalf("exe = %q, checked = %v", b, checked)
	}
	if b, _ := os.ReadFile(exe + ".old"); string(b) != "old" {
		t.Errorf(".old = %q, want the previous binary kept for the caller", b)
	}
	if _, err := os.Stat(exe + ".new"); !os.IsNotExist(err) {
		t.Error(".new left behind")
	}
}

func TestInstallRollsBackWhenCheckFails(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "agentswap")
	os.WriteFile(exe, []byte("old"), 0o755)
	boom := errors.New("boom")
	if err := Install(exe, []byte("new"), func() error { return boom }); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "old" {
		t.Fatalf("exe = %q, want old", b)
	}
	if _, err := os.Stat(exe + ".old"); !os.IsNotExist(err) {
		t.Error(".old left behind")
	}
}

func TestHomebrew(t *testing.T) {
	if !Homebrew("/opt/homebrew/Cellar/agentswap/1.0.0/bin/agentswap") || Homebrew(`C:\Users\u\AppData\Local\agentswap\bin\agentswap.exe`) {
		t.Error("Homebrew detection")
	}
}

func TestCache(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "update.json")
	if c := LoadCache(p); !c.Checked.IsZero() || c.Latest != "" {
		t.Fatalf("empty cache = %+v", c)
	}
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	if err := SaveCache(p, Cache{Checked: now, Latest: "v1.2.0"}); err != nil {
		t.Fatal(err)
	}
	if c := LoadCache(p); !c.Checked.Equal(now) || c.Latest != "v1.2.0" {
		t.Fatalf("cache = %+v", c)
	}
}
