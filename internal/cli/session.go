package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/WellWells/agentswap/internal/claude"
	"github.com/WellWells/agentswap/internal/fsx"
	"github.com/WellWells/agentswap/internal/procs"
	"github.com/WellWells/agentswap/internal/store"
	"github.com/WellWells/agentswap/internal/swap"
)

type sessionExit struct {
	error
	code int
}

func sessionCommand(e Env, prog string, p provider, m *swap.Manager, cmd string, args []string) error {
	if p.name != "claude" {
		return usageError(e.Lang.T("sessionClaudeOnly", cmd))
	}
	if cmd != "run" {
		return sessionMapCommand(e, prog, m, cmd, args)
	}
	var query string
	if len(args) > 0 && args[0] != "--" {
		query, args = args[0], args[1:]
		if query == "" || (query != "-" && strings.HasPrefix(query, "-")) {
			return usageError(e.Lang.T("sessionRunUsage", prog))
		}
	}
	if len(args) > 0 {
		if args[0] != "--" {
			return usageError(e.Lang.T("sessionRunUsage", prog))
		}
		args = args[1:]
	}
	if e.Exec == nil {
		return errors.New(e.Lang.T("noExec"))
	}
	account, err := sessionAccount(e, m, query)
	if err != nil {
		return err
	}
	var env []string
	if account != nil {
		release, err := procs.RegisterSession(m.S.Dir)
		if err != nil {
			return err
		}
		defer release()
		env, err = prepareSession(e, m, *account, args)
		if err != nil {
			return err
		}
	}
	err = e.Exec(env, "claude", args...)
	var child interface{ ExitCode() int }
	if errors.As(err, &child) && child.ExitCode() > 0 {
		return sessionExit{err, child.ExitCode()}
	}
	return err
}

func sessionAccount(e Env, m *swap.Manager, query string) (*store.Account, error) {
	var key string
	if query == "" {
		mappings, err := readSessionMappings(m.S.Dir)
		if err != nil || len(mappings) == 0 {
			return nil, err
		}
		cwd, err := sessionDirectory(".", true)
		if err != nil {
			return nil, err
		}
		for {
			if mapped, ok := mappings.exact(cwd); ok {
				key = mappings[mapped]
				break
			}
			parent := filepath.Dir(cwd)
			if parent == cwd {
				return nil, nil
			}
			cwd = parent
		}
	}
	r, err := m.S.Load()
	if err != nil {
		return nil, err
	}
	if query != "" {
		i, err := r.Find(query)
		if err != nil {
			return nil, withQuery(query, err)
		}
		return &r.Accounts[i], nil
	}
	i := r.Index(key)
	if i < 0 {
		return nil, errors.New(e.Lang.T("sessionStaleMap"))
	}
	return &r.Accounts[i], nil
}

const (
	configEnv       = "CLAUDE_CONFIG_DIR"
	secureEnv       = "CLAUDE_SECURESTORAGE_CONFIG_DIR"
	sessionDirEnv   = "AGENTSWAP_CLAUDE_SESSION_DIR"
	parentConfigEnv = "AGENTSWAP_CLAUDE_PARENT_CONFIG_DIR"
	parentSecureEnv = "AGENTSWAP_CLAUDE_PARENT_SECURE_DIR"
)

func sessionManagementEnv(e Env) Env {
	dir := e.Getenv(sessionDirEnv)
	if dir == "" || dir != e.Getenv(configEnv) || dir != e.Getenv(secureEnv) {
		return e
	}
	root, err := filepath.Abs(filepath.Join(e.dir("AGENTSWAP_HOME", ".agentswap"), "claude", "sessions"))
	hash, decodeErr := hex.DecodeString(filepath.Base(dir))
	if err != nil || decodeErr != nil || len(hash) != sha256.Size || filepath.Dir(dir) != root {
		return e
	}
	getenv := e.Getenv
	e.Getenv = func(key string) string {
		switch key {
		case configEnv:
			return getenv(parentConfigEnv)
		case secureEnv:
			return getenv(parentSecureEnv)
		}
		return getenv(key)
	}
	return e
}

