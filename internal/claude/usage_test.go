package claude

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type api struct {
	usage    map[string]string
	refresh  func(body map[string]string) (int, string)
	hits     []string
	betaSeen bool
}

func (a *api) server(t *testing.T) *httptest.Server {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.hits = append(a.hits, r.URL.Path)
		switch r.URL.Path {
		case "/api/oauth/usage":
			a.betaSeen = r.Header.Get("anthropic-beta") == "oauth-2025-04-20"
			body, ok := a.usage[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
			if !ok {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			if body == "429" {
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
			io.WriteString(w, body)
		case "/v1/oauth/token":
			var b map[string]string
			json.NewDecoder(r.Body).Decode(&b)
			code, out := a.refresh(b)
			w.WriteHeader(code)
			io.WriteString(w, out)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

const usageBody = `{"five_hour":{"utilization":42.4,"resets_at":"2026-10-08T12:00:00.000000+00:00"},"seven_day":{"utilization":10,"resets_at":"2026-10-12T00:00:00Z"},"limits":[{"scope":{"model":{"display_name":"Sonnet"}},"percent":55,"resets_at":"2026-10-12T00:00:00Z"}],"seven_day_opus":{"utilization":99,"resets_at":null}}`

var testNow = time.Date(2026, 10, 8, 10, 0, 0, 0, time.UTC)

func usageProvider(t *testing.T, a *api) Provider {
	s := a.server(t)
	p, _ := setup(t)
	p.BaseURL, p.RefreshURL = s.URL, s.URL+"/v1/oauth/token"
	p.Now = func() time.Time { return testNow }
	return p
}

func snapWith(access, refresh string, expires int64) []byte {
	c, _ := json.Marshal(map[string]any{"claudeAiOauth": map[string]any{"accessToken": access, "refreshToken": refresh, "expiresAt": expires, "keep": "me"}})
	return Pack(c, []byte(oauth("u", "o", "a@x")))
}

func TestUsageParsesWindows(t *testing.T) {
	a := &api{usage: map[string]string{"at": usageBody}}
	u, refreshed, err := usageProvider(t, a).Usage(context.Background(), snapWith("at", "rt", testNow.Add(time.Hour).UnixMilli()), true)
	if err != nil || refreshed != nil || !a.betaSeen {
		t.Fatalf("err=%v refreshed=%v beta=%v", err, refreshed != nil, a.betaSeen)
	}
	if len(u.Windows) != 3 || !u.Live {
		t.Fatalf("windows %+v", u.Windows)
	}
	w0, w1, w2 := u.Windows[0], u.Windows[1], u.Windows[2]
	if w0.UsedPercent != 42 || w0.Minutes != 300 || w0.ResetsAt.IsZero() || w1.Minutes != 7*24*60 || w1.Label != "" || w2.Label != "Sonnet" || w2.UsedPercent != 55 {
		t.Fatalf("%+v", u.Windows)
	}
}

func TestUsageFallsBackToModelFieldsWithoutLimits(t *testing.T) {
	a := &api{usage: map[string]string{"at": `{"five_hour":null,"seven_day_opus":{"utilization":5,"resets_at":"2026-10-12T00:00:00Z"}}`}}
	u, _, err := usageProvider(t, a).Usage(context.Background(), snapWith("at", "rt", testNow.Add(time.Hour).UnixMilli()), true)
	if err != nil || len(u.Windows) != 1 || u.Windows[0].Label != "Opus" {
		t.Fatalf("%+v %v", u.Windows, err)
	}
}

func TestInactiveExpiringTokenIsRefreshedAndKeepsFields(t *testing.T) {
	a := &api{usage: map[string]string{"at2": usageBody}}
	a.refresh = func(b map[string]string) (int, string) {
		if b["refresh_token"] != "rt" || b["client_id"] != ClientID || b["grant_type"] != "refresh_token" {
			return 400, `{"error":"bad"}`
		}
		return 200, `{"access_token":"at2","refresh_token":"rt2","expires_in":3600,"scope":"user:inference user:profile"}`
	}
	_, refreshed, err := usageProvider(t, a).Usage(context.Background(), snapWith("at", "rt", testNow.Add(time.Minute).UnixMilli()), false)
	if err != nil || refreshed == nil {
		t.Fatalf("err=%v", err)
	}
	ai, _ := aiOf(refreshed)
	if ai.AccessToken != "at2" || ai.RefreshToken != "rt2" || int64(ai.ExpiresAt) != testNow.Add(time.Hour).UnixMilli() {
		t.Fatalf("%+v", ai)
	}
	if !strings.Contains(string(refreshed), `"keep":"me"`) || !strings.Contains(string(refreshed), `"user:profile"`) {
		t.Fatalf("%s", refreshed)
	}
	if id, err := Identify(refreshed); err != nil || id.Key != "claude:u:o" {
		t.Fatalf("%+v %v", id, err)
	}
}

func TestInactive401RefreshesOnceThenRetries(t *testing.T) {
	a := &api{usage: map[string]string{"at2": usageBody}}
	a.refresh = func(map[string]string) (int, string) { return 200, `{"access_token":"at2","expires_in":3600}` }
	_, refreshed, err := usageProvider(t, a).Usage(context.Background(), snapWith("old", "rt", testNow.Add(time.Hour).UnixMilli()), false)
	if err != nil || refreshed == nil {
		t.Fatalf("err=%v", err)
	}
	if ai, _ := aiOf(refreshed); ai.RefreshToken != "rt" {
		t.Fatalf("refresh token lost: %+v", ai)
	}
}

func TestDeadRefreshIsLoginExpired(t *testing.T) {
	a := &api{usage: map[string]string{}}
	a.refresh = func(map[string]string) (int, string) { return 400, `{"error":"invalid_grant"}` }
	_, _, err := usageProvider(t, a).Usage(context.Background(), snapWith("old", "rt", testNow.Add(time.Hour).UnixMilli()), false)
	if !errors.Is(err, ErrLoginExpired) {
		t.Fatalf("err=%v", err)
	}
}

func TestOtherRefreshFailureIsNotLoginExpired(t *testing.T) {
	a := &api{usage: map[string]string{}}
	a.refresh = func(map[string]string) (int, string) { return 400, `{"error":"invalid_client"}` }
	_, _, err := usageProvider(t, a).Usage(context.Background(), snapWith("old", "rt", testNow.Add(time.Hour).UnixMilli()), false)
	if err == nil || errors.Is(err, ErrLoginExpired) {
		t.Fatalf("err=%v", err)
	}
}

func TestActiveNeverRefreshes(t *testing.T) {
	a := &api{usage: map[string]string{}}
	a.refresh = func(map[string]string) (int, string) { t.Error("refreshed active account"); return 500, "" }
	_, refreshed, err := usageProvider(t, a).Usage(context.Background(), snapWith("old", "rt", testNow.Add(-time.Hour).UnixMilli()), true)
	if !errors.Is(err, ErrActiveStale) || refreshed != nil {
		t.Fatalf("err=%v", err)
	}
}

func TestRateLimitedIsNotRetried(t *testing.T) {
	a := &api{usage: map[string]string{"at": "429"}}
	_, _, err := usageProvider(t, a).Usage(context.Background(), snapWith("at", "rt", testNow.Add(time.Hour).UnixMilli()), true)
	if !errors.Is(err, ErrRateLimited) || len(a.hits) != 1 {
		t.Fatalf("err=%v hits=%d", err, len(a.hits))
	}
}
