package antigravity

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
	"strings"
	"time"

	"github.com/WellWells/agentswap/internal/jsonx"
	"github.com/WellWells/agentswap/internal/swap"
)

const (
	DefaultBaseURL    = "https://cloudcode-pa.googleapis.com"
	DefaultRefreshURL = "https://oauth2.googleapis.com/token"
	day               = 24 * 60
	week              = 7 * day
)

var (
	ErrLoginExpired = errors.New("login expired; sign in to this account in agy again")
	ErrRateLimited  = errors.New("rate limited")
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

func (p Provider) expiring(expiry string) bool {
	t, err := time.Parse(time.RFC3339Nano, expiry)
	return err != nil || t.Sub(p.now()) < 5*time.Minute
}

func (p Provider) Usage(ctx context.Context, snap []byte, active bool) (swap.Usage, []byte, error) {
	c, err := parse(snap)
	if err != nil {
		return swap.Usage{}, nil, err
	}
	access := c.Token.AccessToken
	ua := ""
	if p.UserAgent != nil {
		ua = p.UserAgent(c.AuthMethod)
	}
	var refreshed []byte
	renew := func() error {
		b, err := p.refresh(ctx, ua, snap, c.Token.RefreshToken)
		if err != nil {
			return err
		}
		nc, err := parse(b)
		if err != nil {
			return err
		}
		refreshed, access = b, nc.Token.AccessToken
		return nil
	}
	if c.Token.RefreshToken != "" && p.expiring(c.Token.Expiry) {
		if err := renew(); err != nil {
			return swap.Usage{}, nil, err
		}
	}
	u, err := p.fetchWithRetry(ctx, ua, access)
	var he httpError
	if errors.As(err, &he) && he == http.StatusUnauthorized {
		if refreshed == nil && c.Token.RefreshToken != "" {
			if rerr := renew(); rerr != nil {
				return swap.Usage{}, nil, rerr
			}
			u, err = p.fetchWithRetry(ctx, ua, access)
		}
		if errors.As(err, &he) && he == http.StatusUnauthorized {
			err = ErrLoginExpired
		}
	}
	if active {
		refreshed = nil
	}
	return u, refreshed, err
}

func (p Provider) fetchWithRetry(ctx context.Context, ua, access string) (swap.Usage, error) {
	u, err := p.fetchUsage(ctx, ua, access)
	if !transient(err) {
		return u, err
	}
	select {
	case <-ctx.Done():
		return u, err
	case <-time.After(300 * time.Millisecond):
	}
	return p.fetchUsage(ctx, ua, access)
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

func (p Provider) call(ctx context.Context, ua, access, method string, body, out any) error {
	base := p.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base, "/")+"/v1internal:"+method, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+access)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", ua)
	resp, err := p.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return ErrRateLimited
	}
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body)
		return httpError(resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

type tier struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (t *tier) label() string {
	if t == nil {
		return ""
	}
	if t.Name != "" {
		return t.Name
	}
	return t.ID
}

func projectID(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var o struct {
		ID string `json:"id"`
	}
	json.Unmarshal(raw, &o)
	return o.ID
}

func windowMinutes(window json.RawMessage, bucketID string) int {
	var s string
	if json.Unmarshal(window, &s) != nil {
		var n float64
		if json.Unmarshal(window, &n) == nil && n > 0 {
			return int(n / 60)
		}
	}
	if d, err := time.ParseDuration(strings.ToLower(s)); err == nil && d > 0 {
		return int(d.Minutes())
	}
	for _, v := range []string{strings.ToLower(s), strings.ToLower(bucketID)} {
		switch {
		case strings.Contains(v, "week"):
			return week
		case strings.Contains(v, "5h") || strings.Contains(v, "five_hour"):
			return 300
		case strings.Contains(v, "day") || strings.Contains(v, "daily"):
			return day
		}
	}
	return 0
}

func groupLabel(name string) string {
	name = strings.TrimSpace(name)
	if len(name) > len(" models") && strings.EqualFold(name[len(name)-len(" models"):], " models") {
		return name[:len(name)-len(" models")]
	}
	return name
}

func (p Provider) fetchUsage(ctx context.Context, ua, access string) (swap.Usage, error) {
	var lr struct {
		Project     json.RawMessage `json:"cloudaicompanionProject"`
		CurrentTier *tier           `json:"currentTier"`
		PaidTier    *tier           `json:"paidTier"`
	}
	if err := p.call(ctx, ua, access, "loadCodeAssist", map[string]any{"metadata": map[string]string{"ideType": "ANTIGRAVITY"}}, &lr); err != nil {
		return swap.Usage{}, err
	}
	body := map[string]string{}
	if id := projectID(lr.Project); id != "" {
		body["project"] = id
	}
	var qs struct {
		Groups []struct {
			DisplayName string `json:"displayName"`
			Buckets     []struct {
				BucketID          string          `json:"bucketId"`
				Window            json.RawMessage `json:"window"`
				RemainingFraction *float64        `json:"remainingFraction"`
				ResetTime         string          `json:"resetTime"`
				DisplayName       string          `json:"displayName"`
				Disabled          bool            `json:"disabled"`
			} `json:"buckets"`
		} `json:"groups"`
	}
	if err := p.call(ctx, ua, access, "retrieveUserQuotaSummary", body, &qs); err != nil {
		return swap.Usage{}, err
	}
	u := swap.Usage{Plan: lr.PaidTier.label(), At: p.now(), Live: true}
	if u.Plan == "" {
		u.Plan = lr.CurrentTier.label()
	}
	for _, g := range qs.Groups {
		for _, b := range g.Buckets {
			if b.Disabled || b.RemainingFraction == nil {
				continue
			}
			used := int(math.Round((1 - *b.RemainingFraction) * 100))
			w := swap.Window{UsedPercent: min(max(used, 0), 100), Minutes: windowMinutes(b.Window, b.BucketID)}
			if !strings.HasPrefix(strings.ToLower(b.BucketID), "gemini") {
				w.Label = groupLabel(g.DisplayName)
				if w.Label == "" {
					w.Label = b.DisplayName
				}
			}
			if t, err := time.Parse(time.RFC3339Nano, b.ResetTime); err == nil {
				w.ResetsAt = t
			}
			u.Windows = append(u.Windows, w)
		}
	}
	return u, nil
}

func (p Provider) refresh(ctx context.Context, ua string, snap []byte, refreshToken string) ([]byte, error) {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {unmask(maskedClientID)},
		"client_secret": {unmask(maskedClientSecret)},
	}
	target := p.RefreshURL
	if target == "" {
		target = DefaultRefreshURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", ua)
	resp, err := swap.NoRedirect(p.client()).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error string `json:"error"`
		}
		json.Unmarshal(data, &e)
		if e.Error == "invalid_grant" {
			return nil, ErrLoginExpired
		}
		return nil, fmt.Errorf("token refresh: %w", httpError(resp.StatusCode))
	}
	var t struct {
		AccessToken  string  `json:"access_token"`
		RefreshToken string  `json:"refresh_token"`
		ExpiresIn    float64 `json:"expires_in"`
		IDToken      string  `json:"id_token"`
	}
	if err := json.Unmarshal(data, &t); err != nil || t.AccessToken == "" {
		return nil, errors.New("token refresh: response has no access_token")
	}
	return p.applyTokens(snap, t.AccessToken, t.RefreshToken, t.IDToken, t.ExpiresIn)
}

func (p Provider) applyTokens(snap []byte, access, refresh, idToken string, expiresIn float64) ([]byte, error) {
	tok, ok, err := jsonx.Get(snap, "token")
	if err != nil || !ok {
		return nil, ErrUnsupportedLogin
	}
	set := func(doc []byte, k, v string) []byte {
		if err != nil {
			return doc
		}
		b, _ := json.Marshal(v)
		doc, err = jsonx.Set(doc, k, b)
		return doc
	}
	tok = set(tok, "access_token", access)
	if refresh != "" {
		tok = set(tok, "refresh_token", refresh)
	}
	if expiresIn > 0 {
		tok = set(tok, "expiry", p.now().Add(time.Duration(expiresIn)*time.Second).Format(time.RFC3339Nano))
	}
	if err != nil {
		return nil, err
	}
	out, err := jsonx.Set(snap, "token", tok)
	if err != nil {
		return nil, err
	}
	if idToken != "" {
		out = set(out, "id_token", idToken)
	}
	return out, err
}
