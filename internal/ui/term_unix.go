//go:build linux || darwin

package ui

import (
	"os"
	"strings"
	"syscall"
	"unsafe"
)

func systemZone() string {
	p, err := os.Readlink("/etc/localtime")
	if err != nil {
		return ""
	}
	if i := strings.Index(p, "zoneinfo/"); i >= 0 {
		return p[i+len("zoneinfo/"):]
	}
	return ""
}

func Terminal(f *os.File) (bool, int) {
	st, err := f.Stat()
	if err != nil || st.Mode()&os.ModeCharDevice == 0 {
		return false, 0
	}
	var ws struct{ rows, cols, x, y uint16 }
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), uintptr(syscall.TIOCGWINSZ), uintptr(unsafe.Pointer(&ws))); e != 0 {
		return true, 0
	}
	return true, int(ws.cols)
}
