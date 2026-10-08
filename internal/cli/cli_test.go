package cli

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/WellWells/agentswap/internal/ui"
)

type harness struct {
	t     *testing.T
	home  string
	codex string
	usage map[string]string
	srv   *httptest.Server
	now   time.Time
	lang  ui.Lang
	exe   string
}

func newHarness(t *testing.T) *harness {
	home := t.TempDir()
	h := &harness{t: t, home: home, codex: filepath.Join(home, ".codex"), usage: map[string]string{}, now: time.Unix(1791436850, 0).Add(-time.Hour)}
	h.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := h.usage[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
		if r.URL.Path != "/backend-api/wham/usage" || !ok {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		io.WriteString(w, body)
	}))
	t.Cleanup(h.srv.Close)
	os.MkdirAll(h.codex, 0o700)
	os.WriteFile(filepath.Join(h.codex, "config.toml"), []byte(`chatgpt_base_url = "`+h.srv.URL+`/backend-api"`+"\n"), 0o600)
	return h
}

func (h *harness) login(email, user, account string) {
	enc := base64.RawURLEncoding
	claims, _ := json.Marshal(map[string]any{
		"email": email,
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_account_id": account,
			"chatgpt_user_id":    user,
			"chatgpt_plan_type":  "plus",
		},
	})
	auth, _ := json.Marshal(map[string]any{
		"auth_mode": "chatgpt",
		"tokens": map[string]any{
			"id_token":      enc.EncodeToString([]byte("{}")) + "." + enc.EncodeToString(claims) + ".sig",
			"access_token":  "at-" + user,
			"refresh_token": "rt-" + user,
			"account_id":    account,
		},
	})
	os.MkdirAll(h.codex, 0o700)
	if err := os.WriteFile(filepath.Join(h.codex, "auth.json"), auth, 0o600); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) liveRefresh() string {
	b, _ := os.ReadFile(filepath.Join(h.codex, "auth.json"))
	var a struct {
		Tokens struct {
			RefreshToken string `json:"refresh_token"`
		} `json:"tokens"`
	}
	json.Unmarshal(b, &a)
	return a.Tokens.RefreshToken
}

func (h *harness) run(args ...string) (int, string, string) {
	var out, errb bytes.Buffer
	code := Run(Env{
		Args:   args,
		Stdout: &out,
		Stderr: &errb,
		Getenv: func(k string) string {
			return map[string]string{"CODEX_REFRESH_TOKEN_URL_OVERRIDE": h.srv.URL + "/oauth/token"}[k]
		},
		Now:     func() time.Time { return h.now },
		Home:    h.home,
		Exe:     h.exe,
		Version: "test",
		Lang:    h.lang,
		Width:   80,
	})
	return code, out.String(), errb.String()
}

func TestProgramNameSelectsProvider(t *testing.T) {
	h := newHarness(t)
	for _, argv0 := range []string{"cxswap", "/usr/local/bin/codexswap", `C:\bin\CXSWAP.EXE`} {
		code, out, errs := h.run(argv0, "list")
		if code != 0 || !strings.Contains(out, "No saved codex accounts") {
			t.Errorf("%s: code=%d out=%q err=%q", argv0, code, out, errs)
		}
	}
	for _, args := range [][]string{{"agentswap", "codex", "list"}, {"agentswap", "cxswap", "list"}} {
		code, out, _ := h.run(args...)
		if code != 0 || !strings.Contains(out, "No saved codex accounts") {
			t.Errorf("%v: %d %q", args, code, out)
		}
	}
}

func TestLinkAndUnlinkCommands(t *testing.T) {
	h := newHarness(t)
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	dir := t.TempDir()
	h.exe = filepath.Join(dir, "agentswap"+ext)
	os.WriteFile(h.exe, []byte("bin"), 0o755)
	code, out, errs := h.run("agentswap", "link")
	if code != 0 || !strings.Contains(out, "Linked cxswap, codexswap, ccswap, claudeswap in "+dir) {
		t.Fatalf("link: code=%d out=%q err=%q", code, out, errs)
	}
	for _, n := range []string{"cxswap", "codexswap", "ccswap", "claudeswap"} {
		if b, err := os.ReadFile(filepath.Join(dir, n+ext)); err != nil || string(b) != "bin" {
			t.Errorf("%s: %q %v", n, b, err)
		}
	}
	code, out, _ = h.run("agentswap", "unlink")
	if code != 0 || !strings.Contains(out, "Removed cxswap, codexswap, ccswap, claudeswap") {
		t.Fatalf("unlink: code=%d out=%q", code, out)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("left %v", entries)
	}
	code, out, _ = h.run("agentswap", "unlink")
	if code != 0 || !strings.Contains(out, "No command links") {
		t.Fatalf("second unlink: code=%d out=%q", code, out)
	}
}

func TestLinkWithoutExecutable(t *testing.T) {
	h := newHarness(t)
	code, _, errs := h.run("agentswap", "link")
	if code != 1 || !strings.Contains(errs, "cannot locate the agentswap executable") {
		t.Fatalf("code=%d err=%q", code, errs)
	}
}

func TestEmptyListShowsFullAddCommand(t *testing.T) {
	h := newHarness(t)
	cases := map[string][]string{
		"`cxswap add`":          {"cxswap"},
		"`codexswap add`":       {"codexswap", "list"},
		"`agentswap codex add`": {"agentswap", "codex"},
	}
	for want, args := range cases {
		_, out, _ := h.run(args...)
		if !strings.Contains(out, want) {
			t.Errorf("%v: want %s in %q", args, want, out)
		}
	}
}

func TestClaudeNotYetSupported(t *testing.T) {
	h := newHarness(t)
	for _, args := range [][]string{{"ccswap"}, {"claudeswap", "list"}, {"agentswap", "claude"}} {
		code, _, errs := h.run(args...)
		if code != 2 || !strings.Contains(errs, "not supported yet") {
			t.Errorf("%v: code=%d err=%q", args, code, errs)
		}
	}
}

func TestUnknownProvider(t *testing.T) {
	h := newHarness(t)
	code, _, errs := h.run("agentswap", "gemini")
	if code != 2 || !strings.Contains(errs, "unknown provider") {
		t.Fatalf("code=%d err=%q", code, errs)
	}
}

func TestVersion(t *testing.T) {
	h := newHarness(t)
	for _, args := range [][]string{{"agentswap", "--version"}, {"cxswap", "version"}} {
		code, out, _ := h.run(args...)
		for _, want := range []string{"agentswap test", "WellsTsai", "https://wellstsai.com", "https://github.com/WellWells/agentswap"} {
			if code != 0 || !strings.Contains(out, want) {
				t.Errorf("%v: missing %q in %q", args, want, out)
			}
		}
	}
}

func TestAddListSwitchFlow(t *testing.T) {
	h := newHarness(t)
	h.login("alice@x.com", "u1", "a1")
	if code, out, errs := h.run("cxswap", "add", "work"); code != 0 || !strings.Contains(out, "alice@x.com") {
		t.Fatalf("add: %d %q %q", code, out, errs)
	}
	h.login("bob@x.com", "u2", "a2")
	if code, _, errs := h.run("cxswap", "add"); code != 0 {
		t.Fatalf("add bob: %q", errs)
	}

	_, out, _ := h.run("cxswap")
	if !strings.Contains(out, "Codex · 1 work <alice@x.com>") || !strings.Contains(out, "Codex · 2 <bob@x.com> · plus  ● active") {
		t.Fatalf("list:\n%s", out)
	}

	code, out, errs := h.run("cxswap", "work")
	if code != 0 || h.liveRefresh() != "rt-u1" || !strings.Contains(out, "Restart Codex") {
		t.Fatalf("switch: %d %q %q live=%s", code, out, errs, h.liveRefresh())
	}
	if code, _, _ := h.run("cxswap", "-"); code != 0 || h.liveRefresh() != "rt-u2" {
		t.Fatalf("switch back: live=%s", h.liveRefresh())
	}
	if code, out, _ := h.run("cxswap", "switch", "2"); code != 0 || !strings.Contains(out, "Already using") {
		t.Fatalf("noop switch: %q", out)
	}
}

func TestStatusReportsUnsavedLogin(t *testing.T) {
	h := newHarness(t)
	h.login("carol@x.com", "u3", "a3")
	code, out, _ := h.run("cxswap", "status")
	if code != 0 || !strings.Contains(out, "carol@x.com") || !strings.Contains(out, "not saved") {
		t.Fatalf("%d %q", code, out)
	}
}

func TestErrorsExitOne(t *testing.T) {
	h := newHarness(t)
	code, _, errs := h.run("cxswap", "add")
	if code != 1 || !strings.Contains(errs, "codex login") {
		t.Fatalf("add without login: %d %q", code, errs)
	}
	code, _, errs = h.run("cxswap", "nobody")
	if code != 1 || !strings.Contains(errs, "no matching account") {
		t.Fatalf("unknown query: %d %q", code, errs)
	}
}

func TestRemoveAndAlias(t *testing.T) {
	h := newHarness(t)
	h.login("alice@x.com", "u1", "a1")
	h.run("cxswap", "add")
	if code, _, errs := h.run("cxswap", "alias", "1", "main"); code != 0 {
		t.Fatalf("alias: %q", errs)
	}
	_, out, _ := h.run("cxswap", "list")
	if !strings.Contains(out, "main") {
		t.Fatalf("alias not shown: %q", out)
	}
	if code, _, errs := h.run("cxswap", "rm", "main"); code != 0 {
		t.Fatalf("rm: %q", errs)
	}
	_, out, _ = h.run("cxswap", "list")
	if !strings.Contains(out, "No saved codex accounts") {
		t.Fatalf("after rm: %q", out)
	}
}

const plusUsage = `{"plan_type":"plus","rate_limit":{"primary_window":{"used_percent":42,"limit_window_seconds":18000,"reset_at":1791436850},"secondary_window":{"used_percent":10,"limit_window_seconds":604800,"reset_at":1791998357}}}`

