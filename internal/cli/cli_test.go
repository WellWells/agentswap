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
	"strings"
	"testing"
	"time"
)

type harness struct {
	t     *testing.T
	home  string
	codex string
	usage map[string]string
	srv   *httptest.Server
	now   time.Time
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
		Version: "test",
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
	code, out, _ := h.run("agentswap", "codex", "list")
	if code != 0 || !strings.Contains(out, "No saved codex accounts") {
		t.Errorf("agentswap codex: %d %q", code, out)
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

func TestAgentswapWithoutProviderShowsUsage(t *testing.T) {
	h := newHarness(t)
	code, _, errs := h.run("agentswap")
	if code != 2 || !strings.Contains(errs, "Usage") {
		t.Fatalf("code=%d err=%q", code, errs)
	}
	code, _, errs = h.run("agentswap", "gemini")
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
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 || !strings.HasPrefix(lines[2], "*") || !strings.Contains(lines[2], "bob@x.com") || !strings.Contains(lines[1], "work") {
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

func row(t *testing.T, out, email string) []string {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, email) {
			return strings.Fields(strings.TrimPrefix(line, "*"))
		}
	}
	t.Fatalf("no row for %s in:\n%s", email, out)
	return nil
}

func TestListShowsLiveUsage(t *testing.T) {
	h := newHarness(t)
	h.usage["at-u1"] = plusUsage
	h.usage["at-u2"] = proliteUsage
	h.login("alice@x.com", "u1", "a1")
	h.run("cxswap", "add")
	h.login("bob@x.com", "u2", "a2")
	h.run("cxswap", "add")
	code, out, errs := h.run("cxswap")
	if code != 0 || !strings.Contains(out, "5H") || !strings.Contains(out, "WEEK") {
		t.Fatalf("%d %q %q", code, out, errs)
	}
	if got := strings.Join(row(t, out, "alice@x.com")[2:], " "); got != "plus 42% 10% now" {
		t.Fatalf("alice row: %q", got)
	}
	if got := strings.Join(row(t, out, "bob@x.com")[2:], " "); got != "prolite - 7% now" {
		t.Fatalf("bob row: %q", got)
	}
}

func TestListTreatsElapsedWindowAsZero(t *testing.T) {
	h := newHarness(t)
	h.usage["at-u1"] = plusUsage
	h.now = time.Unix(1791436850, 0).Add(time.Minute)
	h.login("alice@x.com", "u1", "a1")
	h.run("cxswap", "add")
	_, out, _ := h.run("cxswap")
	if got := row(t, out, "alice@x.com")[3]; got != "0%" {
		t.Fatalf("5h after reset: %q\n%s", got, out)
	}
}

func TestListMarksUnavailableUsage(t *testing.T) {
	h := newHarness(t)
	h.login("alice@x.com", "u1", "a1")
	h.run("cxswap", "add")
	code, out, _ := h.run("cxswap")
	if code != 0 || !strings.Contains(out, "unavailable") {
		t.Fatalf("%d %q", code, out)
	}
}

func TestStatusShowsUsageWindows(t *testing.T) {
	h := newHarness(t)
	h.usage["at-u1"] = plusUsage
	h.login("alice@x.com", "u1", "a1")
	h.run("cxswap", "add")
	code, out, _ := h.run("cxswap", "status")
	for _, want := range []string{"Using #1 alice@x.com", "5h:", "42%", "week:", "10%", "resets"} {
		if code != 0 || !strings.Contains(out, want) {
			t.Fatalf("missing %q in %q", want, out)
		}
	}
}
