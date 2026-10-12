//go:build !windows && !linux && !darwin

package ui

import (
	"errors"
	"os"
)

func noEcho(f *os.File) (func(), error) {
	return nil, errors.New("hidden input is not supported on this platform")
}
