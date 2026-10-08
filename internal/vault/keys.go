package vault

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/WellWells/agentswap/internal/execx"
	"github.com/WellWells/agentswap/internal/fsx"
)

const (
	securityBin = "/usr/bin/security"
	service     = "agentswap"
	account     = "vault-key"
)

func newKey() ([]byte, error) {
	k := make([]byte, 32)
	_, err := rand.Read(k)
	return k, err
}

func decodeKey(b []byte) ([]byte, error) {
	k, err := hex.DecodeString(strings.TrimSpace(string(b)))
	if err != nil || len(k) != 32 {
		return nil, errors.New("malformed vault key")
	}
	return k, nil
}

var errKeyLost = errors.New("the keyring no longer returns the vault key that encrypted saved accounts; refusing to create a new one")

func markerPath(dir string) string { return filepath.Join(dir, ".vault-keyring") }

func keyID(k []byte) string {
	sum := sha256.Sum256(k)
	return hex.EncodeToString(sum[:8])
}

func hasMarker(dir string) bool {
	_, err := os.Stat(markerPath(dir))
	return err == nil
}

func checkMarker(dir string, k []byte, create bool) ([]byte, error) {
	b, err := os.ReadFile(markerPath(dir))
	switch {
	case err == nil && strings.TrimSpace(string(b)) != keyID(k):
		return nil, errKeyLost
	case errors.Is(err, fs.ErrNotExist) && create:
		if err := writeMarker(dir, k); err != nil {
			return nil, err
		}
	}
	return k, nil
}

func writeMarker(dir string, k []byte) error {
	return fsx.WriteAtomic(markerPath(dir), []byte(keyID(k)+"\n"), 0o600)
}

type keychainKey struct {
	run execx.Runner
	dir string
}

func (keychainKey) scheme() byte { return schemeKeyring }

func (s keychainKey) key(create bool) ([]byte, error) {
	out, code, err := s.run(nil, securityBin, "find-generic-password", "-a", account, "-s", service, "-w")
	if err != nil {
		return nil, err
	}
	if code == 0 {
		k, err := decodeKey(out)
		if err != nil {
			return nil, err
		}
		return checkMarker(s.dir, k, create)
	}
	if code != 44 {
		return nil, fmt.Errorf("security find-generic-password: exit %d", code)
	}
	if !create || hasMarker(s.dir) {
		return nil, errKeyLost
	}
	k, err := newKey()
	if err != nil {
		return nil, err
	}
	secret := hex.EncodeToString([]byte(hex.EncodeToString(k)))
	line := fmt.Sprintf("add-generic-password -a %q -s %q -X %s\n", account, service, secret)
	if _, code, err := s.run([]byte(line), securityBin, "-i"); err != nil || code != 0 {
		return nil, fmt.Errorf("security add-generic-password: exit %d %v", code, err)
	}
	return k, writeMarker(s.dir, k)
}

type secretToolKey struct {
	run execx.Runner
	dir string
}

func (secretToolKey) scheme() byte { return schemeKeyring }

func (s secretToolKey) key(create bool) ([]byte, error) {
	out, code, err := s.run(nil, "secret-tool", "lookup", "service", service, "key", "vault")
	if err == nil && code == 0 && len(bytes.TrimSpace(out)) > 0 {
		k, err := decodeKey(out)
		if err != nil {
			return nil, err
		}
		return checkMarker(s.dir, k, create)
	}
	if hasMarker(s.dir) || !create {
		return nil, errKeyLost
	}
	if err != nil {
		return nil, errUnavailable
	}
	k, err := newKey()
	if err != nil {
		return nil, err
	}
	if _, code, err := s.run([]byte(hex.EncodeToString(k)), "secret-tool", "store", "--label=agentswap vault key", "service", service, "key", "vault"); err != nil || code != 0 {
		return nil, errUnavailable
	}
	return k, writeMarker(s.dir, k)
}

type fileKey struct {
	dir       string
	warn      io.Writer
	machineID func() string
}

func (fileKey) scheme() byte { return schemeFile }

func (s fileKey) key(create bool) ([]byte, error) {
	path := filepath.Join(s.dir, ".key")
	b, err := os.ReadFile(path)
	var k []byte
	switch {
	case err == nil:
		if k, err = decodeKey(b); err != nil {
			return nil, err
		}
	case errors.Is(err, fs.ErrNotExist) && create:
		if k, err = newKey(); err != nil {
			return nil, err
		}
		if err := fsx.WriteAtomic(path, []byte(hex.EncodeToString(k)+"\n"), 0o600); err != nil {
			return nil, err
		}
		fmt.Fprintf(s.warn, "agentswap: no OS keyring found; saved accounts are encrypted with %s, which is weaker than a keyring\n", path)
	default:
		return nil, err
	}
	mac := hmac.New(sha256.New, k)
	mac.Write([]byte("agentswap-vault"))
	mac.Write([]byte(s.machineID()))
	return mac.Sum(nil), nil
}

func machineID() string {
	for _, p := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id"} {
		if b, err := os.ReadFile(p); err == nil {
			return strings.TrimSpace(string(b))
		}
	}
	return ""
}

func newDarwin(dir string, run execx.Runner) Vault { return newKeyed(keychainKey{run: run, dir: dir}) }

func newLinux(dir string, warn io.Writer, run execx.Runner, id func() string) Vault {
	return newKeyed(secretToolKey{run: run, dir: dir}, fileKey{dir: dir, warn: warn, machineID: id})
}
