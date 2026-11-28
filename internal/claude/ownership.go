package claude

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/WellWells/agentswap/internal/swap"
)

const familySkew = 60_000

func (p Provider) CheckReplacement(saved, incoming []byte, freshLogin bool) error {
	known, err := replacementKnown(saved, incoming, freshLogin)
	if err != nil || known {
		return err
	}
	return p.VerifyOwner(incoming)
}

func (p Provider) CheckRepair(saved, incoming []byte) error {
	if saved != nil {
		if _, err := replacementKnown(saved, incoming, false); err != nil {
			return err
		}
	}
	return p.VerifyOwner(incoming)
}

func replacementKnown(saved, incoming []byte, freshLogin bool) (bool, error) {
	old, err := aiOf(saved)
	if err != nil {
		return false, err
	}
	next, err := aiOf(incoming)
	if err != nil {
		return false, err
	}
	if old.RefreshTokenExpiresAt > 0 && next.RefreshTokenExpiresAt > 0 && next.RefreshTokenExpiresAt < old.RefreshTokenExpiresAt-familySkew {
		return false, swap.ErrStaleLogin
	}
	newFamily := old.RefreshTokenExpiresAt > 0 && next.RefreshTokenExpiresAt > old.RefreshTokenExpiresAt+familySkew
	if !newFamily && next.ExpiresAt > 0 && old.ExpiresAt > next.ExpiresAt {
		return false, swap.ErrStaleLogin
	}
	if freshLogin {
		return false, nil
	}
	if old.RefreshTokenExpiresAt > 0 && next.RefreshTokenExpiresAt == 0 {
		return false, swap.ErrUnverified
	}
	if old.ExpiresAt > 0 && next.ExpiresAt == 0 {
		return false, swap.ErrUnverified
	}
	if next.RefreshToken == old.RefreshToken {
		return true, nil
	}
	if !newFamily && (next.ExpiresAt <= old.ExpiresAt || old.ExpiresAt == 0) {
		return false, swap.ErrUnverified
	}
	return next.AccessToken != "" && old.AccessToken == next.AccessToken, nil
}

func (p Provider) VerifyOwner(raw []byte) error {
	id, err := p.Identify(raw)
	if err != nil {
		return err
	}
	ai, err := aiOf(raw)
	if err != nil {
		return err
	}
	return p.verifyOwner(ai, id)
}

func (p Provider) verifyOwner(ai aiOauth, id swap.Identity) error {
	if ai.AccessToken == "" || (ai.ExpiresAt > 0 && ai.ExpiresAt <= float64(p.now().UnixMilli())) {
		return swap.ErrUnverified
	}
	base := strings.TrimRight(p.BaseURL, "/")
	if base == "" {
		base = DefaultBaseURL
	}
	if base != DefaultBaseURL {
		u, err := url.Parse(base)
		if err != nil || !swap.SecureURL(base) || u.User != nil || (u.Hostname() != "localhost" && !net.ParseIP(u.Hostname()).IsLoopback()) {
			return swap.ErrUnverified
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/oauth/profile", nil)
	if err != nil {
		return swap.ErrUnverified
	}
	req.Header.Set("Accept", axiosAccept)
	req.Header.Set("Authorization", "Bearer "+ai.AccessToken)
	req.Header.Set("anthropic-beta", betaHeader)
	if p.UserAgent != nil {
		req.Header.Set("User-Agent", p.UserAgent())
	}
	resp, err := swap.NoRedirect(p.client()).Do(req)
	if err != nil {
		return swap.ErrUnverified
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return swap.ErrUnverified
	}
	var profile struct {
		Account      struct{ UUID string } `json:"account"`
		Organization struct{ UUID string } `json:"organization"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&profile); err != nil || profile.Account.UUID == "" {
		return swap.ErrUnverified
	}
	if !strings.HasPrefix(id.Key, "claude:"+profile.Account.UUID+":") {
		return swap.ErrMismatch
	}
	if profile.Organization.UUID == "" && !strings.HasSuffix(id.Key, ":") {
		return swap.ErrUnverified
	}
	if "claude:"+profile.Account.UUID+":"+profile.Organization.UUID != id.Key {
		return swap.ErrMismatch
	}
	return nil
}
