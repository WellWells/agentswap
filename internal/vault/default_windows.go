//go:build windows

package vault

import "io"

func Default(dir string, warn io.Writer) Vault { return dpapi{} }
