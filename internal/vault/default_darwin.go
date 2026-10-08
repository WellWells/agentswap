//go:build darwin

package vault

import (
	"io"

	"github.com/WellWells/agentswap/internal/execx"
)

func Default(dir string, warn io.Writer) Vault { return newDarwin(execx.Run) }
