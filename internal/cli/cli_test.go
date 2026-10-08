package cli

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
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
	"github.com/WellWells/agentswap/internal/vault"
)

type harness struct {
	t      *testing.T
	home   string
	codex  string
	usage  map[string]string
	claude map[string]string
	stdin  string
	srv    *httptest.Server
	now    time.Time
	lang   ui.Lang
	env    map[string]string
	exe    string
	exec   func(env []string, name string, args ...string) error
	output func(env []string, name string, args ...string) ([]byte, error)
}

func newHarness(t *testing.T) *harness {
	home := t.TempDir()
	h := &harness{t: t, home: home, codex: filepath.Join(home, ".codex"), usage: map[string]string{}, claude: map[string]string{}, now: time.Unix(1791436850, 0).Add(-time.Hour)}
	h.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/oauth/usage" {
			body, ok := h.claude[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
			if !ok {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			io.WriteString(w, body)
			return
		}
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
			if v, ok := h.env[k]; ok {
				return v
			}
			return map[string]string{
				"CODEX_REFRESH_TOKEN_URL_OVERRIDE": h.srv.URL + "/oauth/token",
				"AGENTSWAP_CLAUDE_API_URL":         h.srv.URL,
				"AGENTSWAP_CLAUDE_TOKEN_URL":       h.srv.URL + "/v1/oauth/token",
			}[k]
		},
		Now:     func() time.Time { return h.now },
		Home:    h.home,
		Exe:     h.exe,
		Version: "test",
		Lang:    h.lang,
		Width:   80,
		Exec:    h.exec,
		Output:  h.output,
		Stdin:   strings.NewReader(h.stdin),
		GOOS:    "linux",
		Vault:   vault.WithKey(bytes.Repeat([]byte{9}, 32)),
	})
	return code, out.String(), errb.String()
}

func hasLine(out string, parts ...string) bool {
	for _, line := range strings.Split(out, "\n") {
		rest, ok := line, true
		for _, p := range parts {
			i := strings.Index(rest, p)
			if i < 0 {
				ok = false
				break
			}
			rest = rest[i+len(p):]
		}
		if ok {
			return true
		}
	}
	return false
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
		"`cxswap add`":          {"cxswap", "status"},
		"`codexswap add`":       {"codexswap", "list"},
		"`agentswap codex add`": {"agentswap", "codex", "status"},
	}
	for want, args := range cases {
		_, out, _ := h.run(args...)
		if !strings.Contains(out, want) {
			t.Errorf("%v: want %s in %q", args, want, out)
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
	_, out, _ := h.run("ccswap", "version")
	want := "agentswap test\n\n  Source  https://github.com/WellWells/agentswap\n  Author  WellsTsai · https://wellstsai.com\n"
	if out != want {
		t.Errorf("layout:\n%s", out)
	}
	h.lang = ui.ZhTW
	_, out, _ = h.run("ccswap", "version")
	want = "agentswap test\n\n  原始碼  https://github.com/WellWells/agentswap\n  作者    WellsTsai · https://wellstsai.com\n"
	if out != want {
		t.Errorf("zh layout:\n%s", out)
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

	_, out, _ := h.run("cxswap", "status")
	if !hasLine(out, "#1 work <alice@x.com>") || !hasLine(out, "#2 <bob@x.com> · plus", "● active") {
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
	code, out, errs := h.run("cxswap", "status")
	for _, want := range []string{
		"Codex ───",
		"#1 <alice@x.com> · plus\n\n5-hour limit ",
		"42% used\n",
		"Weekly limit ",
		"● active\n\nWeekly limit ",
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
	_, out, _ := h.run("cxswap", "status")
	if !hasLine(out, "#1 <bob@x.com> · prolite", "★ suggested: cxswap 1") {
		t.Fatalf("cxswap:\n%s", out)
	}
	_, out, _ = h.run("agentswap", "status")
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
	_, out, _ := h.run("cxswap", "status")
	if !strings.Contains(out, "5-hour limit\n") || !strings.Contains(out, "  0% used\n") || strings.Contains(out, "42% used") {
		t.Fatalf("5h window after its reset:\n%s", out)
	}
}

func TestListMarksUnavailableUsage(t *testing.T) {
	h := newHarness(t)
	h.login("alice@x.com", "u1", "a1")
	h.run("cxswap", "add")
	code, out, _ := h.run("cxswap", "status")
	if code != 0 || !strings.Contains(out, "Usage unavailable (HTTP 500)") {
		t.Fatalf("%d %q", code, out)
	}
}

func TestStatusShowsAllAccounts(t *testing.T) {
	h := newHarness(t)
	setupTwo(t, h)
	code, out, _ := h.run("cxswap", "status")
	for _, want := range []string{
		"#1 <alice@x.com> · plus\n\n5-hour limit ",
		"42% used\n",
		"● active\n\nWeekly limit ",
		"7% used\n",
	} {
		if code != 0 || !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestStatusIncludesUnsavedLoginWithSavedAccounts(t *testing.T) {
	h := newHarness(t)
	setupTwo(t, h)
	h.usage["at-u3"] = plusUsage
	h.login("carol@x.com", "u3", "a3")
	_, out, _ := h.run("cxswap", "status")
	for _, want := range []string{"#1 <alice@x.com>", "#2 <bob@x.com>", "● active  not saved, run `cxswap add`\n\n5-hour limit"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func daemon(status string, restartErr error, calls *[]string) func([]string, string, ...string) ([]byte, error) {
	return func(env []string, name string, args ...string) ([]byte, error) {
		cmd := name + " " + strings.Join(args, " ")
		*calls = append(*calls, cmd)
		switch cmd {
		case "codex app-server daemon version":
			if status == "" {
				return nil, errors.New("exit status 1")
			}
			return []byte(`{"status":"` + status + `","backend":"pid"}`), nil
		case "codex app-server daemon restart":
			return []byte("restarted\n"), restartErr
		}
		return nil, errors.New("unexpected " + cmd)
	}
}

func TestSwitchRestartsRunningDaemon(t *testing.T) {
	h := newHarness(t)
	setupTwo(t, h)
	var calls []string
	h.output = daemon("running", nil, &calls)
	code, out, _ := h.run("cxswap", "1")
	if code != 0 || strings.Join(calls, ",") != "codex app-server daemon version,codex app-server daemon restart" || !strings.Contains(out, "Restarted the Codex app-server daemon") {
		t.Fatalf("%d calls=%v\n%s", code, calls, out)
	}
	if strings.Contains(out, "restarted\n") {
		t.Fatalf("daemon output leaked:\n%s", out)
	}
	calls = nil
	if _, out, _ := h.run("cxswap", "1"); len(calls) != 0 || !strings.Contains(out, "Already using") {
		t.Fatalf("noop switch restarted the daemon: %v", calls)
	}
}

func TestSwitchSkipsStoppedDaemon(t *testing.T) {
	for _, status := range []string{"stopped", ""} {
		h := newHarness(t)
		setupTwo(t, h)
		var calls []string
		h.output = daemon(status, nil, &calls)
		code, out, _ := h.run("cxswap", "1")
		if code != 0 || len(calls) != 1 || strings.Contains(out, "daemon") {
			t.Fatalf("%q: %d calls=%v\n%s", status, code, calls, out)
		}
	}
}

func TestSwitchReportsFailedDaemonRestart(t *testing.T) {
	h := newHarness(t)
	setupTwo(t, h)
	var calls []string
	h.output = daemon("running", errors.New("exit status 2"), &calls)
	code, out, _ := h.run("cxswap", "1")
	if code != 0 || h.liveRefresh() != "rt-u1" || !strings.Contains(out, "`codex app-server daemon restart`") || !strings.Contains(out, "exit status 2") {
		t.Fatalf("%d\n%s", code, out)
	}
}

func TestAgentswapOverview(t *testing.T) {
	h := newHarness(t)
	code, out, _ := h.run("agentswap", "status")
	if code != 0 || !strings.Contains(out, "No saved accounts yet") {
		t.Fatalf("empty overview: %d %q", code, out)
	}
	setupTwo(t, h)
	code, out, _ = h.run("agentswap", "status")
	if code != 0 || !strings.Contains(out, "Codex ───") || !strings.Contains(out, "#1 <alice@x.com>") || !strings.Contains(out, "#2 <bob@x.com>") {
		t.Fatalf("overview: %d\n%s", code, out)
	}
	code, out, _ = h.run("agentswap", "help")
	if code != 0 || !strings.Contains(out, "Usage") {
		t.Fatalf("help: %d %q", code, out)
	}
}

func TestNoArgsShowsHelp(t *testing.T) {
	h := newHarness(t)
	setupTwo(t, h)
	for _, args := range [][]string{{"cxswap"}, {"ccswap"}, {"agentswap"}, {"agentswap", "codex"}} {
		code, out, errs := h.run(args...)
		if code != 0 || errs != "" || !strings.HasPrefix(out, "Usage: ") || strings.Contains(out, "% used") {
			t.Fatalf("%v: %d %q %q", args, code, out, errs)
		}
		_, help, _ := h.run(append(args, "help")...)
		if out != help {
			t.Fatalf("%v: no-args output differs from help:\n%s\n---\n%s", args, out, help)
		}
	}
}

func TestChineseMessages(t *testing.T) {
	h := newHarness(t)
	h.lang = ui.ZhTW
	_, out, _ := h.run("cxswap", "status")
	if !strings.Contains(out, "尚未儲存任何 codex 帳號") {
		t.Fatalf("empty list: %q", out)
	}
	setupTwo(t, h)
	code, out, _ := h.run("cxswap", "1")
	if code != 0 || !strings.Contains(out, "已切換到 alice@x.com") || !strings.Contains(out, "請重新啟動 Codex") {
		t.Fatalf("switch: %q", out)
	}
	_, out, _ = h.run("cxswap", "status")
	if !strings.Contains(out, "● 使用中") || !strings.Contains(out, "5 小時額度") || !strings.Contains(out, "已用 42%") {
		t.Fatalf("status:\n%s", out)
	}
	code, _, errs := h.run("cxswap", "nobody")
	if code != 1 || !strings.Contains(errs, "找不到符合的帳號：nobody") {
		t.Fatalf("not found: %d %q", code, errs)
	}
}

func TestListShowsNumbersAndSwitchCommands(t *testing.T) {
	h := newHarness(t)
	setupTwo(t, h)
	delete(h.usage, "at-u1")
	delete(h.usage, "at-u2")
	code, out, errs := h.run("cxswap", "list")
	for _, want := range []string{
		"Codex accounts\n",
		"  1  <alice@x.com>  plus  cxswap 1\n",
		"  2  <bob@x.com>    plus  ● active\n",
		"`cxswap <number>`",
		"`cxswap -`",
	} {
		if code != 0 || !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s%s", want, out, errs)
		}
	}
	if strings.Contains(out, "used") || strings.Contains(out, "unavailable") {
		t.Fatalf("list should not query usage:\n%s", out)
	}
	_, out, _ = h.run("agentswap", "codex", "ls")
	if !strings.Contains(out, "agentswap codex 1\n") {
		t.Fatalf("agentswap codex ls:\n%s", out)
	}
}

func TestListShowsUnsavedLogin(t *testing.T) {
	h := newHarness(t)
	h.login("alice@x.com", "u1", "a1")
	h.run("cxswap", "add")
	h.login("carol@x.com", "u3", "a3")
	_, out, _ := h.run("cxswap", "list")
	if !strings.Contains(out, "  1  <alice@x.com>  plus  cxswap 1\n") || !strings.Contains(out, "  -  <carol@x.com>  plus  not saved, run `cxswap add`") {
		t.Fatalf("%s", out)
	}
}

func TestUsageViewPointsToList(t *testing.T) {
	h := newHarness(t)
	h.login("alice@x.com", "u1", "a1")
	h.run("cxswap", "add")
	if _, out, _ := h.run("cxswap", "status"); strings.Contains(out, "cxswap list") {
		t.Fatalf("single account needs no switch hint:\n%s", out)
	}
	h.login("bob@x.com", "u2", "a2")
	h.run("cxswap", "add")
	if _, out, _ := h.run("cxswap", "status"); !strings.Contains(out, "`cxswap <number>`") || !strings.Contains(out, "`cxswap list`") {
		t.Fatalf("missing switch hint:\n%s", out)
	}
}

func TestUnknownNumberPointsToList(t *testing.T) {
	h := newHarness(t)
	h.login("alice@x.com", "u1", "a1")
	h.run("cxswap", "add")
	code, _, errs := h.run("cxswap", "3")
	if code != 1 || !strings.Contains(errs, "no account #3") || !strings.Contains(errs, "`cxswap list`") {
		t.Fatalf("%d %q", code, errs)
	}
	h.lang = ui.ZhTW
	_, _, errs = h.run("cxswap", "3")
	if !strings.Contains(errs, "沒有編號 3 的帳號") {
		t.Fatalf("%q", errs)
	}
}

func TestLoginRunsInIsolatedHome(t *testing.T) {
	h := newHarness(t)
	h.login("alice@x.com", "u1", "a1")
	h.run("cxswap", "add")
	var loginHome string
	h.exec = func(env []string, name string, args ...string) error {
		if name != "codex" || strings.Join(args, " ") != "login" {
			t.Fatalf("ran %s %v", name, args)
		}
		for _, kv := range env {
			if v, ok := strings.CutPrefix(kv, "CODEX_HOME="); ok {
				loginHome = v
			}
		}
		if loginHome == "" || loginHome == h.codex {
			t.Fatalf("login must not use the live CODEX_HOME: %v", env)
		}
		if _, err := os.Stat(filepath.Join(loginHome, "config.toml")); err != nil {
			t.Fatalf("config.toml not copied: %v", err)
		}
		live := h.codex
		h.codex = loginHome
		h.login("bob@x.com", "u2", "a2")
		h.codex = live
		return nil
	}
	code, out, errs := h.run("cxswap", "login", "work")
	if code != 0 || !strings.Contains(out, "work <bob@x.com>") || !strings.Contains(out, "`cxswap 2`") {
		t.Fatalf("%d %q %q", code, out, errs)
	}
	if h.liveRefresh() != "rt-u1" {
		t.Fatalf("live changed to %s", h.liveRefresh())
	}
	if _, err := os.Stat(loginHome); !os.IsNotExist(err) {
		t.Fatalf("temporary login home left behind: %v", err)
	}
	if code, _, _ := h.run("cxswap", "work"); code != 0 || h.liveRefresh() != "rt-u2" {
		t.Fatalf("switch to imported account: live=%s", h.liveRefresh())
	}
}

func TestLoginFailureSavesNothing(t *testing.T) {
	h := newHarness(t)
	h.exec = func(env []string, name string, args ...string) error { return errors.New("exit status 1") }
	code, _, errs := h.run("cxswap", "login")
	if code != 1 || !strings.Contains(errs, "exit status 1") {
		t.Fatalf("%d %q", code, errs)
	}
	h.exec = func(env []string, name string, args ...string) error { return nil }
	code, _, errs = h.run("cxswap", "login")
	if code != 1 || !strings.Contains(errs, "no credentials") {
		t.Fatalf("%d %q", code, errs)
	}
	if _, out, _ := h.run("cxswap", "list"); !strings.Contains(out, "No saved codex accounts") {
		t.Fatalf("%s", out)
	}
}

func TestAddAnotherHintUsesLogin(t *testing.T) {
	h := newHarness(t)
	h.login("alice@x.com", "u1", "a1")
	h.run("cxswap", "add")
	_, out, _ := h.run("cxswap", "list")
	if !strings.Contains(out, "`cxswap login`") {
		t.Fatalf("%s", out)
	}
}

func TestLoginRefusesKeyringStore(t *testing.T) {
	h := newHarness(t)
	os.WriteFile(filepath.Join(h.codex, "config.toml"), []byte("cli_auth_credentials_store = \"keyring\"\n"), 0o600)
	ran := false
	h.exec = func(env []string, name string, args ...string) error { ran = true; return nil }
	if code, _, errs := h.run("cxswap", "login"); code != 1 || ran || !strings.Contains(errs, "keyring") {
		t.Fatalf("%d ran=%v %q", code, ran, errs)
	}
}

func (h *harness) claudeLogin(uuid, org, email, token string) {
	dir := filepath.Join(h.home, ".claude")
	os.MkdirAll(dir, 0o700)
	creds := `{"claudeAiOauth":{"accessToken":"` + token + `","refreshToken":"r-` + token + `","expiresAt":9999999999999,"subscriptionType":"max"},"mcpOAuth":{"keep":true}}`
	os.WriteFile(filepath.Join(dir, ".credentials.json"), []byte(creds), 0o600)
	cfg := `{"oauthAccount":{"accountUuid":"` + uuid + `","organizationUuid":"` + org + `","emailAddress":"` + email + `"},"projects":{"p":1}}`
	os.WriteFile(filepath.Join(h.home, ".claude.json"), []byte(cfg), 0o600)
}

const claudeUsageBody = `{"five_hour":{"utilization":30,"resets_at":"2026-10-08T12:00:00Z"},"seven_day":{"utilization":10,"resets_at":"2026-10-12T00:00:00Z"}}`

func TestClaudeAddUsageSwitchFlow(t *testing.T) {
	h := newHarness(t)
	h.claude = map[string]string{"t1": claudeUsageBody, "t2": claudeUsageBody}
	if code, out, _ := h.run("ccswap", "status"); code != 0 || !strings.Contains(out, "ccswap add") || strings.Contains(out, "ccswap login") {
		t.Fatalf("empty: %d %q", code, out)
	}
	h.claudeLogin("u1", "o1", "a@x", "t1")
	if code, out, errs := h.run("ccswap", "add", "work"); code != 0 || !strings.Contains(out, "work <a@x> [max]") {
		t.Fatalf("%d %q %q", code, out, errs)
	}
	h.claudeLogin("u2", "o2", "b@x", "t2")
	if code, _, errs := h.run("ccswap", "add"); code != 0 {
		t.Fatalf("%d %q", code, errs)
	}
	code, out, _ := h.run("ccswap", "status")
	if code != 0 || !strings.Contains(out, "Claude Code ───") || !strings.Contains(out, "#1 work <a@x>") || !strings.Contains(out, "30% used") {
		t.Fatalf("%d %s", code, out)
	}
	if code, out, errs := h.run("ccswap", "work"); code != 0 || !strings.Contains(out, "Switched to work <a@x>") || !strings.Contains(out, "Claude Code") {
		t.Fatalf("%d %q %q", code, out, errs)
	}
	cfg, _ := os.ReadFile(filepath.Join(h.home, ".claude.json"))
	creds, _ := os.ReadFile(filepath.Join(h.home, ".claude", ".credentials.json"))
	if !strings.Contains(string(cfg), `"u1"`) || !strings.Contains(string(cfg), `"projects":{"p":1}`) || !strings.Contains(string(creds), `"mcpOAuth":{"keep":true}`) || !strings.Contains(string(creds), `"t1"`) {
		t.Fatalf("cfg=%s creds=%s", cfg, creds)
	}
	snaps, _ := filepath.Glob(filepath.Join(h.home, ".agentswap", "claude", "accounts", "*.json"))
	if len(snaps) != 2 {
		t.Fatalf("snapshots %v", snaps)
	}
	for _, s := range snaps {
		if b, _ := os.ReadFile(s); !vault.Sealed(b) {
			t.Fatalf("%s not sealed", s)
		}
	}
	if code, out, _ := h.run("agentswap", "status"); code != 0 || !strings.Contains(out, "#2 <b@x>") {
		t.Fatalf("overview %d %s", code, out)
	}
}

func TestClaudeLoginPointsToOfficialApp(t *testing.T) {
	h := newHarness(t)
	h.exec = func([]string, string, ...string) error { t.Fatal("ran a login command"); return nil }
	code, _, errs := h.run("ccswap", "login")
	if code == 0 || !strings.Contains(errs, "/login") {
		t.Fatalf("%d %q", code, errs)
	}
	_, out, _ := h.run("ccswap", "help")
	if strings.Contains(out, "ccswap login") || !strings.Contains(out, "ccswap import") {
		t.Fatalf("help %s", out)
	}
}

func writeCswapFixture(t *testing.T, dir string) {
	os.MkdirAll(filepath.Join(dir, "credentials"), 0o700)
	os.MkdirAll(filepath.Join(dir, "configs"), 0o700)
	os.WriteFile(filepath.Join(dir, "sequence.json"), []byte(`{"sequence":[1,2,3],"accounts":{"1":{"email":"a@x","alias":"home"},"2":{"email":"b@x","alias":"work"},"3":{"email":"k@token.local","kind":"api_key"}}}`), 0o600)
	for i, u := range []string{"u1", "u2"} {
		email := []string{"a@x", "b@x"}[i]
		c := `{"claudeAiOauth":{"accessToken":"old-` + u + `","refreshToken":"rt-` + u + `"}}`
		os.WriteFile(filepath.Join(dir, "credentials", fmt.Sprintf(".creds-%d-%s.enc", i+1, email)), []byte(base64.StdEncoding.EncodeToString([]byte(c))), 0o600)
		os.WriteFile(filepath.Join(dir, "configs", fmt.Sprintf(".claude-config-%d-%s.json", i+1, email)), []byte(`{"oauthAccount":{"accountUuid":"`+u+`","organizationUuid":"o","emailAddress":"`+email+`"}}`), 0o600)
	}
}

func TestClaudeImportMenuChoosesCswap(t *testing.T) {
	h := newHarness(t)
	writeCswapFixture(t, filepath.Join(h.home, ".local", "share", "claude-swap"))
	h.claudeLogin("u2", "o", "b@x", "fresh")
	h.stdin = "1\n"
	code, out, errs := h.run("ccswap", "import")
	if code != 0 {
		t.Fatalf("%d %q %q", code, out, errs)
	}
	for _, want := range []string{"1) cswap", "Imported home <a@x>", "Imported work <b@x>", "k@token.local", "cswap purge"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in\n%s", want, out)
		}
	}
	creds, _ := os.ReadFile(filepath.Join(h.home, ".claude", ".credentials.json"))
	if !strings.Contains(string(creds), "fresh") {
		t.Fatalf("live overwritten by cswap copy: %s", creds)
	}
	_, list, _ := h.run("ccswap", "list")
	if !strings.Contains(list, "home") || !strings.Contains(list, "work") {
		t.Fatalf("list %s", list)
	}
	h.stdin = "1\n"
	code, out, _ = h.run("ccswap", "import")
	if code != 0 || strings.Count(out, "skipped") != 2 {
		t.Fatalf("second import %d %s", code, out)
	}
}

func TestClaudeImportMenuCancel(t *testing.T) {
	h := newHarness(t)
	h.stdin = "\n"
	code, out, _ := h.run("ccswap", "import")
	if code != 0 || !strings.Contains(out, "Cancelled") {
		t.Fatalf("%d %q", code, out)
	}
	if _, err := os.Stat(filepath.Join(h.home, ".agentswap", "claude", "registry.json")); err == nil {
		t.Fatal("imported on cancel")
	}
}

func TestImportOnlyForClaude(t *testing.T) {
	h := newHarness(t)
	if code, _, _ := h.run("cxswap", "import"); code != 2 {
		t.Fatalf("code %d", code)
	}
}

func TestStatusShowsOneAccount(t *testing.T) {
	h := newHarness(t)
	setupTwo(t, h)
	code, out, errs := h.run("cxswap", "status", "1")
	if code != 0 || !strings.Contains(out, "#1 <alice@x.com>") || strings.Contains(out, "bob@x.com") {
		t.Fatalf("%d %q %q", code, out, errs)
	}
	code, out, _ = h.run("cxswap", "status", "bob")
	if code != 0 || !hasLine(out, "#2 <bob@x.com> · prolite", "● active") || strings.Contains(out, "alice") {
		t.Fatalf("%d %q", code, out)
	}
	code, out, _ = h.run("cxswap", "status")
	if code != 0 || !strings.Contains(out, "alice") || !strings.Contains(out, "bob") {
		t.Fatalf("all %d %q", code, out)
	}
	code, _, errs = h.run("cxswap", "status", "9")
	if code != 1 || !strings.Contains(errs, "no account #9") {
		t.Fatalf("missing %d %q", code, errs)
	}
}

func TestHelpIsSpecificToEachCommand(t *testing.T) {
	h := newHarness(t)
	cases := []struct {
		args       []string
		want, deny []string
	}{
		{[]string{"cxswap", "help"}, []string{"cxswap status [account]", "cxswap login [alias]", "cxswap <account>", "<account> is the number shown by `cxswap list`"}, []string{"import"}},
		{[]string{"ccswap", "--help"}, []string{"ccswap status [account]", "ccswap import", "/login", "<account> is the number shown by `ccswap list`"}, []string{"ccswap login", "from cswap", "asks where"}},
		{[]string{"agentswap", "help"}, []string{"agentswap claude <command>", "agentswap codex <command>", "agentswap link", "ccswap help"}, []string{"login [alias]"}},
	}
	for _, c := range cases {
		code, out, _ := h.run(c.args...)
		if code != 0 {
			t.Fatalf("%v: code %d", c.args, code)
		}
		for _, w := range c.want {
			if !strings.Contains(out, w) {
				t.Errorf("%v: missing %q in\n%s", c.args, w, out)
			}
		}
		for _, d := range c.deny {
			if strings.Contains(out, d) {
				t.Errorf("%v: should not contain %q", c.args, d)
			}
		}
	}
	h.lang = ui.ZhTW
	_, out, _ := h.run("ccswap", "help")
	if strings.Contains(out, "從 cswap") || strings.Contains(out, "詢問來源") {
		t.Errorf("zh help names a third-party tool or is verbose:\n%s", out)
	}
	for _, w := range []string{"ccswap status [帳號]", "ccswap import", "<帳號>"} {
		if !strings.Contains(out, w) {
			t.Errorf("zh: missing %q in\n%s", w, out)
		}
	}
}

func TestLangShowsCurrentAndChoices(t *testing.T) {
	h := newHarness(t)
	code, out, _ := h.run("agentswap", "lang")
	if code != 0 {
		t.Fatalf("code %d", code)
	}
	for _, w := range []string{"Language: English (en), follows the system locale", "  1  en         English   ● active\n", "  2  zh-TW/zht  正體中文\n", "  3  zh-CN/zhc  简体中文\n", "agentswap lang <number|code>", "agentswap lang auto"} {
		if !strings.Contains(out, w) {
			t.Errorf("missing %q in\n%s", w, out)
		}
	}
}

func TestLangSetPersistsAndAuto(t *testing.T) {
	h := newHarness(t)
	code, out, _ := h.run("agentswap", "language", "zhc")
	if code != 0 || !strings.Contains(out, "界面语言已设置为 简体中文（zh-CN）") {
		t.Fatalf("code %d: %s", code, out)
	}
	b, _ := os.ReadFile(filepath.Join(h.home, ".agentswap", "config.json"))
	if !strings.Contains(string(b), `"lang": "zh-CN"`) {
		t.Fatalf("config: %s", b)
	}
	_, out, _ = h.run("cxswap", "list")
	if !strings.Contains(out, "尚未保存任何 codex 账号") {
		t.Errorf("saved language not used: %s", out)
	}
	_, out, _ = h.run("agentswap", "lang")
	if !strings.Contains(out, "由 `agentswap lang` 设置") || !strings.Contains(out, "3  zh-CN/zhc  简体中文  ● 使用中") {
		t.Errorf("show: %s", out)
	}
	h.lang = ui.ZhTW
	code, out, _ = h.run("agentswap", "lang", "auto")
	if code != 0 || !strings.Contains(out, "介面語言恢復跟隨系統語系：正體中文（zh-TW）") {
		t.Fatalf("auto: code %d: %s", code, out)
	}
	_, out, _ = h.run("agentswap", "lang")
	if !strings.Contains(out, "跟隨系統語系") {
		t.Errorf("after auto: %s", out)
	}
}

func TestLangSetByNumber(t *testing.T) {
	h := newHarness(t)
	for n, want := range map[string]string{"2": "介面語言已設為 正體中文（zh-TW）", "3": "界面语言已设置为 简体中文（zh-CN）", "1": "Language set to English (en)"} {
		code, out, _ := h.run("agentswap", "lang", n)
		if code != 0 || !strings.Contains(out, want) {
			t.Errorf("lang %s: code %d: %s", n, code, out)
		}
	}
}

func TestLangEnvOverridesSaved(t *testing.T) {
	h := newHarness(t)
	h.run("agentswap", "lang", "zh-TW")
	h.env = map[string]string{"AGENTSWAP_LANG": "en"}
	_, out, _ := h.run("cxswap", "list")
	if !strings.Contains(out, "No saved codex accounts") {
		t.Errorf("env should win: %s", out)
	}
	_, out, _ = h.run("agentswap", "lang", "zh-CN")
	if !strings.Contains(out, "AGENTSWAP_LANG=en") {
		t.Errorf("missing override warning: %s", out)
	}
	_, out, _ = h.run("agentswap", "lang")
	if !strings.Contains(out, "set by AGENTSWAP_LANG") {
		t.Errorf("show: %s", out)
	}
	h.env = map[string]string{"AGENTSWAP_LANG": "fr"}
	_, out, _ = h.run("cxswap", "list")
	if !strings.Contains(out, "尚未保存任何 codex 账号") {
		t.Errorf("invalid AGENTSWAP_LANG should not hide the saved language: %s", out)
	}
}

func TestLangRejectsUnknownCode(t *testing.T) {
	h := newHarness(t)
	for _, args := range [][]string{{"zh-HK"}, {"zhs"}, {"0"}, {"4"}, {"en", "zh-TW"}} {
		code, _, errOut := h.run(append([]string{"agentswap", "lang"}, args...)...)
		if code != 2 || errOut == "" {
			t.Errorf("%v: code %d err %q", args, code, errOut)
		}
	}
	if _, err := os.Stat(filepath.Join(h.home, ".agentswap", "config.json")); err == nil {
		t.Error("config written for an invalid code")
	}
	if code, _, _ := h.run("cxswap", "lang", "en"); code == 0 {
		t.Error("lang should only be an agentswap command")
	}
}
