//go:build !windows && !darwin

package vault

import (
	"io"

	"github.com/WellWells/agentswap/internal/execx"
)

func Default(dir string, warn io.Writer) Vault { return newLinux(dir, warn, execx.Run, machineID) }
