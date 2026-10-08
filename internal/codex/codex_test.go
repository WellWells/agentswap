package codex

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fakeJWT(claims map[string]any) string {
	b, _ := json.Marshal(claims)
	enc := base64.RawURLEncoding
	return enc.EncodeToString([]byte(`{"alg":"none"}`)) + "." + enc.EncodeToString(b) + ".sig"
}

func chatgptAuth(email, user, account, plan string) []byte {
	b, _ := json.Marshal(map[string]any{
		"auth_mode":      "chatgpt",
		"OPENAI_API_KEY": nil,
		"tokens": map[string]any{
			"id_token": fakeJWT(map[string]any{
				"email": email,
				"sub":   "sub-" + user,
				"https://api.openai.com/auth": map[string]any{
					"chatgpt_account_id": account,
					"chatgpt_user_id":    user,
					"chatgpt_plan_type":  plan,
				},
			}),
			"access_token":  "at",
			"refresh_token": "rt",
			"account_id":    account,
		},
		"last_refresh": "2026-10-08T00:00:00Z",
	})
	return b
}

func TestIdentifyChatGPT(t *testing.T) {
	id, err := Identify(chatgptAuth("a@x.com", "user-1", "acct-1", "plus"))
	if err != nil {
		t.Fatal(err)
	}
	if id.Key != "chatgpt:user-1:acct-1" || id.Email != "a@x.com" || id.Plan != "plus" || id.Mode != "chatgpt" {
		t.Fatalf("got %+v", id)
	}
}

func TestIdentifySameUserDifferentWorkspace(t *testing.T) {
	a, _ := Identify(chatgptAuth("a@x.com", "user-1", "personal", "plus"))
	b, _ := Identify(chatgptAuth("a@x.com", "user-1", "team", "team"))
	if a.Key == b.Key {
		t.Fatal("workspaces must be distinct accounts")
	}
}

func TestIdentifyFallsBackToSubAndTokenAccountID(t *testing.T) {
	b, _ := json.Marshal(map[string]any{
		"tokens": map[string]any{
			"id_token":   fakeJWT(map[string]any{"email": "a@x.com", "sub": "sub-9"}),
			"account_id": "acct-9",
		},
	})
	id, err := Identify(b)
	if err != nil || id.Key != "chatgpt:sub-9:acct-9" {
		t.Fatalf("got %+v, %v", id, err)
	}
}

func TestIdentifyAPIKey(t *testing.T) {
	id, err := Identify([]byte(`{"OPENAI_API_KEY":"sk-proj-abcdefghijklmnop1234"}`))
	if err != nil {
		t.Fatal(err)
	}
	if id.Mode != "apikey" || !strings.HasPrefix(id.Key, "apikey:") || strings.Contains(id.Key, "abcdefgh") {
		t.Fatalf("got %+v", id)
	}
	if id.Email != "sk-...1234" {
		t.Fatalf("label %q", id.Email)
	}
}

func TestIdentifyRejectsEmptyAndGarbage(t *testing.T) {
	for _, in := range []string{`{}`, `{"OPENAI_API_KEY":null}`, `not json`, `{"tokens":{"id_token":"bad"}}`} {
		if _, err := Identify([]byte(in)); err == nil {
			t.Errorf("%s: want error", in)
		}
	}
}

func TestStoreModeFromConfig(t *testing.T) {
	cases := map[string]string{
		"":                    "file",
		"model = \"gpt-5\"\n": "file",
		"cli_auth_credentials_store = \"keyring\"\n":                                                                                 "keyring",
		"  cli_auth_credentials_store='auto' # x\n":                                                                                  "auto",
		"cli_auth_credentials_store = \"file\"\n":                                                                                    "file",
		"# cli_auth_credentials_store = \"keyring\"\n":                                                                               "file",
		"[profiles.x]\ncli_auth_credentials_store = \"keyring\"\n":                                                                   "file",
		"\"cli_auth_credentials_store\" = \"keyring\"\n":                                                                             "keyring",
		"'cli_auth_credentials_store' = 'keyring'\r\n":                                                                               "keyring",
		strings.Repeat("#", 70000) + "\ncli_auth_credentials_store = \"keyring\"\n":                                                  "keyring",
		"notes = \"\"\"\n[x]\ncli_auth_credentials_store = \"file\"\n\"\"\"\ncli_auth_credentials_store = \"keyring\"\n":             "keyring",
		"a = [\n  \"x\", # c\n  [1, 2],\n]\nt = { k = \"]\" }\nd = 1979-05-27 07:32:00Z\ncli_auth_credentials_store = \"keyring\"\n": "keyring",
		"x.cli_auth_credentials_store = \"keyring\"\n":                                                                               "file",
	}
	for in, want := range cases {
		if got, err := storeMode([]byte(in)); err != nil || got != want {
			t.Errorf("%q: got %s, %v want %s", in, got, err, want)
		}
	}
	for _, in := range []string{"cli_auth_credentials_store = \"keyring\n", "= 1\n", "a = [1,\n", "a = 1 2\n"} {
		if _, err := storeMode([]byte(in)); err == nil {
			t.Errorf("%q: malformed config accepted", in)
		}
	}
}

func TestWriteLiveRefusesUnparsableConfig(t *testing.T) {
	p := Provider{Home: t.TempDir()}
	os.WriteFile(filepath.Join(p.Home, "config.toml"), []byte("cli_auth_credentials_store = \"keyring\n"), 0o600)
	os.WriteFile(p.AuthPath(), []byte("old"), 0o600)
	if err := p.WriteLive([]byte("new")); err == nil {
		t.Fatal("WriteLive accepted an unparsable config.toml")
	}
	if b, _ := os.ReadFile(p.AuthPath()); string(b) != "old" {
		t.Fatalf("auth.json overwritten: %q", b)
	}
}

func TestProviderReadWriteLive(t *testing.T) {
	home := t.TempDir()
	p := Provider{Home: home}
	if _, err := p.ReadLive(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing auth.json: %v", err)
	}
	if err := p.WriteLive([]byte("x")); err != nil {
		t.Fatal(err)
	}
	b, err := p.ReadLive()
	if err != nil || string(b) != "x" {
		t.Fatalf("got %q, %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(home, "auth.json")); err != nil {
		t.Fatal(err)
	}
}

func TestProviderRefusesKeyringStore(t *testing.T) {
	home := t.TempDir()
	os.WriteFile(filepath.Join(home, "config.toml"), []byte("cli_auth_credentials_store = \"keyring\"\n"), 0o600)
	p := Provider{Home: home}
	if _, err := p.ReadLive(); !errors.Is(err, ErrKeyringStore) {
		t.Fatalf("read: %v", err)
	}
	if err := p.WriteLive([]byte("x")); !errors.Is(err, ErrKeyringStore) {
		t.Fatalf("write: %v", err)
	}
}
