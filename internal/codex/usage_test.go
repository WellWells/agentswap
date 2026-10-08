package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var now = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)

func authJSON(access, refresh, user, account string) []byte {
	b, _ := json.MarshalIndent(map[string]any{
		"auth_mode":      "chatgpt",
		"OPENAI_API_KEY": nil,
		"tokens": map[string]any{
			"id_token": fakeJWT(map[string]any{
				"email": user + "@x.com",
				"https://api.openai.com/auth": map[string]any{
					"chatgpt_account_id": account,
					"chatgpt_user_id":    user,
				},
			}),
			"access_token":  access,
			"refresh_token": refresh,
			"account_id":    account,
		},
		"last_refresh": "2026-10-01T00:00:00Z",
	}, "", "  ")
	return b
}

func accessToken(name string, exp time.Time) string {
	return fakeJWT(map[string]any{"exp": exp.Unix(), "name": name})
}

const proliteBody = `{"plan_type":"prolite","rate_limit":{"primary_window":{"used_percent":7,"limit_window_seconds":604800,"reset_after_seconds":100,"reset_at":1791970861},"secondary_window":null}}`

const plusBody = `{"plan_type":"plus","rate_limit":{"primary_window":{"used_percent":42,"limit_window_seconds":18000,"reset_at":1791436850},"secondary_window":{"used_percent":10,"limit_window_seconds":604800,"reset_at":1791998357}}}`

type backend struct {
	srv      *httptest.Server
	usage    map[string]string
	refresh  func(w http.ResponseWriter, body map[string]string)
	refreshN atomic.Int32
	lastUA   atomic.Value
}

