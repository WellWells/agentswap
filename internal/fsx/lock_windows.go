//go:build windows

package fsx

import (
	"os"
	"syscall"
	"unsafe"
)

var (
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	procLockFileEx   = kernel32.NewProc("LockFileEx")
	procUnlockFileEx = kernel32.NewProc("UnlockFileEx")
)

const (
	lockExclusive     = 0x2
	lockFailImmediate = 0x1
	errLockViolation  = syscall.Errno(33)
)

func tryLock(f *os.File, exclusive bool) (bool, error) {
	var ol syscall.Overlapped
	flags := uintptr(lockFailImmediate)
	if exclusive {
		flags |= lockExclusive
	}
	r, _, err := procLockFileEx.Call(f.Fd(), flags, 0, 1, 0, uintptr(unsafe.Pointer(&ol)))
	if r != 0 {
		return true, nil
	}
	if err == errLockViolation {
		return false, nil
	}
	return false, err
}

func unlock(f *os.File) error {
	var ol syscall.Overlapped
	if r, _, err := procUnlockFileEx.Call(f.Fd(), 0, 1, 0, uintptr(unsafe.Pointer(&ol))); r == 0 {
		return err
	}
	return nil
}