const sessionAuthOverrides = `
ANTHROPIC_API_KEY ANTHROPIC_AUTH_TOKEN ANTHROPIC_BASE_URL ANTHROPIC_CUSTOM_HEADERS
ANTHROPIC_PROFILE ANTHROPIC_IDENTITY_TOKEN_FILE ANTHROPIC_FEDERATION_RULE_ID
CLAUDE_API_KEY CLAUDE_CODE_API_BASE_URL CLAUDE_CODE_CUSTOM_OAUTH_URL CLAUDE_CODE_OAUTH_CLIENT_ID
CLAUDE_CODE_API_KEY_FILE_DESCRIPTOR CLAUDE_CODE_OAUTH_TOKEN CLAUDE_CODE_OAUTH_REFRESH_TOKEN
CLAUDE_CODE_OAUTH_TOKEN_FILE_DESCRIPTOR CLAUDE_CODE_GATEWAY_TOKEN_FILE_DESCRIPTOR
CLAUDE_CODE_SESSION_ACCESS_TOKEN CLAUDE_CODE_SESSIONS_OAUTH_TOKEN CLAUDE_SESSION_INGRESS_TOKEN_FILE
CLAUDE_CODE_USE_BEDROCK CLAUDE_CODE_USE_VERTEX CLAUDE_CODE_USE_FOUNDRY CLAUDE_CODE_USE_ANTHROPIC_AWS
CLAUDE_CODE_USE_GOOGLE_CLOUD CLAUDE_CODE_SIMPLE CLAUDE_LOCAL_OAUTH_API_BASE
`

func prepareSession(e Env, m *swap.Manager, account store.Account, args []string) ([]string, error) {
	if e.Output == nil {
		return nil, errors.New(e.Lang.T("noExec"))
	}
	for _, name := range strings.Fields(sessionAuthOverrides) {
		if e.Getenv(name) != "" {
			return nil, errors.New(e.Lang.T("sessionAuthOverride", name))
		}
	}
	sum := sha256.Sum256([]byte(account.Key))
	dir, err := filepath.Abs(filepath.Join(m.S.Dir, "sessions", hex.EncodeToString(sum[:])))
	if err != nil {
		return nil, err
	}
	settings, err := sessionSettings(e, dir, args)
	if err != nil {
		return nil, err
	}
	parent := sessionManagementEnv(e)
	childEnv := []string{configEnv + "=" + dir, secureEnv + "=" + dir, sessionDirEnv + "=" + dir,
		"AGENTSWAP_HOME=" + filepath.Dir(filepath.Dir(filepath.Dir(dir))),
		"ANTHROPIC_CONFIG_DIR=" + filepath.Join(dir, "anthropic")}
	for _, pair := range [][2]string{{configEnv, parentConfigEnv}, {secureEnv, parentSecureEnv}} {
		value := parent.Getenv(pair[0])
		if value != "" {
			value, err = filepath.Abs(value)
			if err != nil {
				return nil, err
			}
			if sameDirectory(value, dir) {
				return nil, errors.New(e.Lang.T("sessionOverlapsDefault"))
			}
		}
		childEnv = append(childEnv, pair[1]+"="+value)
	}
	if st, err := os.Lstat(dir); err == nil && !st.IsDir() {
		return nil, errors.New(e.Lang.T("sessionOverlapsDefault"))
	}
	lock, err := fsx.Acquire(filepath.Join(dir, ".login.lock"), 15*time.Second)
	if err != nil {
		return nil, err
	}
	defer lock.Release()
	readAccount := func() (claude.OAuthAccount, error) {
		cfg := claude.New(func(k string) string {
			if k == configEnv || k == secureEnv {
				return dir
			}
			return e.Getenv(k)
		}, e.Home, e.goos(), e.Run)
		return cfg.ReadAccount()
	}
	id, err := readAccount()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	status := func() (bool, error) {
		result, runErr := e.Output(childEnv, "claude", append(settings, "auth", "status", "--json")...)
		var s struct {
			LoggedIn    *bool  `json:"loggedIn"`
			AuthMethod  string `json:"authMethod"`
			APIProvider string `json:"apiProvider"`
			OrgID       string `json:"orgId"`
			AccountUUID string `json:"accountUuid"`
			AccountID   string `json:"accountId"`
			Email       string `json:"email"`
			ConfigDir   string `json:"configDirectory"`
		}
		if json.Unmarshal(result, &s) != nil || s.LoggedIn == nil {
			if runErr != nil {
				return false, fmt.Errorf("claude auth status: %w", runErr)
			}
			return false, errors.New(e.Lang.T("sessionAuthStatus"))
		}
		if !*s.LoggedIn && s.AuthMethod == "none" {
			return false, nil
		}
		if runErr != nil || !*s.LoggedIn || s.AuthMethod != "claude.ai" || s.APIProvider != "firstParty" ||
			s.OrgID != id.Organization() ||
			(s.ConfigDir != "" && !sameDirectory(s.ConfigDir, dir)) {
			return false, errors.New(e.Lang.T("sessionAuthStatus"))
		}
		if s.AccountUUID != "" || s.AccountID != "" {
			if s.AccountUUID != "" && s.AccountUUID != id.AccountUUID || s.AccountID != "" && s.AccountID != id.AccountUUID {
				return false, errors.New(e.Lang.T("sessionWrongAccount", label(account)))
			}
		} else {
			verifiedEmail := false
			for _, email := range []string{account.Email, id.Email} {
				if email == "" || strings.EqualFold(email, id.AccountUUID) {
					continue
				}
				if s.Email != "" && !strings.EqualFold(s.Email, email) {
					return false, errors.New(e.Lang.T("sessionWrongAccount", label(account)))
				}
				verifiedEmail = s.Email != ""
			}
			if !verifiedEmail {
				return false, errors.New(e.Lang.T("sessionMissingIdentity"))
			}
		}
		return true, nil
	}
	if err == nil && id.Key() == account.Key {
		ok, err := status()
		if err != nil || ok {
			return childEnv, err
		}
	}
	fmt.Fprintln(e.Stderr, e.Lang.T("sessionSignIn", label(account)))
	if err := e.Exec(childEnv, "claude", "auth", "login", "--claudeai"); err != nil {
		return nil, fmt.Errorf("claude auth login: %w", err)
	}
	id, err = readAccount()
	if errors.Is(err, os.ErrNotExist) {
		return nil, errors.New(e.Lang.T("loginNoAuth"))
	}
	if err != nil {
		return nil, err
	}
	if id.Key() != account.Key {
		return nil, errors.New(e.Lang.T("sessionWrongAccount", label(account)))
	}
	ok, err := status()
	if err == nil && !ok {
		err = errors.New(e.Lang.T("sessionAuthStatus"))
	}
	return childEnv, err
}

