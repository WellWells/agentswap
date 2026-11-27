//go:build !windows

package fsx

import (
	"errors"
	"os"
	"syscall"
)

func tryLock(f *os.File, exclusive bool) (bool, error) {
	mode := syscall.LOCK_SH
	if exclusive {
		mode = syscall.LOCK_EX
	}
	err := syscall.Flock(int(f.Fd()), mode|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EINTR) {
		return false, nil
	}
	return err == nil, err
}

func unlock(f *os.File) error { return syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }
