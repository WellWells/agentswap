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

type keychainKey struct{ run execx.Runner }

func (keychainKey) scheme() byte { return schemeKeyring }

func (s keychainKey) key(create bool) ([]byte, error) {
	out, code, err := s.run(nil, securityBin, "find-generic-password", "-a", account, "-s", service, "-w")
	if err != nil {
		return nil, err
	}
	if code == 0 {
		return decodeKey(out)
	}
	if code != 44 {
		return nil, fmt.Errorf("security find-generic-password: exit %d", code)
	}
	if !create {
		return nil, errors.New("vault key missing from keychain")
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
	return k, nil
}

type secretToolKey struct{ run execx.Runner }

func (secretToolKey) scheme() byte { return schemeKeyring }

func (s secretToolKey) key(create bool) ([]byte, error) {
	out, code, err := s.run(nil, "secret-tool", "lookup", "service", service, "key", "vault")
	if err != nil {
		return nil, errUnavailable
	}
	if code == 0 && len(bytes.TrimSpace(out)) > 0 {
		return decodeKey(out)
	}
	if !create {
		return nil, errors.New("vault key missing from secret service")
	}
	k, err := newKey()
	if err != nil {
		return nil, err
	}
	if _, code, err := s.run([]byte(hex.EncodeToString(k)), "secret-tool", "store", "--label=agentswap vault key", "service", service, "key", "vault"); err != nil || code != 0 {
		return nil, errUnavailable
	}
	return k, nil
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

func newDarwin(run execx.Runner) Vault { return newKeyed(keychainKey{run}) }

func newLinux(dir string, warn io.Writer, run execx.Runner, id func() string) Vault {
	return newKeyed(secretToolKey{run}, fileKey{dir: dir, warn: warn, machineID: id})
}
