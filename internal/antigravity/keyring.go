package antigravity

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io/fs"

	"github.com/WellWells/agentswap/internal/execx"
)

const (
	prefixBase64  = "go-keyring-base64:"
	prefixEncoded = "go-keyring-encoded:"
	maxCommand    = 4032
)

func decodeValue(b []byte) ([]byte, error) {
	switch {
	case bytes.HasPrefix(b, []byte(prefixBase64)):
		return base64.StdEncoding.DecodeString(string(b[len(prefixBase64):]))
	case bytes.HasPrefix(b, []byte(prefixEncoded)):
		return hex.DecodeString(string(b[len(prefixEncoded):]))
	}
	return b, nil
}

func runner(r execx.Runner) execx.Runner {
	if r == nil {
		return execx.Run
	}
	return r
}

type keychain struct{ run execx.Runner }

func (k keychain) security(stdin []byte, args ...string) ([]byte, int, error) {
	return runner(k.run)(stdin, "/usr/bin/security", args...)
}

func (k keychain) Read() ([]byte, error) {
	out, code, err := k.security(nil, "find-generic-password", "-s", service, "-a", user, "-w")
	if err != nil {
		return nil, err
	}
	if code == 44 {
		return nil, fs.ErrNotExist
	}
	if code != 0 {
		return nil, fmt.Errorf("security find-generic-password: exit %d", code)
	}
	return decodeValue(bytes.TrimSuffix(out, []byte("\n")))
}

func (k keychain) Write(b []byte) error {
	line := fmt.Sprintf("add-generic-password -U -s %s -a %s -w %s%s\n", service, user, prefixBase64, base64.StdEncoding.EncodeToString(b))
	if len(line) > maxCommand {
		return fmt.Errorf("credentials are too large to write to the keychain safely (%d bytes)", len(b))
	}
	_, code, err := k.security([]byte(line), "-i")
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("security add-generic-password: exit %d", code)
	}
	return nil
}

type secretTool struct{ run execx.Runner }

func (s secretTool) Read() ([]byte, error) {
	out, code, err := runner(s.run)(nil, "secret-tool", "lookup", "service", service, "username", user)
	if err != nil {
		return nil, fmt.Errorf("secret-tool: %v: %w", err, fs.ErrNotExist)
	}
	if code != 0 || len(out) == 0 {
		return nil, fs.ErrNotExist
	}
	return decodeValue(bytes.TrimSuffix(out, []byte("\n")))
}

func (s secretTool) Write(b []byte) error {
	_, code, err := runner(s.run)(b, "secret-tool", "store", "--label=Password for '"+user+"' on '"+service+"'", "service", service, "username", user)
	if err != nil {
		return fmt.Errorf("secret-tool: %w", err)
	}
	if code != 0 {
		return fmt.Errorf("secret-tool store: exit %d", code)
	}
	return nil
}

func pgrep(run execx.Runner) func() (bool, error) {
	return func() (bool, error) {
		_, code, err := runner(run)(nil, "pgrep", "-x", "agy")
		return err == nil && code == 0, nil
	}
}
