package claude

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/WellWells/agentswap/internal/store"
	"github.com/WellWells/agentswap/internal/swap"
)

func creds(access, refresh string) string {
	b, _ := json.Marshal(map[string]any{"claudeAiOauth": map[string]any{
		"accessToken": access, "refreshToken": refresh, "expiresAt": 1791440000000, "scopes": []string{"user:inference"}, "subscriptionType": "max",
	}})
	return string(b)
}

func oauth(uuid, org, email string) string {
	return `{"accountUuid":"` + uuid + `","organizationUuid":"` + org + `","emailAddress":"` + email + `"}`
}

func setup(t *testing.T) (Provider, string) {
	home := t.TempDir()
	return New(func(string) string { return "" }, home, "linux", nil), home
}

func writeLive(t *testing.T, p Provider, credsJSON, config string) {
	t.Helper()
	os.MkdirAll(p.ConfigDir, 0o700)
	if err := os.WriteFile(filepath.Join(p.ConfigDir, ".credentials.json"), []byte(credsJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.GlobalConfig, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestPaths(t *testing.T) {
	p, home := setup(t)
	if p.ConfigDir != filepath.Join(home, ".claude") || p.GlobalConfig != filepath.Join(home, ".claude.json") || p.Keychain != nil {
		t.Fatalf("%+v", p)
	}
	dir := filepath.Join(home, "alt")
	q := New(func(k string) string { return map[string]string{"CLAUDE_CONFIG_DIR": dir}[k] }, home, "linux", nil)
	if q.ConfigDir != dir || q.GlobalConfig != filepath.Join(dir, ".claude.json") {
		t.Fatalf("%+v", q)
	}
	if m := New(func(string) string { return "" }, home, "darwin", nil); m.Keychain == nil || m.Keychain.Service != "Claude Code-credentials" {
		t.Fatalf("%+v", m.Keychain)
	}
}

func TestIdentify(t *testing.T) {
	id, err := Identify(Pack([]byte(creds("at", "rt")), []byte(oauth("u1", "o1", "a@x"))))
	if err != nil || id != (swap.Identity{Key: "claude:u1:o1", Email: "a@x", Plan: "max", Mode: "oauth"}) {
		t.Fatalf("%+v %v", id, err)
	}
	if _, err := Identify(Pack([]byte(creds("", "")), []byte(oauth("u1", "o1", "a@x")))); !errors.Is(err, ErrWiped) {
		t.Fatalf("wiped: %v", err)
	}
	if _, err := Identify(Pack([]byte(creds("sk-ant-oat", "")), []byte(oauth("u1", "o1", "a@x")))); !errors.Is(err, ErrUnsupportedLogin) {
		t.Fatalf("setup-token: %v", err)
	}
	if _, err := Identify(Pack([]byte(creds("at", "rt")), []byte(`{"emailAddress":"t@token.local","accountUuid":""}`))); !errors.Is(err, ErrUnsupportedLogin) {
		t.Fatalf("no uuid: %v", err)
	}
}

func TestReadLiveKeepsOnlyAccountKeys(t *testing.T) {
	p, _ := setup(t)
	if _, err := p.ReadLive(); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("empty: %v", err)
	}
	live := strings.TrimSuffix(creds("at", "rt"), "}") + `,"mcpOAuth":{"notion":{"accessToken":"m"}},"trustedDeviceToken":"td"}`
	writeLive(t, p, live, `{"oauthAccount":`+oauth("u1", "o1", "a@x")+`,"projects":{}}`)
	raw, err := p.ReadLive()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "mcpOAuth") || !strings.Contains(string(raw), "trustedDeviceToken") {
		t.Fatalf("envelope: %s", raw)
	}
	if id, _ := p.Identify(raw); id.Key != "claude:u1:o1" {
		t.Fatalf("%+v", id)
	}
}

func TestWriteLivePreservesOtherConfig(t *testing.T) {
	p, _ := setup(t)
	config := "{\n  \"numStartups\": 9,\n  \"oauthAccount\": " + oauth("u1", "o1", "a@x") + ",\n  \"projects\": {\"C:/w\": {}}\n}\n"
	live := "{\n  \"claudeAiOauth\": {\"accessToken\": \"at1\", \"refreshToken\": \"rt1\"},\n  \"mcpOAuth\": {\"notion\": 1},\n  \"pluginSecrets\": {\"p\": 2}\n}"
	writeLive(t, p, live, config)
	if err := p.WriteLive(Pack([]byte(creds("at2", "rt2")), []byte(oauth("u2", "o2", "b@x")))); err != nil {
		t.Fatal(err)
	}
	gotCfg, _ := os.ReadFile(p.GlobalConfig)
	if want := strings.Replace(config, oauth("u1", "o1", "a@x"), oauth("u2", "o2", "b@x"), 1); string(gotCfg) != want {
		t.Fatalf("config\n%s", gotCfg)
	}
	gotCreds, _ := os.ReadFile(filepath.Join(p.ConfigDir, ".credentials.json"))
	s := string(gotCreds)
	if !strings.Contains(s, `"mcpOAuth": {"notion": 1}`) || !strings.Contains(s, `"pluginSecrets": {"p": 2}`) || !strings.Contains(s, "rt2") || strings.Contains(s, "rt1") {
		t.Fatalf("creds\n%s", s)
	}
	raw, _ := p.ReadLive()
	if id, _ := p.Identify(raw); id.Key != "claude:u2:o2" {
		t.Fatalf("%+v", id)
	}
}

func TestWriteLiveCreatesMissingFiles(t *testing.T) {
	p, _ := setup(t)
	if err := p.WriteLive(Pack([]byte(creds("at", "rt")), []byte(oauth("u", "o", "a@x")))); err != nil {
		t.Fatal(err)
	}
	raw, err := p.ReadLive()
	if err != nil {
		t.Fatal(err)
	}
	if id, _ := p.Identify(raw); id.Key != "claude:u:o" {
		t.Fatalf("%+v", id)
	}
}

func TestWipedLiveDoesNotOverwriteSnapshot(t *testing.T) {
	p, home := setup(t)
	m := &swap.Manager{P: p, S: store.Store{Dir: filepath.Join(home, "data")}, LockTimeout: time.Second}
	writeLive(t, p, creds("at", "rt"), `{"oauthAccount":`+oauth("u1", "o1", "a@x")+`}`)
	if _, err := m.Add(""); err != nil {
		t.Fatal(err)
	}
	writeLive(t, p, creds("", ""), `{"oauthAccount":`+oauth("u1", "o1", "a@x")+`}`)
	if _, err := m.Status(); err != nil {
		t.Fatal(err)
	}
	snap, _ := m.S.ReadSnapshot("claude:u1:o1")
	if !strings.Contains(string(snap), `"rt"`) {
		t.Fatalf("snapshot overwritten: %s", snap)
	}
}

func TestWriteLiveRestoresCredentialsWhenConfigWriteFails(t *testing.T) {
	p, _ := setup(t)
	writeLive(t, p, creds("at1", "rt1"), `{"oauthAccount":`+oauth("u1", "o1", "a@x")+`}`)
	os.Remove(p.GlobalConfig)
	os.MkdirAll(p.GlobalConfig, 0o700)
	if err := p.WriteLive(Pack([]byte(creds("at2", "rt2")), []byte(oauth("u2", "o2", "b@x")))); err == nil {
		t.Fatal("expected error")
	}
	got, _ := os.ReadFile(filepath.Join(p.ConfigDir, ".credentials.json"))
	if !strings.Contains(string(got), "rt1") || strings.Contains(string(got), "rt2") {
		t.Fatalf("credentials not restored: %s", got)
	}
}

func TestSyncDoesNotSaveAnotherAccountsTokens(t *testing.T) {
	p, home := setup(t)
	m := &swap.Manager{P: p, S: store.Store{Dir: filepath.Join(home, "data")}, LockTimeout: time.Second}
	writeLive(t, p, creds("atA", "rtA"), `{"oauthAccount":`+oauth("uA", "o", "a@x")+`}`)
	m.Add("")
	writeLive(t, p, creds("atB", "rtB"), `{"oauthAccount":`+oauth("uB", "o", "b@x")+`}`)
	m.Add("")
	writeLive(t, p, creds("atB", "rtB"), `{"oauthAccount":`+oauth("uA", "o", "a@x")+`}`)
	if _, err := m.Status(); err != nil {
		t.Fatal(err)
	}
	snap, _ := m.S.ReadSnapshot("claude:uA:o")
	if !strings.Contains(string(snap), "rtA") {
		t.Fatalf("account A snapshot overwritten with B's tokens: %s", snap)
	}
}