func newBackend(t *testing.T) *backend {
	b := &backend{usage: map[string]string{}}
	b.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/backend-api/wham/usage":
			b.lastUA.Store(r.Header.Get("User-Agent"))
			tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			body, ok := b.usage[tok+"|"+r.Header.Get("ChatGPT-Account-ID")]
			if !ok {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			io.WriteString(w, body)
		case "/oauth/token":
			b.refreshN.Add(1)
			var req map[string]string
			json.NewDecoder(r.Body).Decode(&req)
			if req["grant_type"] != "refresh_token" || req["client_id"] != ClientID {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			b.refresh(w, req)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(b.srv.Close)
	return b
}

func (b *backend) provider(t *testing.T) Provider {
	return Provider{
		Home:       t.TempDir(),
		BaseURL:    b.srv.URL + "/backend-api",
		RefreshURL: b.srv.URL + "/oauth/token",
		UserAgent:  "agentswap/test",
		Now:        func() time.Time { return now },
	}
}

func TestBackendUsageURL(t *testing.T) {
	cases := map[string]string{
		"https://chatgpt.com":               "https://chatgpt.com/backend-api/wham/usage",
		"https://chatgpt.com/backend-api/":  "https://chatgpt.com/backend-api/wham/usage",
		"https://chat.openai.com":           "https://chat.openai.com/backend-api/wham/usage",
		"http://127.0.0.1:9000/backend-api": "http://127.0.0.1:9000/backend-api/wham/usage",
		"http://localhost:8080/":            "http://localhost:8080/api/codex/usage",
	}
	for in, want := range cases {
		if got := usageURL(in); got != want {
			t.Errorf("%s: got %s", in, got)
		}
	}
}

func TestBaseURLComesFromConfig(t *testing.T) {
	p := Provider{Home: t.TempDir()}
	if got := p.baseURL(); got != DefaultBaseURL {
		t.Fatalf("default: %s", got)
	}
	os.WriteFile(filepath.Join(p.Home, "config.toml"), []byte("chatgpt_base_url = \"http://proxy.local/backend-api\"\n"), 0o600)
	if got := p.baseURL(); got != "http://proxy.local/backend-api" {
		t.Fatalf("config: %s", got)
	}
}

func TestUsageLiveBucketsWindowsByLength(t *testing.T) {
	b := newBackend(t)
	tok := accessToken("a", now.Add(240*time.Hour))
	b.usage[tok+"|acct-1"] = proliteBody
	u, refreshed, err := b.provider(t).Usage(context.Background(), authJSON(tok, "rt", "user-1", "acct-1"), true)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed != nil || !u.Live || u.Plan != "prolite" || len(u.Windows) != 1 {
		t.Fatalf("got %+v refreshed=%v", u, refreshed != nil)
	}
	w := u.Windows[0]
	if w.UsedPercent != 7 || w.Minutes != 10080 || !w.ResetsAt.Equal(time.Unix(1791970861, 0)) {
		t.Fatalf("window %+v", w)
	}
	if b.lastUA.Load() != "agentswap/test" {
		t.Fatalf("user agent %v", b.lastUA.Load())
	}
}

func TestUsageReadsBothWindows(t *testing.T) {
	b := newBackend(t)
	tok := accessToken("a", now.Add(time.Hour))
	b.usage[tok+"|acct-1"] = plusBody
	u, _, err := b.provider(t).Usage(context.Background(), authJSON(tok, "rt", "user-1", "acct-1"), false)
	if err != nil || len(u.Windows) != 2 || u.Windows[0].Minutes != 300 || u.Windows[1].UsedPercent != 10 {
		t.Fatalf("got %+v, %v", u, err)
	}
}

func TestUsageRefreshesExpiringInactiveAccount(t *testing.T) {
	b := newBackend(t)
	oldTok := accessToken("old", now.Add(time.Minute))
	newTok := accessToken("new", now.Add(240*time.Hour))
	b.usage[newTok+"|acct-1"] = proliteBody
	b.refresh = func(w http.ResponseWriter, req map[string]string) {
		if req["refresh_token"] != "rt-old" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		fmt.Fprintf(w, `{"access_token":%q,"refresh_token":"rt-new"}`, newTok)
	}
	u, refreshed, err := b.provider(t).Usage(context.Background(), authJSON(oldTok, "rt-old", "user-1", "acct-1"), false)
	if err != nil || !u.Live {
		t.Fatalf("got %+v, %v", u, err)
	}
	var a map[string]any
	if err := json.Unmarshal(refreshed, &a); err != nil {
		t.Fatalf("refreshed snapshot: %v", err)
	}
	tokens := a["tokens"].(map[string]any)
	if tokens["refresh_token"] != "rt-new" || tokens["access_token"] != newTok || tokens["id_token"] == "" || tokens["account_id"] != "acct-1" {
		t.Fatalf("tokens %+v", tokens)
	}
	if a["auth_mode"] != "chatgpt" || a["last_refresh"] == "2026-10-01T00:00:00Z" {
		t.Fatalf("top-level %+v", a)
	}
	if !strings.Contains(string(refreshed), "\n  ") {
		t.Fatal("snapshot no longer pretty-printed")
	}
}

func TestUsageNeverRefreshesActiveAccount(t *testing.T) {
	b := newBackend(t)
	b.refresh = func(w http.ResponseWriter, req map[string]string) { io.WriteString(w, `{}`) }
	tok := accessToken("old", now.Add(-time.Hour))
	_, refreshed, err := b.provider(t).Usage(context.Background(), authJSON(tok, "rt", "user-1", "acct-1"), true)
	if err == nil || refreshed != nil || b.refreshN.Load() != 0 {
		t.Fatalf("err=%v refreshed=%v refresh calls=%d", err, refreshed != nil, b.refreshN.Load())
	}
}

func TestUsageRefreshesOnceAfter401(t *testing.T) {
	b := newBackend(t)
	staleTok := accessToken("stale", now.Add(240*time.Hour))
	newTok := accessToken("new", now.Add(240*time.Hour))
	b.usage[newTok+"|acct-1"] = proliteBody
	b.refresh = func(w http.ResponseWriter, req map[string]string) {
		fmt.Fprintf(w, `{"access_token":%q,"refresh_token":"rt-2"}`, newTok)
	}
	u, refreshed, err := b.provider(t).Usage(context.Background(), authJSON(staleTok, "rt", "user-1", "acct-1"), false)
	if err != nil || refreshed == nil || u.Plan != "prolite" || b.refreshN.Load() != 1 {
		t.Fatalf("got %+v %v refreshes=%d", u, err, b.refreshN.Load())
	}
}

func TestDeadRefreshTokenReportsLoginExpired(t *testing.T) {
	b := newBackend(t)
	b.refresh = func(w http.ResponseWriter, req map[string]string) {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"error":{"code":"refresh_token_reused"}}`)
	}
	tok := accessToken("old", now.Add(-time.Hour))
	_, refreshed, err := b.provider(t).Usage(context.Background(), authJSON(tok, "rt", "user-1", "acct-1"), false)
	if !errors.Is(err, ErrLoginExpired) || refreshed != nil {
		t.Fatalf("err=%v refreshed=%v", err, refreshed != nil)
	}
}

func writeSession(t *testing.T, home, name, user, account string, lines ...string) {
	t.Helper()
	p := filepath.Join(home, "sessions", "2026", "10", name[:2], "rollout-"+name+".jsonl")
	os.MkdirAll(filepath.Dir(p), 0o700)
	meta := fmt.Sprintf(`{"timestamp":"2026-10-08T00:00:00Z","type":"session_meta","payload":{"creator_user_id":%q,"creator_account_id":%q}}`, user, account)
	if err := os.WriteFile(p, []byte(meta+"\n"+strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func rateLine(ts string, primary int) string {
	return fmt.Sprintf(`{"timestamp":%q,"type":"event_msg","payload":{"type":"token_count","rate_limits":{"limit_id":"codex","plan_type":"plus","primary":{"used_percent":%d,"window_minutes":300,"resets_at":1791436850},"secondary":{"used_percent":3.4,"window_minutes":10080,"resets_at":1791998357}}}}`, ts, primary)
}

func TestUsageFallsBackToSessionLog(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }))
	defer srv.Close()
	p := Provider{Home: t.TempDir(), BaseURL: srv.URL + "/backend-api", Now: func() time.Time { return now }}
	writeSession(t, p.Home, "07T09-00-00", "user-1", "acct-1", rateLine("2026-10-07T09:00:00Z", 50))
	writeSession(t, p.Home, "08T01-00-00", "user-1", "acct-1",
		rateLine("2026-10-08T01:00:00Z", 10),
		`{"timestamp":"2026-10-08T01:30:00Z","type":"event_msg","payload":{"type":"agent_message"}}`,
		rateLine("2026-10-08T02:00:00Z", 12))
	writeSession(t, p.Home, "08T05-00-00", "user-2", "acct-2", rateLine("2026-10-08T05:00:00Z", 99))
	tok := accessToken("a", now.Add(240*time.Hour))
	u, _, err := p.Usage(context.Background(), authJSON(tok, "rt", "user-1", "acct-1"), true)
	if err != nil {
		t.Fatal(err)
	}
	if u.Live || !u.At.Equal(time.Date(2026, 10, 8, 2, 0, 0, 0, time.UTC)) || u.Plan != "plus" || len(u.Windows) != 2 {
		t.Fatalf("got %+v", u)
	}
	if u.Windows[0].UsedPercent != 12 || u.Windows[1].UsedPercent != 3 || u.Windows[1].Minutes != 10080 {
		t.Fatalf("windows %+v", u.Windows)
	}
}

func TestUsageUnavailableForAPIKey(t *testing.T) {
	b := newBackend(t)
	if _, _, err := b.provider(t).Usage(context.Background(), []byte(`{"OPENAI_API_KEY":"sk-abcdefgh12345678"}`), true); !errors.Is(err, ErrNoUsage) {
		t.Fatalf("got %v", err)
	}
}

func TestUsageRetriesOnceOnServerError(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		io.WriteString(w, proliteBody)
	}))
	defer srv.Close()
	p := Provider{Home: t.TempDir(), BaseURL: srv.URL + "/backend-api", Now: func() time.Time { return now }}
	tok := accessToken("a", now.Add(240*time.Hour))
	u, _, err := p.Usage(context.Background(), authJSON(tok, "rt", "user-1", "acct-1"), true)
	if err != nil || !u.Live || calls.Load() != 2 {
		t.Fatalf("got %+v err=%v calls=%d", u, err, calls.Load())
	}
}

func TestUsageDoesNotRetryClientErrors(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	p := Provider{Home: t.TempDir(), BaseURL: srv.URL + "/backend-api", Now: func() time.Time { return now }}
	tok := accessToken("a", now.Add(240*time.Hour))
	if _, _, err := p.Usage(context.Background(), authJSON(tok, "rt", "user-1", "acct-1"), true); err == nil || calls.Load() != 1 {
		t.Fatalf("err=%v calls=%d", err, calls.Load())
	}
}

func TestRevokedActiveTokenReportsLoginExpired(t *testing.T) {
	b := newBackend(t)
	p := b.provider(t)
	writeSession(t, p.Home, "08T01-00-00", "user-1", "acct-1", rateLine("2026-10-08T01:00:00Z", 10))
	tok := accessToken("revoked", now.Add(240*time.Hour))
	if _, _, err := p.Usage(context.Background(), authJSON(tok, "rt", "user-1", "acct-1"), true); !errors.Is(err, ErrLoginExpired) {
		t.Fatalf("got %v", err)
	}
}

func TestStill401AfterRefreshReportsLoginExpired(t *testing.T) {
	b := newBackend(t)
	b.refresh = func(w http.ResponseWriter, req map[string]string) {
		fmt.Fprintf(w, `{"access_token":%q,"refresh_token":"rt-2"}`, accessToken("also-bad", now.Add(240*time.Hour)))
	}
	tok := accessToken("stale", now.Add(240*time.Hour))
	if _, _, err := b.provider(t).Usage(context.Background(), authJSON(tok, "rt", "user-1", "acct-1"), false); !errors.Is(err, ErrLoginExpired) {
		t.Fatalf("got %v", err)
	}
}
