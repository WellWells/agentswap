package official

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestFindBuild(t *testing.T) {
	data := "xx" + strings.Repeat("a", 3000) + "googlefile:/google_src/files/994719654/depot/google3" + strings.Repeat("b", 100)
	if got := findBuild(strings.NewReader(data)); got != "994719654" {
		t.Fatalf("whole: %q", got)
	}
	if got := findBuild(iotest.OneByteReader(strings.NewReader(data))); got != "994719654" {
		t.Fatalf("split: %q", got)
	}
	if got := findBuild(strings.NewReader("google_src/files/12/depot google_src/files/1234567/depot")); got != "1234567" {
		t.Fatalf("second match: %q", got)
	}
	if got := findBuild(strings.NewReader("no marker google_src/files/123456789")); got != "" {
		t.Fatalf("unterminated: %q", got)
	}
}

type fakeRun struct {
	out   map[string]string
	calls int
}

func (f *fakeRun) run(stdin []byte, name string, args ...string) ([]byte, int, error) {
	f.calls++
	key := filepath.Base(name) + " " + strings.Join(args, " ")
	out, ok := f.out[key]
	if !ok {
		return nil, -1, errors.New("unexpected " + key)
	}
	return []byte(out), 0, nil
}

func TestAgyDetectsVersionAndBuildWithCache(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "agy")
	os.WriteFile(exe, []byte("binary google_src/files/123456789/depot/google3 tail"), 0o755)
	f := &fakeRun{out: map[string]string{"agy --version": "1.4.0\n"}}
	look := func(name string) (string, error) {
		if name == "agy" {
			return exe, nil
		}
		return "", errors.New("missing")
	}
	cache := filepath.Join(dir, "clients.json")
	d := &Detector{Cache: cache, GOOS: "linux", GOARCH: "amd64", Run: f.run, Look: look}
	want := "antigravity/cli/1.4.0 (aidev_client; os_type=linux; arch=amd64; cl=123456789; auth_method=consumer)"
	if got := d.Agy(""); got != want {
		t.Fatalf("%q", got)
	}
	d2 := &Detector{Cache: cache, GOOS: "linux", GOARCH: "amd64", Run: f.run, Look: look}
	if got := d2.Agy("consumer"); got != want || f.calls != 1 {
		t.Fatalf("cached: %q calls=%d", got, f.calls)
	}
	os.WriteFile(exe, []byte("newer google_src/files/223456789/depot"), 0o755)
	f.out["agy --version"] = "agy 1.5.0"
	d3 := &Detector{Cache: cache, GOOS: "linux", GOARCH: "amd64", Run: f.run, Look: look}
	if got := d3.Agy("enterprise"); !strings.Contains(got, "cli/1.5.0 ") || !strings.Contains(got, "cl=223456789;") || !strings.HasSuffix(got, "auth_method=enterprise)") {
		t.Fatalf("changed binary: %q", got)
	}
}

func TestFallbacks(t *testing.T) {
	d := &Detector{GOOS: "windows", GOARCH: "amd64", Getenv: env(nil), Look: func(string) (string, error) { return "", errors.New("missing") }}
	if got := d.Agy("consumer"); got != "antigravity/cli/1.3.1 (aidev_client; os_type=windows; arch=amd64; cl=994719654; auth_method=consumer)" {
		t.Fatalf("agy %q", got)
	}
	if got := d.Claude(); got != "claude-code/"+ClaudeVersion {
		t.Fatalf("claude %q", got)
	}
	dir := t.TempDir()
	exe := filepath.Join(dir, "agy")
	os.WriteFile(exe, []byte("no build here"), 0o755)
	f := &fakeRun{out: map[string]string{"agy --version": "1.9.9"}}
	d = &Detector{GOOS: "darwin", GOARCH: "arm64", Run: f.run, Look: func(string) (string, error) { return exe, nil }}
	if got := d.Agy("consumer"); !strings.Contains(got, "cli/1.3.1 ") || !strings.Contains(got, "cl=994719654;") {
		t.Fatalf("no build mixes versions: %q", got)
	}
}

func TestCodexAndClaude(t *testing.T) {
	dir := t.TempDir()
	f := &fakeRun{out: map[string]string{
		"codex --version":         "codex-cli 0.161.0\n",
		"claude --version":        "2.1.300 (Claude Code)\n",
		"sw_vers -productVersion": "15.1\n",
	}}
	look := func(name string) (string, error) {
		p := filepath.Join(dir, name)
		os.WriteFile(p, []byte(name), 0o755)
		return p, nil
	}
	d := &Detector{GOOS: "darwin", GOARCH: "arm64", Run: f.run, Look: look, Getenv: env(map[string]string{"WT_SESSION": "x", "TERM": "xterm-256color"})}
	if got := d.Codex(); got != "codex_cli_rs/0.161.0 (Mac OS 15.1; arm64) WindowsTerminal" {
		t.Fatalf("codex %q", got)
	}
	if got := d.Claude(); got != "claude-code/2.1.300" {
		t.Fatalf("claude %q", got)
	}
	d = &Detector{GOOS: "linux", GOARCH: "arm64", Getenv: env(nil), Look: func(string) (string, error) { return "", errors.New("missing") }}
	if got := d.Codex(); !strings.HasPrefix(got, "codex_cli_rs/"+CodexVersion+" (") || !strings.HasSuffix(got, "; aarch64) unknown") {
		t.Fatalf("codex fallback %q", got)
	}
}

func TestTerminal(t *testing.T) {
	cases := []struct {
		env  map[string]string
		want string
	}{
		{map[string]string{"TERM_PROGRAM": "vscode", "TERM_PROGRAM_VERSION": "1.95.0", "WT_SESSION": "x"}, "vscode/1.95.0"},
		{map[string]string{"TERM_PROGRAM": "Apple_Terminal"}, "Apple_Terminal"},
		{map[string]string{"WT_SESSION": "x", "TERM": "xterm-256color"}, "WindowsTerminal"},
		{map[string]string{"TERM": "xterm-256color"}, "xterm-256color"},
		{map[string]string{"TERM_PROGRAM": "My Term(1)"}, "My_Term_1_"},
		{nil, "unknown"},
	}
	for _, c := range cases {
		if got := Terminal(env(c.env)); got != c.want {
			t.Errorf("%v: %q, want %q", c.env, got, c.want)
		}
	}
}

func TestLinuxName(t *testing.T) {
	cases := map[string]string{
		"NAME=\"Ubuntu\"\nID=ubuntu\nVERSION_ID=\"24.04\"\n": "Ubuntu 24.04",
		"ID=arch\nBUILD_ID=rolling\n":                        "Arch Linux Rolling Release",
		"ID=weird\nVERSION_ID=3\n":                           "Linux 3",
		"":                                                   "Linux",
	}
	for in, want := range cases {
		if got := LinuxName(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}
