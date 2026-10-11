package transfer

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/WellWells/agentswap/internal/antigravity"
	"github.com/WellWells/agentswap/internal/claude"
	"github.com/WellWells/agentswap/internal/codex"
)

// Reconstruct converts an input token raw JSON into the native snapshot format
// required by the target provider's Identify and WriteLive methods.
func Reconstruct(provider string, tokenRaw json.RawMessage) ([]byte, error) {
	if len(strings.TrimSpace(string(tokenRaw))) == 0 {
		return nil, errors.New("empty token")
	}

	switch provider {
	case "codex":
		return reconstructCodex(tokenRaw)
	case "claude":
		return reconstructClaude(tokenRaw)
	case "antigravity":
		return reconstructAntigravity(tokenRaw)
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedProvider, provider)
	}
}

func reconstructCodex(raw json.RawMessage) ([]byte, error) {
	// 1. Lenient check: if already a valid full snapshot
	if _, err := codex.Identify(raw); err == nil {
		return []byte(raw), nil
	}

	// 2. Check if it's a bare string (API Key)
	var apiKey string
	if err := json.Unmarshal(raw, &apiKey); err == nil && apiKey != "" {
		snap, err := json.Marshal(map[string]string{"OPENAI_API_KEY": apiKey})
		if err != nil {
			return nil, err
		}
		if _, err := codex.Identify(snap); err != nil {
			return nil, fmt.Errorf("invalid api key: %w", err)
		}
		return snap, nil
	}

	// 3. Check if it's a minimal tokens object or nested object
	var obj struct {
		APIKey       *string `json:"OPENAI_API_KEY"`
		IDToken      string  `json:"id_token"`
		RefreshToken string  `json:"refresh_token"`
		AccountID    string  `json:"account_id"`
		AccessToken  string  `json:"access_token"`
		Tokens       *struct {
			IDToken      string `json:"id_token"`
			RefreshToken string `json:"refresh_token"`
			AccountID    string `json:"account_id"`
			AccessToken  string `json:"access_token"`
		} `json:"tokens"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("parse codex token: %w", err)
	}

	if obj.APIKey != nil && *obj.APIKey != "" {
		snap, err := json.Marshal(map[string]string{"OPENAI_API_KEY": *obj.APIKey})
		if err != nil {
			return nil, err
		}
		if _, err := codex.Identify(snap); err != nil {
			return nil, fmt.Errorf("invalid api key in object: %w", err)
		}
		return snap, nil
	}

	idToken := obj.IDToken
	refreshToken := obj.RefreshToken
	accountID := obj.AccountID
	accessToken := obj.AccessToken

	if obj.Tokens != nil {
		if idToken == "" {
			idToken = obj.Tokens.IDToken
		}
		if refreshToken == "" {
			refreshToken = obj.Tokens.RefreshToken
		}
		if accountID == "" {
			accountID = obj.Tokens.AccountID
		}
		if accessToken == "" {
			accessToken = obj.Tokens.AccessToken
		}
	}

	if idToken != "" {
		tokMap := map[string]string{
			"id_token": idToken,
		}
		if refreshToken != "" {
			tokMap["refresh_token"] = refreshToken
		}
		if accountID != "" {
			tokMap["account_id"] = accountID
		}
		if accessToken != "" {
			tokMap["access_token"] = accessToken
		}
		snap, err := json.Marshal(map[string]any{"tokens": tokMap})
		if err != nil {
			return nil, err
		}
		if _, err := codex.Identify(snap); err != nil {
			return nil, fmt.Errorf("reconstructed codex credentials invalid: %w", err)
		}
		return snap, nil
	}

	return nil, errors.New("cannot reconstruct codex credentials from token")
}

func reconstructClaude(raw json.RawMessage) ([]byte, error) {
	// 1. Lenient check: if already a valid full snapshot
	if _, err := claude.Identify(raw); err == nil {
		return []byte(raw), nil
	}

	// 2. Parse minimal object
	var obj struct {
		RefreshToken     string  `json:"refreshToken"`
		RefreshTokenAlt  string  `json:"refresh_token"`
		AccessToken      string  `json:"accessToken"`
		AccessTokenAlt   string  `json:"access_token"`
		AccountUUID      string  `json:"accountUuid"`
		AccountUUIDAlt   string  `json:"account_uuid"`
		OrganizationUUID *string `json:"organizationUuid"`
		OrgUUIDAlt       *string `json:"organization_uuid"`
		EmailAddress     string  `json:"emailAddress"`
		EmailAlt         string  `json:"email"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("parse claude token: %w", err)
	}

	refreshToken := obj.RefreshToken
	if refreshToken == "" {
		refreshToken = obj.RefreshTokenAlt
	}
	accessToken := obj.AccessToken
	if accessToken == "" {
		accessToken = obj.AccessTokenAlt
	}
	accountUUID := obj.AccountUUID
	if accountUUID == "" {
		accountUUID = obj.AccountUUIDAlt
	}
	orgUUID := obj.OrganizationUUID
	if orgUUID == nil {
		orgUUID = obj.OrgUUIDAlt
	}
	email := obj.EmailAddress
	if email == "" {
		email = obj.EmailAlt
	}

	credsData := map[string]any{
		"claudeAiOauth": map[string]any{
			"accessToken":  accessToken,
			"refreshToken": refreshToken,
		},
	}
	credsBytes, err := json.Marshal(credsData)
	if err != nil {
		return nil, err
	}

	oauthData := map[string]any{
		"accountUuid":  accountUUID,
		"emailAddress": email,
	}
	if orgUUID != nil {
		oauthData["organizationUuid"] = *orgUUID
	}
	oauthBytes, err := json.Marshal(oauthData)
	if err != nil {
		return nil, err
	}

	snap := claude.Pack(credsBytes, oauthBytes)
	if _, err := claude.Identify(snap); err != nil {
		return nil, fmt.Errorf("reconstructed claude credentials invalid: %w", err)
	}
	return snap, nil
}

