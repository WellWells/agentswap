package antigravity

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"strings"
	"testing"
)

func jwt(claims map[string]any) string {
	b, _ := json.Marshal(claims)
	enc := base64.RawURLEncoding
	return enc.EncodeToString([]byte(`{"alg":"RS256"}`)) + "." + enc.EncodeToString(b) + ".sig"
}

func blob(sub, email, access, refresh, expiry string) []byte {
	id := jwt(map[string]any{"sub": sub, "email": email, "email_verified": true})
	return []byte(`{"token":{"access_token":"` + access + `","token_type":"Bearer","refresh_token":"` + refresh + `","expiry":"` + expiry + `"},"auth_method":"consumer","id_token":"` + id + `"}`)
}

const future = "2026-10-08T11:00:00.123456789+08:00"

type memKeyring struct {
	b      []byte
	err    error
	writes int
}

func (m *memKeyring) Read() ([]byte, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.b == nil {
		return nil, fs.ErrNotExist
	}
	return m.b, nil
}

func (m *memKeyring) Write(b []byte) error {
	m.writes++
	m.b = append([]byte(nil), b...)
	return nil
}

func TestIdentify(t *testing.T) {
	id, err := Identify(blob("111", "a@gmail.com", "at", "rt", future))
	if err != nil {
		t.Fatal(err)
	}
	if id.Key != "google:111" || id.Email != "a@gmail.com" || id.Mode != "oauth" {
		t.Fatalf("%+v", id)
	}
}

func TestIdentifyRejects(t *testing.T) {
	if _, err := Identify(blob("111", "a@gmail.com", "at", "", future)); !errors.Is(err, ErrUnsupportedLogin) {
		t.Fatalf("no refresh token: %v", err)
	}
	if _, err := Identify([]byte(`{"token":{"refresh_token":"rt"}}`)); !errors.Is(err, ErrUnsupportedLogin) {
		t.Fatalf("no id_token: %v", err)
	}
	if _, err := Identify([]byte(`{"token":{"refresh_token":"rt"},"id_token":"a.!!!.c"}`)); !errors.Is(err, ErrUnsupportedLogin) {
		t.Fatalf("bad id_token: %v", err)
	}
	if _, err := Identify([]byte(`not json`)); err == nil {
		t.Fatal("bad json accepted")
	}
}

func TestSameLogin(t *testing.T) {
	var p Provider
	a := blob("1", "a@x", "at1", "rt1", future)
	if !p.SameLogin(a, blob("2", "b@x", "other", "rt1", future)) {
		t.Fatal("same refresh token")
	}
	if !p.SameLogin(a, blob("2", "b@x", "at1", "other", future)) {
		t.Fatal("same access token")
	}
	if p.SameLogin(a, blob("2", "b@x", "at2", "rt2", future)) {
		t.Fatal("different tokens")
	}
	if p.SameLogin(a, []byte("x")) {
		t.Fatal("bad json")
	}
}

func TestReadWriteLive(t *testing.T) {
	k := &memKeyring{}
	running := false
	p := Provider{Keyring: k, Running: func() (bool, error) { return running, nil }}
	if _, err := p.ReadLive(); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("empty keyring: %v", err)
	}
	b := blob("1", "a@x", "at", "rt", future)
	if err := p.WriteLive(b); err != nil {
		t.Fatal(err)
	}
	got, err := p.ReadLive()
	if err != nil || !bytes.Equal(got, b) {
		t.Fatalf("%s %v", got, err)
	}
	running = true
	if err := p.WriteLive(blob("2", "b@x", "x", "y", future)); !errors.Is(err, ErrRunning) {
		t.Fatalf("running: %v", err)
	}
	if k.writes != 1 || !bytes.Equal(k.b, b) {
		t.Fatal("wrote while agy was running")
	}
}

func TestDecodeValue(t *testing.T) {
	raw := []byte(`{"token":{}}`)
	cases := map[string][]byte{
		"plain":   raw,
		"base64":  []byte("go-keyring-base64:" + base64.StdEncoding.EncodeToString(raw)),
		"encoded": []byte("go-keyring-encoded:" + hex.EncodeToString(raw)),
	}
	for name, in := range cases {
		got, err := decodeValue(in)
		if err != nil || !bytes.Equal(got, raw) {
			t.Fatalf("%s: %q %v", name, got, err)
		}
	}
	if _, err := decodeValue([]byte("go-keyring-base64:***")); err == nil {
		t.Fatal("bad base64 accepted")
	}
}

