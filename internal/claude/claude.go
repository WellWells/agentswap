package claude

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"time"

	"github.com/WellWells/agentswap/internal/execx"
	"github.com/WellWells/agentswap/internal/fsx"
	"github.com/WellWells/agentswap/internal/jsonx"
	"github.com/WellWells/agentswap/internal/swap"
)

var (
	ErrUnsupportedLogin = errors.New("only Claude subscription (OAuth) logins are supported; setup-token and API key logins are not supported yet")
	ErrWiped            = errors.New("Claude Code cleared this login after its token was rejected; log in again")
)

var shared = map[string]bool{"mcpOAuth": true, "mcpOAuthClientConfig": true, "mcpXaaIdp": true, "mcpXaaIdpConfig": true, "pluginSecrets": true}

type Provider struct {
	ConfigDir    string
	GlobalConfig string
	Keychain     *Keychain
	BaseURL      string
	RefreshURL   string
	UserAgent    func() string
	Client       *http.Client
	Now          func() time.Time
	LockWait     time.Duration
}

func New(getenv func(string) string, home, goos string, run execx.Runner) Provider {
	p := Provider{
		ConfigDir:    filepath.Join(home, ".claude"),
		GlobalConfig: filepath.Join(home, ".claude.json"),
	}
	if dir := getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		p.ConfigDir, p.GlobalConfig = dir, filepath.Join(dir, ".claude.json")
	}
	if _, err := os.Stat(filepath.Join(p.ConfigDir, ".config.json")); err == nil {
		p.GlobalConfig = filepath.Join(p.ConfigDir, ".config.json")
	}
	if goos == "darwin" {
		p.Keychain = &Keychain{Service: keychainService(getenv), Account: keychainAccount(getenv), Run: run}
	}
	return p
}

func keychainService(getenv func(string) string) string {
	dir := getenv("CLAUDE_SECURESTORAGE_CONFIG_DIR")
	if dir == "" {
		dir = getenv("CLAUDE_CONFIG_DIR")
	}
	if dir == "" {
		return "Claude Code-credentials"
	}
	sum := sha256.Sum256([]byte(dir))
	return "Claude Code-credentials-" + hex.EncodeToString(sum[:])[:8]
}

func keychainAccount(getenv func(string) string) string {
	if u := getenv("USER"); u != "" {
		return u
	}
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return "claude-code-user"
}

func (p Provider) Name() string { return "claude" }

func (p Provider) credPath() string { return filepath.Join(p.ConfigDir, ".credentials.json") }

func (p Provider) readCreds() ([]byte, error) {
	if p.Keychain != nil {
		return p.Keychain.Read()
	}
	return os.ReadFile(p.credPath())
}

func (p Provider) writeCreds(b []byte) error {
	if p.Keychain == nil {
		return fsx.WriteAtomic(p.credPath(), b, 0o600)
	}
	if err := p.Keychain.Write(b); err != nil {
		return err
	}
	if _, err := os.Stat(p.credPath()); err == nil {
		now := time.Now()
		os.Chtimes(p.credPath(), now, now)
	}
	return nil
}

func Pack(credentials, oauthAccount []byte) []byte {
	var b bytes.Buffer
	b.WriteString(`{"credentials":`)
	b.Write(credentials)
	b.WriteString(`,"oauthAccount":`)
	b.Write(oauthAccount)
	b.WriteString("}")
	return b.Bytes()
}

type envelope struct {
	Credentials  json.RawMessage `json:"credentials"`
	OAuthAccount json.RawMessage `json:"oauthAccount"`
}

func unpack(b []byte) (envelope, error) {
	var e envelope
	if err := json.Unmarshal(b, &e); err != nil {
		return e, fmt.Errorf("claude snapshot: %w", err)
	}
	if len(e.Credentials) == 0 || len(e.OAuthAccount) == 0 {
		return e, errors.New("claude snapshot: missing credentials or oauthAccount")
	}
	return e, nil
}

func accountPart(creds []byte) ([]byte, error) {
	ms, err := jsonx.Members(creds)
	if err != nil {
		return nil, fmt.Errorf(".credentials.json: %w", err)
	}
	var keep []jsonx.Member
	for _, m := range ms {
		if !shared[m.Key] {
			keep = append(keep, m)
		}
	}
	return jsonx.Encode(keep, false), nil
}

type aiOauth struct {
	AccessToken      string   `json:"accessToken"`
	RefreshToken     string   `json:"refreshToken"`
	ExpiresAt        float64  `json:"expiresAt"`
	SubscriptionType string   `json:"subscriptionType"`
	Scopes           []string `json:"scopes"`
}

