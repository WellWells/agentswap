//go:build !windows

package claude

import (
	"os"
	"syscall"
	"time"
)

func openLockDir(path string) (*os.File, error) { return os.Open(path) }

func touchLockDir(f *os.File) error {
	t := syscall.NsecToTimeval(time.Now().UnixNano())
	return syscall.Futimes(int(f.Fd()), []syscall.Timeval{t, t})
}

func removeLockDir(f *os.File) error { return syscall.Rmdir(f.Name()) }
