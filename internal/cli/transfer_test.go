package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const testPass = "correct horse"

func threeAgents(t *testing.T) *harness {
	h := newHarness(t)
	h.login("alice@x.com", "u1", "a1")
	if code, _, errs := h.run("cxswap", "add", "cx-work"); code != 0 {
		t.Fatalf("cxswap add: %s", errs)
	}
	h.claudeLogin("uuid-1", "org-1", "claude@x.com", "tok-1")
	if code, _, errs := h.run("ccswap", "add", "cc-main"); code != 0 {
		t.Fatalf("ccswap add: %s", errs)
	}
	h.agyLogin("s1", "agy@x.com", "g1")
	if code, _, errs := h.run("agswap", "add", "ag-dev"); code != 0 {
		t.Fatalf("agswap add: %s", errs)
	}
	h.interactive = true
	return h
}

func exportTo(t *testing.T, h *harness, args ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "accounts.agentswap")
	h.secrets = []string{testPass, testPass}
	code, out, errs := h.run(append(append([]string{}, args...), path)...)
	if code != 0 {
		t.Fatalf("%v: %d %q %q", args, code, out, errs)
	}
	return path
}

func TestExportImportAllAgents(t *testing.T) {
	h := threeAgents(t)
	path := filepath.Join(t.TempDir(), "accounts.agentswap")
	h.secrets = []string{testPass, testPass}
	code, out, errs := h.run("agentswap", "export", path)
	if code != 0 {
		t.Fatalf("export: %d %q %q", code, out, errs)
	}
	for _, want := range []string{"Passphrase (at least 8 characters)", "Exported cx-work <alice@x.com>", "(Claude Code)", "Wrote 3 account(s) to " + path, "cannot be recovered", "refresh tokens rotate"} {
		if !strings.Contains(out, want) {
			t.Fatalf("export output missing %q:\n%s", want, out)
		}
	}
	data, _ := os.ReadFile(path)
	for _, secret := range []string{"rt-u1", "at-u1", "tok-1", "alice@x.com", "cx-work"} {
		if bytes.Contains(data, []byte(secret)) {
			t.Fatalf("export file leaks %q", secret)
		}
	}
	if runtime.GOOS != "windows" {
		if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
			t.Fatalf("mode %v", st.Mode().Perm())
		}
	}

	h2 := newHarness(t)
	h2.interactive = true
	h2.secrets = []string{testPass}
	code, out, errs = h2.run("agentswap", "import", path)
	if code != 0 {
		t.Fatalf("import: %d %q %q", code, out, errs)
	}
	for _, want := range []string{"Imported cx-work <alice@x.com>", "Imported cc-main <claude@x.com>", "Imported ag-dev <agy@x.com>", "Delete " + path} {
		if !strings.Contains(out, want) {
			t.Fatalf("import output missing %q:\n%s", want, out)
		}
	}
	if _, err := os.Stat(filepath.Join(h2.codex, "auth.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("import wrote live Codex credentials: %v", err)
	}
	if h2.agy != nil {
		t.Fatal("import wrote live agy credentials")
	}
	if code, _, errs := h2.run("cxswap", "cx-work"); code != 0 {
		t.Fatalf("switch: %s", errs)
	}
	want, _ := os.ReadFile(filepath.Join(h.codex, "auth.json"))
	got, _ := os.ReadFile(filepath.Join(h2.codex, "auth.json"))
	if !bytes.Equal(got, want) {
		t.Fatalf("snapshot changed in transit:\n%s\n%s", want, got)
	}
	if code, _, errs := h2.run("ccswap", "cc-main"); code != 0 {
		t.Fatalf("ccswap switch: %s", errs)
	}
	creds, _ := os.ReadFile(filepath.Join(h2.home, ".claude", ".credentials.json"))
	if !strings.Contains(string(creds), `"subscriptionType":"max"`) || !strings.Contains(string(creds), "r-tok-1") {
		t.Fatalf("claude credentials: %s", creds)
	}
}

func TestImportSkipsSavedAccountsAndKeepsLive(t *testing.T) {
	h := threeAgents(t)
	path := exportTo(t, h, "cxswap", "export")
	live := filepath.Join(h.codex, "auth.json")
	b, _ := os.ReadFile(live)
	os.WriteFile(live, bytes.Replace(b, []byte("rt-u1"), []byte("rt-rotated"), 1), 0o600)
	h.secrets = []string{testPass}
	code, out, errs := h.run("cxswap", "import", path)
	if code != 0 || !strings.Contains(out, "Already saved, skipped: cx-work <alice@x.com>") {
		t.Fatalf("%d %q %q", code, out, errs)
	}
	if h.liveRefresh() != "rt-rotated" {
		t.Fatalf("live refresh token = %q", h.liveRefresh())
	}
}

func TestProviderExportAndImportOnlyTouchTheirAgent(t *testing.T) {
	h := threeAgents(t)
	cx := exportTo(t, h, "cxswap", "export")
	h2 := newHarness(t)
	h2.interactive = true
	h2.secrets = []string{testPass}
	if code, out, errs := h2.run("agentswap", "import", cx); code != 0 || !strings.Contains(out, "Imported cx-work") || strings.Contains(out, "cc-main") {
		t.Fatalf("%d %q %q", code, out, errs)
	}

	all := exportTo(t, h, "agentswap", "export")
	h3 := newHarness(t)
	h3.interactive = true
	h3.secrets = []string{testPass}
	code, out, errs := h3.run("ccswap", "import", all)
	if code != 0 || !strings.Contains(out, "Imported cc-main") || strings.Contains(out, "cx-work") || strings.Contains(out, "ag-dev") {
		t.Fatalf("%d %q %q", code, out, errs)
	}
	if _, out, _ := h3.run("cxswap", "list"); strings.Contains(out, "cx-work") {
		t.Fatalf("ccswap import saved a Codex account:\n%s", out)
	}
	h3.secrets = []string{testPass}
	if code, out, _ := h3.run("agswap", "import", cx); code != 0 || !strings.Contains(out, "No accounts to import") {
		t.Fatalf("%d %q", code, out)
	}
}

func TestExportRefusals(t *testing.T) {
	h := threeAgents(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "out.agentswap")
	cases := []struct {
		name        string
		interactive bool
		secrets     []string
		want        string
	}{
		{"not a terminal", false, []string{testPass, testPass}, "interactive terminal"},
		{"too short", true, []string{"short", "short"}, "at least 8 characters"},
		{"mismatch", true, []string{testPass, testPass + "!"}, "do not match"},
	}
	for _, c := range cases {
		h.interactive, h.secrets = c.interactive, c.secrets
		code, _, errs := h.run("agentswap", "export", path)
		if code != 1 || !strings.Contains(errs, c.want) {
			t.Fatalf("%s: %d %q", c.name, code, errs)
		}
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s: file written", c.name)
		}
	}
	os.WriteFile(path, []byte("keep me"), 0o600)
	h.interactive, h.secrets = true, []string{testPass, testPass}
	if code, _, errs := h.run("agentswap", "export", path); code != 1 || !strings.Contains(errs, "already exists") {
		t.Fatalf("existing file: %d %q", code, errs)
	}
	if b, _ := os.ReadFile(path); string(b) != "keep me" {
		t.Fatal("existing file overwritten")
	}
	if code, _, _ := h.run("agentswap", "export"); code != 2 {
		t.Fatalf("missing path: %d", code)
	}
	if code, _, _ := h.run("agentswap", "export", "--zip", path+"2"); code != 2 {
		t.Fatalf("unknown flag: %d", code)
	}
	empty := newHarness(t)
	empty.interactive = true
	if code, _, errs := empty.run("agentswap", "export", filepath.Join(dir, "none")); code != 1 || !strings.Contains(errs, "no saved accounts") {
		t.Fatalf("empty: %d %q", code, errs)
	}
}

func TestImportRefusals(t *testing.T) {
	h := threeAgents(t)
	path := exportTo(t, h, "agentswap", "export")
	h2 := newHarness(t)
	h2.interactive = true
	h2.secrets = []string{"wrong passphrase"}
	if code, _, errs := h2.run("agentswap", "import", path); code != 1 || !strings.Contains(errs, "wrong passphrase") {
		t.Fatalf("wrong passphrase: %d %q", code, errs)
	}
	h2.interactive = false
	if code, _, errs := h2.run("agentswap", "import", path); code != 1 || !strings.Contains(errs, "interactive terminal") {
		t.Fatalf("not a terminal: %d %q", code, errs)
	}
	other := filepath.Join(t.TempDir(), "other.json")
	os.WriteFile(other, []byte(`{"codex":[{"alias":"x","token":"sk-1"}]}`), 0o600)
	if code, _, errs := h2.run("agentswap", "import", other); code != 1 || !strings.Contains(errs, "not an agentswap export file") {
		t.Fatalf("foreign file: %d %q", code, errs)
	}
	if code, _, _ := h2.run("cxswap", "import"); code != 2 {
		t.Fatalf("missing path: %d", code)
	}
	if code, _, _ := h2.run("agentswap", "import"); code != 2 {
		t.Fatalf("missing path: %d", code)
	}
	if _, out, _ := h2.run("cxswap", "list"); strings.Contains(out, "cx-work") {
		t.Fatalf("refused import still saved accounts:\n%s", out)
	}
}

func TestExportPlainNeedsTwoConfirmations(t *testing.T) {
	h := threeAgents(t)
	dir := t.TempDir()
	cancelled := []struct {
		name        string
		interactive bool
		stdin       string
		args        []string
	}{
		{"no", true, "n\n", nil},
		{"enter", true, "\n", nil},
		{"wrong word", true, "y\nyes\n", nil},
		{"--yes does not skip", true, "", []string{"--yes"}},
	}
	for i, c := range cancelled {
		path := filepath.Join(dir, c.name)
		h.interactive, h.stdin = c.interactive, c.stdin
		args := append([]string{"agentswap", "export", "--plain", path}, c.args...)
		code, out, errs := h.run(args...)
		if code != 0 || !strings.Contains(out, "WARNING") || !strings.Contains(out, "Cancelled.") {
			t.Fatalf("%d %s: %d %q %q", i, c.name, code, out, errs)
		}
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s: file written", c.name)
		}
	}
	h.interactive, h.stdin = false, "y\nplaintext\n"
	if code, _, errs := h.run("agentswap", "export", "--plain", filepath.Join(dir, "pipe")); code != 1 || !strings.Contains(errs, "interactive terminal") {
		t.Fatalf("non-interactive: %d %q", code, errs)
	}

	path := filepath.Join(dir, "plain.json")
	h.interactive, h.stdin = true, "y\nplaintext\n"
	code, out, errs := h.run("agentswap", "export", "--plain", path)
	if code != 0 || !strings.Contains(out, "Type plaintext to confirm") || !strings.Contains(out, "NOT encrypted") {
		t.Fatalf("%d %q %q", code, out, errs)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `"accounts"`) || strings.Contains(string(data), `"kdf"`) {
		t.Fatalf("plain file:\n%s", data)
	}
	h2 := newHarness(t)
	code, out, errs = h2.run("agentswap", "import", path)
	if code != 0 || !strings.Contains(out, "Imported cx-work") || !strings.Contains(out, "is not encrypted; delete it now") {
		t.Fatalf("plain import: %d %q %q", code, out, errs)
	}
}

func TestClaudeImportWithoutFileStillOpensMenu(t *testing.T) {
	h := newHarness(t)
	h.stdin = "\n"
	if code, out, _ := h.run("ccswap", "import"); code != 0 || !strings.Contains(out, "cswap (claude-swap)") {
		t.Fatalf("%d %q", code, out)
	}
}

func TestHelpListsExportAndImport(t *testing.T) {
	h := newHarness(t)
	for _, prog := range []string{"agentswap", "cxswap", "ccswap", "agswap"} {
		_, out, _ := h.run(prog, "help")
		if !strings.Contains(out, "export <file>") || !strings.Contains(out, "import ") {
			t.Fatalf("%s help:\n%s", prog, out)
		}
	}
}
