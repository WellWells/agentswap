package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/WellWells/agentswap/internal/update"
)

type release struct {
	tag      string
	bin      string
	badSum   bool
	latests  atomic.Int32
	download atomic.Int32
}

func (r *release) serve(t *testing.T) update.Source {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	tw.WriteHeader(&tar.Header{Name: "agentswap", Mode: 0o755, Size: int64(len(r.bin)), Typeflag: tar.TypeReg})
	tw.Write([]byte(r.bin))
	tw.Close()
	gz.Close()
	archive := buf.Bytes()
	asset := update.Asset("linux", runtime.GOARCH)
	sum := sha256.Sum256(archive)
	if r.badSum {
		sum = sha256.Sum256(nil)
	}
	sums := hex.EncodeToString(sum[:]) + "  " + asset + "\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/o/r/releases/latest":
			r.latests.Add(1)
			http.Redirect(w, req, "/o/r/releases/tag/"+r.tag, http.StatusFound)
		case "/o/r/releases/download/" + r.tag + "/checksums.txt":
			w.Write([]byte(sums))
		case "/o/r/releases/download/" + r.tag + "/" + asset:
			r.download.Add(1)
			w.Write(archive)
		default:
			http.NotFound(w, req)
		}
	}))
	t.Cleanup(srv.Close)
	return update.Source{Base: srv.URL, Repo: "o/r"}
}

func updateHarness(t *testing.T, r *release) (*harness, *[]string) {
	h := newHarness(t)
	h.version = "v1.0.0"
	h.releases = r.serve(t)
	h.exe = filepath.Join(t.TempDir(), "agentswap")
	os.WriteFile(h.exe, []byte("old"), 0o755)
	var calls []string
	h.output = func(env []string, name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return nil, nil
	}
	return h, &calls
}

func TestUpdateInstallsLatestRelease(t *testing.T) {
	r := &release{tag: "v1.1.0", bin: "new"}
	h, calls := updateHarness(t, r)
	code, out, errOut := h.run("agentswap", "update")
	if code != 0 {
		t.Fatalf("code %d: %s", code, errOut)
	}
	if !strings.Contains(out, "Updated agentswap v1.0.0 → v1.1.0") {
		t.Fatalf("stdout = %q", out)
	}
	if b, _ := os.ReadFile(h.exe); string(b) != "new" {
		t.Fatalf("exe = %q", b)
	}
	if want := []string{h.exe + " version", h.exe + " link"}; strings.Join(*calls, "|") != strings.Join(want, "|") {
		t.Fatalf("calls = %q, want %q", *calls, want)
	}
	if _, err := os.Stat(h.exe + ".old"); !os.IsNotExist(err) {
		t.Error(".old left behind")
	}
	if c := update.LoadCache(filepath.Join(h.home, ".agentswap", "update.json")); c.Latest != "v1.1.0" {
		t.Fatalf("cache = %+v", c)
	}
}

func TestUpgradeAlias(t *testing.T) {
	r := &release{tag: "v1.1.0", bin: "new"}
	h, _ := updateHarness(t, r)
	if code, _, errOut := h.run("agentswap", "upgrade"); code != 0 {
		t.Fatalf("code %d: %s", code, errOut)
	}
	if b, _ := os.ReadFile(h.exe); string(b) != "new" {
		t.Fatalf("exe = %q", b)
	}
}

func TestUpdateAlreadyLatest(t *testing.T) {
	for _, tag := range []string{"v1.0.0", "v1.0.0-rc.9"} {
		r := &release{tag: tag, bin: "new"}
		h, _ := updateHarness(t, r)
		code, out, _ := h.run("agentswap", "update")
		if code != 0 || !strings.Contains(out, "agentswap v1.0.0 is up to date") {
			t.Fatalf("%s: code %d, stdout %q", tag, code, out)
		}
		if r.download.Load() != 0 {
			t.Fatalf("%s: downloaded anyway", tag)
		}
	}
}

