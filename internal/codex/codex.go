package codex

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/WellWells/agentswap/internal/fsx"
	"github.com/WellWells/agentswap/internal/swap"
)

var (
	ErrNoCredentials = errors.New("auth.json has no ChatGPT tokens or API key")
	ErrKeyringStore  = errors.New("codex stores credentials outside auth.json (cli_auth_credentials_store); only \"file\" is supported")
)

type Provider struct {
	Home       string
	BaseURL    string
	RefreshURL string
	UserAgent  func() string
	Client     *http.Client
	Now        func() time.Time
}

const Originator = "codex_cli_rs"

func (p Provider) Name() string { return "codex" }

func (p Provider) identify(req *http.Request) {
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Originator", Originator)
	if p.UserAgent != nil {
		req.Header.Set("User-Agent", p.UserAgent())
	}
}

func (p Provider) AuthPath() string { return filepath.Join(p.Home, "auth.json") }

func (p Provider) check() error {
	b, err := os.ReadFile(filepath.Join(p.Home, "config.toml"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	m, err := storeMode(b)
	if err != nil {
		return err
	}
	if m != "file" {
		return fmt.Errorf("%w (current: %s)", ErrKeyringStore, m)
	}
	return nil
}

func (p Provider) ReadLive() ([]byte, error) {
	if err := p.check(); err != nil {
		return nil, err
	}
	return os.ReadFile(p.AuthPath())
}

func (p Provider) WriteLive(b []byte) error {
	if err := p.check(); err != nil {
		return err
	}
	return fsx.WriteAtomic(p.AuthPath(), b, 0o600)
}

func (p Provider) Identify(b []byte) (swap.Identity, error) { return Identify(b) }

func Identify(b []byte) (swap.Identity, error) {
	var a struct {
		APIKey *string `json:"OPENAI_API_KEY"`
		Tokens *struct {
			IDToken   string `json:"id_token"`
			AccountID string `json:"account_id"`
		} `json:"tokens"`
	}
	if err := json.Unmarshal(b, &a); err != nil {
		return swap.Identity{}, fmt.Errorf("auth.json: %w", err)
	}
	if a.Tokens != nil && a.Tokens.IDToken != "" {
		var c struct {
			Email string `json:"email"`
			Sub   string `json:"sub"`
			Auth  struct {
				AccountID string `json:"chatgpt_account_id"`
				UserID    string `json:"chatgpt_user_id"`
				Plan      string `json:"chatgpt_plan_type"`
			} `json:"https://api.openai.com/auth"`
		}
		if err := jwtClaims(a.Tokens.IDToken, &c); err != nil {
			return swap.Identity{}, fmt.Errorf("id_token: %w", err)
		}
		user := firstNonEmpty(c.Auth.UserID, c.Sub)
		account := firstNonEmpty(c.Auth.AccountID, a.Tokens.AccountID)
		if user == "" || account == "" {
			return swap.Identity{}, errors.New("id_token: missing user or account id")
		}
		return swap.Identity{Key: "chatgpt:" + user + ":" + account, Email: c.Email, Plan: c.Auth.Plan, Mode: "chatgpt"}, nil
	}
	if a.APIKey != nil && *a.APIKey != "" {
		k := *a.APIKey
		sum := sha256.Sum256([]byte(k))
		label := "sk-..."
		if len(k) >= 8 {
			label += k[len(k)-4:]
		}
		return swap.Identity{Key: "apikey:" + hex.EncodeToString(sum[:8]), Email: label, Mode: "apikey"}, nil
	}
	return swap.Identity{}, ErrNoCredentials
}

func jwtClaims(tok string, v any) error {
	parts := strings.Split(tok, ".")
	if len(parts) < 2 {
		return errors.New("not a JWT")
	}
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}

func storeMode(config []byte) (string, error) {
	v, err := topLevel(config, "cli_auth_credentials_store")
	if err != nil || v == "" {
		return "file", err
	}
	return strings.ToLower(v), nil
}
