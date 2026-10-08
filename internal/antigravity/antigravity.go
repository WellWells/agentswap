package antigravity

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/WellWells/agentswap/internal/execx"
	"github.com/WellWells/agentswap/internal/swap"
)

var (
	ErrRunning          = errors.New("agy is running; close every agy process before switching accounts")
	ErrUnsupportedLogin = errors.New("this agy login has no refresh token or Google account id; sign in to agy again")
)

const (
	service = "gemini"
	user    = "antigravity"
)

type Keyring interface {
	Read() ([]byte, error)
	Write([]byte) error
}

type Provider struct {
	Keyring    Keyring
	Running    func() (bool, error)
	BaseURL    string
	RefreshURL string
	UserAgent  func(authMethod string) string
	Client     *http.Client
	Now        func() time.Time
}

func New(getenv func(string) string, goos string, run execx.Runner) Provider {
	p := Provider{BaseURL: getenv("AGENTSWAP_ANTIGRAVITY_API_URL"), RefreshURL: getenv("AGENTSWAP_ANTIGRAVITY_TOKEN_URL")}
	switch goos {
	case "windows":
		p.Keyring = winCred{target: service + ":" + user}
		p.Running = func() (bool, error) { return processRunning("agy.exe") }
	case "darwin":
		p.Keyring, p.Running = keychain{run: run}, pgrep(run)
	default:
		p.Keyring, p.Running = secretTool{run: run}, pgrep(run)
	}
	return p
}

func (p Provider) Name() string { return "antigravity" }

func (p Provider) ReadLive() ([]byte, error) { return p.Keyring.Read() }

func (p Provider) WriteLive(b []byte) error {
	if p.Running != nil {
		running, err := p.Running()
		if err != nil {
			return err
		}
		if running {
			return ErrRunning
		}
	}
	return p.Keyring.Write(b)
}

type token struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	Expiry       string `json:"expiry"`
}

type creds struct {
	Token      token  `json:"token"`
	IDToken    string `json:"id_token"`
	AuthMethod string `json:"auth_method"`
}

func parse(b []byte) (creds, error) {
	var c creds
	if err := json.Unmarshal(b, &c); err != nil {
		return c, fmt.Errorf("antigravity credentials: %w", err)
	}
	return c, nil
}

func claims(idToken string) (sub, email string) {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return "", ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return "", ""
	}
	var c struct {
		Sub   string `json:"sub"`
		Email string `json:"email"`
	}
	if json.Unmarshal(payload, &c) != nil {
		return "", ""
	}
	return c.Sub, c.Email
}

func Identify(b []byte) (swap.Identity, error) {
	c, err := parse(b)
	if err != nil {
		return swap.Identity{}, err
	}
	sub, email := claims(c.IDToken)
	if sub == "" || c.Token.RefreshToken == "" {
		return swap.Identity{}, ErrUnsupportedLogin
	}
	return swap.Identity{Key: "google:" + sub, Email: email, Mode: "oauth"}, nil
}

func (p Provider) Identify(b []byte) (swap.Identity, error) { return Identify(b) }

func (p Provider) SameLogin(a, b []byte) bool {
	x, errA := parse(a)
	y, errB := parse(b)
	if errA != nil || errB != nil {
		return false
	}
	return (x.Token.RefreshToken != "" && x.Token.RefreshToken == y.Token.RefreshToken) || (x.Token.AccessToken != "" && x.Token.AccessToken == y.Token.AccessToken)
}
