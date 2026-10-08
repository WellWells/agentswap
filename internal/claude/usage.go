package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/WellWells/agentswap/internal/jsonx"
	"github.com/WellWells/agentswap/internal/official"
	"github.com/WellWells/agentswap/internal/swap"
)

const (
	DefaultBaseURL    = "https://api.anthropic.com"
	DefaultRefreshURL = "https://platform.claude.com/v1/oauth/token"
	ClientID          = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	betaHeader        = "oauth-2025-04-20"
	week              = 7 * 24 * 60
	axiosAccept       = "application/json, text/plain, */*"
	pluginScope       = "user:plugins"
)

var defaultScopes = []string{"user:profile", "user:inference", "user:sessions:claude_code", "user:mcp_servers", "user:file_upload"}

var (
	ErrLoginExpired = errors.New("login expired; log in to this account again")
	ErrRateLimited  = errors.New("rate limited")
	ErrActiveStale  = errors.New("token expired")
)

type httpError int

func (e httpError) Error() string { return fmt.Sprintf("HTTP %d", int(e)) }

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

func (p Provider) Usage(ctx context.Context, snap []byte, active bool) (swap.Usage, []byte, error) {
	ai, err := aiOf(snap)
	if err != nil {
		return swap.Usage{}, nil, err
	}
	var refreshed []byte
	renew := func() error {
		b, err := p.refresh(ctx, snap, ai.RefreshToken, ai.Scopes)
		if err != nil {
			return err
		}
		refreshed, snap = b, b
		ai, err = aiOf(b)
		return err
	}
	if !active && ai.RefreshToken != "" && ai.ExpiresAt > 0 && time.UnixMilli(int64(ai.ExpiresAt)).Sub(p.now()) < 5*time.Minute {
		if err := renew(); err != nil {
			return swap.Usage{}, nil, err
		}
	}
	u, err := p.fetchWithRetry(ctx, ai.AccessToken)
	var he httpError
	if errors.As(err, &he) && he == http.StatusUnauthorized {
		if active {
			return swap.Usage{}, nil, ErrActiveStale
		}
		if refreshed == nil && ai.RefreshToken != "" {
			if rerr := renew(); rerr != nil {
				return swap.Usage{}, nil, rerr
			}
			u, err = p.fetchWithRetry(ctx, ai.AccessToken)
		}
		if errors.As(err, &he) && he == http.StatusUnauthorized {
			return swap.Usage{}, refreshed, ErrLoginExpired
		}
	}
	return u, refreshed, err
}