func sessionSettings(e Env, dir string, args []string) ([]string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	for _, file := range []string{filepath.Join(dir, "settings.json"), filepath.Join(cwd, ".claude", "settings.json"), filepath.Join(cwd, ".claude", "settings.local.json")} {
		raw, err := os.ReadFile(file)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if err := checkSessionSettings(e, raw, file); err != nil {
			return nil, err
		}
	}
	var settings []string
	for i := 0; i < len(args); i++ {
		name, value, inline := strings.Cut(args[i], "=")
		switch name {
		case "--":
			return settings, nil
		case "--bare":
			return nil, errors.New(e.Lang.T("sessionBare"))
		case "--setting-sources", "--settings":
			settings = append(settings, args[i])
			if !inline {
				i++
				if i == len(args) {
					return nil, usageError(e.Lang.T("needsArgs", name, 1))
				}
				settings = append(settings, args[i])
				value = args[i]
			}
			if name == "--settings" {
				raw := []byte(value)
				if !strings.HasPrefix(strings.TrimSpace(value), "{") {
					raw, err = os.ReadFile(value)
					if err != nil {
						return nil, err
					}
				}
				if err := checkSessionSettings(e, raw, "--settings"); err != nil {
					return nil, err
				}
			}
		}
	}
	return settings, nil
}

func checkSessionSettings(e Env, raw []byte, source string) error {
	var settings struct {
		Env          map[string]string `json:"env"`
		APIKeyHelper string            `json:"apiKeyHelper"`
	}
	if err := json.Unmarshal(raw, &settings); err != nil {
		return fmt.Errorf("%s: %w", source, err)
	}
	if settings.APIKeyHelper != "" {
		return errors.New(e.Lang.T("sessionSettingsOverride", "apiKeyHelper", source))
	}
	for _, key := range strings.Fields(sessionAuthOverrides + " " + configEnv + " " + secureEnv) {
		if settings.Env[key] != "" {
			return errors.New(e.Lang.T("sessionSettingsOverride", key, source))
		}
	}
	return nil
}