type call struct {
	stdin string
	name  string
	args  []string
}

type fakeRun struct {
	calls []call
	out   []byte
	code  int
	err   error
}

func (f *fakeRun) run(stdin []byte, name string, args ...string) ([]byte, int, error) {
	f.calls = append(f.calls, call{string(stdin), name, args})
	return f.out, f.code, f.err
}

func TestKeychain(t *testing.T) {
	raw := blob("1", "a@x", "at", "rt", future)
	f := &fakeRun{out: []byte("go-keyring-base64:" + base64.StdEncoding.EncodeToString(raw) + "\n")}
	k := keychain{run: f.run}
	got, err := k.Read()
	if err != nil || !bytes.Equal(got, raw) {
		t.Fatalf("%s %v", got, err)
	}
	c := f.calls[0]
	if c.name != "/usr/bin/security" || strings.Join(c.args, " ") != "find-generic-password -s gemini -a antigravity -w" {
		t.Fatalf("%+v", c)
	}
	f.code = 44
	if _, err := k.Read(); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing: %v", err)
	}
	f.code = 0
	if err := k.Write(raw); err != nil {
		t.Fatal(err)
	}
	c = f.calls[len(f.calls)-1]
	want := "add-generic-password -U -s gemini -a antigravity -w go-keyring-base64:" + base64.StdEncoding.EncodeToString(raw) + "\n"
	if strings.Join(c.args, " ") != "-i" || c.stdin != want {
		t.Fatalf("%+v", c)
	}
	if err := k.Write(bytes.Repeat([]byte("x"), 3000)); err == nil {
		t.Fatal("oversized value accepted")
	}
}

func TestSecretTool(t *testing.T) {
	raw := blob("1", "a@x", "at", "rt", future)
	f := &fakeRun{out: raw}
	s := secretTool{run: f.run}
	got, err := s.Read()
	if err != nil || !bytes.Equal(got, raw) {
		t.Fatalf("%s %v", got, err)
	}
	if c := f.calls[0]; c.name != "secret-tool" || strings.Join(c.args, " ") != "lookup service gemini username antigravity" {
		t.Fatalf("%+v", c)
	}
	f.out, f.code = nil, 1
	if _, err := s.Read(); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing: %v", err)
	}
	f.code, f.err = -1, errors.New("executable file not found")
	if _, err := s.Read(); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("no secret-tool: %v", err)
	}
	f.code, f.err = 0, nil
	if err := s.Write(raw); err != nil {
		t.Fatal(err)
	}
	c := f.calls[len(f.calls)-1]
	if c.stdin != string(raw) || strings.Join(c.args, " ") != "store --label=Password for 'antigravity' on 'gemini' service gemini username antigravity" {
		t.Fatalf("%+v", c)
	}
}

func TestPgrep(t *testing.T) {
	f := &fakeRun{}
	running := pgrep(f.run)
	if ok, err := running(); !ok || err != nil {
		t.Fatalf("exit 0: %v %v", ok, err)
	}
	if c := f.calls[0]; c.name != "pgrep" || strings.Join(c.args, " ") != "-x agy" {
		t.Fatalf("%+v", c)
	}
	f.code = 1
	if ok, _ := running(); ok {
		t.Fatal("exit 1 counted as running")
	}
	f.code, f.err = -1, errors.New("not found")
	if ok, err := running(); ok || err != nil {
		t.Fatalf("no pgrep: %v %v", ok, err)
	}
}

func TestNewPicksKeyring(t *testing.T) {
	env := func(k string) string {
		return map[string]string{"AGENTSWAP_ANTIGRAVITY_API_URL": "http://api", "AGENTSWAP_ANTIGRAVITY_TOKEN_URL": "http://tok"}[k]
	}
	if _, ok := New(env, "windows", nil).Keyring.(winCred); !ok {
		t.Fatal("windows")
	}
	if _, ok := New(env, "darwin", nil).Keyring.(keychain); !ok {
		t.Fatal("darwin")
	}
	p := New(env, "linux", nil)
	if _, ok := p.Keyring.(secretTool); !ok || p.BaseURL != "http://api" || p.RefreshURL != "http://tok" || p.Running == nil {
		t.Fatalf("%+v", p)
	}
}