func TestUpdateRejectsBadChecksum(t *testing.T) {
	r := &release{tag: "v1.1.0", bin: "new", badSum: true}
	h, calls := updateHarness(t, r)
	code, _, errOut := h.run("agentswap", "update")
	if code != 1 || !strings.Contains(errOut, "does not match checksums.txt") {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
	if b, _ := os.ReadFile(h.exe); string(b) != "old" || len(*calls) != 0 {
		t.Fatalf("exe = %q, calls = %q", b, *calls)
	}
}

func TestUpdateRollsBackWhenNewBinaryFails(t *testing.T) {
	r := &release{tag: "v1.1.0", bin: "new"}
	h, _ := updateHarness(t, r)
	h.output = func(env []string, name string, args ...string) ([]byte, error) {
		return []byte("bad binary"), errors.New("exit status 1")
	}
	code, _, errOut := h.run("agentswap", "update")
	if code != 1 || !strings.Contains(errOut, "bad binary") {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
	if b, _ := os.ReadFile(h.exe); string(b) != "old" {
		t.Fatalf("exe = %q", b)
	}
}

func TestUpdateKeepsNewBinaryWhenLinkFails(t *testing.T) {
	r := &release{tag: "v1.1.0", bin: "new"}
	h, _ := updateHarness(t, r)
	h.output = func(env []string, name string, args ...string) ([]byte, error) {
		if args[0] == "link" {
			return []byte("Access is denied."), errors.New("exit status 1")
		}
		return nil, nil
	}
	code, _, errOut := h.run("agentswap", "update")
	if code != 1 || !strings.Contains(errOut, "updated to v1.1.0, but recreating the command names failed") || !strings.Contains(errOut, "run `agentswap link`") {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
	if b, _ := os.ReadFile(h.exe); string(b) != "new" {
		t.Fatalf("exe = %q", b)
	}
}

func TestRemoveOldKeepsBinaryStillUsedByAliases(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "agentswap")
	alias := filepath.Join(dir, "cxswap")
	if runtime.GOOS == "windows" {
		exe += ".exe"
		alias += ".exe"
	}
	os.WriteFile(exe, []byte("new"), 0o755)
	os.WriteFile(exe+".old", []byte("old"), 0o755)
	if err := os.Link(exe+".old", alias); err != nil {
		t.Skip("hard links unsupported:", err)
	}
	e := Env{Exe: exe}
	e.removeOld()
	if _, err := os.Stat(exe + ".old"); err != nil {
		t.Fatal(".old removed while cxswap still links to it")
	}
	os.Remove(alias)
	e.removeOld()
	if _, err := os.Stat(exe + ".old"); !os.IsNotExist(err) {
		t.Fatal("orphaned .old not removed")
	}
}

func TestUpdateRefusesDevAndHomebrew(t *testing.T) {
	r := &release{tag: "v1.1.0", bin: "new"}
	h, _ := updateHarness(t, r)
	h.version = "dev-abc123"
	code, _, errOut := h.run("agentswap", "update")
	if code != 1 || !strings.Contains(errOut, "this build (dev-abc123) cannot update itself") {
		t.Fatalf("dev: code %d, stderr %q", code, errOut)
	}

	h.version = "v1.0.0"
	h.exe = filepath.Join(t.TempDir(), "Cellar", "agentswap", "1.0.0", "bin", "agentswap")
	code, _, errOut = h.run("agentswap", "update")
	if code != 1 || !strings.Contains(errOut, "brew upgrade agentswap") {
		t.Fatalf("brew: code %d, stderr %q", code, errOut)
	}
	if r.latests.Load() != 0 {
		t.Fatal("refused updates must not contact GitHub")
	}
}

func TestUpdateNotice(t *testing.T) {
	r := &release{tag: "v1.1.0", bin: "new"}
	h, _ := updateHarness(t, r)
	h.notify = true
	notice := "A new agentswap v1.1.0 is available (current v1.0.0). Run `agentswap update` to update."

	code, out, errOut := h.run("agentswap", "version")
	if code != 0 || !strings.Contains(out, "agentswap v1.0.0") || !strings.Contains(errOut, notice) {
		t.Fatalf("first run: code %d, stdout %q, stderr %q", code, out, errOut)
	}
	if r.latests.Load() != 1 {
		t.Fatalf("latest requests = %d", r.latests.Load())
	}

	h.now = h.now.Add(11 * time.Hour)
	_, _, errOut = h.run("agentswap", "version")
	if !strings.Contains(errOut, notice) || r.latests.Load() != 1 {
		t.Fatalf("cached run: stderr %q, requests %d", errOut, r.latests.Load())
	}

	h.now = h.now.Add(2 * time.Hour)
	r.tag = "v1.0.0"
	_, _, errOut = h.run("agentswap", "version")
	if errOut != "" || r.latests.Load() != 2 {
		t.Fatalf("after 13h: stderr %q, requests %d", errOut, r.latests.Load())
	}
}

func TestUpdateNoticeSkipped(t *testing.T) {
	r := &release{tag: "v1.1.0", bin: "new"}
	h, _ := updateHarness(t, r)

	h.run("agentswap", "version")
	h.notify = true
	h.env = map[string]string{"AGENTSWAP_NO_UPDATE_CHECK": "1"}
	h.run("agentswap", "version")
	h.env = nil
	h.version = "dev-abc123"
	h.run("agentswap", "version")
	if r.latests.Load() != 0 {
		t.Fatalf("latest requests = %d, want 0", r.latests.Load())
	}

	h.version = "v1.0.0"
	_, out, errOut := h.run("agentswap", "update")
	if strings.Contains(errOut, "is available") || !strings.Contains(out, "Updated") || r.latests.Load() != 1 {
		t.Fatalf("update: stdout %q, stderr %q, requests %d", out, errOut, r.latests.Load())
	}
}

func TestUpdateNoticeForHomebrewAndProviders(t *testing.T) {
	r := &release{tag: "v1.1.0", bin: "new"}
	h, _ := updateHarness(t, r)
	h.notify = true
	h.exe = "/opt/homebrew/Cellar/agentswap/1.0.0/bin/agentswap"
	_, _, errOut := h.run("cxswap", "help")
	if !strings.Contains(errOut, "Run `brew upgrade agentswap` to update.") {
		t.Fatalf("stderr = %q", errOut)
	}
}

func TestUpdateNoticeGivesUpOnSlowNetwork(t *testing.T) {
	unblock := make(chan struct{})
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		hits.Add(1)
		<-unblock
		http.Redirect(w, req, "/o/r/releases/tag/v9.0.0", http.StatusFound)
	}))
	defer srv.Close()
	defer close(unblock)
	h := newHarness(t)
	h.version = "v1.0.0"
	h.notify = true
	h.releases = update.Source{Base: srv.URL, Repo: "o/r"}
	start := time.Now()
	_, _, errOut := h.run("agentswap", "version")
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("waited %v", d)
	}
	if errOut != "" {
		t.Fatalf("stderr = %q", errOut)
	}
	h.run("agentswap", "version")
	if hits.Load() != 1 {
		t.Fatalf("requests = %d, want 1", hits.Load())
	}
}
