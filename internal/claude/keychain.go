package claude

import (
	"errors"

	"github.com/WellWells/agentswap/internal/execx"
)

type Keychain struct {
	Service string
	Account string
	Run     execx.Runner
}

func (k Keychain) Read() ([]byte, error) { return nil, errors.New("keychain not implemented") }
func (k Keychain) Write([]byte) error    { return errors.New("keychain not implemented") }
func (k Keychain) Delete() error         { return errors.New("keychain not implemented") }
