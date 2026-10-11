package transfer

import (
	"encoding/json"
	"errors"
	"fmt"
)

// ErrUnsupportedProvider indicates the provider name is not recognized.
var ErrUnsupportedProvider = errors.New("unsupported provider")

// Extract extracts minimal recovery token from a provider's local snapshot bytes.
func Extract(provider string, snapshot []byte) (json.RawMessage, error) {
	switch provider {
	case "codex":
		return extractCodex(snapshot)
	case "claude":
		return extractClaude(snapshot)
	case "antigravity":
		return extractAntigravity(snapshot)
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedProvider, provider)
	}
}

func extractCodex(b []byte) (json.RawMessage, error) {
	var a struct {
		AuthMode    string  `json:"auth_mode"`
		APIKey      *string `json:"OPENAI_API_KEY"`
		Tokens      *struct {
			IDToken      string `json:"id_token"`
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
			AccountID    string `json:"account_id"`
			LastRefresh  string `json:"last_refresh"`
		} `json:"tokens"`
		LastRefresh string `json:"last_refresh"`
	}
	if err := json.Unmarshal(b, &a); err != nil {
		return nil, fmt.Errorf("codex snapshot: %w", err)
	}
	if a.APIKey != nil && *a.APIKey != "" {
		res, err := json.Marshal(*a.APIKey)
		if err != nil {
			return nil, err
		}
		return json.RawMessage(res), nil
	}
	if a.Tokens != nil && a.Tokens.IDToken != "" {
		lastRefresh := a.LastRefresh
		if lastRefresh == "" && a.Tokens.LastRefresh != "" {
			lastRefresh = a.Tokens.LastRefresh
		}
		authMode := a.AuthMode
		if authMode == "" {
			authMode = "chatgpt"
		}
		minTokens := struct {
			AuthMode     string `json:"auth_mode,omitempty"`
			IDToken      string `json:"id_token"`
			AccessToken  string `json:"access_token,omitempty"`
			RefreshToken string `json:"refresh_token,omitempty"`
			AccountID    string `json:"account_id,omitempty"`
			LastRefresh  string `json:"last_refresh,omitempty"`
		}{
			AuthMode:     authMode,
			IDToken:      a.Tokens.IDToken,
			AccessToken:  a.Tokens.AccessToken,
			RefreshToken: a.Tokens.RefreshToken,
			AccountID:    a.Tokens.AccountID,
			LastRefresh:  lastRefresh,
		}
		res, err := json.Marshal(minTokens)
		if err != nil {
			return nil, err
		}
		return json.RawMessage(res), nil
	}
	return nil, errors.New("codex snapshot: neither OPENAI_API_KEY nor tokens.id_token found")
}

func extractClaude(b []byte) (json.RawMessage, error) {
	var snap struct {
		Credentials json.RawMessage `json:"credentials"`
		OAuth       json.RawMessage `json:"oauthAccount"`
	}
	if err := json.Unmarshal(b, &snap); err != nil {
		return nil, fmt.Errorf("claude snapshot: %w", err)
	}
	if len(snap.Credentials) == 0 || len(snap.OAuth) == 0 {
		return nil, errors.New("claude snapshot: missing credentials or oauthAccount")
	}

	var creds struct {
		ClaudeAiOauth *struct {
			AccessToken      string  `json:"accessToken"`
			RefreshToken     string  `json:"refreshToken"`
			ExpiresAt        float64 `json:"expiresAt"`
			SubscriptionType string  `json:"subscriptionType"`
		} `json:"claudeAiOauth"`
	}
	if err := json.Unmarshal(snap.Credentials, &creds); err != nil {
		return nil, fmt.Errorf("claude credentials: %w", err)
	}
	if creds.ClaudeAiOauth == nil {
		return nil, errors.New("claude credentials: missing claudeAiOauth")
	}

	var acct struct {
		AccountUUID      string  `json:"accountUuid"`
		OrganizationUUID *string `json:"organizationUuid"`
		EmailAddress     string  `json:"emailAddress"`
	}
	if err := json.Unmarshal(snap.OAuth, &acct); err != nil {
		return nil, fmt.Errorf("claude oauthAccount: %w", err)
	}

	minClaude := struct {
		RefreshToken     string  `json:"refreshToken"`
		AccessToken      string  `json:"accessToken,omitempty"`
		ExpiresAt        float64 `json:"expiresAt,omitempty"`
		SubscriptionType string  `json:"subscriptionType,omitempty"`
		AccountUUID      string  `json:"accountUuid"`
		OrganizationUUID *string `json:"organizationUuid,omitempty"`
		EmailAddress     string  `json:"emailAddress,omitempty"`
	}{
		RefreshToken:     creds.ClaudeAiOauth.RefreshToken,
		AccessToken:      creds.ClaudeAiOauth.AccessToken,
		ExpiresAt:        creds.ClaudeAiOauth.ExpiresAt,
		SubscriptionType: creds.ClaudeAiOauth.SubscriptionType,
		AccountUUID:      acct.AccountUUID,
		OrganizationUUID: acct.OrganizationUUID,
		EmailAddress:     acct.EmailAddress,
	}

	res, err := json.Marshal(minClaude)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(res), nil
}

func extractAntigravity(b []byte) (json.RawMessage, error) {
	var snap struct {
		Token struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
		} `json:"token"`
		IDToken    string `json:"id_token"`
		AuthMethod string `json:"auth_method"`
	}
	if err := json.Unmarshal(b, &snap); err != nil {
		return nil, fmt.Errorf("antigravity snapshot: %w", err)
	}
	authMethod := snap.AuthMethod
	if authMethod == "" {
		authMethod = "consumer"
	}
	minAgy := struct {
		IDToken      string `json:"id_token"`
		RefreshToken string `json:"refresh_token"`
		AccessToken  string `json:"access_token,omitempty"`
		AuthMethod   string `json:"auth_method,omitempty"`
	}{
		IDToken:      snap.IDToken,
		RefreshToken: snap.Token.RefreshToken,
		AccessToken:  snap.Token.AccessToken,
		AuthMethod:   authMethod,
	}
	res, err := json.Marshal(minAgy)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(res), nil
}