func reconstructAntigravity(raw json.RawMessage) ([]byte, error) {
	// 1. Lenient check: if already a valid full snapshot
	if _, err := antigravity.Identify(raw); err == nil {
		return []byte(raw), nil
	}

	// 2. Parse minimal object
	var obj struct {
		IDToken         string `json:"id_token"`
		IDTokenAlt      string `json:"idToken"`
		RefreshToken    string `json:"refresh_token"`
		RefreshTokenAlt string `json:"refreshToken"`
		AccessToken     string `json:"access_token"`
		AccessTokenAlt  string `json:"accessToken"`
		Token           *struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
		} `json:"token"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("parse antigravity token: %w", err)
	}

	idToken := obj.IDToken
	if idToken == "" {
		idToken = obj.IDTokenAlt
	}
	refreshToken := obj.RefreshToken
	if refreshToken == "" {
		refreshToken = obj.RefreshTokenAlt
	}
	accessToken := obj.AccessToken
	if accessToken == "" {
		accessToken = obj.AccessTokenAlt
	}

	if obj.Token != nil {
		if refreshToken == "" {
			refreshToken = obj.Token.RefreshToken
		}
		if accessToken == "" {
			accessToken = obj.Token.AccessToken
		}
	}

	tokMap := map[string]string{
		"refresh_token": refreshToken,
	}
	if accessToken != "" {
		tokMap["access_token"] = accessToken
	}

	credsData := map[string]any{
		"token":    tokMap,
		"id_token": idToken,
	}
	snap, err := json.Marshal(credsData)
	if err != nil {
		return nil, err
	}
	if _, err := antigravity.Identify(snap); err != nil {
		return nil, fmt.Errorf("reconstructed antigravity credentials invalid: %w", err)
	}
	return snap, nil
}
