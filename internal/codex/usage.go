package codex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/WellWells/agentswap/internal/swap"
)

const (
	DefaultBaseURL    = "https://chatgpt.com/backend-api"
	DefaultRefreshURL = "https://auth.openai.com/oauth/token"
	ClientID          = "app_EMoamEEZ73f0CkXaXp7hrann"
	maxSessionFiles   = 300
)

var (
	ErrNoUsage      = errors.New("usage is only available for ChatGPT logins")
	ErrLoginExpired = errors.New("login expired; log in to this account again")
)

type httpError int

func (e httpError) Error() string { return fmt.Sprintf("HTTP %d", int(e)) }

type creds struct {
	access  string
	refresh string
	user    string
	account string
}

func parseCreds(b []byte) (creds, error) {
	var a struct {
		Tokens *struct {
			IDToken      string `json:"id_token"`
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
			AccountID    string `json:"account_id"`
		} `json:"tokens"`
	}
	if err := json.Unmarshal(b, &a); err != nil {
		return creds{}, err
	}
	if a.Tokens == nil || a.Tokens.AccessToken == "" {
		return creds{}, ErrNoUsage
	}
	var c struct {
		Sub  string `json:"sub"`
		Auth struct {
			AccountID string `json:"chatgpt_account_id"`
			UserID    string `json:"chatgpt_user_id"`
		} `json:"https://api.openai.com/auth"`
	}
	jwtClaims(a.Tokens.IDToken, &c)
	return creds{
		access:  a.Tokens.AccessToken,
		refresh: a.Tokens.RefreshToken,
		user:    firstNonEmpty(c.Auth.UserID, c.Sub),
		account: firstNonEmpty(a.Tokens.AccountID, c.Auth.AccountID),
	}, nil
}

func (p Provider) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

func (p Provider) client() *http.Client {
	if p.Client != nil {
		return p.Client
	}
	return http.DefaultClient
}

func (p Provider) baseURL() string {
	if p.BaseURL != "" {
		return p.BaseURL
	}
	if b, err := os.ReadFile(filepath.Join(p.Home, "config.toml")); err == nil {
		if v := topLevel(b, "chatgpt_base_url"); v != "" {
			return v
		}
	}
	return DefaultBaseURL
}

func usageURL(base string) string {
	base = strings.TrimRight(base, "/")
	if (strings.HasPrefix(base, "https://chatgpt.com") || strings.HasPrefix(base, "https://chat.openai.com")) && !strings.Contains(base, "/backend-api") {
		base += "/backend-api"
	}
	if strings.Contains(base, "/backend-api") {
		return base + "/wham/usage"
	}
	return base + "/api/codex/usage"
}

func (p Provider) Usage(ctx context.Context, snap []byte, active bool) (swap.Usage, []byte, error) {
	c, err := parseCreds(snap)
	if err != nil {
		return swap.Usage{}, nil, err
	}
	var refreshed []byte
	renew := func() error {
		b, err := p.refreshTokens(ctx, snap, c.refresh)
		if err != nil {
			return err
		}
		refreshed, snap = b, b
		c, err = parseCreds(b)
		return err
	}
	if !active && c.refresh != "" && p.expiresSoon(c.access) {
		if err := renew(); err != nil {
			return swap.Usage{}, nil, err
		}
	}
	u, err := p.fetchWithRetry(ctx, c)
	var he httpError
	if errors.As(err, &he) && he == http.StatusUnauthorized && !active && refreshed == nil && c.refresh != "" {
		if rerr := renew(); rerr != nil {
			return swap.Usage{}, nil, rerr
		}
		u, err = p.fetchWithRetry(ctx, c)
	}
	if errors.As(err, &he) && he == http.StatusUnauthorized {
		return swap.Usage{}, refreshed, ErrLoginExpired
	}
	if err != nil {
		if su, ok := p.sessionUsage(c.user, c.account); ok {
			return su, refreshed, nil
		}
		return swap.Usage{}, refreshed, err
	}
	return u, refreshed, nil
}

func (p Provider) fetchWithRetry(ctx context.Context, c creds) (swap.Usage, error) {
	u, err := p.fetchUsage(ctx, c)
	if !transient(err) {
		return u, err
	}
	select {
	case <-ctx.Done():
		return u, err
	case <-time.After(300 * time.Millisecond):
	}
	return p.fetchUsage(ctx, c)
}

func transient(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var he httpError
	if errors.As(err, &he) {
		return he >= 500
	}
	var ue *url.Error
	return errors.As(err, &ue)
}

func (p Provider) expiresSoon(access string) bool {
	var c struct {
		Exp int64 `json:"exp"`
	}
	if jwtClaims(access, &c) != nil || c.Exp == 0 {
		return false
	}
	return time.Unix(c.Exp, 0).Sub(p.now()) < 5*time.Minute
}

type apiWindow struct {
	UsedPercent        float64 `json:"used_percent"`
	LimitWindowSeconds int64   `json:"limit_window_seconds"`
	ResetAt            int64   `json:"reset_at"`
}

func (w *apiWindow) window() swap.Window {
	out := swap.Window{UsedPercent: int(math.Round(w.UsedPercent)), Minutes: int(w.LimitWindowSeconds / 60)}
	if w.ResetAt > 0 {
		out.ResetsAt = time.Unix(w.ResetAt, 0)
	}
	return out
}