func aiOf(b []byte) (aiOauth, error) {
	e, err := unpack(b)
	if err != nil {
		return aiOauth{}, err
	}
	var c struct {
		AI *aiOauth `json:"claudeAiOauth"`
	}
	if err := json.Unmarshal(e.Credentials, &c); err != nil {
		return aiOauth{}, err
	}
	if c.AI == nil {
		return aiOauth{}, ErrUnsupportedLogin
	}
	return *c.AI, nil
}

func Identify(b []byte) (swap.Identity, error) {
	e, err := unpack(b)
	if err != nil {
		return swap.Identity{}, err
	}
	var acct struct {
		AccountUUID string  `json:"accountUuid"`
		OrgUUID     *string `json:"organizationUuid"`
		Email       string  `json:"emailAddress"`
	}
	if err := json.Unmarshal(e.OAuthAccount, &acct); err != nil {
		return swap.Identity{}, fmt.Errorf("oauthAccount: %w", err)
	}
	ai, err := aiOf(b)
	if err != nil {
		return swap.Identity{}, err
	}
	if acct.AccountUUID == "" {
		return swap.Identity{}, ErrUnsupportedLogin
	}
	if ai.AccessToken == "" && ai.RefreshToken == "" {
		return swap.Identity{}, ErrWiped
	}
	if ai.RefreshToken == "" {
		return swap.Identity{}, ErrUnsupportedLogin
	}
	org := ""
	if acct.OrgUUID != nil {
		org = *acct.OrgUUID
	}
	return swap.Identity{Key: "claude:" + acct.AccountUUID + ":" + org, Email: acct.Email, Plan: ai.SubscriptionType, Mode: "oauth"}, nil
}

func (p Provider) Identify(b []byte) (swap.Identity, error) { return Identify(b) }

func (p Provider) SameLogin(a, b []byte) bool {
	x, errA := aiOf(a)
	y, errB := aiOf(b)
	if errA != nil || errB != nil {
		return false
	}
	return (x.RefreshToken != "" && x.RefreshToken == y.RefreshToken) || (x.AccessToken != "" && x.AccessToken == y.AccessToken)
}

func (p Provider) ReadLive() ([]byte, error) {
	creds, err := p.readCreds()
	if err != nil {
		return nil, err
	}
	global, err := os.ReadFile(p.GlobalConfig)
	if err != nil {
		return nil, err
	}
	oauth, ok, err := jsonx.Get(global, "oauthAccount")
	if err != nil {
		return nil, fmt.Errorf("%s: %w", p.GlobalConfig, err)
	}
	if !ok || string(oauth) == "null" {
		return nil, fs.ErrNotExist
	}
	part, err := accountPart(creds)
	if err != nil {
		return nil, err
	}
	return Pack(part, oauth), nil
}

func (p Provider) WriteLive(b []byte) error {
	e, err := unpack(b)
	if err != nil {
		return err
	}
	acct, err := jsonx.Members(e.Credentials)
	if err != nil {
		return err
	}
	var keep []jsonx.Member
	pretty := false
	live, err := p.readCreds()
	switch {
	case err == nil:
		ms, err := jsonx.Members(live)
		if err != nil {
			return fmt.Errorf(".credentials.json: %w", err)
		}
		for _, m := range ms {
			if shared[m.Key] {
				keep = append(keep, m)
			}
		}
		pretty = bytes.Contains(live, []byte("\n"))
	case !errors.Is(err, fs.ErrNotExist):
		return err
	}
	if err := p.writeCreds(jsonx.Encode(append(keep, acct...), pretty)); err != nil {
		return err
	}
	if err := p.writeConfig(e.OAuthAccount); err != nil {
		if live != nil {
			p.writeCreds(live)
		}
		return err
	}
	return nil
}

func (p Provider) writeConfig(oauthAccount []byte) error {
	perm := os.FileMode(0o600)
	global, err := os.ReadFile(p.GlobalConfig)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		global = []byte("{}")
	case err != nil:
		return err
	default:
		if st, err := os.Stat(p.GlobalConfig); err == nil {
			perm = st.Mode().Perm()
		}
	}
	out, err := jsonx.Set(global, "oauthAccount", oauthAccount)
	if err != nil {
		return fmt.Errorf("%s: %w", p.GlobalConfig, err)
	}
	return fsx.WriteAtomic(p.GlobalConfig, out, perm)
}
