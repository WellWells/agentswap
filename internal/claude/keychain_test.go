package claude

import (
	"encoding/hex"
	"errors"
	"io/fs"
	"strings"
	"testing"
)

type fakeSecurity struct {
	items map[string]string
	calls [][]string
	stdin []string
}

func (f *fakeSecurity) run(stdin []byte, name string, args ...string) ([]byte, int, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	if stdin != nil {
		f.stdin = append(f.stdin, string(stdin))
	}
	if name != "/usr/bin/security" {
		return nil, 1, nil
	}
	if len(args) == 1 && args[0] == "-i" {
		_, rest, _ := strings.Cut(string(stdin), `-s "`)
		svc, _, _ := strings.Cut(rest, `"`)
		fields := strings.Fields(string(stdin))
		v, _ := hex.DecodeString(fields[len(fields)-1])
		f.items[svc] = string(v)
		return nil, 0, nil
	}
	svc := args[len(args)-1]
	switch args[0] {
	case "find-generic-password":
		v, ok := f.items[svc]
		if !ok {
			return nil, 44, nil
		}
		return []byte(v + "\n"), 0, nil
	case "add-generic-password":
		v, _ := hex.DecodeString(args[len(args)-1])
		f.items[args[5]] = string(v)
		return nil, 0, nil
	}
	return nil, 1, nil
}

func TestKeychainReadWrite(t *testing.T) {
	f := &fakeSecurity{items: map[string]string{}}
	k := Keychain{Service: "Claude Code-credentials", Account: "wells", Run: f.run}
	if _, err := k.Read(); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing: %v", err)
	}
	secret := `{"claudeAiOauth":{"refreshToken":"rt-secret"}}`
	if err := k.Write([]byte(secret)); err != nil {
		t.Fatal(err)
	}
	for _, c := range f.calls {
		if strings.Contains(strings.Join(c, " "), "rt-secret") || strings.Contains(strings.Join(c, " "), hex.EncodeToString([]byte(secret))) {
			t.Fatalf("secret in argv: %v", c)
		}
	}
	if !strings.Contains(f.stdin[0], "-U") || strings.Contains(f.stdin[0], " -A") {
		t.Fatalf("stdin %q", f.stdin[0])
	}
	got, err := k.Read()
	if err != nil || string(got) != secret {
		t.Fatalf("%q %v", got, err)
	}
}

func TestKeychainServiceSuffix(t *testing.T) {
	env := map[string]string{"CLAUDE_CONFIG_DIR": "/Users/w/.claude-alt"}
	got := keychainService(func(k string) string { return env[k] })
	if !strings.HasPrefix(got, "Claude Code-credentials-") || len(got) != len("Claude Code-credentials-")+8 {
		t.Fatalf("%q", got)
	}
	env["CLAUDE_SECURESTORAGE_CONFIG_DIR"] = "/x"
	if keychainService(func(k string) string { return env[k] }) == got {
		t.Fatal("securestorage dir not preferred")
	}
}

func TestDarwinProviderUsesKeychain(t *testing.T) {
	f := &fakeSecurity{items: map[string]string{}}
	p := New(func(k string) string { return map[string]string{"USER": "wells"}[k] }, t.TempDir(), "darwin", f.run)
	if err := p.WriteLive(Pack([]byte(creds("at", "rt")), []byte(oauth("u", "o", "a@x")))); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.items["Claude Code-credentials"], `"rt"`) {
		t.Fatalf("items %v", f.items)
	}
	raw, err := p.ReadLive()
	if err != nil {
		t.Fatal(err)
	}
	if id, _ := p.Identify(raw); id.Key != "claude:u:o" {
		t.Fatalf("%+v", id)
	}
}

func TestKeychainNeverPutsLargeSecretInArgv(t *testing.T) {
	f := &fakeSecurity{items: map[string]string{}}
	k := Keychain{Service: "Claude Code-credentials", Account: "wells", Run: f.run}
	big := `{"claudeAiOauth":{"refreshToken":"` + strings.Repeat("x", 3000) + `"}}`
	if err := k.Write([]byte(big)); err == nil {
		t.Fatal("oversized write accepted")
	}
	for _, c := range f.calls {
		if len(c) > 2 && c[1] == "add-generic-password" {
			t.Fatalf("secret passed in argv: %v", c[:6])
		}
	}
}
