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
		APIKey *string `json:"OPENAI_API_KEY"`
		Tokens *struct {
			IDToken      string `json:"id_token"`
			RefreshToken string `json:"refresh_token"`
			AccountID    string `json:"account_id"`
		} `json:"tokens"`
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
		minTokens := struct {
			IDToken      string `json:"id_token"`
			RefreshToken string `json:"refresh_token,omitempty"`
			AccountID    string `json:"account_id,omitempty"`
		}{
			IDToken:      a.Tokens.IDToken,
			RefreshToken: a.Tokens.RefreshToken,
			AccountID:    a.Tokens.AccountID,
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
			AccessToken  string `json:"accessToken"`
			RefreshToken string `json:"refreshToken"`
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
		AccountUUID      string  `json:"accountUuid"`
		OrganizationUUID *string `json:"organizationUuid,omitempty"`
		EmailAddress     string  `json:"emailAddress,omitempty"`
	}{
		RefreshToken:     creds.ClaudeAiOauth.RefreshToken,
		AccessToken:      creds.ClaudeAiOauth.AccessToken,
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
		IDToken string `json:"id_token"`
	}
	if err := json.Unmarshal(b, &snap); err != nil {
		return nil, fmt.Errorf("antigravity snapshot: %w", err)
	}
	minAgy := struct {
		IDToken      string `json:"id_token"`
		RefreshToken string `json:"refresh_token"`
		AccessToken  string `json:"access_token,omitempty"`
	}{
		IDToken:      snap.IDToken,
		RefreshToken: snap.Token.RefreshToken,
		AccessToken:  snap.Token.AccessToken,
	}
	res, err := json.Marshal(minAgy)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(res), nil
}