func (p Provider) fetchUsage(ctx context.Context, c creds) (swap.Usage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, usageURL(p.baseURL()), nil)
	if err != nil {
		return swap.Usage{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.access)
	p.identify(req)
	if c.account != "" {
		req.Header.Set("ChatGPT-Account-ID", c.account)
	}
	resp, err := p.client().Do(req)
	if err != nil {
		return swap.Usage{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body)
		return swap.Usage{}, httpError(resp.StatusCode)
	}
	var r struct {
		PlanType  string `json:"plan_type"`
		RateLimit *struct {
			Primary   *apiWindow `json:"primary_window"`
			Secondary *apiWindow `json:"secondary_window"`
		} `json:"rate_limit"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return swap.Usage{}, err
	}
	u := swap.Usage{Plan: r.PlanType, At: p.now(), Live: true}
	if r.RateLimit != nil {
		for _, w := range []*apiWindow{r.RateLimit.Primary, r.RateLimit.Secondary} {
			if w != nil {
				u.Windows = append(u.Windows, w.window())
			}
		}
	}
	return u, nil
}

func (p Provider) refreshTokens(ctx context.Context, snap []byte, refreshToken string) ([]byte, error) {
	body, _ := json.Marshal(map[string]string{"client_id": ClientID, "grant_type": "refresh_token", "refresh_token": refreshToken})
	url := p.RefreshURL
	if url == "" {
		url = DefaultRefreshURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	p.identify(req)
	resp, err := p.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusUnauthorized || deadRefresh(string(data)) {
			return nil, ErrLoginExpired
		}
		return nil, fmt.Errorf("token refresh: %w", httpError(resp.StatusCode))
	}
	var t map[string]json.RawMessage
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, fmt.Errorf("token refresh: %w", err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(snap, &top); err != nil {
		return nil, err
	}
	var tokens map[string]json.RawMessage
	if err := json.Unmarshal(top["tokens"], &tokens); err != nil {
		return nil, err
	}
	for _, k := range []string{"id_token", "access_token", "refresh_token"} {
		if v, ok := t[k]; ok && string(v) != "null" && string(v) != `""` {
			tokens[k] = v
		}
	}
	top["tokens"], _ = json.Marshal(tokens)
	top["last_refresh"], _ = json.Marshal(p.now().UTC().Format(time.RFC3339Nano))
	return json.MarshalIndent(top, "", "  ")
}

func deadRefresh(body string) bool {
	for _, code := range []string{"refresh_token_expired", "refresh_token_reused", "refresh_token_invalidated", "invalid_grant"} {
		if strings.Contains(body, code) {
			return true
		}
	}
	return false
}

type sessionWindow struct {
	UsedPercent   float64 `json:"used_percent"`
	WindowMinutes int     `json:"window_minutes"`
	ResetsAt      int64   `json:"resets_at"`
}

func (p Provider) sessionUsage(user, account string) (swap.Usage, bool) {
	var files []string
	filepath.WalkDir(filepath.Join(p.Home, "sessions"), func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(path, ".jsonl") {
			files = append(files, path)
		}
		return nil
	})
	sort.Sort(sort.Reverse(sort.StringSlice(files)))
	if len(files) > maxSessionFiles {
		files = files[:maxSessionFiles]
	}
	for _, f := range files {
		if u, ok := sessionFileUsage(f, user, account); ok {
			return u, true
		}
	}
	return swap.Usage{}, false
}

func sessionFileUsage(path, user, account string) (swap.Usage, bool) {
	f, err := os.Open(path)
	if err != nil {
		return swap.Usage{}, false
	}
	defer f.Close()
	r := bufio.NewReader(f)
	first, err := r.ReadBytes('\n')
	if err != nil && len(first) == 0 {
		return swap.Usage{}, false
	}
	var meta struct {
		Payload struct {
			User    string `json:"creator_user_id"`
			Account string `json:"creator_account_id"`
		} `json:"payload"`
	}
	if json.Unmarshal(first, &meta) != nil || meta.Payload.User != user || meta.Payload.Account != account {
		return swap.Usage{}, false
	}
	var last []byte
	for {
		line, err := r.ReadBytes('\n')
		if bytes.Contains(line, []byte(`"rate_limits"`)) {
			last = line
		}
		if err != nil {
			break
		}
	}
	if last == nil {
		return swap.Usage{}, false
	}
	var ev struct {
		Timestamp time.Time `json:"timestamp"`
		Payload   struct {
			RateLimits *struct {
				PlanType  string         `json:"plan_type"`
				Primary   *sessionWindow `json:"primary"`
				Secondary *sessionWindow `json:"secondary"`
			} `json:"rate_limits"`
		} `json:"payload"`
	}
	if json.Unmarshal(last, &ev) != nil || ev.Payload.RateLimits == nil {
		return swap.Usage{}, false
	}
	rl := ev.Payload.RateLimits
	u := swap.Usage{Plan: rl.PlanType, At: ev.Timestamp}
	for _, w := range []*sessionWindow{rl.Primary, rl.Secondary} {
		if w == nil {
			continue
		}
		win := swap.Window{UsedPercent: int(math.Round(w.UsedPercent)), Minutes: w.WindowMinutes}
		if w.ResetsAt > 0 {
			win.ResetsAt = time.Unix(w.ResetsAt, 0)
		}
		u.Windows = append(u.Windows, win)
	}
	return u, true
}
