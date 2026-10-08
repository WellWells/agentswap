//go:build !windows

package antigravity

import "errors"

var errNoCredManager = errors.New("Windows Credential Manager is only available on Windows")

type winCred struct{ target string }

func (winCred) Read() ([]byte, error) { return nil, errNoCredManager }

func (winCred) Write([]byte) error { return errNoCredManager }

func processRunning(string) (bool, error) { return false, nil }
