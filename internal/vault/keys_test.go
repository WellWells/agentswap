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
	sealed, err := newDarwin(f.run).Seal([]byte("x"))
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
	got, err := newDarwin(f.run).Open(sealed)
	if err != nil || string(got) != "x" {
		t.Fatalf("reopen: %q %v", got, err)
	}
}

func TestDarwinMissingKeyIsErrKey(t *testing.T) {
	f := &fakeRunner{secrets: map[string]string{}}
	sealed, _ := newDarwin(f.run).Seal([]byte("x"))
	delete(f.secrets, "keychain")
	if _, err := newDarwin(f.run).Open(sealed); !errors.Is(err, ErrKey) {
		t.Fatalf("err = %v", err)
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