func (p Provider) fetchWithRetry(ctx context.Context, access string) (swap.Usage, error) {
	u, err := p.fetchUsage(ctx, access)
	if !transient(err) {
		return u, err
	}
	select {
	case <-ctx.Done():
		return u, err
	case <-time.After(300 * time.Millisecond):
	}
	return p.fetchUsage(ctx, access)
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

type apiWindow struct {
	Utilization *float64 `json:"utilization"`
	ResetsAt    *string  `json:"resets_at"`
}

func window(pct *float64, resets *string, minutes int, label string) (swap.Window, bool) {
	if pct == nil {
		return swap.Window{}, false
	}
	w := swap.Window{UsedPercent: int(math.Round(*pct)), Minutes: minutes, Label: label}
	if resets != nil {
		if t, err := time.Parse(time.RFC3339, *resets); err == nil {
			w.ResetsAt = t
		}
	}
	return w, true
}

func (p Provider) fetchUsage(ctx context.Context, access string) (swap.Usage, error) {
	base := p.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/api/oauth/usage", nil)
	if err != nil {
		return swap.Usage{}, err
	}
	req.Header.Set("Accept", axiosAccept)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+access)
	req.Header.Set("anthropic-beta", betaHeader)
	if p.UserAgent != nil {
		req.Header.Set("User-Agent", p.UserAgent())
	}
	resp, err := p.client().Do(req)
	if err != nil {
		return swap.Usage{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return swap.Usage{}, ErrRateLimited
	}
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body)
		return swap.Usage{}, httpError(resp.StatusCode)
	}
	var r struct {
		FiveHour *apiWindow `json:"five_hour"`
		SevenDay *apiWindow `json:"seven_day"`
		Opus     *apiWindow `json:"seven_day_opus"`
		Sonnet   *apiWindow `json:"seven_day_sonnet"`
		Limits   []struct {
			Scope struct {
				Model struct {
					DisplayName string `json:"display_name"`
				} `json:"model"`
			} `json:"scope"`
			Percent  *float64 `json:"percent"`
			ResetsAt *string  `json:"resets_at"`
		} `json:"limits"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return swap.Usage{}, err
	}
	u := swap.Usage{At: p.now(), Live: true}
	add := func(w swap.Window, ok bool) {
		if ok {
			u.Windows = append(u.Windows, w)
		}
	}
	if r.FiveHour != nil {
		add(window(r.FiveHour.Utilization, r.FiveHour.ResetsAt, 300, ""))
	}
	if r.SevenDay != nil {
		add(window(r.SevenDay.Utilization, r.SevenDay.ResetsAt, week, ""))
	}
	for _, l := range r.Limits {
		if l.Scope.Model.DisplayName != "" {
			add(window(l.Percent, l.ResetsAt, week, l.Scope.Model.DisplayName))
		}
	}
	if len(r.Limits) == 0 {
		if r.Opus != nil {
			add(window(r.Opus.Utilization, r.Opus.ResetsAt, week, "Opus"))
		}
		if r.Sonnet != nil {
			add(window(r.Sonnet.Utilization, r.Sonnet.ResetsAt, week, "Sonnet"))
		}
	}
	return u, nil
}

func refreshScopes(granted []string) string {
	scopes := append([]string(nil), defaultScopes...)
	for _, s := range granted {
		if s == pluginScope {
			scopes = append(scopes, pluginScope)
			break
		}
	}
	return strings.Join(scopes, " ")
}

func (p Provider) refresh(ctx context.Context, snap []byte, refreshToken string, granted []string) ([]byte, error) {
	body, _ := json.Marshal(struct {
		GrantType    string `json:"grant_type"`
		RefreshToken string `json:"refresh_token"`
		ClientID     string `json:"client_id"`
		Scope        string `json:"scope"`
	}{"refresh_token", refreshToken, ClientID, refreshScopes(granted)})
	target := p.RefreshURL
	if target == "" {
		target = DefaultRefreshURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", axiosAccept)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", official.ClaudeAxios)
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
		if deadRefresh(resp.StatusCode, data) {
			return nil, ErrLoginExpired
		}
		return nil, fmt.Errorf("token refresh: %w", httpError(resp.StatusCode))
	}
	var t struct {
		AccessToken  string  `json:"access_token"`
		RefreshToken string  `json:"refresh_token"`
		ExpiresIn    float64 `json:"expires_in"`
		Scope        string  `json:"scope"`
	}
	if err := json.Unmarshal(data, &t); err != nil || t.AccessToken == "" {
		return nil, errors.New("token refresh: response has no access_token")
	}
	return p.applyTokens(snap, t.AccessToken, t.RefreshToken, t.ExpiresIn, t.Scope)
}

func deadRefresh(status int, body []byte) bool {
	if status != http.StatusBadRequest && status != http.StatusUnauthorized && status != http.StatusForbidden {
		return false
	}
	var e struct {
		Error string `json:"error"`
	}
	json.Unmarshal(body, &e)
	return e.Error == "invalid_grant"
}

func (p Provider) applyTokens(snap []byte, access, refresh string, expiresIn float64, scope string) ([]byte, error) {
	e, err := unpack(snap)
	if err != nil {
		return nil, err
	}
	ai, ok, err := jsonx.Get(e.Credentials, "claudeAiOauth")
	if err != nil || !ok {
		return nil, ErrUnsupportedLogin
	}
	set := func(k string, v any) {
		if err != nil {
			return
		}
		b, _ := json.Marshal(v)
		ai, err = jsonx.Set(ai, k, b)
	}
	set("accessToken", access)
	if refresh != "" {
		set("refreshToken", refresh)
	}
	if expiresIn > 0 {
		set("expiresAt", json.Number(strconv.FormatInt(p.now().UnixMilli()+int64(expiresIn*1000), 10)))
	}
	if scope != "" {
		set("scopes", strings.Fields(scope))
	}
	if err != nil {
		return nil, err
	}
	creds, err := jsonx.Set(e.Credentials, "claudeAiOauth", ai)
	if err != nil {
		return nil, err
	}
	return Pack(creds, e.OAuthAccount), nil
}
