package claude

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"github.com/WellWells/agentswap/internal/execx"
	"github.com/WellWells/agentswap/internal/jsonx"
)

type CswapAccount struct {
	Slot     int
	Email    string
	Alias    string
	Snapshot []byte
	Err      error
}

func CswapDirs(home, goos string, getenv func(string) string) []string {
	legacy := filepath.Join(home, ".claude-swap-backup")
	if goos != "linux" {
		return []string{legacy}
	}
	data := getenv("XDG_DATA_HOME")
	if data == "" {
		data = filepath.Join(home, ".local", "share")
	}
	return []string{filepath.Join(data, "claude-swap"), legacy}
}

func CswapKeychain(run execx.Runner) func(int, string) ([]byte, error) {
	return func(slot int, email string) ([]byte, error) {
		return Keychain{Service: "claude-swap", Account: fmt.Sprintf("account-%d-%s", slot, email), Run: run}.Read()
	}
}

func ReadCswap(dir string, keychain func(slot int, email string) ([]byte, error)) ([]CswapAccount, error) {
	b, err := os.ReadFile(filepath.Join(dir, "sequence.json"))
	if err != nil {
		return nil, err
	}
	var seq struct {
		Sequence []int `json:"sequence"`
		Accounts map[string]struct {
			Email string `json:"email"`
			Alias string `json:"alias"`
			Kind  string `json:"kind"`
		} `json:"accounts"`
	}
	if err := json.Unmarshal(b, &seq); err != nil {
		return nil, fmt.Errorf("sequence.json: %w", err)
	}
	var out []CswapAccount
	for _, n := range seq.Sequence {
		a, ok := seq.Accounts[strconv.Itoa(n)]
		if !ok {
			continue
		}
		ca := CswapAccount{Slot: n, Email: a.Email, Alias: a.Alias}
		ca.Snapshot, ca.Err = cswapSnapshot(dir, n, a.Email, a.Kind, keychain)
		out = append(out, ca)
	}
	return out, nil
}

func cswapSnapshot(dir string, n int, email, kind string, keychain func(int, string) ([]byte, error)) ([]byte, error) {
	if kind == "api_key" {
		return nil, ErrUnsupportedLogin
	}
	if email == "" || email == "." || email == ".." || strings.ContainsAny(email, `/\:`) || strings.IndexFunc(email, unicode.IsControl) >= 0 {
		return nil, fmt.Errorf("sequence.json: invalid email %q", email)
	}
	name := fmt.Sprintf(".creds-%d-%s.enc", n, email)
	enc, err := os.ReadFile(filepath.Join(dir, "credentials", name))
	var raw []byte
	switch {
	case err == nil:
		if raw, err = base64.StdEncoding.DecodeString(strings.TrimSpace(string(enc))); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
	case errors.Is(err, fs.ErrNotExist) && keychain != nil:
		if raw, err = keychain(n, email); err != nil {
			return nil, err
		}
	default:
		return nil, err
	}
	if !bytes.HasPrefix(bytes.TrimSpace(raw), []byte("{")) {
		return nil, ErrUnsupportedLogin
	}
	cfgName := fmt.Sprintf(".claude-config-%d-%s.json", n, email)
	cfg, err := os.ReadFile(filepath.Join(dir, "configs", cfgName))
	if err != nil {
		return nil, err
	}
	oauth, ok, err := jsonx.Get(cfg, "oauthAccount")
	if err != nil {
		return nil, fmt.Errorf("%s: %w", cfgName, err)
	}
	if !ok {
		return nil, fmt.Errorf("%s: no oauthAccount", cfgName)
	}
	part, err := accountPart(raw)
	if err != nil {
		return nil, err
	}
	return Pack(part, oauth), nil
}
