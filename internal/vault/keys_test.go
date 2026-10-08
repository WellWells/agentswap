package vault

import (
	"bytes"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type fakeRunner struct {
	secrets map[string]string
	calls   []string
	missing bool
	broken  bool
}

func (f *fakeRunner) run(stdin []byte, name string, args ...string) ([]byte, int, error) {
	f.calls = append(f.calls, name+" "+strings.Join(args, " "))
	if f.missing {
		return nil, -1, errors.New("exec: not found")
	}
	if f.broken {
		return nil, 1, nil
	}
	switch {
	case name == "/usr/bin/security" && args[0] == "find-generic-password":
		v, ok := f.secrets["keychain"]
		if !ok {
			return nil, 44, nil
		}
		return []byte(v + "\n"), 0, nil
	case name == "/usr/bin/security" && len(args) == 1 && args[0] == "-i":
		fields := strings.Fields(string(stdin))
		h, _ := hex.DecodeString(fields[len(fields)-1])
		f.secrets["keychain"] = string(h)
		return nil, 0, nil
	case name == "secret-tool" && args[0] == "lookup":
		v, ok := f.secrets["secret"]
		if !ok {
			return nil, 1, nil
		}
		return []byte(v), 0, nil
	case name == "secret-tool" && args[0] == "store":
		f.secrets["secret"] = string(stdin)
		return nil, 0, nil
	}
	return nil, 1, nil
}

func TestDarwinKeychainCreatesAndReusesKey(t *testing.T) {
	f := &fakeRunner{secrets: map[string]string{}}
	sealed, err := newDarwin(t.TempDir(), f.run).Seal([]byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	if sealed[len(Magic)] != schemeKeyring {
		t.Fatalf("scheme %d", sealed[len(Magic)])
	}
	for _, c := range f.calls {
		if strings.Contains(c, f.secrets["keychain"]) {
			t.Fatalf("key leaked into argv: %s", c)
		}
	}
	got, err := newDarwin(t.TempDir(), f.run).Open(sealed)
	if err != nil || string(got) != "x" {
		t.Fatalf("reopen: %q %v", got, err)
	}
}

func TestDarwinMissingKeyIsErrKey(t *testing.T) {
	f := &fakeRunner{secrets: map[string]string{}}
	dir := t.TempDir()
	sealed, _ := newDarwin(dir, f.run).Seal([]byte("x"))
	delete(f.secrets, "keychain")
	if _, err := newDarwin(dir, f.run).Open(sealed); !errors.Is(err, ErrKey) {
		t.Fatalf("err = %v", err)
	}
}

func TestLostKeyringKeyIsNeverRecreated(t *testing.T) {
	dir := t.TempDir()
	f := &fakeRunner{secrets: map[string]string{}}
	if _, err := newDarwin(dir, f.run).Seal([]byte("x")); err != nil {
		t.Fatal(err)
	}
	delete(f.secrets, "keychain")
	if _, err := newDarwin(dir, f.run).Seal([]byte("y")); !errors.Is(err, ErrKey) {
		t.Fatalf("darwin err = %v", err)
	}
	if _, ok := f.secrets["keychain"]; ok {
		t.Fatal("darwin key was recreated")
	}
	ldir := t.TempDir()
	g := &fakeRunner{secrets: map[string]string{}}
	if _, err := newLinux(ldir, &bytes.Buffer{}, g.run, func() string { return "m" }).Seal([]byte("x")); err != nil {
		t.Fatal(err)
	}
	delete(g.secrets, "secret")
	if _, err := newLinux(ldir, &bytes.Buffer{}, g.run, func() string { return "m" }).Seal([]byte("y")); !errors.Is(err, ErrKey) {
		t.Fatalf("linux err = %v", err)
	}
	if _, ok := g.secrets["secret"]; ok {
		t.Fatal("linux key was recreated")
	}
	if _, err := os.Stat(filepath.Join(ldir, ".key")); err == nil {
		t.Fatal("fell back to a key file while keyring data exists")
	}
}

func TestExistingKeyringKeyIsProtectedAfterLoss(t *testing.T) {
	key := hex.EncodeToString(bytes.Repeat([]byte{7}, 32))
	for _, name := range []string{"keychain", "secret"} {
		dir := t.TempDir()
		f := &fakeRunner{secrets: map[string]string{name: key}}
		open := func() Vault {
			if name == "keychain" {
				return newDarwin(dir, f.run)
			}
			return newLinux(dir, &bytes.Buffer{}, f.run, func() string { return "m" })
		}
		if _, err := open().Seal([]byte("x")); err != nil {
			t.Fatal(err)
		}
		delete(f.secrets, name)
		if _, err := open().Seal([]byte("y")); !errors.Is(err, ErrKey) {
			t.Fatalf("%s: err = %v", name, err)
		}
		if _, ok := f.secrets[name]; ok {
			t.Fatalf("%s: key was recreated", name)
		}
	}
}

func TestLinuxPrefersSecretTool(t *testing.T) {
	f := &fakeRunner{secrets: map[string]string{}}
	var warn bytes.Buffer
	sealed, err := newLinux(t.TempDir(), &warn, f.run, func() string { return "m1" }).Seal([]byte("x"))
	if err != nil || sealed[len(Magic)] != schemeKeyring || warn.Len() != 0 {
		t.Fatalf("err=%v warn=%q", err, warn.String())
	}
}

func TestLinuxFallsBackToKeyFileWithWarning(t *testing.T) {
	dir := t.TempDir()
	f := &fakeRunner{missing: true}
	var warn bytes.Buffer
	sealed, err := newLinux(dir, &warn, f.run, func() string { return "m1" }).Seal([]byte("x"))
	if err != nil || sealed[len(Magic)] != schemeFile {
		t.Fatalf("err=%v", err)
	}
	if !strings.Contains(warn.String(), "keyring") {
		t.Fatalf("warn = %q", warn.String())
	}
	st, err := os.Stat(filepath.Join(dir, ".key"))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && st.Mode().Perm() != 0o600 {
		t.Fatalf("perm %v", st.Mode().Perm())
	}
	warn.Reset()
	got, err := newLinux(dir, &warn, f.run, func() string { return "m1" }).Open(sealed)
	if err != nil || string(got) != "x" || warn.Len() != 0 {
		t.Fatalf("reopen %q %v %q", got, err, warn.String())
	}
	if _, err := newLinux(dir, &warn, f.run, func() string { return "m2" }).Open(sealed); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("other machine: %v", err)
	}
}

func TestLinuxBrokenKeyringFallsBack(t *testing.T) {
	f := &fakeRunner{broken: true}
	sealed, err := newLinux(t.TempDir(), &bytes.Buffer{}, f.run, func() string { return "m1" }).Seal([]byte("x"))
	if err != nil || sealed[len(Magic)] != schemeFile {
		t.Fatalf("err=%v", err)
	}
}