const proliteUsage = `{"plan_type":"prolite","rate_limit":{"primary_window":{"used_percent":7,"limit_window_seconds":604800,"reset_at":1791970861}}}`

func setupTwo(t *testing.T, h *harness) {
	h.usage["at-u1"] = plusUsage
	h.usage["at-u2"] = proliteUsage
	h.login("alice@x.com", "u1", "a1")
	h.run("cxswap", "add")
	h.login("bob@x.com", "u2", "a2")
	h.run("cxswap", "add")
}

func TestListShowsUsageCards(t *testing.T) {
	h := newHarness(t)
	setupTwo(t, h)
	code, out, errs := h.run("cxswap")
	for _, want := range []string{
		"Codex · 1 <alice@x.com> · plus\n\nCurrent session\n",
		"42% used\nResets ",
		"Current week\n",
		"Codex · 2 <bob@x.com> · prolite  ● active\n\nCurrent week\n",
		"7% used\n",
	} {
		if code != 0 || !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s%s", want, out, errs)
		}
	}
	if strings.Contains(out, "suggested") {
		t.Fatalf("active account has the most headroom, nothing to suggest:\n%s", out)
	}
}

func TestSuggestsAccountWithMoreHeadroom(t *testing.T) {
	h := newHarness(t)
	h.usage["at-u1"] = plusUsage
	h.usage["at-u2"] = proliteUsage
	h.login("bob@x.com", "u2", "a2")
	h.run("cxswap", "add")
	h.login("alice@x.com", "u1", "a1")
	h.run("cxswap", "add")
	_, out, _ := h.run("cxswap")
	if !strings.Contains(out, "Codex · 1 <bob@x.com> · prolite  ★ suggested: cxswap 1") {
		t.Fatalf("cxswap:\n%s", out)
	}
	_, out, _ = h.run("agentswap")
	if !strings.Contains(out, "★ suggested: agentswap codex 1") {
		t.Fatalf("agentswap:\n%s", out)
	}
}

func TestListTreatsElapsedWindowAsZero(t *testing.T) {
	h := newHarness(t)
	h.usage["at-u1"] = plusUsage
	h.now = time.Unix(1791436850, 0).Add(time.Minute)
	h.login("alice@x.com", "u1", "a1")
	h.run("cxswap", "add")
	_, out, _ := h.run("cxswap")
	if !strings.Contains(out, "Current session\n") || !strings.Contains(out, "  0% used\n") || strings.Contains(out, "42% used") {
		t.Fatalf("5h window after its reset:\n%s", out)
	}
}

func TestListMarksUnavailableUsage(t *testing.T) {
	h := newHarness(t)
	h.login("alice@x.com", "u1", "a1")
	h.run("cxswap", "add")
	code, out, _ := h.run("cxswap")
	if code != 0 || !strings.Contains(out, "Usage unavailable (HTTP 500)") {
		t.Fatalf("%d %q", code, out)
	}
}

func TestStatusShowsActiveCardOnly(t *testing.T) {
	h := newHarness(t)
	setupTwo(t, h)
	code, out, _ := h.run("cxswap", "status")
	if code != 0 || !strings.Contains(out, "Codex · 2 <bob@x.com> · prolite  ● active") || strings.Contains(out, "alice@x.com") {
		t.Fatalf("%d:\n%s", code, out)
	}
}

func TestAgentswapOverview(t *testing.T) {
	h := newHarness(t)
	code, out, _ := h.run("agentswap")
	if code != 0 || !strings.Contains(out, "No saved accounts yet") {
		t.Fatalf("empty overview: %d %q", code, out)
	}
	setupTwo(t, h)
	code, out, _ = h.run("agentswap")
	if code != 0 || !strings.Contains(out, "Codex · 1 <alice@x.com>") || !strings.Contains(out, "Codex · 2 <bob@x.com>") {
		t.Fatalf("overview: %d\n%s", code, out)
	}
	code, out, _ = h.run("agentswap", "help")
	if code != 0 || !strings.Contains(out, "Usage") {
		t.Fatalf("help: %d %q", code, out)
	}
}

func TestChineseMessages(t *testing.T) {
	h := newHarness(t)
	h.lang = ui.ZhTW
	_, out, _ := h.run("cxswap")
	if !strings.Contains(out, "尚未儲存任何 codex 帳號") {
		t.Fatalf("empty list: %q", out)
	}
	setupTwo(t, h)
	code, out, _ := h.run("cxswap", "1")
	if code != 0 || !strings.Contains(out, "已切換到 alice@x.com") || !strings.Contains(out, "請重新啟動 Codex") {
		t.Fatalf("switch: %q", out)
	}
	_, out, _ = h.run("cxswap", "status")
	if !strings.Contains(out, "● 使用中") || !strings.Contains(out, "目前工作階段") || !strings.Contains(out, "已用 42%") {
		t.Fatalf("status:\n%s", out)
	}
	code, _, errs := h.run("cxswap", "nobody")
	if code != 1 || !strings.Contains(errs, "找不到符合的帳號：nobody") {
		t.Fatalf("not found: %d %q", code, errs)
	}
}
