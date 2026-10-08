package claude

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"io/fs"
	"strings"

	"github.com/WellWells/agentswap/internal/execx"
)

const securityBin = "/usr/bin/security"

type Keychain struct {
	Service string
	Account string
	Run     execx.Runner
}

func (k Keychain) run(stdin []byte, args ...string) ([]byte, int, error) {
	r := k.Run
	if r == nil {
		r = execx.Run
	}
	return r(stdin, securityBin, args...)
}

func (k Keychain) Read() ([]byte, error) {
	out, code, err := k.run(nil, "find-generic-password", "-a", k.Account, "-w", "-s", k.Service)
	if err != nil {
		return nil, err
	}
	if code == 44 {
		return nil, fs.ErrNotExist
	}
	if code != 0 {
		return nil, fmt.Errorf("security find-generic-password: exit %d", code)
	}
	return bytes.TrimSuffix(out, []byte("\n")), nil
}

func quote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\`, `"`, `\"`).Replace(s) + `"`
}

func (k Keychain) Write(b []byte) error {
	value := hex.EncodeToString(b)
	line := fmt.Sprintf("add-generic-password -U -a %s -s %s -X %s\n", quote(k.Account), quote(k.Service), value)
	var code int
	var err error
	if len(line) <= 4032 {
		_, code, err = k.run([]byte(line), "-i")
	} else {
		_, code, err = k.run(nil, "add-generic-password", "-U", "-a", k.Account, "-s", k.Service, "-X", value)
	}
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("security add-generic-password: exit %d", code)
	}
	return nil
}
