package antigravity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

var testNow = time.Date(2026, 10, 8, 10, 0, 0, 0, time.UTC)

const quotaBody = `{"groups":[
 {"displayName":"Gemini","buckets":[
  {"bucketId":"gemini-5h","window":"18000s","remainingFraction":0.75,"resetTime":"2026-10-08T12:00:00Z","displayName":"5 hours"},
  {"bucketId":"gemini-weekly","remainingFraction":0.1,"resetTime":"2026-10-12T00:00:00Z"}]},
 {"displayName":"Claude and GPT","buckets":[
  {"bucketId":"3p-5h","remainingFraction":1,"resetTime":"2026-10-08T13:00:00Z"},
  {"bucketId":"3p-weekly","resetTime":"2026-10-12T00:00:00Z"},
  {"bucketId":"3p-daily","remainingFraction":0.5,"disabled":true}]}]}`

type api struct {
	valid    map[string]bool
	project  string
	quota    string
	refresh  func(url.Values) (int, string)
	hits     []string
	projects []string
	agents   []string
}

func (a *api) server(t *testing.T) *httptest.Server {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.hits = append(a.hits, r.URL.Path)
		a.agents = append(a.agents, r.Header.Get("User-Agent"))
		if r.URL.Path == "/token" {
			r.ParseForm()
			code, out := a.refresh(r.PostForm)
			w.WriteHeader(code)
			io.WriteString(w, out)
			return
		}
		if !a.valid[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")] {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/v1internal:loadCodeAssist":
			var b struct {
				Metadata struct {
					IdeType string `json:"ideType"`
				} `json:"metadata"`
			}
			json.NewDecoder(r.Body).Decode(&b)
			if b.Metadata.IdeType != "ANTIGRAVITY" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			project := a.project
			if project == "" {
				project = `"proj-1"`
			}
			io.WriteString(w, `{"cloudaicompanionProject":`+project+`,"currentTier":{"id":"free-tier","name":"Free"},"paidTier":{"id":"g1-pro-tier","name":"Google AI Pro"}}`)
		case "/v1internal:retrieveUserQuotaSummary":
			var b struct {
				Project string `json:"project"`
			}
			json.NewDecoder(r.Body).Decode(&b)
			a.projects = append(a.projects, b.Project)
			quota := a.quota
			if quota == "" {
				quota = quotaBody
			}
			io.WriteString(w, quota)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

func usageProvider(t *testing.T, a *api) Provider {
	s := a.server(t)
	return Provider{BaseURL: s.URL, RefreshURL: s.URL + "/token", UserAgent: "agentswap/test", Now: func() time.Time { return testNow }}
}

func TestUsageParsesQuota(t *testing.T) {
	a := &api{valid: map[string]bool{"at": true}}
	p := usageProvider(t, a)
	u, refreshed, err := p.Usage(context.Background(), blob("1", "a@x", "at", "rt", "2026-10-08T11:00:00Z"), true)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed != nil {
		t.Fatal("refreshed a fresh token")
	}
	if u.Plan != "Google AI Pro" || !u.Live || !u.At.Equal(testNow) {
		t.Fatalf("%+v", u)
	}
	if len(u.Windows) != 3 {
		t.Fatalf("%+v", u.Windows)
	}
	w := u.Windows[0]
	if w.UsedPercent != 25 || w.Minutes != 300 || w.Label != "" || !w.ResetsAt.Equal(time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("5h %+v", w)
	}
	if w := u.Windows[1]; w.UsedPercent != 90 || w.Minutes != 7*24*60 || w.Label != "" {
		t.Fatalf("weekly %+v", w)
	}
	if w := u.Windows[2]; w.UsedPercent != 0 || w.Minutes != 300 || w.Label != "Claude and GPT" {
		t.Fatalf("3p %+v", w)
	}
	if len(a.projects) != 1 || a.projects[0] != "proj-1" {
		t.Fatalf("projects %v", a.projects)
	}
	if a.agents[0] != "agentswap/test (antigravity)" {
		t.Fatalf("agent %q", a.agents[0])
	}
}

func TestUsageProjectObject(t *testing.T) {
	a := &api{valid: map[string]bool{"at": true}, project: `{"id":"proj-2","name":"x"}`}
	p := usageProvider(t, a)
	if _, _, err := p.Usage(context.Background(), blob("1", "a@x", "at", "rt", "2026-10-08T11:00:00Z"), true); err != nil {
		t.Fatal(err)
	}
	if a.projects[0] != "proj-2" {
		t.Fatalf("projects %v", a.projects)
	}
}

func refreshOK(seen *url.Values) func(url.Values) (int, string) {
	return func(v url.Values) (int, string) {
		*seen = v
		return 200, `{"access_token":"new","expires_in":3599,"token_type":"Bearer","id_token":"h.e.s"}`
	}
}

func TestUsageRefreshesExpiringInactive(t *testing.T) {
	var seen url.Values
	a := &api{valid: map[string]bool{"new": true}, refresh: refreshOK(&seen)}
	p := usageProvider(t, a)
	snap := blob("1", "a@x", "old", "rt", "2026-10-08T10:03:00Z")
	_, refreshed, err := p.Usage(context.Background(), snap, false)
	if err != nil {
		t.Fatal(err)
	}
	if seen.Get("grant_type") != "refresh_token" || seen.Get("refresh_token") != "rt" || seen.Get("client_id") != unmask(maskedClientID) || seen.Get("client_secret") == "" {
		t.Fatalf("%v", seen)
	}
	if !strings.HasSuffix(seen.Get("client_id"), ".apps.googleusercontent.com") {
		t.Fatalf("client id %q", seen.Get("client_id"))
	}
	want := strings.Replace(string(snap), `"access_token":"old"`, `"access_token":"new"`, 1)
	want = strings.Replace(want, `"expiry":"2026-10-08T10:03:00Z"`, `"expiry":"2026-10-08T10:59:59Z"`, 1)
	want = want[:strings.Index(want, `"id_token":"`)] + `"id_token":"h.e.s"}`
	if string(refreshed) != want {
		t.Fatalf("got\n%s\nwant\n%s", refreshed, want)
	}
}

func TestUsageActiveRefreshesInMemoryOnly(t *testing.T) {
	var seen url.Values
	a := &api{valid: map[string]bool{"new": true}, refresh: refreshOK(&seen)}
	p := usageProvider(t, a)
	u, refreshed, err := p.Usage(context.Background(), blob("1", "a@x", "old", "rt", "2026-10-08T09:00:00Z"), true)
	if err != nil || len(u.Windows) == 0 {
		t.Fatalf("%+v %v", u, err)
	}
	if refreshed != nil {
		t.Fatal("returned refreshed credentials for the active account")
	}
}

func TestUsageRetriesAfter401(t *testing.T) {
	var seen url.Values
	a := &api{valid: map[string]bool{"new": true}, refresh: refreshOK(&seen)}
	p := usageProvider(t, a)
	_, refreshed, err := p.Usage(context.Background(), blob("1", "a@x", "revoked", "rt", "2026-10-08T11:00:00Z"), false)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(refreshed, []byte(`"access_token":"new"`)) {
		t.Fatalf("%s", refreshed)
	}
}

func TestUsageLoginExpired(t *testing.T) {
	a := &api{valid: map[string]bool{}, refresh: func(url.Values) (int, string) {
		return 400, `{"error":"invalid_grant","error_description":"Token has been expired or revoked."}`
	}}
	p := usageProvider(t, a)
	if _, _, err := p.Usage(context.Background(), blob("1", "a@x", "old", "rt", "2026-10-08T09:00:00Z"), false); !errors.Is(err, ErrLoginExpired) {
		t.Fatalf("expiring: %v", err)
	}
	if _, _, err := p.Usage(context.Background(), blob("1", "a@x", "old", "rt", "2026-10-08T11:00:00Z"), true); !errors.Is(err, ErrLoginExpired) {
		t.Fatalf("401: %v", err)
	}
	a.refresh = func(url.Values) (int, string) { return 401, `{"error":"invalid_client"}` }
	if _, _, err := p.Usage(context.Background(), blob("1", "a@x", "old", "rt", "2026-10-08T09:00:00Z"), false); err == nil || errors.Is(err, ErrLoginExpired) {
		t.Fatalf("invalid_client: %v", err)
	}
}

func TestWindowMinutes(t *testing.T) {
	cases := []struct {
		window, id string
		want       int
	}{
		{`"18000s"`, "x", 300},
		{`"604800s"`, "x", 7 * 24 * 60},
		{`"5h"`, "x", 300},
		{`"weekly"`, "x", 7 * 24 * 60},
		{`18000`, "x", 300},
		{``, "gemini-5h", 300},
		{``, "3p-weekly", 7 * 24 * 60},
		{`"WEEKLY"`, "x", 7 * 24 * 60},
		{`"FIVE_HOURS"`, "x", 300},
		{``, "gemini-daily", 24 * 60},
		{``, "other", 0},
	}
	for _, c := range cases {
		if got := windowMinutes(json.RawMessage(c.window), c.id); got != c.want {
			t.Errorf("%s %s: %d, want %d", c.window, c.id, got, c.want)
		}
	}
}
